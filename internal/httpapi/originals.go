package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (a API) originalFile(w http.ResponseWriter, r *http.Request) {
	if a.Originals == nil {
		writeError(w, http.StatusServiceUnavailable, "download_unavailable", "原书下载服务尚未就绪。")
		return
	}
	ranges := r.Header.Values("Range")
	if len(ranges) > 1 || (len(ranges) == 1 && (len(ranges[0]) > 256 || strings.Contains(ranges[0], ","))) {
		writeError(w, http.StatusBadRequest, "unsupported_range", "仅支持长度不超过 256 字节的单段 Range 请求。")
		return
	}
	// Extend the built-in server's shorter JSON response deadline for file
	// transfer; a wrapping server may impose its own tighter deadline.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(w, http.StatusServiceUnavailable, "download_unavailable", "当前连接无法开始文件传输，请稍后重试。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	original, err := a.Originals.OpenOriginal(ctx, r.PathValue("versionID"))
	if err != nil {
		a.originalError(w, err)
		return
	}
	defer original.Content.Close()
	contentType := "application/epub+zip"
	if original.Format == domain.PDF {
		contentType = "application/pdf"
	}
	filename := original.Version.SHA256 + "." + string(original.Format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("ETag", `"sha256-`+original.Version.SHA256+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	reader := &transferReader{source: original.Content}
	http.ServeContent(w, r.WithContext(ctx), filename, original.Version.CreatedAt, reader)
	if reader.err != nil {
		// Headers or bytes may already be sent. Abort the response instead of
		// appending JSON to a truncated book or returning a complete stream.
		if ctx.Err() == nil {
			a.Logger.Error("original_transfer_failed")
		}
		panic(http.ErrAbortHandler)
	}
}

type transferReader struct {
	source io.ReadSeeker
	err    error
}

func (r *transferReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if err != nil {
		r.err = err
	}
	return n, err
}
func (r *transferReader) Seek(offset int64, whence int) (int64, error) {
	n, err := r.source.Seek(offset, whence)
	if err != nil {
		r.err = err
	}
	return n, err
}

func (a API) originalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "内容版本不存在。")
	case errors.Is(err, domain.ErrDownloadBusy):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "download_busy", "原书下载达到并发限制，请稍后重试。")
	case errors.Is(err, domain.ErrArchiveConflict):
		writeError(w, http.StatusConflict, "archive_conflict", "归档与登记版本不一致，未发送书籍内容，请检查状态存储。")
	case errors.Is(err, domain.ErrOriginalUnavailable):
		writeError(w, http.StatusServiceUnavailable, "original_unavailable", "原书归档不可读取，请检查状态存储及权限。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "download_timeout", "读取或校验原书超时，请稍后重试。")
	default:
		a.Logger.Error("original_open_failed")
		writeError(w, http.StatusInternalServerError, "download_failed", "读取原书失败，请检查服务状态。")
	}
}
