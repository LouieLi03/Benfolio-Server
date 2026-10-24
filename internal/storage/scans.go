package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

var _ domain.ScanQueue = (*Store)(nil)

const scanColumns = `id, library_id, payload_json, state, attempts, candidate_files, result_json,
    error_code, error_message, created_at, updated_at, started_at, finished_at, import_summary_json`

func scanTaskRow(row *sql.Row) (domain.ScanTask, error) {
	item := domain.ScanTask{Kind: "source_scan"}
	var result, code, message, started, finished, imported sql.NullString
	var created, updated, payload string
	err := row.Scan(&item.ID, &item.LibraryID, &payload, &item.State, &item.Attempts, &item.CandidateFiles,
		&result, &code, &message, &created, &updated, &started, &finished, &imported)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ScanTask{}, domain.ErrTaskNotFound
	}
	if err != nil {
		return domain.ScanTask{}, err
	}
	var request struct {
		Stage string `json:"stage"`
	}
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return domain.ScanTask{}, err
	}
	if !domain.IsScanStage(request.Stage) {
		return domain.ScanTask{}, errors.New("不支持的扫描任务阶段")
	}
	item.Stage = request.Stage
	if imported.Valid {
		item.Import = &domain.ScanImportSummary{}
		if err := json.Unmarshal([]byte(imported.String), item.Import); err != nil {
			return domain.ScanTask{}, err
		}
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return domain.ScanTask{}, err
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return domain.ScanTask{}, err
	}
	if result.Valid {
		item.Result = &domain.SourceDiscovery{}
		if err := json.Unmarshal([]byte(result.String), item.Result); err != nil {
			return domain.ScanTask{}, err
		}
	}
	if code.Valid {
		item.Failure = &domain.TaskFailure{Code: code.String, Message: message.String}
	}
	for _, field := range []struct {
		value  sql.NullString
		target **time.Time
	}{{started, &item.StartedAt}, {finished, &item.FinishedAt}} {
		if field.value.Valid {
			parsed, err := time.Parse(time.RFC3339Nano, field.value.String)
			if err != nil {
				return domain.ScanTask{}, err
			}
			*field.target = &parsed
		}
	}
	return item, nil
}

func (s *Store) ScanTask(ctx context.Context, ownerID, taskID string) (domain.ScanTask, error) {
	return scanTaskRow(s.db.QueryRowContext(ctx, "SELECT "+scanColumns+" FROM tasks WHERE id = ? AND user_id = ? AND kind = 'source_scan'", taskID, ownerID))
}

