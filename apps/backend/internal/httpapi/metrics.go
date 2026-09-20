package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

// Metrics records every request's route pattern, status and latency.
//
// It must be the innermost middleware, wrapping the mux directly: the mux sets r.Pattern on
// the request it is handed, and outer middleware that call r.WithContext hand it a copy.
// Labelling by pattern rather than path keeps cabal and proposal ids out of the label set.
func Metrics() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := newResponseRecorder(w)
			defer func() {
				status := recorder.status
				if rec := recover(); rec != nil {
					// Recover (outside this middleware) writes the 500; count it as one
					// and let the panic keep travelling.
					telemetry.ObserveHTTP(r.Pattern, r.Method, http.StatusInternalServerError, time.Since(start))
					panic(rec)
				}
				telemetry.ObserveHTTP(r.Pattern, r.Method, status, time.Since(start))
			}()
			next.ServeHTTP(recorder, r)
		})
	}
}

// MetricsEndpoint guards the Prometheus scrape endpoint. Metrics describe money flow and
// upstream health, so they are not public: with a token set, the scraper must present it as
// a bearer token; without one, only direct loopback callers are served.
//
// "Direct" matters: behind a reverse proxy on the same host every internet request arrives
// from 127.0.0.1, so the socket address alone would open /metrics to the world. A request
// that carries a forwarding header came through a proxy and is refused, the same rule the
// faker seed endpoint uses. A scraper that sits behind a proxy must use METRICS_TOKEN.
func MetricsEndpoint(token string, handler http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			presented, ok := bearerToken(r)
			if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				writeJSONError(r.Context(), w, http.StatusUnauthorized, "unauthorized")
				return
			}
		} else if !isLoopbackRequest(r) {
			writeJSONError(r.Context(), w, http.StatusNotFound, "not found")
			return
		}
		handler.ServeHTTP(w, r)
	})
}
