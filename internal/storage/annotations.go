package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.AnnotationStore = (*Store)(nil)

const annotationColumns = "id, document_version_id, kind, anchor_json, body_json, revision, deleted_at, created_at, updated_at, device_id"

type annotationScanner interface{ Scan(...any) error }

func annotationRow(row annotationScanner) (domain.Annotation, error) {
	var item domain.Annotation
	var anchor, body, created, updated string
	var deleted sql.NullString
	err := row.Scan(&item.ID, &item.DocumentVersionID, &item.Kind, &anchor, &body, &item.Revision,
		&deleted, &created, &updated, &item.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, domain.ErrAnnotationNotFound
	}
	if err != nil {
		return item, err
	}
	var location struct {
		EPUB *domain.EPUBAnchor `json:"epub"`
		PDF  *domain.PDFAnchor  `json:"pdf"`
	}
	if json.Unmarshal([]byte(anchor), &location) != nil || !json.Valid([]byte(body)) ||
		(location.EPUB == nil) == (location.PDF == nil) || item.Revision < 1 || item.Revision > domain.MaxMutationRevision {
		return domain.Annotation{}, errors.New("已存批注损坏")
	}
	item.EPUB, item.PDF, item.Body = location.EPUB, location.PDF, json.RawMessage(body)
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.Annotation{}, err
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.Annotation{}, err
	}
	if deleted.Valid {
		when, err := time.Parse(time.RFC3339Nano, deleted.String)
		if err != nil {
			return domain.Annotation{}, err
		}
		item.DeletedAt = &when
	}
	return item, nil
}

func annotationVersion(ctx context.Context, query rowQuerier, versionID string) error {
	var exists int
	err := query.QueryRowContext(ctx, "SELECT 1 FROM document_versions WHERE id = ?", versionID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrVersionNotFound
	}
	return err
}

func (s *Store) ReadAnnotation(ctx context.Context, userID, versionID, annotationID string) (domain.Annotation, error) {
	if err := annotationVersion(ctx, s.db, versionID); err != nil {
		return domain.Annotation{}, err
	}
	return annotationRow(s.db.QueryRowContext(ctx, "SELECT "+annotationColumns+
		" FROM annotations WHERE user_id = ? AND document_version_id = ? AND id = ?", userID, versionID, annotationID))
}

