package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

// RequestIDHeader carries the correlation id in and out of the API.
const RequestIDHeader = "X-Request-Id"

// maxInboundRequestIDLen bounds what a client may pass so a caller cannot push
// unbounded text into every log line for a request.
const maxInboundRequestIDLen = 64

type requestIDContextKey struct{}

// WithRequestID returns ctx carrying id as the correlation id.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, id)
}

// RequestIDFromContext returns the correlation id set by RequestID middleware.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDContextKey{}).(string)
	return id
}

// RequestID accepts a client-supplied X-Request-Id (when it is short and
// printable) or mints one, puts it on the request context for logs and echoes
// it on the response so an app-side error report can be traced to server logs.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set(RequestIDHeader, id)
			next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
		})
	}
}

func sanitizeRequestID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxInboundRequestIDLen {
		return ""
	}
	for _, ch := range raw {
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '-', ch == '_':
		default:
			return ""
		}
	}
	return raw
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failing is fatal for ids; fall back to an empty-ish marker
		// rather than panicking a request that is otherwise serviceable.
		return "unknown"
	}
	return hex.EncodeToString(buf[:])
}

// RequestIDLogHandler decorates a slog.Handler so every *Context log call made
// while serving a request carries request_id without threading it by hand.
type RequestIDLogHandler struct {
	inner slog.Handler
}

// NewRequestIDLogHandler wraps inner with request-id enrichment.
func NewRequestIDLogHandler(inner slog.Handler) *RequestIDLogHandler {
	return &RequestIDLogHandler{inner: inner}
}

func (h *RequestIDLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RequestIDLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := RequestIDFromContext(ctx); id != "" {
		record = record.Clone()
		record.AddAttrs(slog.String("request_id", id))
	}
	return h.inner.Handle(ctx, record)
}

func (h *RequestIDLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RequestIDLogHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *RequestIDLogHandler) WithGroup(name string) slog.Handler {
	return &RequestIDLogHandler{inner: h.inner.WithGroup(name)}
}
