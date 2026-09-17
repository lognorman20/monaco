package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type requestLog struct {
	route string
}

func newRequestLog(r *http.Request, route string) *requestLog {
	auth := authHeaderStatus(r)
	attrs := []any{
		"route", route,
		"method", r.Method,
		"path", r.URL.Path,
		"auth", auth,
	}
	if query := strings.TrimSpace(r.URL.RawQuery); query != "" {
		attrs = append(attrs, "query", query)
	}
	slog.InfoContext(r.Context(), "http request", attrs...)
	return &requestLog{route: route}
}

func authHeaderStatus(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return "missing"
	}
	if _, ok := bearerToken(r); ok {
		return "present"
	}
	return "invalid"
}

func (l *requestLog) done(ctx context.Context, branch string, status int, attrs ...any) {
	args := []any{
		"route", l.route,
		"branch", branch,
		"status", status,
	}
	args = append(args, attrs...)

	switch {
	case status >= 500:
		slog.ErrorContext(ctx, "http response", args...)
	case status >= 400:
		slog.WarnContext(ctx, "http response", args...)
	default:
		slog.InfoContext(ctx, "http response", args...)
	}
}

func logJSONError(ctx context.Context, log *requestLog, branch string, w http.ResponseWriter, status int, message string, attrs ...any) {
	log.done(ctx, branch, status, attrs...)
	writeJSONError(w, status, message)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func logNoContent(ctx context.Context, log *requestLog, branch string, attrs ...any) {
	log.done(ctx, branch, http.StatusNoContent, attrs...)
}

func logJSONOK(ctx context.Context, log *requestLog, branch string, attrs ...any) {
	log.done(ctx, branch, http.StatusOK, attrs...)
}
