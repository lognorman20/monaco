package httpapi

import (
	"net/http"
)

// Middleware wraps an http.Handler with a cross-cutting concern.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first listed runs first on the way in
// and last on the way out: Chain(h, a, b) serves a(b(h)).
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		if middlewares[i] == nil {
			continue
		}
		h = middlewares[i](h)
	}
	return h
}

// responseRecorder tracks whether a handler wrote a response, so recovery and
// idempotency middleware can tell "nothing written yet" from "half a body out".
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written int64
	wrote   bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.wrote {
		return
	}
	r.wrote = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush forwards to the underlying writer when it supports flushing.
func (r *responseRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
