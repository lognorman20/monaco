package pyth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHermesClient_ChartSeries_servesHistoryWithNoAPIKey(t *testing.T) {
	// The chart client is built with whatever PYTH_API_KEY is, including "". Hermes
	// refuses every request without a key, but Benchmarks is a public endpoint and
	// takes none — which is the whole reason the price chain skips its entitlement
	// gate for a keyless source. Building the client inside a `PythAPIKey != ""`
	// branch put the two back together and cost every chart on a deployment with no
	// key at all, while .env.example promised operators the opposite.
	ClearFeedRegistry()

	// ChartSeries reads the wall clock, so the bars are placed relative to it.
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
	if !keyless.HasKeylessHistory() {
		t.Fatal("a client with a Benchmarks source has keyless history whatever Hermes thinks of our key")
	}

	series, err := keyless.ChartSeries(context.Background(), "AAPLx", ChartRange1Y)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) != 2 || series.Source != ChartSourceBenchmarks {
		t.Fatalf("series = %+v, want two Benchmarks points", series)
	}
}

func TestHermesClient_HasKeylessHistory_goesFalseWhenTheSourceBreakerTrips(t *testing.T) {
	// With Benchmarks out, the only history path left is the Hermes sampler, which
	// does need the entitlement — so the chain's gate has to come back.
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer hermes.Close()

	failing := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := NewHermesClientWithHTTP(hermes.URL, hermes.Client(), "test-pyth-key").WithSeriesSource(failing)
	if !client.HasKeylessHistory() {
		t.Fatal("expected keyless history before the source has failed")
	}
	if _, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1Y); err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if client.HasKeylessHistory() {
		t.Fatal("the breaker is open, so there is no keyless history and the entitlement gate must return")
	}
}

func TestHermesClient_ChartSeries_cachesAnEmptyAnswer(t *testing.T) {
	// "This feed has nothing in this window" is an upstream answer. Not caching it
	// made the symbols with no history the most expensive ones in the catalog: the
	// detail route asks for two ranges, and the asset screen polls.
	ClearFeedRegistry()

	var sourceCalls atomic.Int32
	empty := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		_, _ = w.Write([]byte(`{"s":"no_data"}`))
	})

	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(empty)
	for i := 0; i < 3; i++ {
		series, err := client.ChartSeries(context.Background(), "NEWx", ChartRange1Y)
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
