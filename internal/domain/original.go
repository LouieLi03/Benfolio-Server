package domain

import (
	"context"
	"errors"
	"io"
)

var (
	ErrBookNotFound        = errors.New("书籍不存在")
	ErrVersionNotFound     = errors.New("内容版本不存在")
	ErrOriginalUnavailable = errors.New("原书归档不可读取")
	ErrDownloadBusy        = errors.New("原书下载已达到并发限制")
)

type VersionQuery struct{ Limit, Offset int }

type VersionPage struct {
	Items   []DocumentVersion `json:"items"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
	HasMore bool              `json:"hasMore"`
}

type VersionDetails struct {
	DocumentVersion
	Format   Format       `json:"format"`
	Metadata BookMetadata `json:"metadata"`
}

// OriginalRecord is internal storage metadata. Never serialize its object key
// or accept it from a client; the public API identifies files by version ID.
type OriginalRecord struct {
	Version   DocumentVersion
	Format    Format
	ObjectKey string `json:"-"`
}

type OriginalCatalog interface {
	Original(context.Context, string) (OriginalRecord, error)
}

type OriginalContent interface {
	io.Reader
	io.Seeker
	io.Closer
}

type OriginalFile struct {
	Content OriginalContent
	Version DocumentVersion
	Format  Format
}

type OriginalReader interface {
	OpenOriginal(context.Context, string) (OriginalFile, error)
}
