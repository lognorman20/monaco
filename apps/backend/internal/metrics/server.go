package metrics

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// NewServer returns the listener that serves GET /metrics and nothing else. It is separate
// from the API server so the public port can never route to it; config only allows a
// non-loopback addr together with a token. A non-empty token is required as a bearer token.
func (m *Metrics) NewServer(addr, token string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", requireBearer(token, m.Handler()))
	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
}

func requireBearer(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, presented, _ := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
		// Hashing first makes the comparison constant time regardless of length.
		got := sha256.Sum256([]byte(strings.TrimSpace(presented)))
		if !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
