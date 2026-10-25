package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/syncvalue"
)

var _ domain.ResourceStore = (*Store)(nil)

const resourceColumns = "r.sha256, r.byte_size, r.media_type, r.object_key, r.created_at, i.document_version_id, i.source_sha256, i.page_index, i.schema_version, i.coordinate_space"

func resourceRow(row annotationScanner) (domain.ResourceRecord, error) {
	var record domain.ResourceRecord
	var created string
	r := &record.Info
	err := row.Scan(&r.SHA256, &r.ByteSize, &r.MediaType, &record.ObjectKey, &created,
		&r.DocumentVersionID, &r.SourceSHA256, &r.PageIndex, &r.SchemaVersion, &r.CoordinateSpace)
	if errors.Is(err, sql.ErrNoRows) {
		return record, domain.ErrResourceNotFound
	}
	if err != nil {
		return record, err
	}
	r.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return record, err
}

func (s *Store) ReadResource(ctx context.Context, userID, digest string) (domain.ResourceRecord, error) {
	return resourceRow(s.db.QueryRowContext(ctx, "SELECT "+resourceColumns+` FROM resources r
		JOIN ink_resources i ON i.resource_sha256 = r.sha256
		JOIN resource_owners o ON o.resource_sha256 = r.sha256
		WHERE o.user_id = ? AND r.sha256 = ?`, userID, digest))
}

func (s *Store) RegisterResource(ctx context.Context, userID string, record domain.ResourceRecord) (domain.ResourceResult, error) {
	var result domain.ResourceResult
	r := record.Info
	if !syncvalue.ValidID(userID) || !syncvalue.ValidID(r.DocumentVersionID) ||
		!syncvalue.ValidSHA256(r.SHA256) || !syncvalue.ValidSHA256(r.SourceSHA256) ||
		r.MediaType != domain.InkMediaType || r.SchemaVersion != 1 || r.CoordinateSpace != domain.InkCoordinateSpace ||
		r.ByteSize < 1 || r.ByteSize > domain.MaxInkBytes || r.PageIndex < 0 ||
		record.ObjectKey != r.SHA256[:2]+"/"+r.SHA256+".ink.json" {
		return result, domain.ErrInvalidResource
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var expected string
	var format domain.Format
	err = tx.QueryRowContext(ctx, `SELECT v.sha256, b.format FROM document_versions v
		JOIN books b ON b.id = v.book_id WHERE v.id = ?`, r.DocumentVersionID).Scan(&expected, &format)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrVersionNotFound
	}
	if err != nil {
		return result, err
	}
	if expected != r.SourceSHA256 || format != domain.PDF {
		return result, domain.ErrResourceBinding
	}
	var rawMetadata string
	if err := tx.QueryRowContext(ctx, "SELECT metadata_json FROM document_metadata WHERE version_id = ?", r.DocumentVersionID).Scan(&rawMetadata); err != nil {
		return result, err
	}
	if !validResourcePage(rawMetadata, r.PageIndex) {
		return result, domain.ErrResourceBinding
	}
	var existingSize int64
	var existingMedia, existingKey string
	err = tx.QueryRowContext(ctx, "SELECT byte_size, media_type, object_key FROM resources WHERE sha256 = ?", r.SHA256).
		Scan(&existingSize, &existingMedia, &existingKey)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `INSERT INTO resources(sha256, byte_size, media_type, object_key, created_at)
			VALUES (?, ?, ?, ?, ?)`, r.SHA256, r.ByteSize, r.MediaType, record.ObjectKey, timestamp()); err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ink_resources
			(resource_sha256, document_version_id, source_sha256, page_index, schema_version, coordinate_space)
			VALUES (?, ?, ?, ?, ?, ?)`, r.SHA256, r.DocumentVersionID, r.SourceSHA256, r.PageIndex, r.SchemaVersion, r.CoordinateSpace); err != nil {
			return result, err
		}
	case err != nil:
		return result, err
	default:
		if existingSize != r.ByteSize || existingMedia != r.MediaType || existingKey != record.ObjectKey {
			return result, domain.ErrResourceConflict
		}
	}
	stored, err := resourceRow(tx.QueryRowContext(ctx, "SELECT "+resourceColumns+" FROM resources r JOIN ink_resources i ON i.resource_sha256 = r.sha256 WHERE r.sha256 = ?", r.SHA256))
	if errors.Is(err, domain.ErrResourceNotFound) {
		return result, domain.ErrResourceConflict
	}
	if err != nil {
		return result, err
	}
	if stored.Info.DocumentVersionID != r.DocumentVersionID || stored.Info.SourceSHA256 != r.SourceSHA256 ||
		stored.Info.PageIndex != r.PageIndex || stored.Info.SchemaVersion != r.SchemaVersion || stored.Info.CoordinateSpace != r.CoordinateSpace {
		return result, domain.ErrResourceConflict
	}
	owner, err := tx.ExecContext(ctx, `INSERT INTO resource_owners(user_id, resource_sha256, created_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, resource_sha256) DO NOTHING`, userID, r.SHA256, timestamp())
	if err != nil {
		return result, err
	}
	added, err := owner.RowsAffected()
	if err != nil {
		return result, err
	}
	result.Resource, result.Reused = stored.Info, added == 0
	if err := tx.Commit(); err != nil {
		return domain.ResourceResult{}, err
	}
	return result, nil
}

func validResourcePage(raw string, page int64) bool {
	var metadata domain.BookMetadata
	return json.Unmarshal([]byte(raw), &metadata) == nil && metadata.PDF != nil && page >= 0 && page < int64(metadata.PDF.PageCount)
}
