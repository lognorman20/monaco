package httpapi

import (
	"net/http"
	"strings"
)

const (
	// DefaultMaxRequestBytes covers every JSON route: the largest body the API
	// accepts today (a proposal with a thesis) is a few kilobytes.
	DefaultMaxRequestBytes int64 = 64 << 10

	// MaxUploadRequestBytes covers multipart profile-photo uploads. The app
	// rejects photos over 2MB; the extra megabyte is multipart framing slack.
	MaxUploadRequestBytes int64 = 3 << 20
)

// uploadRoutePrefixes are the routes allowed the larger multipart budget.
var uploadRoutePrefixes = []string{"/v1/me/profile-photo"}

// LimitRequestBody caps how much a handler can be made to read, so a client
// cannot pin API memory with an endless body. GET/DELETE style requests carry
// no body budget beyond the default.
func LimitRequestBody(defaultLimit int64) Middleware {
	if defaultLimit <= 0 {
		defaultLimit = DefaultMaxRequestBytes
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := defaultLimit
			if isUploadRoute(r.URL.Path) {
				limit = MaxUploadRequestBytes
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isUploadRoute(path string) bool {
	for _, prefix := range uploadRoutePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
