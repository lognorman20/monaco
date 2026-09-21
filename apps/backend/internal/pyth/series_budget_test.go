package pyth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The Benchmarks breaker is process-wide, so what trips it decides whether one
// request's bad luck blanks the day change on every stock for everyone.

func TestHermesClient_seriesFromSource_callerDeadlineLeavesTheBreakerClosed(t *testing.T) {
	ClearFeedRegistry()
	resetEquityDeniedForTest()

	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)
	var slow atomic.Bool
	slow.Store(true)
	source := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			// Slower than the caller is willing to wait.
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"s":"ok","t":[%d,%d],"c":[200.0,202.0]}`,
			etUnix(2026, time.September, 21, 15, 55),
			etUnix(2026, time.September, 22, 11, 55),
		)))
	})
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(source)
	client.now = func() time.Time { return now }
	client.charts = newChartCache(func() time.Time { return now })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if got := client.DayChange(ctx, "AAPLc"); got != nil {
		t.Fatalf("DayChange = %q past the caller's deadline, want nil", *got)
	}
	if !client.seriesBreaker.allows(now) {
		t.Fatal("the caller's own deadline opened the process-wide breaker")
	}

	// A client that hangs up is the same: its request ended, Benchmarks did not.
	hungUp, hangUp := context.WithCancel(context.Background())
	hangUp()
	_, _ = client.ChartSeries(hungUp, "AAPLc", ChartRange1Y)
	if !client.seriesBreaker.allows(now) {
		t.Fatal("a cancelled request opened the process-wide breaker")
	}

	// The next caller, with time to wait, is served by Benchmarks.
	slow.Store(false)
	got := client.DayChange(context.Background(), "MSFTc")
	if got == nil || *got != "0.010000" {
		t.Fatalf("DayChange = %v for the next caller, want 0.010000", got)
	}
}

func TestHermesClient_seriesFromSource_upstreamTimeoutStillTripsTheBreaker(t *testing.T) {
	ClearFeedRegistry()
	resetEquityDeniedForTest()

	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	// The HTTP client's own timeout, not the caller's: that is Benchmarks being slow.
	source := NewBenchmarksClientWithHTTP(server.URL, &http.Client{Timeout: 50 * time.Millisecond})
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(source)
	client.now = func() time.Time { return now }

	if got := client.DayChange(context.Background(), "AAPLc"); got != nil {
		t.Fatalf("DayChange = %q, want nil", *got)
	}
	if client.seriesBreaker.allows(now) {
		t.Fatal("an upstream timeout must open the breaker")
	}
}

func TestHermesClient_seriesFromSource_aSymbolErrorIsAnAnswerAboutThatSymbol(t *testing.T) {
	ClearFeedRegistry()
	resetEquityDeniedForTest()

	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	source := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Query().Get("symbol"), "NOPE") {
			_, _ = w.Write([]byte(`{"s":"error","errmsg":"Symbol not found"}`))
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"s":"ok","t":[%d,%d],"c":[200.0,202.0]}`,
			etUnix(2026, time.September, 21, 15, 55),
			etUnix(2026, time.September, 22, 11, 55),
		)))
	})
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(source)
	client.now = func() time.Time { return now }
	client.charts = newChartCache(func() time.Time { return now })

	series, err := client.ChartSeries(context.Background(), "NOPEc", ChartRange1D)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) != 0 || series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("series = %+v, want that symbol's empty answer", series)
	}
	if !client.seriesBreaker.allows(now) {
		t.Fatal("one symbol's error status opened the breaker for every symbol")
	}
	if got := client.DayChange(context.Background(), "AAPLc"); got == nil || *got != "0.010000" {
		t.Fatalf("DayChange(AAPLc) = %v after another symbol's error, want 0.010000", got)
	}
	// The symbol's answer is cached like any other empty answer.
	before := calls.Load()
	_, _ = client.ChartSeries(context.Background(), "NOPEc", ChartRange1D)
	if calls.Load() != before {
		t.Fatal("the symbol's error answer was not cached")
	}
}

// historicalSampler serves Hermes historical prices for feed-aapl. fails decides,
// per request number (1-based), whether that sample fails.
func historicalSampler(t *testing.T, fails func(n int32) bool) *httptest.Server {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			http.NotFound(w, r)
			return
		}
		if fails(n.Add(1)) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestHermesClient_seriesFromHermes_aRunCutShortByTheDeadlineIsNotServedOrCached(t *testing.T) {
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	RegisterFeedID("AAPLc", "feed-aapl")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := historicalSampler(t, func(n int32) bool {
		if n == 4 {
			// The caller's budget runs out a few samples into a 1Y run.
			cancel()
		}
		return false
	})
	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")

	series, _ := client.ChartSeries(ctx, "AAPLc", ChartRange1Y)
	if len(series.Points) != 0 || series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("series = %d points, want the truncated run discarded", len(series.Points))
	}
	if _, cached := client.charts.get("AAPLc", ChartRange1Y); cached {
		t.Fatal("a run cut short by the deadline was cached as the 1Y answer")
	}
}

func TestHermesClient_seriesFromHermes_aRunWithFailedSamplesIsDrawnButNotCached(t *testing.T) {
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	RegisterFeedID("AAPLc", "feed-aapl")

	server := historicalSampler(t, func(n int32) bool { return n%3 == 0 })
	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")

	series, _ := client.ChartSeries(context.Background(), "AAPLc", ChartRange1W)
	want := len(chartSampleTimes(ChartRange1W, time.Now()))
	if len(series.Points) == 0 || len(series.Points) >= want {
		t.Fatalf("points = %d of %d, want a partial run", len(series.Points), want)
	}
	if _, cached := client.charts.get("AAPLc", ChartRange1W); cached {
		t.Fatal("a partial run was cached as the range's answer")
	}
}

func TestHermesClient_seriesFromHermes_aCompleteRunIsCached(t *testing.T) {
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	RegisterFeedID("AAPLc", "feed-aapl")

	server := historicalSampler(t, func(int32) bool { return false })
	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")

	series, _ := client.ChartSeries(context.Background(), "AAPLc", ChartRange1W)
	if len(series.Points) == 0 {
		t.Fatal("want a sampled series")
	}
	if _, cached := client.charts.get("AAPLc", ChartRange1W); !cached {
		t.Fatal("a complete run should be cached")
	}
}
