package pyth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func benchmarksServer(t *testing.T, handler http.HandlerFunc) *BenchmarksClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewBenchmarksClientWithHTTP(server.URL, server.Client())
}

// tradingNoon is a Tuesday inside the regular session, so "now" is never a weekend
// in a test that would otherwise be time-of-run dependent.
var tradingNoon = time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC)

func TestBenchmarks_Series_decodesOHLCAndPreviousClose(t *testing.T) {
	t.Parallel()

	var gotQuery string
	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{
			"s":"ok",
			"t":[1758542400,1758553200,1758556800],
			"o":[229.0,230.0,231.0],
			"h":[229.5,230.9,231.8],
			"l":[228.2,229.7,230.4],
			"c":[229.4,230.5,231.4]
		}`))
	})

	// 1758542400 is 2026-09-22 12:00 UTC (08:00 ET, pre-market); the other two are
	// later the same ET day, so all three belong to one session.
	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if !strings.Contains(gotQuery, "symbol=Equity.US.AAPL%2FUSD") {
		t.Fatalf("query = %q, want the equity feed symbol", gotQuery)
	}
	if !strings.Contains(gotQuery, "resolution=5") {
		t.Fatalf("query = %q, want 5-minute bars for 1D", gotQuery)
	}
	if len(series.Points) != 3 {
		t.Fatalf("points = %d, want 3", len(series.Points))
	}
	if series.Points[0].OpenUsdcMicros != 229_000_000 {
		t.Fatalf("open = %d, want 229000000", series.Points[0].OpenUsdcMicros)
	}
	if series.Points[2].PriceUsdcMicros != 231_400_000 {
		t.Fatalf("close = %d, want 231400000", series.Points[2].PriceUsdcMicros)
	}
	if series.Points[1].HighUsdcMicros != 230_900_000 || series.Points[1].LowUsdcMicros != 229_700_000 {
		t.Fatalf("high/low = %d/%d", series.Points[1].HighUsdcMicros, series.Points[1].LowUsdcMicros)
	}
	if series.Source != ChartSourceBenchmarks || series.Range != ChartRange1D {
		t.Fatalf("source/range = %q/%q", series.Source, series.Range)
	}
}

func TestBenchmarks_Series_previousCloseComesFromTheBarBeforeTheWindow(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		// The first bar is 2026-09-21 (the prior ET session); the rest are 09-22.
		_, _ = w.Write([]byte(`{
			"s":"ok",
			"t":[1758470400,1758542400,1758556800],
			"o":[225.0,229.0,231.0],
			"h":[226.0,229.5,231.8],
			"l":[224.0,228.2,230.4],
			"c":[226.5,229.4,231.4]
		}`))
	})

	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want the 2 bars from the latest session", len(series.Points))
	}
	if series.PreviousCloseUsdcMicros == nil {
		t.Fatal("expected a previous close from the prior session's last bar")
	}
	if *series.PreviousCloseUsdcMicros != 226_500_000 {
		t.Fatalf("previous close = %d, want 226500000", *series.PreviousCloseUsdcMicros)
	}
}

func TestBenchmarks_Series_longRangesUseDailyAndWeeklyBars(t *testing.T) {
	t.Parallel()

	cases := map[ChartRange]string{
		ChartRange1W:  "resolution=30",
		ChartRange1M:  "resolution=60",
		ChartRange3M:  "resolution=D",
		ChartRange1Y:  "resolution=D",
		ChartRangeAll: "resolution=W",
	}
	for chartRange, wantResolution := range cases {
		var gotQuery string
		client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.RawQuery
			_, _ = w.Write([]byte(`{"s":"no_data"}`))
		})
		if _, err := client.Series(context.Background(), "AAPLx", chartRange, tradingNoon); err != nil {
			t.Fatalf("%s: %v", chartRange, err)
		}
		if !strings.Contains(gotQuery, wantResolution) {
			t.Fatalf("%s query = %q, want %q", chartRange, gotQuery, wantResolution)
		}
	}
}

func TestBenchmarks_Series_noDataIsAnEmptySeriesNotAnError(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"s":"no_data"}`))
	})

	series, err := client.Series(context.Background(), "NEWx", ChartRange1Y, tradingNoon)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 0 {
		t.Fatalf("points = %d, want 0", len(series.Points))
	}
	if series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("emptyReason = %q, want %q", series.EmptyReason, EmptyReasonNoHistory)
	}
}

