package scan

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

type Runner struct {
	ctx      context.Context
	queue    domain.ScanQueue
	scanner  domain.SourceScanner
	importer domain.ScanBookImporter
	ownerID  string
	logger   *slog.Logger
	wake     chan struct{}
}

var _ domain.ScanService = (*Runner)(nil)

func NewRunner(ctx context.Context, queue domain.ScanQueue, scanner domain.SourceScanner, importer domain.ScanBookImporter, ownerID string, logger *slog.Logger) *Runner {
	return &Runner{ctx: ctx, queue: queue, scanner: scanner, importer: importer, ownerID: ownerID, logger: logger, wake: make(chan struct{}, 1)}
}

func (r *Runner) StartScan(ctx context.Context, libraryID, stage string) (domain.ScanTask, bool, error) {
	if !domain.IsScanStage(stage) {
		return domain.ScanTask{}, false, domain.ErrInvalidScanStage
	}
	if domain.IsImportScan(stage) && r.importer == nil {
		return domain.ScanTask{}, false, domain.ErrScanUnavailable
	}
	if r.ctx.Err() != nil {
		return domain.ScanTask{}, false, domain.ErrScanUnavailable
	}
	if err := r.scanner.ValidateLibrary(ctx, libraryID); err != nil {
		return domain.ScanTask{}, false, err
	}
	task, reused, err := r.queue.EnqueueScan(ctx, r.ownerID, libraryID, stage)
	if err == nil {
		select {
		case r.wake <- struct{}{}:
		default:
		}
	}
	return task, reused, err
}

func (r *Runner) Task(ctx context.Context, taskID string) (domain.ScanTask, error) {
	return r.queue.ScanTask(ctx, r.ownerID, taskID)
}

func (r *Runner) Run() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for r.ctx.Err() == nil {
		task, err := r.queue.ClaimScan(r.ctx, r.ownerID)
		if err != nil && r.ctx.Err() == nil {
			r.logger.Error("scan_task_claim_failed")
		}
		if task != nil && err == nil {
			r.execute(*task)
			continue
		}
		select {
		case <-r.ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
	}
}

func (r *Runner) execute(task domain.ScanTask) {
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Minute)
	defer cancel()
	count := 0
	var imported *domain.ScanImportSummary
	if domain.IsImportScan(task.Stage) {
		imported = &domain.ScanImportSummary{Failures: []domain.ScanFileFailure{}}
	}
	persist := func() error {
		return r.queue.RecordScanProgress(ctx, r.ownerID, task.ID, domain.ScanProgress{Stage: task.Stage, CandidateFiles: count, Import: imported})
	}
	visit := func(domain.SourceFile) error {
		count++
		if count%128 == 0 {
			return persist()
		}
		return nil
	}
	importVisit := func(file domain.SourceFile, reader domain.SourceReader) error {
		count++
		if err := r.importCandidate(ctx, task.LibraryID, task.Stage, file, reader, imported); err != nil {
			return err
		}
		if domain.ScanAcceptsFormat(task.Stage, file.Format) || count%128 == 0 {
			return persist()
		}
		return nil
	}
	var report domain.SourceDiscovery
	var err error
	// A preview request may own the source briefly. Keep this task running and
	// retry without advancing attempts or creating another task.
	for {
		if imported != nil {
			report, err = r.scanner.ScanForImport(ctx, task.LibraryID, importVisit)
		} else {
			report, err = r.scanner.Scan(ctx, task.LibraryID, visit)
		}
		if !errors.Is(err, domain.ErrDiscoveryBusy) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(250 * time.Millisecond):
		}
	}
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	if report.LibraryID == "" {
		report.LibraryID = task.LibraryID
		report.Items = []domain.SourceFile{}
		report.IncompleteReasons = []string{}
	}
	failure := scanFailure(err, report.Complete)
	if failure == nil && imported != nil && imported.FailedFiles > 0 {
		failure = &domain.TaskFailure{Code: "import_errors", Message: "扫描已结束，部分书籍导入未确认成功，请查看逐文件错误摘要。"}
	}
	if failure == nil && imported != nil && imported.ProcessedFiles != report.CandidateFiles {
		failure = &domain.TaskFailure{Code: "import_incomplete", Message: "发现与处理数量不一致，请检查任务统计后重新扫描。"}
	}
	// Shutdown cancels source work, but recording the final state needs a short
	// independent context. If storage is unavailable, startup recovery handles it.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := r.queue.FinishScan(finishCtx, r.ownerID, task, report, imported, failure); err != nil {
		r.logger.Error("scan_task_finish_failed", "task_id", task.ID)
	}
}

func scanFailure(err error, complete bool) *domain.TaskFailure {
	switch {
	case errors.Is(err, context.Canceled):
		return &domain.TaskFailure{Code: "interrupted", Message: "执行被中断，请重新发起扫描。"}
	case errors.Is(err, context.DeadlineExceeded):
		return &domain.TaskFailure{Code: "scan_timeout", Message: "扫描超过执行期限，请缩小来源范围。"}
	case errors.Is(err, domain.ErrSourceNotConfigured), errors.Is(err, domain.ErrLibraryNotFound):
		return &domain.TaskFailure{Code: "source_not_configured", Message: "任务书库未绑定当前来源。"}
	case errors.Is(err, domain.ErrSourceUnavailable):
		return &domain.TaskFailure{Code: "source_unavailable", Message: "无法读取来源，请检查挂载和权限。"}
	case errors.Is(err, domain.ErrArchiveConflict):
		return &domain.TaskFailure{Code: "archive_conflict", Message: "归档校验失败，已停止后续导入；未覆盖原件。"}
	case err != nil:
		return &domain.TaskFailure{Code: "scan_failed", Message: "扫描未完成，请检查服务状态后重试。"}
	case !complete:
		return &domain.TaskFailure{Code: "incomplete_discovery", Message: "文件发现不完整，请查看结果中的原因。"}
	default:
		return nil
	}
}
