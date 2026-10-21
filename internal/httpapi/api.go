package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LouieLi03/Benfolio-Server/internal/domain"
)

type API struct {
	Catalog          domain.Catalog
	Owner            domain.User
	InstanceID       string
	SchemaVersion    int
	BuildVersion     string
	SourceConfigured bool
	APIKey           string
	Ping             func(context.Context) error
	Logger           *slog.Logger
}

func (a API) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/server", a.serverInfo)
	api.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.Owner)
	})
	api.HandleFunc("GET /api/v1/capabilities", a.capabilities)
	api.HandleFunc("GET /api/v1/libraries", a.libraries)
	api.HandleFunc("GET /api/v1/books", a.books)
	// Reserved routes deliberately fail instead of returning fake upload success.
	for _, pattern := range []string{
		"POST /api/v1/libraries", "POST /api/v1/libraries/{libraryID}/scan",
		"GET /api/v1/books/{bookID}", "GET /api/v1/books/{bookID}/versions",
		"GET /api/v1/versions/{versionID}/file",
		"GET /api/v1/versions/{versionID}/progress", "PUT /api/v1/versions/{versionID}/progress",
		"GET /api/v1/versions/{versionID}/annotations", "PUT /api/v1/versions/{versionID}/annotations/{annotationID}",
		"DELETE /api/v1/versions/{versionID}/annotations/{annotationID}",
		"GET /api/v1/versions/{versionID}/working-pdf",
		"POST /api/v1/versions/{versionID}/exports", "GET /api/v1/changes",
		"PUT /api/v1/resources/{sha256}", "GET /api/v1/resources/{sha256}",
	} {
		api.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusNotImplemented, "not_implemented", "此能力尚未实现，未保存或同步任何数据。")
		})
	}
	api.HandleFunc("/api/", notFound)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := a.Ping(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "数据库尚未就绪。")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.Handle("/api/", a.authenticate(api))
	mux.HandleFunc("/", notFound)
	return a.recoverRequests(mux)
}

func (a API) authenticate(next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(a.APIKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("Authorization")
		provided := sha256.Sum256([]byte(strings.TrimPrefix(value, "Bearer ")))
		if !strings.HasPrefix(value, "Bearer ") || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Benfolio"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "请提供有效的 API Key。")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a API) recoverRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				// Never log a panic value, URL query, headers or body containing user data.
				a.Logger.Error("request_panicked", "method", r.Method)
				writeError(w, http.StatusInternalServerError, "internal_error", "服务处理失败，请稍后重试。")
			}
		}()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (a API) serverInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id": a.InstanceID, "name": "Benfolio", "version": a.BuildVersion,
		"protocolVersion": 1, "schemaVersion": a.SchemaVersion, "stage": "scaffold",
		"sourceConfigured": a.SourceConfigured,
	})
}

func (a API) capabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"protocolVersion": 1,
		"available": map[string]bool{
			"serverIdentity": true, "singleOwnerAPIKey": true, "catalogRead": true,
			"libraryManagement": false, "scan": false, "download": false,
			"progressSync": false, "annotationSync": false, "resourceTransfer": false,
			"pdfWorkingCopy": false, "pdfExport": false, "epubAnnotationExport": false,
			"backupRestore": false, "webReader": false, "multiUser": false,
		},
	})
}

func (a API) libraries(w http.ResponseWriter, r *http.Request) {
	items, err := a.Catalog.Libraries(r.Context())
	if err != nil {
		a.Logger.Error("catalog_read_failed", "operation", "libraries")
		writeError(w, http.StatusInternalServerError, "storage_error", "读取书库失败。")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a API) books(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	limit, ok := integerParam(params.Get("limit"), 30, 1, 100)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "limit 必须在 1–100 之间。")
		return
	}
	offset, ok := integerParam(params.Get("offset"), 0, 0, 1_000_000)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_pagination", "offset 必须在 0–1000000 之间。")
		return
	}
	format := domain.Format(params.Get("format"))
	if format != "" && format != domain.EPUB && format != domain.PDF {
		writeError(w, http.StatusBadRequest, "invalid_format", "format 仅支持 epub 或 pdf。")
		return
	}
	query := params.Get("query")
	if len(query) > 1024 {
		writeError(w, http.StatusBadRequest, "query_too_long", "搜索词过长。")
		return
	}
	page, err := a.Catalog.Books(r.Context(), domain.BookQuery{Query: query, Format: format, Limit: limit, Offset: offset})
	if err != nil {
		a.Logger.Error("catalog_read_failed", "operation", "books")
		writeError(w, http.StatusInternalServerError, "storage_error", "读取书目失败。")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func integerParam(value string, fallback, min, max int) (int, bool) {
	if value == "" {
		return fallback, true
	}
	number, err := strconv.Atoi(value)
	return number, err == nil && number >= min && number <= max
}

func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "not_found", "接口不存在。")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
