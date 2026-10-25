package domain

import (
	"context"
	"encoding/json"
	"errors"
)

var (
	ErrInvalidAnnotation  = errors.New("批注请求无效")
	ErrAnnotationNotFound = errors.New("批注不存在")
	ErrAnnotationIdentity = errors.New("批注标识已绑定其他版本或类型")
	ErrAnnotationDeleted  = errors.New("批注已删除")
	ErrAnnotationCursor   = errors.New("批注游标不属于当前版本")
)

type AnnotationMutation struct {
	ID           string            `json:"id"`
	DeviceID     string            `json:"deviceID"`
	BaseRevision int64             `json:"baseRevision"`
	Payload      AnnotationPayload `json:"payload"`
}

type AnnotationPayload struct {
	SourceSHA256 string          `json:"sourceSHA256"`
	Kind         string          `json:"kind,omitempty"`
	EPUB         json.RawMessage `json:"epub,omitempty"`
	PDF          json.RawMessage `json:"pdf,omitempty"`
	Body         json.RawMessage `json:"body,omitempty"`
}

type AnnotationResult struct {
	Annotation Annotation `json:"annotation"`
	ChangeSeq  int64      `json:"changeSeq,string"`
	Replayed   bool       `json:"replayed"`
}

type AnnotationPage struct {
	Items      []Annotation `json:"items"`
	NextCursor string       `json:"nextCursor"`
	HasMore    bool         `json:"hasMore"`
}

type AnnotationConflict struct{ Current *Annotation }

func (*AnnotationConflict) Error() string { return "批注已被其他操作更新" }

type AnnotationService interface {
	List(context.Context, string, string, string, int) (AnnotationPage, error)
	Get(context.Context, string, string, string) (Annotation, error)
	Put(context.Context, string, string, string, AnnotationMutation) (AnnotationResult, error)
	Delete(context.Context, string, string, string, AnnotationMutation) (AnnotationResult, error)
}

type AnnotationStore interface {
	Version(context.Context, string) (VersionDetails, error)
	ReadAnnotations(context.Context, string, string, string, int) (AnnotationPage, error)
	ReadAnnotation(context.Context, string, string, string) (Annotation, error)
	CommitAnnotation(context.Context, string, string, string, AnnotationMutation, bool, string) (AnnotationResult, error)
}
