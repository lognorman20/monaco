package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d", rec.Code)
	}
	return rec.Body.String()
}

func TestHandler_exposesEveryMetricFamilyAfterUse(t *testing.T) {
	// Arrange
	ObserveHTTP("GET /v1/groups/{id}", http.MethodGet, 200, 20*time.Millisecond)
	MoneyMoved(EventSwapBuy, 1_000_000)
	ObserveUpstream(UpstreamJupiter, OutcomeRateLimited, time.Millisecond)
	SetRelayerBalance(5_000_000)
	BreakerOpened("pyth")
	PriceFallback("cost_basis")

	// Act
	body := scrape(t)

	// Assert
	for _, want := range []string{
		`monaco_http_requests_total{method="GET",route="GET /v1/groups/{id}",status="200"}`,
		`monaco_money_events_total{event="swap_buy",outcome="ok"}`,
		`monaco_money_volume_usdc_micros_total{event="swap_buy"}`,
		`monaco_upstream_requests_total{outcome="rate_limited",service="jupiter"}`,
		`monaco_relayer_balance_lamports 5e+06`,
		`monaco_price_breaker_opens_total{source="pyth"}`,
		`monaco_price_fallbacks_total{tier="cost_basis"}`,
		`go_goroutines`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestObserveHTTP_emptyRoute_isLabelledUnmatched(t *testing.T) {
	before := testutil.ToFloat64(httpRequests.WithLabelValues("unmatched", "GET", "404"))

	ObserveHTTP("", http.MethodGet, 404, time.Millisecond)

	if got := testutil.ToFloat64(httpRequests.WithLabelValues("unmatched", "GET", "404")); got != before+1 {
		t.Fatalf("unmatched counter = %v, want %v", got, before+1)
	}
}

func TestMoneyMoved_zeroOrNegativeAmount_countsEventButNoVolume(t *testing.T) {
	events := testutil.ToFloat64(moneyEvents.WithLabelValues(EventWithdrawal, OutcomeOK))
	volume := testutil.ToFloat64(moneyVolume.WithLabelValues(EventWithdrawal))

	MoneyMoved(EventWithdrawal, 0)
	MoneyMoved(EventWithdrawal, -5)

	if got := testutil.ToFloat64(moneyEvents.WithLabelValues(EventWithdrawal, OutcomeOK)); got != events+2 {
		t.Fatalf("events = %v, want %v", got, events+2)
	}
	if got := testutil.ToFloat64(moneyVolume.WithLabelValues(EventWithdrawal)); got != volume {
		t.Fatalf("volume moved by non-positive amounts: %v -> %v", volume, got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransport_classifiesOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		err     error
		outcome string
	}{
		{"ok", 200, nil, OutcomeOK},
		{"client error", 404, nil, "client_error"},
		{"rate limited", 429, nil, OutcomeRateLimited},
		{"server error", 503, nil, "server_error"},
		{"transport error", 0, errors.New("dial tcp: connection refused"), "transport_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			service := "test_" + strings.ReplaceAll(tc.name, " ", "_")
			transport := Transport(service, roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(""))}, nil
			}))
			req := httptest.NewRequest(http.MethodGet, "https://upstream.example/x?secret=1", nil)

			// Act
			resp, err := transport.RoundTrip(req)

			// Assert
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v passed through", err, tc.err)
			}
			if tc.err == nil && resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d passed through", resp.StatusCode, tc.status)
			}
			if got := testutil.ToFloat64(upstreamRequests.WithLabelValues(service, tc.outcome)); got != 1 {
				t.Fatalf("counter{%s,%s} = %v, want 1", service, tc.outcome, got)
			}
		})
	}
}

func TestInstrumentClient_doesNotMutateCallerClient(t *testing.T) {
	original := &http.Client{Timeout: time.Second}

	wrapped := InstrumentClient("test_copy", original)

	if original.Transport != nil {
		t.Fatal("caller's client transport was mutated")
	}
	if wrapped.Timeout != time.Second {
		t.Fatalf("timeout = %v, want it preserved", wrapped.Timeout)
	}
	if InstrumentClient("test_nil", nil) == nil {
		t.Fatal("nil client must yield a usable client")
	}
}

func TestTransportFunc_labelsPerRequest(t *testing.T) {
	transport := TransportFunc(func(r *http.Request) string {
		if r.URL.Host == "rpc.example" {
			return "test_rpc"
		}
		return "test_api"
	}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))

	_, _ = transport.RoundTrip(httptest.NewRequest(http.MethodPost, "https://rpc.example/", nil))
	_, _ = transport.RoundTrip(httptest.NewRequest(http.MethodGet, "https://api.example/v1", nil))

	if got := testutil.ToFloat64(upstreamRequests.WithLabelValues("test_rpc", OutcomeOK)); got != 1 {
		t.Fatalf("rpc counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(upstreamRequests.WithLabelValues("test_api", OutcomeOK)); got != 1 {
		t.Fatalf("api counter = %v, want 1", got)
	}
}

