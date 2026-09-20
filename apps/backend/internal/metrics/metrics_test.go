package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func scrape(t *testing.T, handler http.Handler, authorization string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func assertContains(t *testing.T, body string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(body, line) {
			t.Errorf("metrics missing %q", line)
		}
	}
	if t.Failed() {
		t.Logf("scrape:\n%s", body)
	}
}

func TestMetrics_workerTicksAndRestarts_areExported(t *testing.T) {
	// Arrange
	m := New()
	m.RegisterWorkers("sweep_poller", "redeem_recovery_poller")

	// Act
	m.ObserveTick("sweep_poller", 20*time.Millisecond, nil)
	m.ObserveTick("sweep_poller", 40*time.Millisecond, errors.New("rpc down"))
	m.WorkerRestarted("sweep_poller")
	_, body := scrape(t, m.Handler(), "")

	// Assert
	assertContains(t, body,
		`monaco_worker_ticks_total{result="success",worker="sweep_poller"} 1`,
		`monaco_worker_ticks_total{result="failure",worker="sweep_poller"} 1`,
		`monaco_worker_tick_duration_seconds_count{worker="sweep_poller"} 2`,
		`monaco_worker_restarts_total{worker="sweep_poller"} 1`,
		`monaco_worker_last_success_timestamp_seconds{worker="sweep_poller"}`,
		// A worker that never restarted must read 0, not be absent, for alert rules.
		`monaco_worker_restarts_total{worker="redeem_recovery_poller"} 0`,
	)
}

func TestHTTPMiddleware_labelsByRoutePattern_notByPath(t *testing.T) {
	// Arrange
	m := New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/groups/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	handler := m.HTTPMiddleware()(mux)

	// Act
	for _, path := range []string{"/v1/groups/a", "/v1/groups/b", "/wp-login.php", "/.env"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	_, body := scrape(t, m.Handler(), "")

	// Assert
	assertContains(t, body,
		`monaco_http_requests_total{method="GET",route="GET /v1/groups/{id}",status="403"} 2`,
		`monaco_http_requests_total{method="GET",route="unmatched",status="404"} 2`,
		`monaco_http_request_duration_seconds_count{method="GET",route="GET /v1/groups/{id}"} 2`,
	)
	if strings.Contains(body, "wp-login") {
		t.Fatal("raw request paths became label values: unbounded cardinality")
	}
}

func TestOpsCollector_exportsBacklogAndFeePayer(t *testing.T) {
	// Arrange
	m := New()
	m.RegisterOps(OpsSource{
		Backlog: func(context.Context) (OpsBacklog, error) {
			return OpsBacklog{
				PendingDeposits:            2,
				OldestPendingDepositAgeSec: 930,
				PendingSwaps:               1,
				OldestPendingSwapAgeSec:    45,
				RedeemJobs:                 map[string]int64{"paying": 1},
				OldestRedeemJobAgeSec:      map[string]int64{"paying": 1200},
			}, nil
		},
		FeePayerLamports: func(context.Context) (uint64, error) { return 5_000_000, nil },
	})

	// Act
	_, body := scrape(t, m.Handler(), "")

	// Assert
	assertContains(t, body,
		"monaco_pending_deposits 2",
		"monaco_pending_deposit_oldest_age_seconds 930",
		"monaco_pending_swaps 1",
		"monaco_pending_swap_oldest_age_seconds 45",
		`monaco_redeem_jobs{status="paying"} 1`,
		`monaco_redeem_job_oldest_age_seconds{status="paying"} 1200`,
		`monaco_redeem_jobs{status="selling"} 0`,
		"monaco_fee_payer_lamports 5e+06",
		`monaco_ops_source_up{source="database"} 1`,
		`monaco_ops_source_up{source="solana_rpc"} 1`,
	)
}

func TestOpsCollector_sourceDown_dropsItsGaugesInsteadOfReportingZero(t *testing.T) {
	// Arrange
	m := New()
	m.RegisterOps(OpsSource{
		Backlog: func(context.Context) (OpsBacklog, error) {
			return OpsBacklog{}, errors.New("connection refused")
		},
		FeePayerLamports: func(context.Context) (uint64, error) { return 0, errors.New("429") },
	})

	// Act
	code, body := scrape(t, m.Handler(), "")

	// Assert
	if code != http.StatusOK {
		t.Fatalf("status = %d: a dead dependency must not take the whole scrape down", code)
	}
	assertContains(t, body,
		`monaco_ops_source_up{source="database"} 0`,
		`monaco_ops_source_up{source="solana_rpc"} 0`,
	)
	for _, absent := range []string{"monaco_pending_deposits ", "monaco_fee_payer_lamports "} {
		if strings.Contains(body, absent) {
			t.Errorf("%q exported while its source is down: a fake 0 hides the incident", absent)
		}
	}
}

func TestOpsCollector_rapidScrapes_queryTheSourceOnce(t *testing.T) {
	// Arrange
	m := New()
	calls := 0
	m.RegisterOps(OpsSource{Backlog: func(context.Context) (OpsBacklog, error) {
		calls++
		return OpsBacklog{}, nil
	}})

	// Act
	for range 5 {
		scrape(t, m.Handler(), "")
	}

	// Assert
	if calls != 1 {
		t.Fatalf("backlog queried %d times for 5 rapid scrapes, want 1", calls)
	}
}

func TestServer_withToken_rejectsMissingAndWrongBearer(t *testing.T) {
	// Arrange
	const token = "0123456789abcdef01234567"
	handler := New().NewServer("0.0.0.0:9090", token).Handler

	cases := map[string]struct {
		authorization string
		want          int
	}{
		"no header":      {"", http.StatusUnauthorized},
		"wrong token":    {"Bearer 0123456789abcdef0123456X", http.StatusUnauthorized},
		"token prefix":   {"Bearer 0123456789abcdef", http.StatusUnauthorized},
		"basic scheme":   {"Basic " + token, http.StatusUnauthorized},
		"correct bearer": {"Bearer " + token, http.StatusOK},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Act
			code, body := scrape(t, handler, tc.authorization)

			// Assert
			if code != tc.want {
				t.Fatalf("status = %d, want %d", code, tc.want)
			}
			if code != http.StatusOK && strings.Contains(body, "monaco_") {
				t.Fatal("metrics leaked in a rejected response")
			}
		})
	}
}

func TestServer_servesOnlyMetrics(t *testing.T) {
	// Arrange
	handler := New().NewServer("127.0.0.1:9090", "").Handler
	rec := httptest.NewRecorder()

	// Act
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for anything but /metrics", rec.Code)
	}
}
