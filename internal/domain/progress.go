package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidProgress = errors.New("阅读进度请求无效")
	ErrProgressSource  = errors.New("阅读进度的原书摘要不匹配")
	ErrMutationReused  = errors.New("操作标识已用于其他内容")
	ErrChangeCursor    = errors.New("变更游标超出当前历史")
)

// Revisions are bounded to integers that remain exact in JavaScript clients.
const MaxMutationRevision int64 = 1<<53 - 1

type ProgressMutation struct {
	ID           string          `json:"id"`
	DeviceID     string          `json:"deviceID"`
	BaseRevision int64           `json:"baseRevision"`
	Payload      ProgressPayload `json:"payload"`
}

type ProgressPayload struct {
	SourceSHA256 string          `json:"sourceSHA256"`
	Locator      json.RawMessage `json:"locator"`
	Completed    bool            `json:"completed"`
}

type ProgressResult struct {
	Progress  Progress `json:"progress"`
	ChangeSeq int64    `json:"changeSeq,string"`
	Replayed  bool     `json:"replayed"`
}

type ProgressConflict struct{ Current Progress }

func (*ProgressConflict) Error() string { return "阅读进度已被其他操作更新" }

type Change struct {
	Seq               int64           `json:"seq,string"`
	DocumentVersionID string          `json:"documentVersionID"`
	EntityType        string          `json:"entityType"`
	EntityID          string          `json:"entityID"`
	Revision          int64           `json:"revision"`
	Operation         string          `json:"operation"`
	Payload           json.RawMessage `json:"payload"`
	CreatedAt         time.Time       `json:"createdAt"`
}

type ChangePage struct {
	Items      []Change `json:"items"`
	NextCursor string   `json:"nextCursor"`
	HasMore    bool     `json:"hasMore"`
}

type ProgressService interface {
	Get(context.Context, string, string) (Progress, error)
	Put(context.Context, string, string, ProgressMutation) (ProgressResult, error)
	Changes(context.Context, string, int64, int) (ChangePage, error)
}

type ProgressStore interface {
	Version(context.Context, string) (VersionDetails, error)
	ReadProgress(context.Context, string, string) (Progress, error)
	CommitProgress(context.Context, string, string, ProgressMutation, string) (ProgressResult, error)
	ReadChanges(context.Context, string, int64, int) (ChangePage, error)
}
