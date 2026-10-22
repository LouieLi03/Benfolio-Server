package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrLibraryNotFound     = errors.New("书库不存在")
	ErrSourceNotConfigured = errors.New("书库未绑定本次启动配置的来源")
	ErrSourceUnavailable   = errors.New("书籍来源暂不可读取")
	ErrDiscoveryBusy       = errors.New("书籍来源正被其他操作占用")
)

// SourceFile is an extension-based candidate, not an imported book or a stable
// document identity. Paths are relative to the configured library source.
type SourceFile struct {
	RelativePath string    `json:"relativePath"`
	Format       Format    `json:"format"`
	ByteSize     int64     `json:"byteSize"`
	ModifiedAt   time.Time `json:"modifiedAt"`
}

type SourceDiscovery struct {
	LibraryID             string       `json:"libraryID"`
	Items                 []SourceFile `json:"items"`
	Complete              bool         `json:"complete"`
	IncompleteReasons     []string     `json:"incompleteReasons"`
	VisitedEntries        int          `json:"visitedEntries"`
	CandidateFiles        int          `json:"candidateFiles"`
	SkippedSymlinks       int          `json:"skippedSymlinks"`
	SkippedDirectories    int          `json:"skippedDirectories"`
	SkippedChangedEntries int          `json:"skippedChangedEntries"`
	SkippedInvalidNames   int          `json:"skippedInvalidNames"`
	Limit                 int          `json:"limit"`
	EntryLimit            int          `json:"entryLimit"`
	DepthLimit            int          `json:"depthLimit"`
}

type SourceBrowser interface {
	Files(context.Context, string, int) (SourceDiscovery, error)
}

type SourceScanner interface {
	ValidateLibrary(context.Context, string) error
	Scan(context.Context, string, func(SourceFile) error) (SourceDiscovery, error)
	// The reader belongs to the current callback. Import synchronously and
	// close content before returning; do not retain the reader or its handles.
	ScanForImport(context.Context, string, func(SourceFile, SourceReader) error) (SourceDiscovery, error)
}
