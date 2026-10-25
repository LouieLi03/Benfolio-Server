package domain

import (
	"context"
	"errors"
	"time"
)

const MaxWorkingPDFBytes int64 = 128 << 20

var (
	ErrInvalidWorkingPDF     = errors.New("工作PDF请求无效")
	ErrWorkingPDFNotFound    = errors.New("工作PDF尚未生成")
	ErrWorkingPDFConflict    = errors.New("工作PDF或批注已更新")
	ErrWorkingPDFUnsupported = errors.New("PDF内容超出当前生成范围")
	ErrWorkingPDFLimit       = errors.New("工作PDF生成超出限制")
	ErrWorkingPDFRender      = errors.New("工作PDF生成失败")
	ErrWorkingPDFBusy        = errors.New("已有工作PDF正在生成")
)

type WorkingPDFInfo struct {
	DocumentVersionID     string     `json:"documentVersionID"`
	SourceSHA256          string     `json:"sourceSHA256"`
	Revision              int64      `json:"revision"`
	FileSHA256            string     `json:"fileSHA256,omitempty"`
	ByteSize              int64      `json:"byteSize"`
	MaterializedChangeSeq int64      `json:"materializedChangeSeq,string"`
	AnnotationCount       int        `json:"annotationCount"`
	DeviceID              string     `json:"deviceID,omitempty"`
	UpdatedAt             *time.Time `json:"updatedAt,omitempty"`
}

type WorkingPDFStatus struct {
	WorkingPDFInfo
	SourceByteSize      int64 `json:"sourceByteSize"`
	LatestAnnotationSeq int64 `json:"latestAnnotationSeq,string"`
	LatestRevision      int64 `json:"latestRevision"`
	Available           bool  `json:"available"`
	Stale               bool  `json:"stale"`
}

type WorkingPDFMutation struct {
	ID            string `json:"id"`
	DeviceID      string `json:"deviceID"`
	BaseRevision  int64  `json:"baseRevision"`
	SourceSHA256  string `json:"sourceSHA256"`
	AnnotationSeq int64  `json:"annotationSeq,string"`
}

type WorkingPDFResult struct {
	WorkingPDF WorkingPDFInfo `json:"workingPDF"`
	ChangeSeq  int64          `json:"changeSeq,string"`
	Replayed   bool           `json:"replayed"`
}

type WorkingPDFSnapshot struct {
	Status      WorkingPDFStatus
	PageCount   int
	Annotations []Annotation
}

type WorkingPDFFile struct {
	Status  WorkingPDFStatus
	Content OriginalContent
}

type WorkingPDFStore interface {
	WorkingStatus(context.Context, string, string, int64) (WorkingPDFStatus, error)
	WorkingSnapshot(context.Context, string, string) (WorkingPDFSnapshot, error)
	WorkingReceipt(context.Context, string, string, string) (*WorkingPDFResult, error)
	CommitWorkingPDF(context.Context, string, string, WorkingPDFMutation, string, string, int64, int) (WorkingPDFResult, error)
}

type WorkingPDFService interface {
	Status(context.Context, string, string, int64) (WorkingPDFStatus, error)
	Build(context.Context, string, string, WorkingPDFMutation) (WorkingPDFResult, error)
	Open(context.Context, string, string, int64) (WorkingPDFFile, error)
}
