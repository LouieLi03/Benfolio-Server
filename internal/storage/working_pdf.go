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

var _ domain.WorkingPDFStore = (*Store)(nil)

func workingStatus(ctx context.Context, query rowQuerier, userID, versionID string, revision int64) (domain.WorkingPDFStatus, error) {
	status := domain.WorkingPDFStatus{WorkingPDFInfo: domain.WorkingPDFInfo{DocumentVersionID: versionID}}
	var format domain.Format
	err := query.QueryRowContext(ctx, `SELECT v.sha256, b.format, v.byte_size FROM document_versions v JOIN books b ON b.id = v.book_id WHERE v.id = ?`, versionID).
		Scan(&status.SourceSHA256, &format, &status.SourceByteSize)
	if errors.Is(err, sql.ErrNoRows) {
		return status, domain.ErrVersionNotFound
	}
	if err != nil {
		return status, err
	}
	if format != domain.PDF {
		return status, domain.ErrWorkingPDFUnsupported
	}
	if err := query.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM changes WHERE user_id = ?
		AND document_version_id = ? AND entity_type = 'annotation'`, userID, versionID).Scan(&status.LatestAnnotationSeq); err != nil {
		return status, err
	}
	err = query.QueryRowContext(ctx, "SELECT revision FROM pdf_working_copies WHERE user_id = ? AND document_version_id = ?", userID, versionID).Scan(&status.LatestRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return status, err
	}
	if revision == 0 {
		revision = status.LatestRevision
	}
	if revision == 0 {
		status.Stale = true
		return status, nil
	}
	var updated, mediaType, objectKey string
	err = query.QueryRowContext(ctx, `SELECT h.revision, h.file_sha256, h.materialized_change_seq, h.annotation_count,
		h.device_id, h.created_at, r.byte_size, r.media_type, r.object_key FROM working_pdf_history h
		JOIN resources r ON r.sha256 = h.file_sha256 WHERE h.user_id = ? AND h.document_version_id = ? AND h.revision = ?`,
		userID, versionID, revision).Scan(&status.Revision, &status.FileSHA256, &status.MaterializedChangeSeq,
		&status.AnnotationCount, &status.DeviceID, &updated, &status.ByteSize, &mediaType, &objectKey)
	if errors.Is(err, sql.ErrNoRows) {
		return status, domain.ErrWorkingPDFNotFound
	}
	if err != nil {
		return status, err
	}
	if !syncvalue.ValidSHA256(status.FileSHA256) || status.ByteSize < 1 || status.ByteSize > domain.MaxWorkingPDFBytes ||
		mediaType != "application/pdf" || objectKey != workingObjectKey(status.FileSHA256) {
		return status, domain.ErrResourceConflict
	}
	when, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return status, err
	}
	status.UpdatedAt, status.Available = &when, true
	status.Stale = status.MaterializedChangeSeq != status.LatestAnnotationSeq
	return status, nil
}

func (s *Store) WorkingStatus(ctx context.Context, userID, versionID string, revision int64) (domain.WorkingPDFStatus, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.WorkingPDFStatus{}, err
	}
	defer tx.Rollback()
	return workingStatus(ctx, tx, userID, versionID, revision)
}

func (s *Store) WorkingSnapshot(ctx context.Context, userID, versionID string) (domain.WorkingPDFSnapshot, error) {
	var result domain.WorkingPDFSnapshot
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	result.Status, err = workingStatus(ctx, tx, userID, versionID, 0)
	if err != nil {
		return result, err
	}
	var rawMetadata string
	if err := tx.QueryRowContext(ctx, "SELECT metadata_json FROM document_metadata WHERE version_id = ?", versionID).Scan(&rawMetadata); err != nil {
		return result, err
	}
	var metadata domain.BookMetadata
	if json.Unmarshal([]byte(rawMetadata), &metadata) != nil || metadata.PDF == nil {
		return result, domain.ErrWorkingPDFUnsupported
	}
	result.PageCount = metadata.PDF.PageCount
	rows, err := tx.QueryContext(ctx, "SELECT "+annotationColumns+
		" FROM annotations WHERE user_id = ? AND document_version_id = ? AND deleted_at IS NULL ORDER BY id LIMIT 1001", userID, versionID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Annotations = []domain.Annotation{}
	for rows.Next() {
		item, err := annotationRow(rows)
		if err != nil {
			return domain.WorkingPDFSnapshot{}, err
		}
		result.Annotations = append(result.Annotations, item)
		if len(result.Annotations) > 1000 {
			return domain.WorkingPDFSnapshot{}, domain.ErrWorkingPDFLimit
		}
	}
	if err := rows.Err(); err != nil {
		return domain.WorkingPDFSnapshot{}, err
	}
	return result, nil
}

func workingReceipt(ctx context.Context, query rowQuerier, userID, mutationID, digest string) (*domain.WorkingPDFResult, error) {
	var oldHash, response string
	err := query.QueryRowContext(ctx, "SELECT request_sha256, response_json FROM mutation_receipts WHERE user_id = ? AND mutation_id = ?", userID, mutationID).Scan(&oldHash, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if oldHash != digest {
		return nil, domain.ErrMutationReused
	}
	var result domain.WorkingPDFResult
	if json.Unmarshal([]byte(response), &result) != nil || result.WorkingPDF.Revision < 1 || result.ChangeSeq < 1 ||
		!syncvalue.ValidSHA256(result.WorkingPDF.FileSHA256) {
		return nil, domain.ErrResourceConflict
	}
	result.Replayed = true
	return &result, nil
}

func (s *Store) WorkingReceipt(ctx context.Context, userID, mutationID, digest string) (*domain.WorkingPDFResult, error) {
	return workingReceipt(ctx, s.db, userID, mutationID, digest)
}

func workingObjectKey(digest string) string { return "working/" + digest[:2] + "/" + digest + ".pdf" }

func (s *Store) CommitWorkingPDF(ctx context.Context, userID, versionID string, command domain.WorkingPDFMutation,
	requestSHA256, fileSHA256 string, byteSize int64, annotationCount int) (domain.WorkingPDFResult, error) {
	var result domain.WorkingPDFResult
	if !syncvalue.ValidSHA256(fileSHA256) || byteSize < 1 || byteSize > domain.MaxWorkingPDFBytes || annotationCount < 0 || annotationCount > 1000 {
		return result, domain.ErrInvalidWorkingPDF
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	previous, err := workingReceipt(ctx, tx, userID, command.ID, requestSHA256)
	if err != nil {
		return result, err
	}
	if previous != nil {
		if previous.WorkingPDF.DocumentVersionID != versionID {
			return result, domain.ErrResourceConflict
		}
		return *previous, nil
	}
	current, err := workingStatus(ctx, tx, userID, versionID, 0)
	if err != nil {
		return result, err
	}
	if current.SourceSHA256 != command.SourceSHA256 || current.Revision != command.BaseRevision ||
		current.LatestAnnotationSeq != command.AnnotationSeq {
		return result, domain.ErrWorkingPDFConflict
	}
	if current.Revision >= domain.MaxMutationRevision {
		return result, domain.ErrWorkingPDFLimit
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO resources(sha256, byte_size, media_type, object_key, created_at)
		VALUES (?, ?, 'application/pdf', ?, ?) ON CONFLICT(sha256) DO NOTHING`, fileSHA256, byteSize, workingObjectKey(fileSHA256), now.Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	var size int64
	var mediaType, objectKey string
	if err := tx.QueryRowContext(ctx, "SELECT byte_size, media_type, object_key FROM resources WHERE sha256 = ?", fileSHA256).Scan(&size, &mediaType, &objectKey); err != nil {
		return result, err
	}
	if size != byteSize || mediaType != "application/pdf" || objectKey != workingObjectKey(fileSHA256) {
		return result, domain.ErrResourceConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_owners(user_id, resource_sha256, created_at)
		VALUES (?, ?, ?) ON CONFLICT(user_id, resource_sha256) DO NOTHING`, userID, fileSHA256, now.Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	info := domain.WorkingPDFInfo{DocumentVersionID: versionID, SourceSHA256: command.SourceSHA256,
		Revision: current.Revision + 1, FileSHA256: fileSHA256, ByteSize: byteSize, MaterializedChangeSeq: command.AnnotationSeq,
		AnnotationCount: annotationCount, DeviceID: command.DeviceID, UpdatedAt: &now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO working_pdf_history
		(user_id, document_version_id, revision, file_sha256, materialized_change_seq, annotation_count, device_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, userID, versionID, info.Revision, fileSHA256, command.AnnotationSeq, annotationCount, command.DeviceID, now.Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	write, err := tx.ExecContext(ctx, `INSERT INTO pdf_working_copies
		(user_id, document_version_id, revision, file_sha256, materialized_change_seq, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, document_version_id) DO UPDATE SET revision = excluded.revision, file_sha256 = excluded.file_sha256,
		materialized_change_seq = excluded.materialized_change_seq, updated_at = excluded.updated_at
		WHERE pdf_working_copies.revision = ?`, userID, versionID, info.Revision, fileSHA256, command.AnnotationSeq, now.Format(time.RFC3339Nano), command.BaseRevision)
	if err != nil {
		return result, err
	}
	count, err := write.RowsAffected()
	if err != nil || count != 1 {
		return result, domain.ErrWorkingPDFConflict
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return result, err
	}
	event, err := tx.ExecContext(ctx, `INSERT INTO changes
		(user_id, document_version_id, entity_type, entity_id, revision, operation, payload_json, created_at)
		VALUES (?, ?, 'working_pdf', ?, ?, 'upsert', ?, ?)`, userID, versionID, versionID, info.Revision, string(payload), now.Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	result.WorkingPDF = info
	result.ChangeSeq, err = event.LastInsertId()
	if err != nil {
		return result, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_receipts(user_id, mutation_id, request_sha256, response_json, created_at)
		VALUES (?, ?, ?, ?, ?)`, userID, command.ID, requestSHA256, string(encoded), now.Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return domain.WorkingPDFResult{}, err
	}
	return result, nil
}
