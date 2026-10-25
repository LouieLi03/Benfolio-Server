package domain

import (
	"context"
	"errors"
	"time"
)

const MaxMarkdownBytes int64 = 16 << 20

var (
	ErrInvalidExport  = errors.New("导出请求无效")
	ErrExportNotFound = errors.New("导出记录不存在")
	ErrExportConflict = errors.New("导出快照与请求不一致")
	ErrExportLimit    = errors.New("导出内容超出限制")
	ErrExportBusy     = errors.New("导出操作达到并发上限")
)

type ExportMutation struct {
	ID              string `json:"id"`
	DeviceID        string `json:"deviceID"`
	Format          string `json:"format"`
	SourceSHA256    string `json:"sourceSHA256"`
	AnnotationSeq   int64  `json:"annotationSeq,string"`
	WorkingRevision int64  `json:"workingRevision,omitempty"`
}

type ExportInfo struct {
	ID                string    `json:"id"`
	Seq               int64     `json:"seq,string"`
	DocumentVersionID string    `json:"documentVersionID"`
	SourceSHA256      string    `json:"sourceSHA256"`
	BookFormat        Format    `json:"bookFormat"`
	Format            string    `json:"format"`
	FileSHA256        string    `json:"fileSHA256"`
	ByteSize          int64     `json:"byteSize"`
	AnnotationSeq     int64     `json:"annotationSeq,string"`
	WorkingRevision   int64     `json:"workingRevision,omitempty"`
	AnnotationCount   int       `json:"annotationCount"`
	StaleAtCreation   bool      `json:"staleAtCreation"`
	DeviceID          string    `json:"deviceID"`
	CreatedAt         time.Time `json:"createdAt"`
}

type ExportResult struct {
	Export   ExportInfo `json:"export"`
	Replayed bool       `json:"replayed"`
}

type ExportSnapshot struct {
	DocumentVersionID string
	SourceSHA256      string
	BookFormat        Format
	Title             string
	Authors           []string
	AnnotationSeq     int64
	Annotations       []Annotation
	PageCount         int
}

type ExportContext struct {
	DocumentVersionID     string `json:"documentVersionID"`
	SourceSHA256          string `json:"sourceSHA256"`
	BookFormat            Format `json:"bookFormat"`
	LatestAnnotationSeq   int64  `json:"latestAnnotationSeq,string"`
	ActiveAnnotationCount int    `json:"activeAnnotationCount"`
	LatestWorkingRevision int64  `json:"latestWorkingRevision"`
}

type ExportFile struct {
	Export  ExportInfo
	Content OriginalContent
}

type ExportStore interface {
	ExportContext(context.Context, string, string) (ExportContext, error)
	ExportSnapshot(context.Context, string, string) (ExportSnapshot, error)
	ExportReceipt(context.Context, string, string, string) (*ExportResult, error)
	ReadExport(context.Context, string, string) (ExportInfo, error)
	CommitExport(context.Context, string, string, ExportMutation, string, ExportInfo) (ExportResult, error)
}

type ExportService interface {
	Context(context.Context, string, string) (ExportContext, error)
	Create(context.Context, string, string, ExportMutation) (ExportResult, error)
	Get(context.Context, string, string) (ExportInfo, error)
	Open(context.Context, string, string) (ExportFile, error)
}
