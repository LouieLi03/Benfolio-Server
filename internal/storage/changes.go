package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (s *Store) ReadChanges(ctx context.Context, userID string, after int64, limit int) (domain.ChangePage, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return domain.ChangePage{}, domain.ErrInvalidProgress
	}
	var latest int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) FROM changes WHERE user_id = ?", userID).Scan(&latest); err != nil {
		return domain.ChangePage{}, err
	}
	if after > latest {
		return domain.ChangePage{}, domain.ErrChangeCursor
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, document_version_id, entity_type, entity_id,
		revision, operation, payload_json, created_at FROM changes
		WHERE user_id = ? AND seq > ? ORDER BY seq LIMIT ?`, userID, after, limit+1)
	if err != nil {
		return domain.ChangePage{}, err
	}
	defer rows.Close()
	page := domain.ChangePage{Items: []domain.Change{}, NextCursor: strconv.FormatInt(after, 10)}
	for rows.Next() {
		var item domain.Change
		var payload, created string
		if err := rows.Scan(&item.Seq, &item.DocumentVersionID, &item.EntityType, &item.EntityID,
			&item.Revision, &item.Operation, &payload, &created); err != nil {
			return domain.ChangePage{}, err
		}
		if !json.Valid([]byte(payload)) {
			return domain.ChangePage{}, errors.New("变更记录损坏")
		}
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return domain.ChangePage{}, err
		}
		item.Payload = json.RawMessage(payload)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.ChangePage{}, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
	}
	if len(page.Items) != 0 {
		page.NextCursor = strconv.FormatInt(page.Items[len(page.Items)-1].Seq, 10)
	}
	return page, nil
}
