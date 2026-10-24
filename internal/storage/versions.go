package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.OriginalCatalog = (*Store)(nil)

func (s *Store) Version(ctx context.Context, versionID string) (domain.VersionDetails, error) {
	record, err := s.Original(ctx, versionID)
	if err != nil {
		return domain.VersionDetails{}, err
	}
	item := domain.VersionDetails{DocumentVersion: record.Version, Format: record.Format}
	var value string
	if err := s.db.QueryRowContext(ctx, "SELECT metadata_json FROM document_metadata WHERE version_id = ?", versionID).Scan(&value); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(value), &item.Metadata); err != nil {
		return item, err
	}
	return item, nil
}

func (s *Store) Book(ctx context.Context, bookID string) (domain.Book, error) {
	var item domain.Book
	var authors, created string
	err := s.db.QueryRowContext(ctx, `SELECT b.id, b.library_id, COALESCE(src.current_version_id, ''),
		b.title, b.authors_json, b.format, b.created_at
		FROM books AS b LEFT JOIN book_sources AS src ON src.book_id = b.id WHERE b.id = ?`, bookID).
		Scan(&item.ID, &item.LibraryID, &item.CurrentVersionID, &item.Title, &authors, &item.Format, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return item, domain.ErrBookNotFound
	}
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(authors), &item.Authors); err != nil {
		return item, err
	}
	if item.Authors == nil {
		item.Authors = []string{}
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return item, err
}

func (s *Store) Versions(ctx context.Context, bookID string, query domain.VersionQuery) (domain.VersionPage, error) {
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || query.Offset > 1_000_000 {
		return domain.VersionPage{}, errors.New("版本分页参数越界")
	}
	if _, err := s.Book(ctx, bookID); err != nil {
		return domain.VersionPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, book_id, sha256, byte_size, created_at
		FROM document_versions WHERE book_id = ? ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`, bookID, query.Limit+1, query.Offset)
	if err != nil {
		return domain.VersionPage{}, err
	}
	defer rows.Close()
	page := domain.VersionPage{Items: []domain.DocumentVersion{}, Limit: query.Limit, Offset: query.Offset}
	for rows.Next() {
		var item domain.DocumentVersion
		var created string
		if err := rows.Scan(&item.ID, &item.BookID, &item.SHA256, &item.ByteSize, &created); err != nil {
			return domain.VersionPage{}, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return domain.VersionPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.VersionPage{}, err
	}
	if len(page.Items) > query.Limit {
		page.HasMore = true
		page.Items = page.Items[:query.Limit]
	}
	return page, nil
}

func (s *Store) Original(ctx context.Context, versionID string) (domain.OriginalRecord, error) {
	var item domain.OriginalRecord
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT v.id, v.book_id, v.sha256, v.byte_size, v.original_object_key, v.created_at, b.format
		FROM document_versions AS v JOIN books AS b ON b.id = v.book_id WHERE v.id = ?`, versionID).
		Scan(&item.Version.ID, &item.Version.BookID, &item.Version.SHA256, &item.Version.ByteSize, &item.ObjectKey, &created, &item.Format)
	if errors.Is(err, sql.ErrNoRows) {
		return item, domain.ErrVersionNotFound
	}
	if err != nil {
		return item, err
	}
	item.Version.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return item, err
}
