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

var _ domain.ExportStore = (*Store)(nil)

func (s *Store) ExportContext(ctx context.Context, userID, versionID string) (domain.ExportContext, error) {
	info := domain.ExportContext{DocumentVersionID: versionID}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return info, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT v.sha256,b.format FROM document_versions v JOIN books b ON b.id=v.book_id WHERE v.id=?`, versionID).Scan(&info.SourceSHA256, &info.BookFormat)
	if errors.Is(err, sql.ErrNoRows) {
		return info, domain.ErrVersionNotFound
	}
	if err != nil {
		return info, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=? AND document_version_id=? AND entity_type='annotation'`, userID, versionID).Scan(&info.LatestAnnotationSeq); err != nil {
		return info, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM annotations WHERE user_id=? AND document_version_id=? AND deleted_at IS NULL", userID, versionID).Scan(&info.ActiveAnnotationCount); err != nil {
		return info, err
	}
	err = tx.QueryRowContext(ctx, "SELECT revision FROM pdf_working_copies WHERE user_id=? AND document_version_id=?", userID, versionID).Scan(&info.LatestWorkingRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return info, err
	}
	return info, nil
}

func (s *Store) ExportSnapshot(ctx context.Context, userID, versionID string) (domain.ExportSnapshot, error) {
	var snapshot domain.ExportSnapshot
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	snapshot.DocumentVersionID = versionID
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT v.sha256,b.format,m.metadata_json FROM document_versions v
		JOIN books b ON b.id=v.book_id LEFT JOIN document_metadata m ON m.version_id=v.id WHERE v.id=?`, versionID).
		Scan(&snapshot.SourceSHA256, &snapshot.BookFormat, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, domain.ErrVersionNotFound
	}
	if err != nil {
		return snapshot, err
	}
	var metadata domain.BookMetadata
	if json.Unmarshal([]byte(raw), &metadata) != nil {
		return snapshot, domain.ErrResourceConflict
	}
	snapshot.Title, snapshot.Authors = metadata.Title, metadata.Authors
	if snapshot.BookFormat == domain.PDF {
		if metadata.PDF == nil || metadata.PDF.PageCount < 1 {
			return snapshot, domain.ErrResourceConflict
		}
		snapshot.PageCount = metadata.PDF.PageCount
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes
		WHERE user_id=? AND document_version_id=? AND entity_type='annotation'`, userID, versionID).Scan(&snapshot.AnnotationSeq); err != nil {
		return snapshot, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+annotationColumns+" FROM annotations WHERE user_id=? AND document_version_id=? AND deleted_at IS NULL ORDER BY id LIMIT 1001", userID, versionID)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	snapshot.Annotations = []domain.Annotation{}
	for rows.Next() {
		note, err := annotationRow(rows)
		if err != nil {
			return domain.ExportSnapshot{}, err
		}
		snapshot.Annotations = append(snapshot.Annotations, note)
		if len(snapshot.Annotations) > 1000 {
			return domain.ExportSnapshot{}, domain.ErrExportLimit
		}
	}
	if err := rows.Err(); err != nil {
		return domain.ExportSnapshot{}, err
	}
	return snapshot, nil
}

