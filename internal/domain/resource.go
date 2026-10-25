package domain

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	InkMediaType             = "application/vnd.benfolio.ink+json"
	InkCoordinateSpace       = "normalized-crop-unrotated-top-left"
	MaxInkBytes        int64 = 2 << 20
)

var (
	ErrInvalidResource     = errors.New("资源格式无效或超出限制")
	ErrResourceDigest      = errors.New("资源摘要不匹配")
	ErrResourceNotFound    = errors.New("资源未登记或当前用户不可访问")
	ErrResourceBinding     = errors.New("资源不属于目标原书版本或页面")
	ErrResourceUnavailable = errors.New("资源文件不可读取")
	ErrResourceConflict    = errors.New("资源文件或元数据不一致")
	ErrResourceBusy        = errors.New("资源操作达到并发上限")
)

type ResourceInfo struct {
	SHA256            string    `json:"sha256"`
	ByteSize          int64     `json:"byteSize"`
	MediaType         string    `json:"mediaType"`
	CreatedAt         time.Time `json:"createdAt"`
	DocumentVersionID string    `json:"documentVersionID"`
	SourceSHA256      string    `json:"sourceSHA256"`
	PageIndex         int64     `json:"pageIndex"`
	SchemaVersion     int       `json:"schemaVersion"`
	CoordinateSpace   string    `json:"coordinateSpace"`
}

type ResourceRecord struct {
	Info      ResourceInfo
	ObjectKey string
}

type ResourceResult struct {
	Resource ResourceInfo `json:"resource"`
	Reused   bool         `json:"reused"`
}

type ResourceFile struct {
	Resource ResourceInfo
	Content  OriginalContent
}

type ResourceStore interface {
	Version(context.Context, string) (VersionDetails, error)
	RegisterResource(context.Context, string, ResourceRecord) (ResourceResult, error)
	ReadResource(context.Context, string, string) (ResourceRecord, error)
}

type ResourceService interface {
	Put(context.Context, string, string, io.Reader) (ResourceResult, error)
	Open(context.Context, string, string) (ResourceFile, error)
}

type InkReference struct {
	ResourceSHA256 string `json:"resourceSHA256"`
}
