package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type requestLog struct {
	route string
	start time.Time
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
	return &requestLog{route: route, start: time.Now()}
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
		"duration_ms", time.Since(l.start).Milliseconds(),
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
	writeJSONError(ctx, w, status, message)
}

// errorResponse is the single error shape every API route returns: a human
// message plus the correlation id a client can quote in a bug report. Reason is
// the optional machine-readable code for routes whose client branches on why
// the request was refused (leave-cabal 409).
type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"requestId,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func writeJSONError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	writeErrorResponse(ctx, w, status, errorResponse{Error: message})
}

// logJSONErrorWithReason is logJSONError for an error that also carries a
// machine-readable reason; the reason lands in both the body and the log line.
func logJSONErrorWithReason(ctx context.Context, log *requestLog, branch string, w http.ResponseWriter, status int, message, reason string, attrs ...any) {
	log.done(ctx, branch, status, append(attrs, "reason", reason)...)
	writeErrorResponse(ctx, w, status, errorResponse{Error: message, Reason: reason})
}

func writeErrorResponse(ctx context.Context, w http.ResponseWriter, status int, body errorResponse) {
	body.RequestID = RequestIDFromContext(ctx)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func logNoContent(ctx context.Context, log *requestLog, branch string, attrs ...any) {
	log.done(ctx, branch, http.StatusNoContent, attrs...)
}

func logJSONOK(ctx context.Context, log *requestLog, branch string, attrs ...any) {
	log.done(ctx, branch, http.StatusOK, attrs...)
}
