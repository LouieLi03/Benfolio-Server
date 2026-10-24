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
	ErrUnsupportedBook   = errors.New("当前仅支持 EPUB 和 PDF 导入")
	ErrInvalidEPUB       = errors.New("EPUB 结构无效或超出解析限制")
	ErrInvalidPDF        = errors.New("PDF 结构无效或超出解析限制")
	ErrEncryptedPDF      = errors.New("当前暂不支持加密 PDF")
	ErrPDFParserFailure  = errors.New("PDF 解析器未能完成处理")
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

type PDFMetadata struct {
	PageCount int    `json:"pageCount"`
	Version   string `json:"version"`
}

type BookMetadata struct {
	Title                   string       `json:"title"`
	Authors                 []string     `json:"authors"`
	Language                string       `json:"language"`
	Identifiers             []string     `json:"identifiers"`
	PackageVersion          string       `json:"packageVersion"`
	HasEncryptionDescriptor bool         `json:"hasEncryptionDescriptor"`
	PDF                     *PDFMetadata `json:"pdf,omitempty"`
}

type ImportRecord struct {
	LibraryID    string
	RelativePath string
	SHA256       string
	ByteSize     int64
	ObjectKey    string
	Format       Format
	Metadata     BookMetadata
}

type ImportResult struct {
	BookID         string          `json:"bookID"`
	Version        DocumentVersion `json:"version"`
	Metadata       BookMetadata    `json:"metadata"`
	BookCreated    bool            `json:"bookCreated"`
	VersionCreated bool            `json:"versionCreated"`
}

type ImportStore interface {
	SaveBook(context.Context, ImportRecord) (ImportResult, error)
}

type BookImporter interface {
	ImportBook(context.Context, string, string) (ImportResult, error)
}

type ScanBookImporter interface {
	ImportBookFrom(context.Context, SourceReader, string, string) (ImportResult, error)
}
