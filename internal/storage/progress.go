package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.ProgressStore = (*Store)(nil)

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) ReadProgress(ctx context.Context, userID, versionID string) (domain.Progress, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM document_versions WHERE id = ?", versionID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Progress{}, domain.ErrVersionNotFound
	}
	if err != nil {
		return domain.Progress{}, err
	}
	return readProgress(ctx, s.db, userID, versionID)
}

func readProgress(ctx context.Context, query rowQuerier, userID, versionID string) (domain.Progress, error) {
	item := domain.Progress{DocumentVersionID: versionID, Locator: json.RawMessage("null")}
	var locator, updated string
	var completed int
	err := query.QueryRowContext(ctx, `SELECT revision, locator_json, completed, updated_at, device_id
		FROM reading_progress WHERE user_id = ? AND document_version_id = ?`, userID, versionID).
		Scan(&item.Revision, &locator, &completed, &updated, &item.DeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return domain.Progress{}, err
	}
	if !json.Valid([]byte(locator)) {
		return domain.Progress{}, errors.New("已存阅读位置损坏")
	}
	when, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.Progress{}, err
	}
	item.Locator, item.Completed, item.UpdatedAt = json.RawMessage(locator), completed == 1, &when
	return item, nil
}

// CommitProgress commits the position, event and receipt as one transaction.
// A replay must be recognized before checking the now-outdated base revision.
func (s *Store) CommitProgress(ctx context.Context, userID, versionID string, command domain.ProgressMutation, requestSHA256 string) (domain.ProgressResult, error) {
	var result domain.ProgressResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var previousHash, responseJSON string
	err = tx.QueryRowContext(ctx, `SELECT request_sha256, response_json FROM mutation_receipts
		WHERE user_id = ? AND mutation_id = ?`, userID, command.ID).Scan(&previousHash, &responseJSON)
	switch {
	case err == nil:
		if previousHash != requestSHA256 {
			return result, domain.ErrMutationReused
		}
		if err := json.Unmarshal([]byte(responseJSON), &result); err != nil {
			return result, err
		}
		if result.Progress.DocumentVersionID != versionID || result.Progress.Revision < 1 || result.ChangeSeq < 1 {
			return domain.ProgressResult{}, errors.New("操作回执损坏")
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
	current, err := readProgress(ctx, tx, userID, versionID)
	if err != nil {
		return result, err
	}
	if current.Revision != command.BaseRevision {
		return result, &domain.ProgressConflict{Current: current}
	}
	if current.Revision >= domain.MaxMutationRevision {
		return result, errors.New("阅读进度修订号达到上限")
	}
	now := time.Now().UTC()
	result.Progress = domain.Progress{DocumentVersionID: versionID, Locator: command.Payload.Locator,
		Completed: command.Payload.Completed, Revision: current.Revision + 1, DeviceID: command.DeviceID, UpdatedAt: &now}
	write, err := tx.ExecContext(ctx, `INSERT INTO reading_progress
		(user_id, document_version_id, revision, locator_json, completed, updated_at, device_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, document_version_id) DO UPDATE SET revision = excluded.revision,
		locator_json = excluded.locator_json, completed = excluded.completed,
		updated_at = excluded.updated_at, device_id = excluded.device_id
		WHERE reading_progress.revision = ?`, userID, versionID, result.Progress.Revision,
		string(result.Progress.Locator), result.Progress.Completed, now.Format(time.RFC3339Nano), command.DeviceID, command.BaseRevision)
	if err != nil {
		return domain.ProgressResult{}, err
	}
	count, err := write.RowsAffected()
	if err != nil || count != 1 {
		return domain.ProgressResult{}, errors.New("阅读进度条件写入未完成")
	}
	payload, err := json.Marshal(result.Progress)
	if err != nil {
		return domain.ProgressResult{}, err
	}
	event, err := tx.ExecContext(ctx, `INSERT INTO changes
		(user_id, document_version_id, entity_type, entity_id, revision, operation, payload_json, created_at)
		VALUES (?, ?, 'progress', ?, ?, 'upsert', ?, ?)`, userID, versionID, versionID,
		result.Progress.Revision, string(payload), now.Format(time.RFC3339Nano))
	if err != nil {
		return domain.ProgressResult{}, err
	}
	result.ChangeSeq, err = event.LastInsertId()
	if err != nil {
		return domain.ProgressResult{}, err
	}
	response, err := json.Marshal(result)
	if err != nil {
		return domain.ProgressResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mutation_receipts
		(user_id, mutation_id, request_sha256, response_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		userID, command.ID, requestSHA256, string(response), now.Format(time.RFC3339Nano)); err != nil {
		return domain.ProgressResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ProgressResult{}, err
	}
	return result, nil
}