func TestBenchmarks_Series_upstreamErrorStatusIsAnError(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"s":"error","errmsg":"unknown symbol"}`))
	})

	if _, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon); err == nil {
		t.Fatal("expected an error so the caller falls back")
	}
}

func TestBenchmarks_Series_rateLimitIsAnError(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`slow down`))
	})

	_, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon)
	if err == nil {
		t.Fatal("expected an error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want the status in the message", err)
	}
}

func TestBenchmarks_Series_malformedBodyIsAnError(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"s":"ok","t":`))
	})

	if _, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestBenchmarks_Series_mismatchedArrayLengthsAreRejected(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"s":"ok","t":[1758542400,1758546000],"c":[229.4]}`))
	})

	// A payload that disagrees with itself cannot be plotted honestly: pairing the
	// wrong close with a timestamp would put a real price at the wrong minute.
	if _, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon); err == nil {
		t.Fatal("expected an error when timestamps and closes disagree")
	}
}

func TestBenchmarks_Series_partialPayloadKeepsUsableBars(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Middle bar has no price; OHLC arrays are short, which the shim does for
		// feeds that only publish a close.
		_, _ = w.Write([]byte(`{
			"s":"ok",
			"t":[1758542400,1758546000,1758556800],
			"c":[229.4,0,231.4]
		}`))
	})

	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want the 2 priced bars", len(series.Points))
	}
	for _, point := range series.Points {
		if point.OpenUsdcMicros != point.PriceUsdcMicros {
			t.Fatalf("open = %d, want the close when the shim omits opens", point.OpenUsdcMicros)
		}
	}
}

func TestBenchmarks_Series_networkFailureIsAnError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := NewBenchmarksClientWithHTTP(server.URL, server.Client())
	server.Close() // nothing is listening any more

	if _, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon); err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestBenchmarks_Series_contextCancellationIsAnError(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.Series(ctx, "AAPLx", ChartRange1D, tradingNoon); err == nil {
		t.Fatal("expected the request to fail when the caller's deadline passes")
	}
}

func TestDownsample_keepsEndpointsAndRespectsTheCap(t *testing.T) {
	t.Parallel()

	bars := make([]ohlcBar, 0, 2_000)
	for i := 0; i < 2_000; i++ {
		bars = append(bars, ohlcBar{timestamp: int64(1_700_000_000 + i*60), close: int64(1_000_000 + i)})
	}
	out := downsample(bars, maxChartPoints)
	if len(out) > maxChartPoints {
		t.Fatalf("len = %d, want <= %d", len(out), maxChartPoints)
	}
	if out[0] != bars[0] {
		t.Fatal("first bar must survive downsampling")
	}
	if out[len(out)-1] != bars[len(bars)-1] {
		t.Fatal("last bar must survive downsampling")
	}
	for i := 1; i < len(out); i++ {
		if out[i].timestamp <= out[i-1].timestamp {
			t.Fatalf("downsampled series is not strictly increasing at %d", i)
		}
	}
}

func TestHermesClient_ChartSeries_fallsBackToSamplingWhenTheSourceFails(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var historyCalls atomic.Int32
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			historyCalls.Add(1)
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer hermes.Close()

	failing := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := NewHermesClientWithHTTP(hermes.URL, hermes.Client(), "test-pyth-key").WithSeriesSource(failing)
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1M)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if series.Source != ChartSourceHermes {
		t.Fatalf("source = %q, want the hermes fallback", series.Source)
	}
	if len(series.Points) == 0 {
		t.Fatal("fallback produced no points")
	}
	if historyCalls.Load() == 0 {
		t.Fatal("expected the fallback to sample the historical endpoint")
	}
}

func TestHermesClient_ChartSeries_breakerStopsRetryingAFailedSource(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer hermes.Close()

	var sourceCalls atomic.Int32
	failing := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := NewHermesClientWithHTTP(hermes.URL, hermes.Client(), "test-pyth-key").WithSeriesSource(failing)
	for _, chartRange := range []ChartRange{ChartRange1M, ChartRange3M, ChartRange1Y} {
		if _, err := client.ChartSeries(context.Background(), "AAPLx", chartRange); err != nil {
			t.Fatalf("ChartSeries(%s): %v", chartRange, err)
		}
	}
	if got := sourceCalls.Load(); got != 1 {
		t.Fatalf("source called %d times, want 1 before the breaker opened", got)
	}
}

func TestHermesClient_ChartSeries_sourceEmptySeriesSkipsTheFanOut(t *testing.T) {
	ClearFeedRegistry()
	RegisterFeedID("AAPLx", "feed-aapl")

	var historyCalls atomic.Int32
	hermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			historyCalls.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer hermes.Close()

	empty := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"s":"no_data"}`))
	})

	client := NewHermesClientWithHTTP(hermes.URL, hermes.Client(), "test-pyth-key").WithSeriesSource(empty)
	series, err := client.ChartSeries(context.Background(), "AAPLx", ChartRange1Y)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("emptyReason = %q", series.EmptyReason)
	}
	if historyCalls.Load() != 0 {
		t.Fatalf("history calls = %d, want 0 — the source already said there is none", historyCalls.Load())
	}
}