// --- pollers ---

func withPollerClock(t *testing.T, now *time.Time) {
	t.Helper()
	pollers.mu.Lock()
	previous := pollers.now
	pollers.now = func() time.Time { return *now }
	pollers.mu.Unlock()
	t.Cleanup(func() {
		pollers.mu.Lock()
		pollers.now = previous
		pollers.mu.Unlock()
	})
}

func forgetPoller(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() {
		pollers.mu.Lock()
		delete(pollers.entries, name)
		pollers.mu.Unlock()
	})
}

func TestGuardTick_panic_isRecoveredCountedAndStillHeartbeats(t *testing.T) {
	// Arrange
	const name = "test_panicking"
	forgetPoller(t, name)
	RegisterPoller(name, time.Second)

	// Act: must not propagate.
	GuardTick(context.Background(), name, func() error { panic("nil map write") })

	// Assert
	if got := testutil.ToFloat64(pollerTicks.WithLabelValues(name, OutcomePanic)); got != 1 {
		t.Fatalf("panic ticks = %v, want 1", got)
	}
	if stale := StalePollers(); contains(stale, name) {
		t.Fatalf("a panicking tick is a live loop, got stale = %v", stale)
	}
}

func TestGuardTick_outcomes(t *testing.T) {
	const name = "test_outcomes"
	forgetPoller(t, name)

	GuardTick(context.Background(), name, func() error { return nil })
	GuardTick(context.Background(), name, func() error { return errors.New("db down") })

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	GuardTick(cancelled, name, func() error { return context.Canceled })

	if got := testutil.ToFloat64(pollerTicks.WithLabelValues(name, OutcomeOK)); got != 2 {
		t.Fatalf("ok ticks = %v, want 2 (success + error during shutdown)", got)
	}
	if got := testutil.ToFloat64(pollerTicks.WithLabelValues(name, OutcomeError)); got != 1 {
		t.Fatalf("error ticks = %v, want 1", got)
	}
}

func TestStalePollers_flagsOnlyPollersPastTheirWindow(t *testing.T) {
	// Arrange
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	withPollerClock(t, &now)
	forgetPoller(t, "test_fast")
	forgetPoller(t, "test_slow")
	RegisterPoller("test_fast", 3*time.Second) // window floors at minStaleAfter
	RegisterPoller("test_slow", 10*time.Minute)

	// Act + Assert: just registered, nothing stale.
	if err := CheckPollers(context.Background()); err != nil {
		t.Fatalf("fresh pollers reported stale: %v", err)
	}

	now = now.Add(minStaleAfter + time.Second)
	stale := StalePollers()
	if !contains(stale, "test_fast") || contains(stale, "test_slow") {
		t.Fatalf("stale = %v, want only test_fast", stale)
	}
	if err := CheckPollers(context.Background()); err == nil || !strings.Contains(err.Error(), "test_fast") {
		t.Fatalf("CheckPollers err = %v, want it to name test_fast", err)
	}

	// A tick clears it.
	GuardTick(context.Background(), "test_fast", func() error { return nil })
	if contains(StalePollers(), "test_fast") {
		t.Fatal("poller still stale after a tick")
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// --- alerts ---

func TestAlert_webhookReceivesSlackShapedMessage_withoutLeakingExtraFields(t *testing.T) {
	// Arrange
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
	}))
	defer server.Close()
	a := newAlerter(server.URL, time.Minute, server.Client())
	a.start()
	defer a.stop()

	// Act
	a.raise(context.Background(), AlertEvent{
		Kind:     "test_wedged",
		Key:      "test_wedged:job-1",
		Severity: SeverityCritical,
		Title:    "Cash out wedged",
		Detail:   "needs manual review",
		Fields:   map[string]string{"job_id": "job-1", "group_id": "g-1"},
	})

	// Assert
	select {
	case body := <-received:
		for _, want := range []string{`"text"`, "[CRITICAL] Cash out wedged", "needs manual review", "group_id: g-1", "job_id: job-1"} {
			if !strings.Contains(body, want) {
				t.Errorf("webhook body %q missing %q", body, want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}

func TestAlert_discordWebhook_usesContentField(t *testing.T) {
	payload := webhookPayload("https://discord.com/api/webhooks/1/abc", AlertEvent{Title: "x", Severity: SeverityWarning})
	if _, ok := payload["content"]; !ok {
		t.Fatalf("payload = %v, want a content field for Discord", payload)
	}
}

func TestAlert_repeatsInsideCooldown_areSuppressed_thenSentAgainAfter(t *testing.T) {
	// Arrange
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	var clockMu sync.Mutex
	a := newAlerter(server.URL, 15*time.Minute, server.Client())
	a.now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	a.start()
	event := AlertEvent{Kind: "test_repeat", Key: "test_repeat:1", Title: "again"}

	// Act
	for i := 0; i < 5; i++ {
		a.raise(context.Background(), event)
	}
	a.raise(context.Background(), AlertEvent{Kind: "test_repeat", Key: "test_repeat:2", Title: "different job"})
	clockMu.Lock()
	now = now.Add(16 * time.Minute)
	clockMu.Unlock()
	a.raise(context.Background(), event)
	a.stop() // drains the queue

	// Assert
	if got := calls.Load(); got != 3 {
		t.Fatalf("webhook calls = %d, want 3 (first, other key, after cooldown)", got)
	}
	if got := testutil.ToFloat64(alertsSent.WithLabelValues("test_repeat", "suppressed")); got != 4 {
		t.Fatalf("suppressed = %v, want 4", got)
	}
}

func TestAlert_webhookDownOrRejecting_neverBlocksOrPanicsTheCaller(t *testing.T) {
	// Arrange: a receiver that hangs longer than the caller should ever wait.
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	defer close(release)
	a := newAlerter(server.URL, time.Nanosecond, server.Client())
	a.start()

	// Act: far more alerts than the queue holds, each with a unique key.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < alertQueueSize*3; i++ {
			a.raise(context.Background(), AlertEvent{Kind: "test_flood", Key: "test_flood:" + time.Duration(i).String(), Title: "flood"})
		}
	}()

	// Assert
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Alert blocked the caller while the webhook was hung")
	}
	if got := testutil.ToFloat64(alertsSent.WithLabelValues("test_flood", "dropped")); got == 0 {
		t.Fatal("expected overflow deliveries to be counted as dropped")
	}
}

