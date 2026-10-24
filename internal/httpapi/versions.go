package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

func (a API) book(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := a.Catalog.Book(ctx, r.PathValue("bookID"))
	if err != nil {
		a.catalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a API) versions(w http.ResponseWriter, r *http.Request) {
	limit, ok := integerParam(r.URL.Query().Get("limit"), 30, 1, 100)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "limit 必须在 1–100 之间。")
		return
	}
	offset, ok := integerParam(r.URL.Query().Get("offset"), 0, 0, 1_000_000)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "offset 必须在 0–1000000 之间。")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	page, err := a.Catalog.Versions(ctx, r.PathValue("bookID"), domain.VersionQuery{Limit: limit, Offset: offset})
	if err != nil {
		a.catalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a API) version(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := a.Catalog.Version(ctx, r.PathValue("versionID"))
	if err != nil {
		a.catalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a API) catalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrVersionNotFound):
		writeError(w, http.StatusNotFound, "version_not_found", "内容版本不存在。")
	case errors.Is(err, domain.ErrBookNotFound):
		writeError(w, http.StatusNotFound, "book_not_found", "书籍不存在。")
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "catalog_timeout", "读取书目超时，请稍后重试。")
	default:
		a.Logger.Error("catalog_read_failed", "operation", "book_versions")
		writeError(w, http.StatusInternalServerError, "storage_error", "读取书籍或版本失败。")
	}
}
