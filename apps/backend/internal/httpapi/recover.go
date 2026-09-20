package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
)

// panicStackBytes bounds the stack captured for a panic log line.
const panicStackBytes = 8 << 10

// Recover turns a handler panic into a logged stack trace and a JSON 500 so a
// single bad request cannot take the API process down with it.
//
// net/http would already recover the panic per connection, but it logs an
// unstructured trace to stderr and closes the connection with no response body:
// the client sees a transport error rather than a 500 it can report.
func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := newResponseRecorder(w)
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					// Documented escape hatch: let net/http handle it.
					panic(rec)
				}

				stack := make([]byte, panicStackBytes)
				stack = stack[:runtime.Stack(stack, false)]
				ctx := r.Context()
				slog.ErrorContext(ctx, "http handler panic",
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprintf("%v", rec),
					"stack", string(stack),
				)

				if recorder.wrote {
					// Headers are already on the wire; the client sees a truncated
					// body, which is the best that can be done at this point.
					return
				}
				writeJSONError(ctx, recorder, http.StatusInternalServerError, "internal server error")
			}()

			next.ServeHTTP(recorder, r)
		})
	}
}
