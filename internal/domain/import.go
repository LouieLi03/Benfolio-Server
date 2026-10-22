package domain

import (
	"context"
	"errors"
	"io"
	"os"
)

var (
	ErrInvalidSourcePath = errors.New("来源相对路径无效")
	ErrSourceChanged     = errors.New("导入期间来源文件发生变化")
	ErrUnsupportedBook   = errors.New("当前只支持单本 EPUB 导入")
	ErrInvalidEPUB       = errors.New("EPUB 结构无效或超出解析限制")
	ErrImportSize        = errors.New("书籍为空或超过导入大小限制")
	ErrArchiveConflict   = errors.New("原书归档校验失败")
)

// SourceContent owns its read handle and the source access slot until Close.
type SourceContent interface {
	io.ReadCloser
	Info() os.FileInfo
	VerifyUnchanged() error
}

type SourceReader interface {
	OpenContent(context.Context, string, string) (SourceContent, error)
}

type EPUBMetadata struct {
	Title                   string   `json:"title"`
	Authors                 []string `json:"authors"`
	Language                string   `json:"language"`
	Identifiers             []string `json:"identifiers"`
	PackageVersion          string   `json:"packageVersion"`
	HasEncryptionDescriptor bool     `json:"hasEncryptionDescriptor"`
}

type ImportRecord struct {
	LibraryID    string
	RelativePath string
	SHA256       string
	ByteSize     int64
	ObjectKey    string
	Metadata     EPUBMetadata
}

type ImportResult struct {
	BookID         string          `json:"bookID"`
	Version        DocumentVersion `json:"version"`
	Metadata       EPUBMetadata    `json:"metadata"`
	BookCreated    bool            `json:"bookCreated"`
	VersionCreated bool            `json:"versionCreated"`
}

type ImportStore interface {
	SaveEPUB(context.Context, ImportRecord) (ImportResult, error)
}

type BookImporter interface {
	ImportEPUB(context.Context, string, string) (ImportResult, error)
}

type ScanBookImporter interface {
	ImportEPUBFrom(context.Context, SourceReader, string, string) (ImportResult, error)
}
