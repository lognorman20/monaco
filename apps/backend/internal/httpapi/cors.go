package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// corsMaxAge is how long a browser may cache a preflight result.
const corsMaxAge = 10 * time.Minute

// corsAllowedHeaders are the request headers the API reads.
var corsAllowedHeaders = strings.Join([]string{
	"Authorization",
	"Content-Type",
	"Idempotency-Key",
	RequestIDHeader,
	agentKeyHeader,
}, ", ")

var corsAllowedMethods = strings.Join([]string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPatch,
	http.MethodOptions,
}, ", ")

// CORS answers cross-origin requests for an explicit allow-list only. A
// wildcard origin is never emitted: with credentials in an Authorization header
// that would let any page on the internet spend a user's session.
//
// Origins come from CORS_ALLOWED_ORIGINS (comma-separated). Empty means no
// browser origin is allowed, which is the correct default for a mobile-only API.
func CORS(allowedOrigins []string) Middleware {
	allowed := normalizeOrigins(allowedOrigins)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				// Vary regardless of the decision: caches must not serve one
				// origin's response to another.
				w.Header().Add("Vary", "Origin")
			}
			permitted := origin != "" && allowed[strings.ToLower(origin)]
			if permitted {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Expose-Headers", RequestIDHeader+", Retry-After")
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
				if !permitted {
					// No allow headers: the browser blocks the real request.
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(corsMaxAge.Seconds())))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func normalizeOrigins(origins []string) map[string]bool {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		origin = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(origin), "/")))
		if origin == "" || origin == "*" {
			// "*" is never honored; see CORS doc comment.
			continue
		}
		allowed[origin] = true
	}
	return allowed
}
