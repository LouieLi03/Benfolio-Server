package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrTaskNotFound     = errors.New("扫描任务不存在")
	ErrTaskState        = errors.New("扫描任务状态已变化")
	ErrScanUnavailable  = errors.New("扫描任务服务正在停止")
	ErrScanStageBusy    = errors.New("已有不同阶段的扫描任务尚未结束")
	ErrInvalidScanStage = errors.New("扫描阶段无效")
)

const (
	ScanDiscovery  = "discovery"
	ScanEPUBImport = "epub_import"
	ScanPDFImport  = "pdf_import"
	ScanBookImport = "book_import"
)

func IsImportScan(stage string) bool {
	return stage == ScanEPUBImport || stage == ScanPDFImport || stage == ScanBookImport
}
func IsScanStage(stage string) bool { return stage == ScanDiscovery || IsImportScan(stage) }
func ScanAcceptsFormat(stage string, format Format) bool {
	switch stage {
	case ScanEPUBImport:
		return format == EPUB
	case ScanPDFImport:
		return format == PDF
	case ScanBookImport:
		return format == EPUB || format == PDF
	default:
		return false
	}
}

type ScanFileFailure struct {
	RelativePath string `json:"relativePath"`
	Code         string `json:"code"`
}

// Counts describe completed file attempts, not an atomic library snapshot.
type ScanImportSummary struct {
	ProcessedFiles    int               `json:"processedFiles"`
	ImportedFiles     int               `json:"importedFiles"`
	NewBooks          int               `json:"newBooks"`
	NewVersions       int               `json:"newVersions"`
	ReusedVersions    int               `json:"reusedVersions"`
	SkippedFiles      int               `json:"skippedFiles"`
	FailedFiles       int               `json:"failedFiles"`
	Failures          []ScanFileFailure `json:"failures"`
	FailuresTruncated bool              `json:"failuresTruncated"`
}

type ScanProgress struct {
	Stage          string
	CandidateFiles int
	Import         *ScanImportSummary
}

type TaskFailure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Success refers only to the named stage. Discovery does not import books.
type ScanTask struct {
	ID             string             `json:"id"`
	LibraryID      string             `json:"libraryID"`
	Kind           string             `json:"kind"`
	Stage          string             `json:"stage"`
	State          string             `json:"state"`
	Attempts       int                `json:"attempts"`
	CandidateFiles int                `json:"candidateFiles"`
	Import         *ScanImportSummary `json:"import,omitempty"`
	Result         *SourceDiscovery   `json:"result,omitempty"`
	Failure        *TaskFailure       `json:"error,omitempty"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
	StartedAt      *time.Time         `json:"startedAt,omitempty"`
	FinishedAt     *time.Time         `json:"finishedAt,omitempty"`
}

type ScanQueue interface {
	EnqueueScan(context.Context, string, string, string) (ScanTask, bool, error)
	ScanTask(context.Context, string, string) (ScanTask, error)
	ClaimScan(context.Context, string) (*ScanTask, error)
	RecordScanProgress(context.Context, string, string, ScanProgress) error
	FinishScan(context.Context, string, ScanTask, SourceDiscovery, *ScanImportSummary, *TaskFailure) error
	RecoverScans(context.Context, string) error
}

type ScanService interface {
	StartScan(context.Context, string, string) (ScanTask, bool, error)
	Task(context.Context, string) (ScanTask, error)
}
