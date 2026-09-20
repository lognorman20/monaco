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

// logJSONIntentError is logJSONError for a refusal that has an agent intent outcome to report.
func logJSONIntentError(ctx context.Context, log *requestLog, branch string, w http.ResponseWriter, status int, message string, intent *intentErrorDetail, attrs ...any) {
	if intent != nil {
		attrs = append(attrs, "intent_id", intent.IntentID, "intent_status", intent.Status)
	}
	log.done(ctx, branch, status, attrs...)
	writeJSONErrorBody(ctx, w, status, errorResponse{Error: message, intentErrorDetail: intent})
}

// errorResponse is the single error shape every API route returns: a human
// message plus the correlation id a client can quote in a bug report. The agent
// intent route adds the intent's outcome as extra top-level fields; every other
// route leaves it nil and its body is unchanged.
type errorResponse struct {
	Error     string `json:"error"`
	RequestID string `json:"requestId,omitempty"`
	*intentErrorDetail
}

// intentErrorDetail is what a refused agent intent adds to the error shape, under the names
// the 200 answer uses, so a bot reads one set of fields whatever the HTTP status. IntentID
// is empty when the intent was refused before it was recorded.
type intentErrorDetail struct {
	IntentID     string `json:"intentId,omitempty"`
	Status       string `json:"status,omitempty"`
	RejectReason string `json:"rejectReason,omitempty"`
}

func writeJSONError(ctx context.Context, w http.ResponseWriter, status int, message string) {
	writeJSONErrorBody(ctx, w, status, errorResponse{Error: message})
}

// writeJSONErrorBody is the one place an error body is encoded; it stamps the request id.
func writeJSONErrorBody(ctx context.Context, w http.ResponseWriter, status int, body errorResponse) {
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
