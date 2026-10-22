package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.Catalog = (*Store)(nil)

func (s *Store) Libraries(ctx context.Context) ([]domain.Library, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at FROM libraries ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Library, 0)
	for rows.Next() {
		var item domain.Library
		var created string
		if err := rows.Scan(&item.ID, &item.Name, &created); err != nil {
			return nil, err
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.SourceConfigured = item.ID == s.SourceLibraryID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) Books(ctx context.Context, query domain.BookQuery) (domain.BookPage, error) {
	if query.Limit < 1 || query.Limit > 100 || query.Offset < 0 || query.Offset > 1_000_000 {
		return domain.BookPage{}, errors.New("书库分页参数越界")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT b.id, b.library_id, COALESCE(src.current_version_id, ''), b.title, b.authors_json, b.format, b.created_at
		FROM books AS b LEFT JOIN book_sources AS src ON src.book_id = b.id
		WHERE (? = '' OR instr(lower(b.title), lower(?)) > 0)
		AND (? = '' OR b.format = ?) ORDER BY b.id LIMIT ? OFFSET ?`,
		query.Query, query.Query, query.Format, query.Format, query.Limit+1, query.Offset)
	if err != nil {
		return domain.BookPage{}, err
	}
	defer rows.Close()
	page := domain.BookPage{Items: make([]domain.Book, 0), Limit: query.Limit, Offset: query.Offset}
	for rows.Next() {
		var item domain.Book
		var authors, created string
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.CurrentVersionID, &item.Title, &authors, &item.Format, &created); err != nil {
			return domain.BookPage{}, err
		}
		if err := json.Unmarshal([]byte(authors), &item.Authors); err != nil {
			return domain.BookPage{}, err
		}
		if item.Authors == nil {
			item.Authors = []string{}
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return domain.BookPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.BookPage{}, err
	}
	if len(page.Items) > query.Limit {
		page.HasMore = true
		page.Items = page.Items[:query.Limit]
	}
	return page, nil
}
