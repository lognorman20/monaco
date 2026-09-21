package pyth

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestHermesClient_ChartSeries_servesHistoryWithNoAPIKey(t *testing.T) {
	// The chart client is built with whatever PYTH_API_KEY is, including "". Hermes
	// refuses every request without a key, but Benchmarks is a public endpoint and
	// takes none, so a deployment with no key still draws charts.
	ClearFeedRegistry()

	now := time.Now().UTC()
	source := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Benchmarks was sent an Authorization header (%q); it takes no key", auth)
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"s":"ok","t":[%d,%d],"c":[229.4,231.4]}`,
			now.AddDate(0, 0, -2).Unix(),
			now.AddDate(0, 0, -1).Unix(),
		)))
	})

	// An unreachable Hermes: if anything falls through to the sampler, this fails.
	keyless := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(source)
	series, err := keyless.ChartSeries(context.Background(), "AAPLc", ChartRange1Y)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) != 2 || series.Source != ChartSourceBenchmarks {
		t.Fatalf("series = %+v, want two Benchmarks points", series)
	}
	if series.Basis != PriceBasisUnderlying || series.BasisSymbol != "AAPL" || series.Range != ChartRange1Y {
		t.Fatalf("basis/symbol/range = %q/%q/%q", series.Basis, series.BasisSymbol, series.Range)
	}
}

func TestHermesClient_ChartSeries_keylessOutageShortCircuitsTheSampler(t *testing.T) {
	// Benchmarks is down and there is no key: the sampler has nothing it may ask, so
	// the answer is an empty series straight away — and, being an outage rather than
	// an answer, it is not cached.
	ClearFeedRegistry()
	resetEquityDeniedForTest()

	var sourceCalls atomic.Int32
	failing := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	now := time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC)
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(failing)
	client.now = func() time.Time { return now }

	series, err := client.ChartSeries(context.Background(), "AAPLc", ChartRange1M)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) != 0 || series.EmptyReason != EmptyReasonNoHistory || series.Range != ChartRange1M {
		t.Fatalf("series = %+v, want an empty 1M series", series)
	}
	if _, cached := client.charts.get("AAPLc", ChartRange1M); cached {
		t.Fatal("an outage must not be cached as an empty answer")
	}

	// The breaker is open, so the next read does not spend another timeout.
	_, _ = client.ChartSeries(context.Background(), "AAPLc", ChartRange1M)
	if sourceCalls.Load() != 1 {
		t.Fatalf("source called %d times inside the breaker's cooldown", sourceCalls.Load())
	}
	// After the cooldown the source is tried again.
	now = now.Add(defaultSeriesBreakerCooldown + time.Second)
	_, _ = client.ChartSeries(context.Background(), "AAPLc", ChartRange1M)
	if sourceCalls.Load() != 2 {
		t.Fatalf("source called %d times after the cooldown, want 2", sourceCalls.Load())
	}
}

func TestHermesClient_ChartSeries_cachesAnEmptyAnswer(t *testing.T) {
	// "This feed has nothing in this window" is an upstream answer. Not caching it
	// made the symbols with no history the most expensive ones in the catalog.
	ClearFeedRegistry()

	var sourceCalls atomic.Int32
	empty := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		_, _ = w.Write([]byte(`{"s":"no_data"}`))
	})

	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(empty)
	for i := 0; i < 3; i++ {
		series, err := client.ChartSeries(context.Background(), "NEWc", ChartRange1Y)
		if err != nil {
			t.Fatalf("ChartSeries: %v", err)
		}
		if series.EmptyReason != EmptyReasonNoHistory {
			t.Fatalf("emptyReason = %q", series.EmptyReason)
		}
	}
	if got := sourceCalls.Load(); got != 1 {
		t.Fatalf("source called %d times for the same empty window, want 1", got)
	}
}