func (s *Store) EnqueueScan(ctx context.Context, ownerID, libraryID, stage string) (domain.ScanTask, bool, error) {
	if !domain.IsScanStage(stage) {
		return domain.ScanTask{}, false, domain.ErrInvalidScanStage
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ScanTask{}, false, err
	}
	defer tx.Rollback()
	existing, err := scanTaskRow(tx.QueryRowContext(ctx, "SELECT "+scanColumns+` FROM tasks
        WHERE user_id = ? AND library_id = ? AND kind = 'source_scan' AND state IN ('queued', 'running')`, ownerID, libraryID))
	if err == nil {
		if existing.Stage != stage {
			return domain.ScanTask{}, false, domain.ErrScanStageBusy
		}
		if err := tx.Commit(); err != nil {
			return domain.ScanTask{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, domain.ErrTaskNotFound) {
		return domain.ScanTask{}, false, err
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.ScanTask{}, false, err
	}
	payload, err := json.Marshal(map[string]string{"libraryID": libraryID, "stage": stage})
	if err != nil {
		return domain.ScanTask{}, false, err
	}
	created := timestamp()
	var summary any
	if domain.IsImportScan(stage) {
		summary, err = scanSummaryJSON(&domain.ScanImportSummary{Failures: []domain.ScanFileFailure{}}, 0)
		if err != nil {
			return domain.ScanTask{}, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tasks
        (id, user_id, library_id, kind, state, payload_json, created_at, updated_at, import_summary_json)
        VALUES (?, ?, ?, 'source_scan', 'queued', ?, ?, ?, ?)`, id, ownerID, libraryID, string(payload), created, created, summary); err != nil {
		return domain.ScanTask{}, false, err
	}
	item, err := scanTaskRow(tx.QueryRowContext(ctx, "SELECT "+scanColumns+" FROM tasks WHERE id = ? AND user_id = ?", id, ownerID))
	if err != nil {
		return domain.ScanTask{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return domain.ScanTask{}, false, err
	}
	return item, false, nil
}

func (s *Store) ClaimScan(ctx context.Context, ownerID string) (*domain.ScanTask, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM tasks WHERE user_id = ? AND kind = 'source_scan'
        AND state = 'queued' ORDER BY created_at, id LIMIT 1`, ownerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	updated := timestamp()
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET state = 'running', attempts = attempts + 1,
        started_at = ?, updated_at = ? WHERE id = ? AND user_id = ? AND kind = 'source_scan' AND state = 'queued'`, updated, updated, id, ownerID)
	if err := requireTaskChange(result, err); err != nil {
		return nil, err
	}
	item, err := scanTaskRow(tx.QueryRowContext(ctx, "SELECT "+scanColumns+" FROM tasks WHERE id = ? AND user_id = ?", id, ownerID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) RecordScanProgress(ctx context.Context, ownerID, taskID string, progress domain.ScanProgress) error {
	if err := checkScanSummaryStage(progress.Stage, progress.Import); err != nil {
		return err
	}
	summary, err := scanSummaryJSON(progress.Import, progress.CandidateFiles)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET candidate_files = ?, import_summary_json = ?, updated_at = ?
        WHERE id = ? AND user_id = ? AND kind = 'source_scan' AND state = 'running' AND candidate_files <= ?
        AND json_extract(payload_json, '$.stage') = ?`, progress.CandidateFiles, summary, timestamp(), taskID, ownerID, progress.CandidateFiles, progress.Stage)
	return requireTaskChange(result, err)
}

func (s *Store) FinishScan(ctx context.Context, ownerID string, task domain.ScanTask, report domain.SourceDiscovery, imported *domain.ScanImportSummary, failure *domain.TaskFailure) error {
	if err := checkScanSummaryStage(task.Stage, imported); err != nil {
		return err
	}
	if failure == nil && (!report.Complete || (imported != nil && (imported.FailedFiles > 0 || imported.ProcessedFiles != report.CandidateFiles))) {
		return errors.New("未完整处理的扫描不能记为成功")
	}
	summary, err := scanSummaryJSON(imported, report.CandidateFiles)
	if err != nil {
		return err
	}
	state := "succeeded"
	var code, message any
	if failure != nil {
		state, code, message = "failed", failure.Code, failure.Message
	}
	resultJSON, err := json.Marshal(report)
	if err != nil {
		return err
	}
	finished := timestamp()
	result, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = ?, candidate_files = MAX(candidate_files, ?),
        result_json = ?, import_summary_json = ?, error_code = ?, error_message = ?, finished_at = ?, updated_at = ?
        WHERE id = ? AND user_id = ? AND kind = 'source_scan' AND state = 'running'
        AND json_extract(payload_json, '$.stage') = ?`,
		state, report.CandidateFiles, string(resultJSON), summary, code, message, finished, finished, task.ID, ownerID, task.Stage)
	return requireTaskChange(result, err)
}

func checkScanSummaryStage(stage string, summary *domain.ScanImportSummary) error {
	if !domain.IsScanStage(stage) || domain.IsImportScan(stage) != (summary != nil) {
		return domain.ErrInvalidScanStage
	}
	return nil
}

func scanSummaryJSON(summary *domain.ScanImportSummary, candidates int) (any, error) {
	if candidates < 0 {
		return nil, errors.New("扫描发现数量无效")
	}
	if summary == nil {
		return nil, nil
	}
	if summary.ProcessedFiles < 0 || summary.ImportedFiles < 0 || summary.NewBooks < 0 || summary.NewVersions < 0 || summary.ReusedVersions < 0 || summary.SkippedFiles < 0 || summary.FailedFiles < 0 ||
		summary.ProcessedFiles != summary.ImportedFiles+summary.SkippedFiles+summary.FailedFiles || summary.ProcessedFiles > candidates ||
		summary.ImportedFiles != summary.NewVersions+summary.ReusedVersions || summary.NewBooks > summary.NewVersions || len(summary.Failures) > 50 || len(summary.Failures) > summary.FailedFiles {
		return nil, errors.New("扫描导入统计无效")
	}
	value, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	return string(value), nil
}

func (s *Store) RecoverScans(ctx context.Context, ownerID string) error {
	finished := timestamp()
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET state = 'failed', error_code = 'interrupted',
        error_message = '上次执行被中断，请重新发起扫描。', finished_at = ?, updated_at = ?
        WHERE user_id = ? AND kind = 'source_scan' AND state = 'running'`, finished, finished, ownerID)
	return err
}

func requireTaskChange(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrTaskState
	}
	return nil
}
