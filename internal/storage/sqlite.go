package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db              *sql.DB
	InstanceID      string
	Owner           domain.User
	SchemaVersion   int
	SourceLibraryID string
}

func Open(ctx context.Context, path string) (*Store, error) {
	u := url.URL{Scheme: "file", Path: path}
	query := u.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err := s.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) initialize(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("配置数据库: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, sha256 TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("创建迁移记录: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	var latest int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&latest); err != nil {
		return err
	}
	if latest > len(entries) {
		return errors.New("数据库比当前程序更新，拒绝降级打开；请使用匹配版本")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, entry := range entries {
		version := i + 1
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), fmt.Sprintf("%03d_", version)) {
			return errors.New("数据库迁移序列不连续")
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		var existingName, existingDigest string
		err = tx.QueryRowContext(ctx, "SELECT name, sha256 FROM schema_migrations WHERE version = ?", version).Scan(&existingName, &existingDigest)
		if err == nil {
			if existingName != entry.Name() || existingDigest != digest {
				return fmt.Errorf("迁移 %d 与数据库记录不一致，拒绝覆盖", version)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("执行迁移 %d: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, sha256, applied_at) VALUES (?, ?, ?, ?)", version, entry.Name(), digest, timestamp()); err != nil {
			return err
		}
	}
	instanceID, err := domain.NewID()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO server_metadata(key, value) VALUES ('instance_id', ?)", instanceID); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT value FROM server_metadata WHERE key = 'instance_id'").Scan(&s.InstanceID); err != nil {
		return err
	}
	ownerID, err := domain.NewID()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO users(id, display_name, role, created_at) VALUES (?, 'Owner', 'owner', ?)", ownerID, timestamp()); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT id, display_name, role FROM users WHERE role = 'owner'").Scan(&s.Owner.ID, &s.Owner.DisplayName, &s.Owner.Role); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.SchemaVersion = len(entries)
	return nil
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *Store) Close() error                   { return s.db.Close() }
func timestamp() string                         { return time.Now().UTC().Format(time.RFC3339Nano) }
