package storage

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.ImportStore = (*Store)(nil)

// SaveEPUB binds a source path to a logical book. Byte versions and their
// metadata remain immutable; returning to old bytes reuses the old version.
func (s *Store) SaveEPUB(ctx context.Context, record domain.ImportRecord) (domain.ImportResult, error) {
	var result domain.ImportResult
	if record.LibraryID == "" || record.LibraryID != s.SourceLibraryID {
		return result, domain.ErrSourceNotConfigured
	}
	digest, err := hex.DecodeString(record.SHA256)
	if err != nil || len(digest) != 32 || record.SHA256 != strings.ToLower(record.SHA256) || record.ByteSize <= 0 || record.ObjectKey != record.SHA256[:2]+"/"+record.SHA256+".epub" || !fs.ValidPath(record.RelativePath) || record.RelativePath == "." || strings.ContainsAny(record.RelativePath, "\\\x00") || record.Metadata.Title == "" {
		return result, errors.New("导入记录无效")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	now := timestamp()
	var format domain.Format
	err = tx.QueryRowContext(ctx, `SELECT b.id, b.format FROM book_sources AS src
		JOIN books AS b ON b.id = src.book_id WHERE src.library_id = ? AND src.relative_path = ?`, record.LibraryID, record.RelativePath).Scan(&result.BookID, &format)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result.BookID, err = domain.NewID()
		if err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO books(id, library_id, title, authors_json, format, created_at) VALUES (?, ?, ?, '[]', 'epub', ?)`, result.BookID, record.LibraryID, record.Metadata.Title, now); err != nil {
			return result, err
		}
		result.BookCreated = true
	case err != nil:
		return result, err
	case format != domain.EPUB:
		return result, domain.ErrUnsupportedBook
	}
	var created, objectKey, metadataJSON string
	result.Version.BookID = result.BookID
	err = tx.QueryRowContext(ctx, `SELECT v.id, v.sha256, v.byte_size, v.original_object_key, v.created_at, m.metadata_json
		FROM document_versions AS v JOIN document_metadata AS m ON m.version_id = v.id
		WHERE v.book_id = ? AND v.sha256 = ?`, result.BookID, record.SHA256).Scan(&result.Version.ID, &result.Version.SHA256, &result.Version.ByteSize, &objectKey, &created, &metadataJSON)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result.Version.ID, err = domain.NewID()
		if err != nil {
			return result, err
		}
		result.Version.SHA256, result.Version.ByteSize = record.SHA256, record.ByteSize
		created = now
		result.Metadata = record.Metadata
		encoded, err := json.Marshal(result.Metadata)
		if err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO document_versions(id, book_id, sha256, byte_size, original_object_key, created_at) VALUES (?, ?, ?, ?, ?, ?)`, result.Version.ID, result.BookID, record.SHA256, record.ByteSize, record.ObjectKey, now); err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO document_metadata(version_id, metadata_json) VALUES (?, ?)`, result.Version.ID, string(encoded)); err != nil {
			return result, err
		}
		result.VersionCreated = true
	case err != nil:
		return result, err
	default:
		if objectKey != record.ObjectKey || result.Version.ByteSize != record.ByteSize {
			return result, domain.ErrArchiveConflict
		}
		if err := json.Unmarshal([]byte(metadataJSON), &result.Metadata); err != nil {
			return result, err
		}
	}
	result.Version.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return result, err
	}
	authors, err := json.Marshal(result.Metadata.Authors)
	if err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE books SET title = ?, authors_json = ? WHERE id = ?`, result.Metadata.Title, string(authors), result.BookID); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO book_sources(library_id, relative_path, book_id, current_version_id, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(library_id, relative_path) DO UPDATE SET current_version_id = excluded.current_version_id, updated_at = excluded.updated_at`, record.LibraryID, record.RelativePath, result.BookID, result.Version.ID, now); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