func exportReceipt(ctx context.Context, query rowQuerier, userID, id, digest string) (*domain.ExportResult, error) {
	var previous, raw string
	err := query.QueryRowContext(ctx, "SELECT request_sha256,response_json FROM mutation_receipts WHERE user_id=? AND mutation_id=?", userID, id).Scan(&previous, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if previous != digest {
		return nil, domain.ErrMutationReused
	}
	var result domain.ExportResult
	if json.Unmarshal([]byte(raw), &result) != nil || result.Export.ID != id || result.Export.Seq < 1 || !syncvalue.ValidSHA256(result.Export.FileSHA256) {
		return nil, domain.ErrResourceConflict
	}
	result.Replayed = true
	return &result, nil
}

func (s *Store) ExportReceipt(ctx context.Context, userID, id, digest string) (*domain.ExportResult, error) {
	return exportReceipt(ctx, s.db, userID, id, digest)
}

func (s *Store) ReadExport(ctx context.Context, userID, id string) (domain.ExportInfo, error) {
	var info domain.ExportInfo
	var revision sql.NullInt64
	var created, mediaType, key string
	err := s.db.QueryRowContext(ctx, `SELECT e.id,e.seq,e.document_version_id,e.source_sha256,e.book_format,e.format,e.file_sha256,r.byte_size,
		e.annotation_seq,e.working_revision,e.annotation_count,e.stale_at_creation,e.device_id,e.created_at,r.media_type,r.object_key
		FROM export_records e LEFT JOIN resources r ON r.sha256=e.file_sha256 WHERE e.user_id=? AND e.id=?`, userID, id).
		Scan(&info.ID, &info.Seq, &info.DocumentVersionID, &info.SourceSHA256, &info.BookFormat, &info.Format, &info.FileSHA256, &info.ByteSize,
			&info.AnnotationSeq, &revision, &info.AnnotationCount, &info.StaleAtCreation, &info.DeviceID, &created, &mediaType, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return info, domain.ErrExportNotFound
	}
	if err != nil {
		return info, err
	}
	if revision.Valid {
		info.WorkingRevision = revision.Int64
	}
	if !syncvalue.ValidSHA256(info.FileSHA256) || info.ByteSize < 1 {
		return info, domain.ErrResourceConflict
	}
	switch info.Format {
	case "pdf":
		if info.BookFormat != domain.PDF || !revision.Valid || revision.Int64 < 1 || info.ByteSize > domain.MaxWorkingPDFBytes || mediaType != "application/pdf" || key != workingObjectKey(info.FileSHA256) {
			return info, domain.ErrResourceConflict
		}
	case "markdown":
		if revision.Valid || info.ByteSize > domain.MaxMarkdownBytes || mediaType != "text/markdown" || key != exportObjectKey(info.FileSHA256) {
			return info, domain.ErrResourceConflict
		}
	default:
		return info, domain.ErrResourceConflict
	}
	info.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	return info, err
}

func exportObjectKey(digest string) string { return "exports/" + digest[:2] + "/" + digest + ".md" }

func (s *Store) CommitExport(ctx context.Context, userID, versionID string, command domain.ExportMutation, requestHash string, info domain.ExportInfo) (domain.ExportResult, error) {
	var result domain.ExportResult
	limit := domain.MaxMarkdownBytes
	if command.Format == "pdf" {
		limit = domain.MaxWorkingPDFBytes
	}
	if !syncvalue.ValidSHA256(info.FileSHA256) || info.ByteSize < 1 || info.ByteSize > limit || info.AnnotationCount < 0 || info.AnnotationCount > 1000 {
		return result, domain.ErrInvalidExport
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	old, err := exportReceipt(ctx, tx, userID, command.ID, requestHash)
	if err != nil {
		return result, err
	}
	if old != nil {
		if old.Export.DocumentVersionID != versionID {
			return result, domain.ErrResourceConflict
		}
		return *old, nil
	}
	var format domain.Format
	var sourceSHA string
	err = tx.QueryRowContext(ctx, `SELECT v.sha256,b.format FROM document_versions v JOIN books b ON b.id=v.book_id WHERE v.id=?`, versionID).Scan(&sourceSHA, &format)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.ErrVersionNotFound
	}
	if err != nil {
		return result, err
	}
	if sourceSHA != command.SourceSHA256 || info.SourceSHA256 != sourceSHA || info.BookFormat != format || info.DocumentVersionID != versionID || info.ID != command.ID || info.Format != command.Format || info.AnnotationSeq != command.AnnotationSeq || info.WorkingRevision != command.WorkingRevision || info.DeviceID != command.DeviceID {
		return result, domain.ErrExportConflict
	}
	var latestSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes WHERE user_id=? AND document_version_id=? AND entity_type='annotation'`, userID, versionID).Scan(&latestSeq); err != nil {
		return result, err
	}
	var revision any
	key, mediaType := exportObjectKey(info.FileSHA256), "text/markdown"
	if command.Format == "pdf" {
		if format != domain.PDF || command.WorkingRevision < 1 {
			return result, domain.ErrInvalidExport
		}
		var hash string
		var seq int64
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT file_sha256,materialized_change_seq,annotation_count FROM working_pdf_history
			WHERE user_id=? AND document_version_id=? AND revision=?`, userID, versionID, command.WorkingRevision).Scan(&hash, &seq, &count); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return result, domain.ErrWorkingPDFNotFound
			}
			return result, err
		}
		if hash != info.FileSHA256 || seq != command.AnnotationSeq || count != info.AnnotationCount {
			return result, domain.ErrExportConflict
		}
		revision = command.WorkingRevision
		key, mediaType = workingObjectKey(info.FileSHA256), "application/pdf"
	} else if command.Format == "markdown" {
		if command.WorkingRevision != 0 || latestSeq != command.AnnotationSeq {
			return result, domain.ErrExportConflict
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM annotations WHERE user_id=? AND document_version_id=? AND deleted_at IS NULL", userID, versionID).Scan(&count); err != nil {
			return result, err
		}
		if count != info.AnnotationCount {
			return result, domain.ErrExportConflict
		}
	} else {
		return result, domain.ErrInvalidExport
	}
	info.StaleAtCreation = latestSeq != command.AnnotationSeq
	if _, err := tx.ExecContext(ctx, `INSERT INTO resources(sha256,byte_size,media_type,object_key,created_at)
		VALUES(?,?,?,?,?) ON CONFLICT(sha256) DO NOTHING`, info.FileSHA256, info.ByteSize, mediaType, key, info.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	var storedSize int64
	var storedType, storedKey string
	if err := tx.QueryRowContext(ctx, "SELECT byte_size,media_type,object_key FROM resources WHERE sha256=?", info.FileSHA256).Scan(&storedSize, &storedType, &storedKey); err != nil {
		return result, err
	}
	if storedSize != info.ByteSize || storedType != mediaType || storedKey != key {
		return result, domain.ErrResourceConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_owners(user_id,resource_sha256,created_at) VALUES(?,?,?)
		ON CONFLICT(user_id,resource_sha256) DO NOTHING`, userID, info.FileSHA256, info.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	event, err := tx.ExecContext(ctx, `INSERT INTO export_records
		(user_id,id,document_version_id,source_sha256,book_format,format,file_sha256,annotation_seq,working_revision,annotation_count,stale_at_creation,device_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, userID, info.ID, versionID, sourceSHA, format, info.Format, info.FileSHA256, info.AnnotationSeq, revision, info.AnnotationCount, info.StaleAtCreation, info.DeviceID, info.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	info.Seq, err = event.LastInsertId()
	if err != nil {
		return result, err
	}
	result.Export = info
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_receipts(user_id,mutation_id,request_sha256,response_json,created_at) VALUES(?,?,?,?,?)`, userID, command.ID, requestHash, string(raw), info.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ExportResult{}, err
	}
	return result, nil
}
