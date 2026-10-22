package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

// ConfigureLibrary runs only at startup, after configuration has checked and
// resolved the directory. This method changes metadata, never source files.
func (s *Store) ConfigureLibrary(ctx context.Context, name, sourcePath string) error {
	if strings.TrimSpace(name) == "" || !filepath.IsAbs(sourcePath) {
		return errors.New("登记书库需要名称与已检查的绝对来源路径")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var libraryID string
	err = tx.QueryRowContext(ctx, "SELECT library_id FROM library_sources WHERE root_path = ?", sourcePath).Scan(&libraryID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		libraryID, err = domain.NewID()
		if err != nil {
			return err
		}
		created := timestamp()
		if _, err := tx.ExecContext(ctx, "INSERT INTO libraries(id, name, relative_path, created_at) VALUES (?, ?, '.', ?)", libraryID, name, created); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO library_sources(library_id, root_path, created_at) VALUES (?, ?, ?)", libraryID, sourcePath, created); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if _, err := tx.ExecContext(ctx, "UPDATE libraries SET name = ? WHERE id = ?", name, libraryID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.SourceLibraryID = libraryID
	return nil
}
