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
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
	"github.com/LouieLi03/Benfolio-Server/internal/jsonvalue"
)

func (a API) exportsReady(w http.ResponseWriter) bool {
	if a.Exports != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "exports_unavailable", "导出服务尚未就绪。")
	return false
}

func (a API) exportContext(w http.ResponseWriter, r *http.Request) {
	if !a.exportsReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Exports.Context(ctx, a.Owner.ID, r.PathValue("versionID"))
	if err != nil {
		a.exportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) createExport(w http.ResponseWriter, r *http.Request) {
	if !a.exportsReady(w) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "请使用 application/json。")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeError(w, http.StatusRequestEntityTooLarge, "export_request_too_large", "导出请求不得超过8192字节。")
		} else {
			a.exportError(w, domain.ErrInvalidExport)
		}
		return
	}
	canonical, err := jsonvalue.Normalize(data)
	if err != nil {
		a.exportError(w, domain.ErrInvalidExport)
		return
	}
	if _, err := jsonvalue.Object(canonical, "id", "deviceID", "format", "sourceSHA256", "annotationSeq", "workingRevision"); err != nil {
		a.exportError(w, domain.ErrInvalidExport)
		return
	}
	var request struct {
		ID              string  `json:"id"`
		DeviceID        string  `json:"deviceID"`
		Format          string  `json:"format"`
		SourceSHA256    string  `json:"sourceSHA256"`
		AnnotationSeq   *string `json:"annotationSeq"`
		WorkingRevision *int64  `json:"workingRevision"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.AnnotationSeq == nil {
		a.exportError(w, domain.ErrInvalidExport)
		return
	}
	seq, err := strconv.ParseInt(*request.AnnotationSeq, 10, 64)
	if err != nil || seq < 0 {
		a.exportError(w, domain.ErrInvalidExport)
		return
	}
	var revision int64
	if request.WorkingRevision != nil {
		revision = *request.WorkingRevision
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(40 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "exports_unavailable", "当前连接无法开始导出。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := a.Exports.Create(ctx, a.Owner.ID, r.PathValue("versionID"), domain.ExportMutation{ID: request.ID, DeviceID: request.DeviceID, Format: request.Format, SourceSHA256: request.SourceSHA256, AnnotationSeq: seq, WorkingRevision: revision})
	if err != nil {
		a.exportError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	w.Header().Set("Location", "../../exports/"+result.Export.ID)
	writeJSON(w, status, result)
}

func (a API) getExport(w http.ResponseWriter, r *http.Request) {
	if !a.exportsReady(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.Exports.Get(ctx, a.Owner.ID, r.PathValue("exportID"))
	if err != nil {
		a.exportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) exportFile(w http.ResponseWriter, r *http.Request) {
	if !a.exportsReady(w) {
		return
	}
	ranges := r.Header.Values("Range")
	if len(ranges) > 1 || len(ranges) == 1 && (len(ranges[0]) > 256 || strings.Contains(ranges[0], ",")) {
		writeError(w, http.StatusBadRequest, "unsupported_range", "仅支持单段且不超过256字节的Range。")
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "exports_unavailable", "当前连接无法开始下载。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	file, err := a.Exports.Open(ctx, a.Owner.ID, r.PathValue("exportID"))
	if err != nil {
		a.exportError(w, err)
		return
	}
	defer file.Content.Close()
	mediaType, suffix := "text/markdown; charset=utf-8", ".notes.md"
	if file.Export.Format == "pdf" {
		mediaType, suffix = "application/pdf", ".annotated.pdf"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Export.FileSHA256 + suffix}))
	w.Header().Set("ETag", `"sha256-`+file.Export.FileSHA256+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Benfolio-Export-Seq", strconv.FormatInt(file.Export.Seq, 10))
	w.Header().Set("X-Benfolio-Annotation-Seq", strconv.FormatInt(file.Export.AnnotationSeq, 10))
	reader := &transferReader{source: file.Content}
	http.ServeContent(w, r.WithContext(ctx), file.Export.FileSHA256+suffix, file.Export.CreatedAt, reader)
	if reader.err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (a API) exportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidExport):
		writeError(w, http.StatusBadRequest, "invalid_export", "请提供有效操作ID、设备ID、原件摘要、字符串批注序号和格式；PDF须显式选择工作修订，Markdown不能携带工作修订。")
	case errors.Is(err, domain.ErrExportNotFound):
		writeError(w, http.StatusNotFound, "export_not_found", "导出记录不存在或不可访问。")
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "原书版本不存在。")
	case errors.Is(err, domain.ErrWorkingPDFNotFound):
		writeError(w, http.StatusNotFound, "working_pdf_not_found", "所选工作PDF修订尚未生成。")
	case errors.Is(err, domain.ErrWorkingPDFUnsupported):
		writeError(w, http.StatusUnprocessableEntity, "export_unsupported", "此书籍格式没有可导出的工作PDF。")
	case errors.Is(err, domain.ErrExportConflict):
		writeError(w, http.StatusConflict, "export_snapshot_conflict", "原件、批注快照或工作修订与请求不一致，未登记新导出；请读取状态后为新操作使用新ID。")
	case errors.Is(err, domain.ErrMutationReused):
		writeError(w, http.StatusConflict, "mutation_id_reused", "操作ID已用于另一请求，请为新操作使用新ID。")
	case errors.Is(err, domain.ErrExportLimit):
		writeError(w, http.StatusUnprocessableEntity, "export_limit", "笔记数量或文件超过当前导出范围，未截断笔记。")
	case errors.Is(err, domain.ErrExportBusy), errors.Is(err, domain.ErrDownloadBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "export_busy", "导出或下载达到并发限制，请稍后重试。")
	case errors.Is(err, domain.ErrResourceConflict):
		writeError(w, http.StatusConflict, "export_archive_conflict", "导出记录或文件未通过完整性校验，未覆盖原文件。")
	case errors.Is(err, domain.ErrResourceUnavailable):
		writeError(w, http.StatusServiceUnavailable, "export_file_unavailable", "导出文件或状态目录不可读取，请检查存储及权限。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "export_timeout", "请求超时，创建结果可能未知，请保留原操作ID及原请求重试。")
	default:
		a.Logger.Error("export_operation_failed")
		writeError(w, http.StatusInternalServerError, "export_failed", "导出操作失败，创建结果可能未知，请保留原操作ID及原请求重试。")
	}
}
