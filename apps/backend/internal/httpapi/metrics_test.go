package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/telemetry"
)

func scrapeMetrics(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	telemetry.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Body.String()
}

// platformChain mirrors cmd/api's middleware order: Metrics innermost, Recover outside it.
func platformChain(mux http.Handler) http.Handler {
	return Chain(mux, RequestID(), Recover(), Metrics())
}

func TestMetrics_labelsByRoutePattern_neverByRawPath(t *testing.T) {
	// Arrange
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/metricstest/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := platformChain(mux)

	// Act
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/metricstest/cabal-secret-id-123", nil))

	// Assert
	body := scrapeMetrics(t)
	if !strings.Contains(body, `monaco_http_requests_total{method="GET",route="GET /v1/metricstest/{id}",status="418"} 1`) {
		t.Fatalf("pattern-labelled counter missing from:\n%s", grepLines(body, "metricstest"))
	}
	if strings.Contains(body, "cabal-secret-id-123") {
		t.Fatal("raw path id leaked into a metric label")
	}
}

func TestMetrics_unmatchedRoute_isOneSeries(t *testing.T) {
	handler := platformChain(http.NewServeMux())

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/wp-admin/scan-1", nil))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/wp-admin/scan-2", nil))

	body := scrapeMetrics(t)
	if strings.Contains(body, "wp-admin") {
		t.Fatal("scanner paths must not mint label values")
	}
	if !strings.Contains(body, `route="unmatched",status="404"`) {
		t.Fatalf("expected unmatched 404 series, got:\n%s", grepLines(body, "unmatched"))
	}
}

func TestMetrics_handlerPanic_countsA500_andRecoverStillAnswers(t *testing.T) {
	// Arrange
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/metricspanic", func(http.ResponseWriter, *http.Request) {
		panic("nil pointer in handler")
	})
	handler := platformChain(mux)
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/metricspanic", nil))

	// Assert
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 from Recover", rec.Code)
	}
	if !strings.Contains(scrapeMetrics(t), `route="POST /v1/metricspanic",status="500"} 1`) {
		t.Fatal("panicking request was not counted as a 500")
	}
}

func TestMetricsEndpoint_withToken_requiresExactBearer(t *testing.T) {
	handler := MetricsEndpoint("scrape-secret", telemetry.Handler())
	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"prefix of token", "Bearer scrape", http.StatusUnauthorized},
		{"not bearer", "Basic scrape-secret", http.StatusUnauthorized},
		{"correct", "Bearer scrape-secret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.RemoteAddr = "127.0.0.1:5555" // loopback must not bypass a configured token
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want != http.StatusOK && strings.Contains(rec.Body.String(), "monaco_") {
				t.Fatal("metrics leaked in a rejected response")
			}
		})
	}
}

func TestMetricsEndpoint_withoutToken_servesLoopbackOnly(t *testing.T) {
	handler := MetricsEndpoint("  ", telemetry.Handler())
	cases := []struct {
		remote string
		want   int
	}{
		{"127.0.0.1:4000", http.StatusOK},
		{"[::1]:4000", http.StatusOK},
		{"203.0.113.9:4000", http.StatusNotFound},
		{"10.0.0.5:4000", http.StatusNotFound},
		{"garbage", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.remote, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.RemoteAddr = tc.remote
			// A spoofed forwarding header must not make a remote caller look local.
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("remote %s: status = %d, want %d", tc.remote, rec.Code, tc.want)
			}
		})
	}
}

func grepLines(body, needle string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