func TestAlert_afterStop_isDroppedNotPanicking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	a := newAlerter(server.URL, time.Minute, server.Client())
	a.start()
	a.stop()
	a.stop() // idempotent

	a.raise(context.Background(), AlertEvent{Kind: "test_late", Title: "late alert during shutdown"})

	if got := testutil.ToFloat64(alertsSent.WithLabelValues("test_late", "dropped")); got != 1 {
		t.Fatalf("dropped = %v, want 1", got)
	}
}

func TestAlert_noWebhook_isLogOnly(t *testing.T) {
	a := newAlerter("", time.Minute, nil)
	a.start()

	a.raise(context.Background(), AlertEvent{Kind: "test_logonly", Title: "no webhook configured"})

	if got := testutil.ToFloat64(alertsSent.WithLabelValues("test_logonly", "log_only")); got != 1 {
		t.Fatalf("log_only = %v, want 1", got)
	}
}

func TestAlert_longDetail_isTruncated(t *testing.T) {
	message := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		message <- string(body)
	}))
	defer server.Close()
	a := newAlerter(server.URL, time.Minute, server.Client())
	a.start()
	defer a.stop()

	a.raise(context.Background(), AlertEvent{Kind: "test_long", Title: "t", Detail: strings.Repeat("x", 5000)})

	select {
	case body := <-message:
		if len(body) > maxAlertDetailSize+200 {
			t.Fatalf("webhook body is %d bytes; detail was not truncated", len(body))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("webhook was never called")
	}
}

func TestInitAlerts_rejectsMalformedURL_acceptsEmpty(t *testing.T) {
	for _, bad := range []string{"not a url", "ftp://example.com/hook", "https://"} {
		if _, err := InitAlerts(bad); err == nil {
			t.Errorf("InitAlerts(%q) = nil error, want rejection", bad)
		}
	}
	stop, err := InitAlerts("")
	if err != nil {
		t.Fatalf("InitAlerts(\"\") = %v, want nil", err)
	}
	stop()
}

func TestInitSentry_emptyDSN_isDisabledNoOp(t *testing.T) {
	flush, err := InitSentry(SentryOptions{})
	if err != nil {
		t.Fatalf("InitSentry: %v", err)
	}
	flush()
	// Must be safe without a client.
	CapturePanic("boom", []byte("stack"), map[string]string{"poller": "x"})
	captureAlert(AlertEvent{Kind: "k", Title: "t"})
}

func TestInitSentry_malformedDSN_isAnError(t *testing.T) {
	if _, err := InitSentry(SentryOptions{DSN: "not-a-dsn"}); err == nil {
		t.Fatal("malformed DSN accepted; a typo must fail boot, not silently disable reporting")
	}
	if sentryEnabled.Load() {
		t.Fatal("sentry marked enabled after a failed init")
	}
}
