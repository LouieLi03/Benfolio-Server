package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
)

func (a API) getProgress(w http.ResponseWriter, r *http.Request) {
	if !a.progressReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Progress.Get(ctx, a.Owner.ID, r.PathValue("versionID"))
	if err != nil {
		a.progressError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) putProgress(w http.ResponseWriter, r *http.Request) {
	if !a.progressReady(w) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "请使用 application/json。")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32*1024))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "progress_request_too_large", "进度请求不得超过 32 KiB。")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_progress", "无法读取进度请求。")
		}
		return
	}
	canonical, err := jsonvalue.Normalize(data)
	if err != nil {
		a.progressError(w, domain.ErrInvalidProgress)
		return
	}
	object, err := jsonvalue.Object(canonical, "id", "deviceID", "baseRevision", "payload")
	if err != nil {
		a.progressError(w, domain.ErrInvalidProgress)
		return
	}
	if _, err := jsonvalue.Object(object["payload"], "sourceSHA256", "locator", "completed"); err != nil {
		a.progressError(w, domain.ErrInvalidProgress)
		return
	}
	// Pointers distinguish required zero/false values from omitted fields.
	var request struct {
		ID           string `json:"id"`
		DeviceID     string `json:"deviceID"`
		BaseRevision *int64 `json:"baseRevision"`
		Payload      *struct {
			SourceSHA256 string          `json:"sourceSHA256"`
			Locator      json.RawMessage `json:"locator"`
			Completed    *bool           `json:"completed"`
		} `json:"payload"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.BaseRevision == nil ||
		request.Payload == nil || request.Payload.Completed == nil {
		a.progressError(w, domain.ErrInvalidProgress)
		return
	}
	command := domain.ProgressMutation{ID: request.ID, DeviceID: request.DeviceID, BaseRevision: *request.BaseRevision,
		Payload: domain.ProgressPayload{SourceSHA256: request.Payload.SourceSHA256,
			Locator: request.Payload.Locator, Completed: *request.Payload.Completed}}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Progress.Put(ctx, a.Owner.ID, r.PathValue("versionID"), command)
	if err != nil {
		a.progressError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) changes(w http.ResponseWriter, r *http.Request) {
	if a.Progress == nil {
		writeError(w, http.StatusServiceUnavailable, "change_feed_unavailable", "变更查询服务尚未就绪。")
		return
	}
	afterText := r.URL.Query().Get("after")
	if afterText == "" {
		afterText = "0"
	}
	after, err := strconv.ParseInt(afterText, 10, 64)
	limit, ok := integerParam(r.URL.Query().Get("limit"), 50, 1, 100)
	if err != nil || after < 0 || !ok {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "after 必须是非负整数游标，limit 范围为 1–100。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Progress.Changes(ctx, a.Owner.ID, after, limit)
	if err != nil {
		a.changeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) changeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrChangeCursor):
		writeError(w, http.StatusConflict, "change_cursor_invalid", "游标超出当前服务历史，请保留本机修改并重新读取；不要直接清空本机数据。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "change_feed_timeout", "变更读取超时，请保留原游标并重试。")
	default:
		a.Logger.Error("change_feed_failed")
		writeError(w, http.StatusInternalServerError, "change_feed_failed", "变更读取失败，请保留原游标与本机修改并重试。")
	}
}

func (a API) progressReady(w http.ResponseWriter) bool {
	if a.Progress != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "progress_unavailable", "阅读进度服务尚未就绪。")
	return false
}

func (a API) progressError(w http.ResponseWriter, err error) {
	var conflict *domain.ProgressConflict
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   map[string]string{"code": "progress_conflict", "message": "阅读位置已更新，请保留本机位置并读取最新进度后处理。"},
			"current": conflict.Current,
		})
	case errors.Is(err, domain.ErrInvalidProgress):
		writeError(w, http.StatusBadRequest, "invalid_progress", "请提供有效的操作ID、设备ID、基础修订号、原书摘要、格式位置和完成状态；不接受重复键或未知请求字段。")
	case errors.Is(err, domain.ErrMutationReused):
		writeError(w, http.StatusConflict, "mutation_id_reused", "此操作ID已用于不同请求，请保留原请求并为新操作使用新ID。")
	case errors.Is(err, domain.ErrProgressSource):
		writeError(w, http.StatusConflict, "source_version_mismatch", "原书摘要与此版本不一致，未保存位置，请核对原件。")
	case errors.Is(err, domain.ErrChangeCursor):
		writeError(w, http.StatusConflict, "change_cursor_invalid", "游标超出当前服务历史，请保留本机修改并重新读取进度；不要直接清空本机数据。")
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "内容版本不存在。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "progress_timeout", "进度请求超时；写入结果可能未知，请使用原操作ID和原请求重试。")
	default:
		a.Logger.Error("progress_operation_failed")
		writeError(w, http.StatusInternalServerError, "progress_failed", "进度操作失败；写入结果可能未知，请保留本机位置并使用原操作ID重试。")
	}
}
