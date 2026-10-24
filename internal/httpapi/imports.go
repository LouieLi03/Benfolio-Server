package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (a API) importBook(w http.ResponseWriter, r *http.Request) {
	if a.Imports == nil {
		writeError(w, http.StatusServiceUnavailable, "import_unavailable", "导入服务尚未就绪。")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "请使用 application/json。")
		return
	}
	var request struct {
		RelativePath string `json:"relativePath"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_import_request", "请提供 relativePath；请求体上限为 8192 字节。")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.RelativePath == "" {
		writeError(w, http.StatusBadRequest, "invalid_import_request", "请提供单个 JSON 对象及非空 relativePath。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := a.Imports.ImportBook(ctx, r.PathValue("libraryID"), request.RelativePath)
	if err != nil {
		a.importError(w, err)
		return
	}
	status := http.StatusOK
	if result.VersionCreated {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

func (a API) importError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidSourcePath):
		writeError(w, http.StatusBadRequest, "invalid_source_path", "请使用来源内的相对路径，不接受绝对路径、上级路径或符号链接。")
	case errors.Is(err, domain.ErrUnsupportedBook):
		writeError(w, http.StatusUnprocessableEntity, "unsupported_book", "当前仅支持 EPUB 和 PDF 导入。")
	case errors.Is(err, domain.ErrImportSize):
		writeError(w, http.StatusUnprocessableEntity, "import_size_limit", "书籍必须非空且不超过 128 MiB。")
	case errors.Is(err, domain.ErrInvalidEPUB):
		writeError(w, http.StatusUnprocessableEntity, "invalid_epub", "EPUB 结构无效或超出当前解析范围。")
	case errors.Is(err, domain.ErrInvalidPDF):
		writeError(w, http.StatusUnprocessableEntity, "invalid_pdf", "PDF 无效、不支持或超出当前解析限制。")
	case errors.Is(err, domain.ErrEncryptedPDF):
		writeError(w, http.StatusUnprocessableEntity, "encrypted_pdf", "当前暂不支持加密 PDF；未保存密码或改写保护设置。")
	case errors.Is(err, domain.ErrPDFParserFailure):
		a.Logger.Error("pdf_parser_failed")
		writeError(w, http.StatusInternalServerError, "pdf_parser_failed", "PDF 解析未能完成，请检查服务状态。")
	case errors.Is(err, domain.ErrSourceChanged):
		writeError(w, http.StatusConflict, "source_changed", "来源文件发生变化，请待文件写入结束后重新导入。")
	case errors.Is(err, domain.ErrArchiveConflict):
		writeError(w, http.StatusConflict, "archive_conflict", "已有原书归档未通过校验，未覆盖归档，请检查状态存储。")
	case errors.Is(err, domain.ErrDiscoveryBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "source_busy", "来源正在扫描、预览或导入，请稍后重试。")
	case errors.Is(err, domain.ErrLibraryNotFound):
		writeError(w, http.StatusNotFound, "library_not_found", "书库不存在。")
	case errors.Is(err, domain.ErrSourceNotConfigured):
		writeError(w, http.StatusConflict, "source_not_configured", "此书库未绑定当前来源。")
	case errors.Is(err, domain.ErrSourceUnavailable):
		writeError(w, http.StatusServiceUnavailable, "source_unavailable", "来源文件不可读取，请检查文件及权限。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "import_timeout", "导入请求超时，可重试同一路径以确认导入结果。")
	default:
		a.Logger.Error("book_import_failed")
		writeError(w, http.StatusInternalServerError, "import_failed", "导入失败，请检查服务日志与状态存储后重试。")
	}
}
