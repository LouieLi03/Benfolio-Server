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

func workingRevision(r *http.Request) (int64, error) {
	value := r.URL.Query().Get("revision")
	if value == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 || n > domain.MaxMutationRevision {
		return 0, domain.ErrInvalidWorkingPDF
	}
	return n, nil
}

func (a API) workingReady(w http.ResponseWriter) bool {
	if a.WorkingPDF != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "working_pdf_unavailable", "工作PDF服务尚未就绪。")
	return false
}

func (a API) workingPDFStatus(w http.ResponseWriter, r *http.Request) {
	if !a.workingReady(w) {
		return
	}
	revision, err := workingRevision(r)
	if err != nil {
		a.workingPDFError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	result, err := a.WorkingPDF.Status(ctx, a.Owner.ID, r.PathValue("versionID"), revision)
	if err != nil {
		a.workingPDFError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) buildWorkingPDF(w http.ResponseWriter, r *http.Request) {
	if !a.workingReady(w) {
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
			writeError(w, http.StatusRequestEntityTooLarge, "working_pdf_request_too_large", "生成请求不得超过8192字节。")
		} else {
			a.workingPDFError(w, domain.ErrInvalidWorkingPDF)
		}
		return
	}
	canonical, err := jsonvalue.Normalize(data)
	if err != nil {
		a.workingPDFError(w, domain.ErrInvalidWorkingPDF)
		return
	}
	if _, err := jsonvalue.Object(canonical, "id", "deviceID", "baseRevision", "sourceSHA256", "annotationSeq"); err != nil {
		a.workingPDFError(w, domain.ErrInvalidWorkingPDF)
		return
	}
	var request struct {
		ID            string  `json:"id"`
		DeviceID      string  `json:"deviceID"`
		BaseRevision  *int64  `json:"baseRevision"`
		SourceSHA256  string  `json:"sourceSHA256"`
		AnnotationSeq *string `json:"annotationSeq"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.BaseRevision == nil || request.AnnotationSeq == nil {
		a.workingPDFError(w, domain.ErrInvalidWorkingPDF)
		return
	}
	seq, err := strconv.ParseInt(*request.AnnotationSeq, 10, 64)
	if err != nil || seq < 0 {
		a.workingPDFError(w, domain.ErrInvalidWorkingPDF)
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(100 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "working_pdf_unavailable", "当前连接无法开始生成，请稍后重试。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := a.WorkingPDF.Build(ctx, a.Owner.ID, r.PathValue("versionID"), domain.WorkingPDFMutation{ID: request.ID, DeviceID: request.DeviceID, BaseRevision: *request.BaseRevision, SourceSHA256: request.SourceSHA256, AnnotationSeq: seq})
	if err != nil {
		a.workingPDFError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a API) workingPDFFile(w http.ResponseWriter, r *http.Request) {
	if !a.workingReady(w) {
		return
	}
	revision, err := workingRevision(r)
	if err != nil {
		a.workingPDFError(w, err)
		return
	}
	ranges := r.Header.Values("Range")
	if len(ranges) > 1 || len(ranges) == 1 && (len(ranges[0]) > 256 || strings.Contains(ranges[0], ",")) {
		writeError(w, http.StatusBadRequest, "unsupported_range", "仅支持单段且不超过256字节的Range。")
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "working_pdf_unavailable", "当前连接无法开始下载。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	file, err := a.WorkingPDF.Open(ctx, a.Owner.ID, r.PathValue("versionID"), revision)
	if err != nil {
		a.workingPDFError(w, err)
		return
	}
	defer file.Content.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Status.FileSHA256 + ".working.pdf"}))
	w.Header().Set("ETag", `"sha256-`+file.Status.FileSHA256+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Benfolio-Working-Revision", strconv.FormatInt(file.Status.Revision, 10))
	w.Header().Set("X-Benfolio-Materialized-Seq", strconv.FormatInt(file.Status.MaterializedChangeSeq, 10))
	w.Header().Set("X-Benfolio-Working-Stale", strconv.FormatBool(file.Status.Stale))
	when := time.Time{}
	if file.Status.UpdatedAt != nil {
		when = *file.Status.UpdatedAt
	}
	reader := &transferReader{source: file.Content}
	http.ServeContent(w, r.WithContext(ctx), file.Status.FileSHA256+".pdf", when, reader)
	if reader.err != nil {
		panic(http.ErrAbortHandler)
	}
}

func (a API) workingPDFError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidWorkingPDF):
		writeError(w, http.StatusBadRequest, "invalid_working_pdf", "请提供有效操作ID、设备ID、基础修订号、原件摘要及字符串annotationSeq。")
	case errors.Is(err, domain.ErrWorkingPDFConflict):
		writeError(w, http.StatusConflict, "working_pdf_conflict", "工作副本或批注已更新，本次未替换当前副本，请保留操作并重新读取状态。")
	case errors.Is(err, domain.ErrWorkingPDFNotFound):
		writeError(w, http.StatusNotFound, "working_pdf_not_found", "所请求的工作PDF尚未生成。")
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "原书版本不存在。")
	case errors.Is(err, domain.ErrWorkingPDFUnsupported):
		writeError(w, http.StatusUnprocessableEntity, "working_pdf_unsupported", "PDF签名、保护、批注或页面几何超出当前生成范围，原件和旧副本保留。")
	case errors.Is(err, domain.ErrWorkingPDFLimit):
		writeError(w, http.StatusUnprocessableEntity, "working_pdf_limit", "原件、批注或生成内容超出当前大小和数量限制，未替换旧副本。")
	case errors.Is(err, domain.ErrWorkingPDFBusy), errors.Is(err, domain.ErrDownloadBusy), errors.Is(err, domain.ErrResourceBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "working_pdf_busy", "生成或下载达到并发限制，请稍后重试。")
	case errors.Is(err, domain.ErrMutationReused):
		writeError(w, http.StatusConflict, "mutation_id_reused", "操作ID已用于另一请求，请保留原请求并为新操作使用新ID。")
	case errors.Is(err, domain.ErrResourceNotFound):
		writeError(w, http.StatusNotFound, "resource_not_found", "批注引用的笔画资源未登记或不可访问。")
	case errors.Is(err, domain.ErrResourceBinding):
		writeError(w, http.StatusConflict, "resource_binding_mismatch", "笔画资源不属于所请求的原件版本或页面。")
	case errors.Is(err, domain.ErrArchiveConflict), errors.Is(err, domain.ErrResourceConflict):
		writeError(w, http.StatusConflict, "working_pdf_archive_conflict", "原件、资源或工作PDF未通过完整性校验，未覆盖文件。")
	case errors.Is(err, domain.ErrOriginalUnavailable), errors.Is(err, domain.ErrResourceUnavailable):
		writeError(w, http.StatusServiceUnavailable, "working_pdf_file_unavailable", "所需文件或状态目录不可读取，请检查存储及权限。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "working_pdf_timeout", "请求超时，生成结果可能未知；请使用原操作ID及原请求重试。")
	default:
		a.Logger.Error("working_pdf_operation_failed")
		writeError(w, http.StatusInternalServerError, "working_pdf_failed", "工作PDF操作失败，生成结果可能未知；请保留原请求并以原操作ID重试。")
	}
}
