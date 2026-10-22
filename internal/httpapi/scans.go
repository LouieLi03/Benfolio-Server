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
)

func (a API) startScan(w http.ResponseWriter, r *http.Request) {
	if a.Scans == nil {
		writeError(w, http.StatusServiceUnavailable, "scan_unavailable", "扫描任务服务尚未就绪。")
		return
	}
	// Keep empty requests read-only; import must be explicitly selected.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_scan_request", "扫描请求体不能超过 1024 字节。")
		return
	}
	stage := domain.ScanDiscovery
	if len(bytes.TrimSpace(body)) != 0 {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "请使用 application/json。")
			return
		}
		var request struct {
			Stage string `json:"stage"`
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_scan_request", "扫描请求仅接受 stage 字段。")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid_scan_request", "请提供单个扫描选项对象。")
			return
		}
		stage = request.Stage
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	task, reused, err := a.Scans.StartScan(ctx, r.PathValue("libraryID"), stage)
	if err != nil {
		a.scanError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/tasks/"+task.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"task": task, "reused": reused})
}

func (a API) scanTask(w http.ResponseWriter, r *http.Request) {
	if a.Scans == nil {
		writeError(w, http.StatusServiceUnavailable, "scan_unavailable", "扫描任务服务尚未就绪。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	task, err := a.Scans.Task(ctx, r.PathValue("taskID"))
	if err != nil {
		a.scanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (a API) scanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidScanStage):
		writeError(w, http.StatusBadRequest, "invalid_scan_stage", "stage 必须为 discovery 或 epub_import。")
	case errors.Is(err, domain.ErrScanStageBusy):
		writeError(w, http.StatusConflict, "scan_stage_busy", "此书库已有其他阶段的扫描，请待任务结束后重试。")
	case errors.Is(err, domain.ErrLibraryNotFound):
		writeError(w, http.StatusNotFound, "library_not_found", "书库不存在。")
	case errors.Is(err, domain.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "task_not_found", "扫描任务不存在。")
	case errors.Is(err, domain.ErrSourceNotConfigured):
		writeError(w, http.StatusConflict, "source_not_configured", "此书库未绑定当前来源。")
	case errors.Is(err, domain.ErrScanUnavailable):
		writeError(w, http.StatusServiceUnavailable, "scan_unavailable", "服务正在停止，请稍后重试。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "scan_request_timeout", "请求未完成，请重试以查询或复用扫描任务。")
	default:
		a.Logger.Error("scan_request_failed")
		writeError(w, http.StatusInternalServerError, "scan_request_failed", "处理扫描请求失败，请稍后重试。")
	}
}
