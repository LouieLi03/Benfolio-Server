package httpapi

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (a API) putResource(w http.ResponseWriter, r *http.Request) {
	if !a.resourcesReady(w) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != domain.InkMediaType {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_resource_type", "当前仅接收 application/vnd.benfolio.ink+json。")
		return
	}
	if r.ContentLength > domain.MaxInkBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "resource_too_large", "手写资源不得超过 2 MiB。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := a.Resources.Put(ctx, a.Owner.ID, r.PathValue("sha256"), http.MaxBytesReader(w, r.Body, domain.MaxInkBytes))
	if err != nil {
		a.resourceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Reused {
		status = http.StatusOK
	}
	w.Header().Set("ETag", `"sha256-`+result.Resource.SHA256+`"`)
	writeJSON(w, status, result)
}

func (a API) getResource(w http.ResponseWriter, r *http.Request) {
	if !a.resourcesReady(w) {
		return
	}
	ranges := r.Header.Values("Range")
	if len(ranges) > 1 || len(ranges) == 1 && (len(ranges[0]) > 256 || strings.Contains(ranges[0], ",")) {
		writeError(w, http.StatusBadRequest, "unsupported_range", "仅支持长度不超过256字节的单段Range。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	file, err := a.Resources.Open(ctx, a.Owner.ID, r.PathValue("sha256"))
	if err != nil {
		a.resourceError(w, err)
		return
	}
	defer file.Content.Close()
	w.Header().Set("Content-Type", file.Resource.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Resource.SHA256 + ".ink.json"}))
	w.Header().Set("ETag", `"sha256-`+file.Resource.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	reader := &transferReader{source: file.Content}
	http.ServeContent(w, r.WithContext(ctx), file.Resource.SHA256+".ink.json", file.Resource.CreatedAt, reader)
	if reader.err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (a API) resourcesReady(w http.ResponseWriter) bool {
	if a.Resources != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "resources_unavailable", "资源服务尚未就绪。")
	return false
}

func (a API) resourceError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "resource_too_large", "手写资源不得超过 2 MiB。")
	case errors.Is(err, domain.ErrInvalidResource):
		writeError(w, http.StatusUnprocessableEntity, "invalid_ink_resource", "手写资源格式、笔画数据或数量不符合当前限制。")
	case errors.Is(err, domain.ErrResourceDigest):
		writeError(w, http.StatusUnprocessableEntity, "resource_digest_mismatch", "实际上传字节与URL中的SHA-256不符，未登记资源。")
	case errors.Is(err, domain.ErrResourceNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "资源未登记或当前用户不可访问。")
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "原书版本不存在。")
	case errors.Is(err, domain.ErrResourceBinding):
		writeError(w, http.StatusConflict, "resource_binding_mismatch", "资源的原书版本、摘要或页码不匹配。")
	case errors.Is(err, domain.ErrResourceConflict):
		writeError(w, http.StatusConflict, "resource_conflict", "资源文件与登记内容不一致，未覆盖原资源，请检查状态存储。")
	case errors.Is(err, domain.ErrResourceUnavailable):
		writeError(w, http.StatusServiceUnavailable, "resource_unavailable", "资源状态目录或文件不可读取，请检查存储及权限。")
	case errors.Is(err, domain.ErrResourceBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "resource_busy", "资源操作达到并发上限，请稍后重试。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "resource_timeout", "资源请求超时；上传结果可能未知，请保留原字节并按同一摘要重试。")
	default:
		a.Logger.Error("resource_operation_failed")
		writeError(w, http.StatusInternalServerError, "resource_failed", "资源操作失败；上传结果可能未知，请保留原字节并按同一摘要重试。")
	}
}