func (s *Store) ReadAnnotations(ctx context.Context, userID, versionID, after string, limit int) (domain.AnnotationPage, error) {
	if limit < 1 || limit > 100 {
		return domain.AnnotationPage{}, domain.ErrInvalidAnnotation
	}
	if err := annotationVersion(ctx, s.db, versionID); err != nil {
		return domain.AnnotationPage{}, err
	}
	if after != "" {
		var exists int
		err := s.db.QueryRowContext(ctx, "SELECT 1 FROM annotations WHERE user_id = ? AND document_version_id = ? AND id = ?",
			userID, versionID, after).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AnnotationPage{}, domain.ErrAnnotationCursor
		}
		if err != nil {
			return domain.AnnotationPage{}, err
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+annotationColumns+
		" FROM annotations WHERE user_id = ? AND document_version_id = ? AND id > ? ORDER BY id LIMIT ?",
		userID, versionID, after, limit+1)
	if err != nil {
		return domain.AnnotationPage{}, err
	}
	defer rows.Close()
	page := domain.AnnotationPage{Items: []domain.Annotation{}, NextCursor: after}
	for rows.Next() {
		item, err := annotationRow(rows)
		if err != nil {
			return domain.AnnotationPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.AnnotationPage{}, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
	}
	if len(page.Items) > 0 {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

// CommitAnnotation retains deletion tombstones and never migrates identities
// between versions. State, event and receipt share the same transaction.
func (s *Store) CommitAnnotation(ctx context.Context, userID, versionID, annotationID string,
	command domain.AnnotationMutation, remove bool, requestSHA256 string) (domain.AnnotationResult, error) {
	var result domain.AnnotationResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var previousHash, responseJSON string
	err = tx.QueryRowContext(ctx, "SELECT request_sha256, response_json FROM mutation_receipts WHERE user_id = ? AND mutation_id = ?",
		userID, command.ID).Scan(&previousHash, &responseJSON)
	switch {
	case err == nil:
		if previousHash != requestSHA256 {
			return result, domain.ErrMutationReused
		}
		if err := json.Unmarshal([]byte(responseJSON), &result); err != nil {
			return result, err
		}
		if result.Annotation.ID != annotationID || result.Annotation.DocumentVersionID != versionID ||
			result.Annotation.Revision < 1 || result.ChangeSeq < 1 {
			return domain.AnnotationResult{}, errors.New("批注操作回执损坏")
		}
		result.Replayed = true
		return result, nil
	case !errors.Is(err, sql.ErrNoRows):
		return result, err
	}
	var sourceSHA256 string
	err = tx.QueryRowContext(ctx, "SELECT sha256 FROM document_versions WHERE id = ?", versionID).Scan(&sourceSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrVersionNotFound
	}
	if err != nil {
		return result, err
	}
	if sourceSHA256 != command.Payload.SourceSHA256 {
		return result, domain.ErrProgressSource
	}
	current, err := annotationRow(tx.QueryRowContext(ctx, "SELECT "+annotationColumns+
		" FROM annotations WHERE user_id = ? AND id = ?", userID, annotationID))
	missing := errors.Is(err, domain.ErrAnnotationNotFound)
	if err != nil && !missing {
		return result, err
	}
	if !missing && current.DocumentVersionID != versionID {
		return result, domain.ErrAnnotationIdentity
	}
	if remove && missing {
		return result, domain.ErrAnnotationNotFound
	}
	if missing && command.BaseRevision != 0 {
		return result, &domain.AnnotationConflict{}
	}
	if !missing {
		if current.Revision != command.BaseRevision {
			return result, &domain.AnnotationConflict{Current: &current}
		}
		if current.DeletedAt != nil {
			return result, domain.ErrAnnotationDeleted
		}
		if !remove && current.Kind != command.Payload.Kind {
			return result, domain.ErrAnnotationIdentity
		}
	}
	if current.Revision >= domain.MaxMutationRevision {
		return result, errors.New("批注修订号达到上限")
	}
	now := time.Now().UTC()
	item := current
	operation := "upsert"
	if remove {
		item.DeletedAt = &now
		operation = "delete"
	} else {
		item.ID, item.DocumentVersionID, item.Kind = annotationID, versionID, command.Payload.Kind
		item.Body, item.EPUB, item.PDF = command.Payload.Body, nil, nil
		if len(command.Payload.EPUB) != 0 {
			if err := json.Unmarshal(command.Payload.EPUB, &item.EPUB); err != nil {
				return result, err
			}
		} else {
			if err := json.Unmarshal(command.Payload.PDF, &item.PDF); err != nil {
				return result, err
			}
		}
	}
	item.Revision, item.DeviceID, item.UpdatedAt = current.Revision+1, command.DeviceID, now
	if missing {
		item.CreatedAt = now
	}
	var resourceSHA256 string
	if !remove && item.Kind == "ink" {
		var reference domain.InkReference
		if json.Unmarshal(item.Body, &reference) != nil || item.PDF == nil || item.PDF.PageIndex == nil {
			return result, domain.ErrInvalidAnnotation
		}
		resourceSHA256 = reference.ResourceSHA256
		var boundVersion, boundSource string
		var boundPage int64
		err := tx.QueryRowContext(ctx, `SELECT i.document_version_id, i.source_sha256, i.page_index
			FROM ink_resources i JOIN resource_owners o ON o.resource_sha256 = i.resource_sha256
			WHERE o.user_id = ? AND i.resource_sha256 = ?`, userID, resourceSHA256).Scan(&boundVersion, &boundSource, &boundPage)
		if errors.Is(err, sql.ErrNoRows) {
			return result, domain.ErrResourceNotFound
		}
		if err != nil {
			return result, err
		}
		if boundVersion != versionID || boundSource != sourceSHA256 || boundPage != *item.PDF.PageIndex {
			return result, domain.ErrResourceBinding
		}
	}
	var written sql.Result
	if remove {
		// Leave stored anchors and note bytes untouched when deleting.
		written, err = tx.ExecContext(ctx, `UPDATE annotations SET revision = ?, deleted_at = ?, updated_at = ?, device_id = ?
			WHERE user_id = ? AND id = ? AND document_version_id = ? AND revision = ? AND deleted_at IS NULL`,
			item.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), command.DeviceID,
			userID, annotationID, versionID, command.BaseRevision)
	} else {
		anchor, marshalErr := json.Marshal(struct {
			EPUB *domain.EPUBAnchor `json:"epub,omitempty"`
			PDF  *domain.PDFAnchor  `json:"pdf,omitempty"`
		}{item.EPUB, item.PDF})
		if marshalErr != nil {
			return result, marshalErr
		}
		written, err = tx.ExecContext(ctx, `INSERT INTO annotations
			(id, user_id, document_version_id, kind, anchor_json, body_json, revision, deleted_at, created_at, updated_at, device_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)
			ON CONFLICT(user_id, id) DO UPDATE SET anchor_json = excluded.anchor_json, body_json = excluded.body_json,
			revision = excluded.revision, updated_at = excluded.updated_at, device_id = excluded.device_id
			WHERE annotations.document_version_id = excluded.document_version_id AND annotations.kind = excluded.kind
			AND annotations.revision = ? AND annotations.deleted_at IS NULL`, annotationID, userID, versionID,
			item.Kind, string(anchor), string(item.Body), item.Revision, item.CreatedAt.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano), command.DeviceID, command.BaseRevision)
	}
	if err != nil {
		return result, err
	}
	count, err := written.RowsAffected()
	if err != nil || count != 1 {
		return result, errors.New("批注条件写入未完成")
	}
	if resourceSHA256 != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO annotation_resources(user_id, annotation_id, resource_sha256)
			VALUES (?, ?, ?) ON CONFLICT(user_id, annotation_id, resource_sha256) DO NOTHING`, userID, annotationID, resourceSHA256); err != nil {
			return domain.AnnotationResult{}, err
		}
	}
	result.Annotation = item
	payload, err := json.Marshal(item)
	if err != nil {
		return domain.AnnotationResult{}, err
	}
	event, err := tx.ExecContext(ctx, `INSERT INTO changes
		(user_id, document_version_id, entity_type, entity_id, revision, operation, payload_json, created_at)
		VALUES (?, ?, 'annotation', ?, ?, ?, ?, ?)`, userID, versionID, annotationID, item.Revision,
		operation, string(payload), now.Format(time.RFC3339Nano))
	if err != nil {
		return domain.AnnotationResult{}, err
	}
	result.ChangeSeq, err = event.LastInsertId()
	if err != nil {
		return domain.AnnotationResult{}, err
	}
	response, err := json.Marshal(result)
	if err != nil {
		return domain.AnnotationResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_receipts
		(user_id, mutation_id, request_sha256, response_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, command.ID, requestSHA256, string(response), now.Format(time.RFC3339Nano)); err != nil {
		return domain.AnnotationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.AnnotationResult{}, err
	}
	return result, nil
}
