package scan

import (
	"context"
	"errors"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (r *Runner) importCandidate(ctx context.Context, libraryID, stage string, file domain.SourceFile, reader domain.SourceReader, summary *domain.ScanImportSummary) error {
	if r.importer == nil {
		return domain.ErrScanUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ScanAcceptsFormat(stage, file.Format) {
		summary.SkippedFiles++
		summary.ProcessedFiles++
		return nil
	}
	fileCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := r.importer.ImportBookFrom(fileCtx, reader, libraryID, file.RelativePath)
	if err == nil {
		summary.ProcessedFiles++
		summary.ImportedFiles++
		if result.BookCreated {
			summary.NewBooks++
		}
		if result.VersionCreated {
			summary.NewVersions++
		} else {
			summary.ReusedVersions++
		}
		return nil
	}
	// A canceled batch may leave the current outcome unconfirmed. Do not
	// claim the in-flight file rolled back or completed in the task counters.
	if batchErr := ctx.Err(); batchErr != nil {
		return batchErr
	}
	code, stop := importFailure(err)
	summary.ProcessedFiles++
	summary.FailedFiles++
	if len(summary.Failures) < 50 {
		summary.Failures = append(summary.Failures, domain.ScanFileFailure{RelativePath: file.RelativePath, Code: code})
	} else {
		summary.FailuresTruncated = true
	}
	if stop {
		return err
	}
	return nil
}

func importFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, domain.ErrInvalidEPUB):
		return "invalid_epub", false
	case errors.Is(err, domain.ErrInvalidPDF):
		return "invalid_pdf", false
	case errors.Is(err, domain.ErrEncryptedPDF):
		return "encrypted_pdf", false
	case errors.Is(err, domain.ErrPDFParserFailure):
		return "pdf_parser_failed", true
	case errors.Is(err, domain.ErrImportSize):
		return "import_size_limit", false
	case errors.Is(err, domain.ErrSourceChanged):
		return "source_changed", false
	case errors.Is(err, domain.ErrInvalidSourcePath):
		return "invalid_source_path", false
	case errors.Is(err, domain.ErrUnsupportedBook):
		return "unsupported_book", false
	case errors.Is(err, domain.ErrSourceUnavailable):
		return "source_unavailable", false
	case errors.Is(err, context.DeadlineExceeded):
		return "import_timeout", false
	case errors.Is(err, domain.ErrArchiveConflict):
		return "archive_conflict", true
	default:
		return "import_failed", true
	}
}
