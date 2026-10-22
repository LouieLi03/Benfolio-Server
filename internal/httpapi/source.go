package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (a API) sourceFiles(w http.ResponseWriter, r *http.Request) {
	limit, ok := integerParam(r.URL.Query().Get("limit"), 100, 1, 200)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit 必须在 1–200 之间。")
		return
	}
	if a.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "文件发现服务尚未就绪。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Sources.Files(ctx, r.PathValue("libraryID"), limit)
	if contextErr := ctx.Err(); contextErr != nil {
		err = contextErr
	}
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, result)
	case errors.Is(err, domain.ErrLibraryNotFound):
		writeError(w, http.StatusNotFound, "library_not_found", "书库不存在。")
	case errors.Is(err, domain.ErrSourceNotConfigured):
		writeError(w, http.StatusConflict, "source_not_configured", "此书库未绑定本次启动配置的来源。")
	case errors.Is(err, domain.ErrDiscoveryBusy):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "discovery_busy", "来源正在扫描、预览或导入，请稍后重试。")
	case errors.Is(err, domain.ErrSourceUnavailable):
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "无法读取来源目录，请检查挂载和权限。")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "discovery_timeout", "文件发现超时，请缩小来源目录范围。")
	case errors.Is(err, context.Canceled):
		return
	default:
		a.Logger.Error("source_discovery_failed")
		writeError(w, http.StatusInternalServerError, "discovery_failed", "文件发现失败，请稍后重试。")
	}
}
