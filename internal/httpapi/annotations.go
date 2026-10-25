package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
)

func (a API) listAnnotations(w http.ResponseWriter, r *http.Request) {
	if !a.annotationsReady(w) {
		return
	}
	limit, ok := integerParam(r.URL.Query().Get("limit"), 50, 1, 100)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "limit 范围为 1–100。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Annotations.List(ctx, a.Owner.ID, r.PathValue("versionID"), r.URL.Query().Get("after"), limit)
	if err != nil {
		a.annotationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) getAnnotation(w http.ResponseWriter, r *http.Request) {
	if !a.annotationsReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Annotations.Get(ctx, a.Owner.ID, r.PathValue("versionID"), r.PathValue("annotationID"))
	if err != nil {
		a.annotationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) putAnnotation(w http.ResponseWriter, r *http.Request)    { a.mutateAnnotation(w, r, false) }
func (a API) deleteAnnotation(w http.ResponseWriter, r *http.Request) { a.mutateAnnotation(w, r, true) }

func (a API) mutateAnnotation(w http.ResponseWriter, r *http.Request, remove bool) {
	if !a.annotationsReady(w) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "请使用 application/json。")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "annotation_request_too_large", "批注请求不得超过 64 KiB。")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_annotation", "无法读取批注请求。")
		}
		return
	}
	canonical, err := jsonvalue.Normalize(data)
	if err != nil {
		a.annotationError(w, domain.ErrInvalidAnnotation)
		return
	}
	object, err := jsonvalue.Object(canonical, "id", "deviceID", "baseRevision", "payload")
	if err != nil {
		a.annotationError(w, domain.ErrInvalidAnnotation)
		return
	}
	fields := []string{"sourceSHA256", "kind", "epub", "pdf", "body"}
	if remove {
		fields = []string{"sourceSHA256"}
	}
	if _, err := jsonvalue.Object(object["payload"], fields...); err != nil {
		a.annotationError(w, domain.ErrInvalidAnnotation)
		return
	}
	var request struct {
		ID           string                    `json:"id"`
		DeviceID     string                    `json:"deviceID"`
		BaseRevision *int64                    `json:"baseRevision"`
		Payload      *domain.AnnotationPayload `json:"payload"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.BaseRevision == nil || request.Payload == nil {
		a.annotationError(w, domain.ErrInvalidAnnotation)
		return
	}
	command := domain.AnnotationMutation{ID: request.ID, DeviceID: request.DeviceID,
		BaseRevision: *request.BaseRevision, Payload: *request.Payload}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var result domain.AnnotationResult
	if remove {
		result, err = a.Annotations.Delete(ctx, a.Owner.ID, r.PathValue("versionID"), r.PathValue("annotationID"), command)
	} else {
		result, err = a.Annotations.Put(ctx, a.Owner.ID, r.PathValue("versionID"), r.PathValue("annotationID"), command)
	}
	if err != nil {
		a.annotationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) annotationsReady(w http.ResponseWriter) bool {
	if a.Annotations != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "annotations_unavailable", "批注服务尚未就绪。")
	return false
}

func (a API) annotationError(w http.ResponseWriter, err error) {
	var conflict *domain.AnnotationConflict
	switch {
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   map[string]string{"code": "annotation_conflict", "message": "批注已更新，请保留本机修改并读取当前记录后处理。"},
			"current": conflict.Current,
		})
	case errors.Is(err, domain.ErrInvalidAnnotation):
		writeError(w, http.StatusBadRequest, "invalid_annotation", "请核对操作ID、基础修订号、原书摘要、批注类型、内容锚点和正文；不接受未知字段或重复键。")
	case errors.Is(err, domain.ErrAnnotationNotFound):
		writeError(w, http.StatusNotFound, "annotation_not_found", "批注不存在。")
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "内容版本不存在。")
	case errors.Is(err, domain.ErrAnnotationIdentity):
		writeError(w, http.StatusConflict, "annotation_identity_mismatch", "此批注ID已绑定其他版本或类型，请为另一条批注使用新ID。")
	case errors.Is(err, domain.ErrAnnotationDeleted):
		writeError(w, http.StatusGone, "annotation_deleted", "批注已删除，原记录保留；不能用旧ID重新创建。")
	case errors.Is(err, domain.ErrAnnotationCursor):
		writeError(w, http.StatusConflict, "annotation_cursor_invalid", "游标不属于当前版本，请保留本机修改并重新读取列表。")
	case errors.Is(err, domain.ErrMutationReused):
		writeError(w, http.StatusConflict, "mutation_id_reused", "操作ID已用于不同请求，请为新操作使用新ID。")
	case errors.Is(err, domain.ErrProgressSource):
		writeError(w, http.StatusConflict, "source_version_mismatch", "原书摘要与目标版本不一致，未保存批注。")
	case errors.Is(err, domain.ErrResourceNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "手写资源未登记或不可访问，请先上传资源再提交笔记。")
	case errors.Is(err, domain.ErrResourceBinding):
		writeError(w, http.StatusConflict, "resource_binding_mismatch", "手写资源不属于此原书版本或页面，未保存笔记。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "annotation_timeout", "批注请求超时，结果可能未知，请保留本机内容并以原操作ID和请求重试。")
	default:
		a.Logger.Error("annotation_operation_failed")
		writeError(w, http.StatusInternalServerError, "annotation_failed", "批注操作失败，写入结果可能未知，请保留本机内容并以原操作ID重试。")
	}
}
