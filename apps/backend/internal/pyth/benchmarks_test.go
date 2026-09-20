package pyth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/marketcal"
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

// etUnix is a wall-clock instant at the exchange, as Benchmarks stamps its bars.
// Written out rather than hardcoded: a UTC epoch is unreadable, and a 1D window is
// now anchored on the exchange's bells, so a test fixture that lands in the wrong
// session has to be obvious on the page.
func etUnix(year int, month time.Month, day, hour, minute int) int64 {
	return time.Date(year, month, day, hour, minute, 0, 0, marketcal.Location()).Unix()
}

func TestBenchmarks_Series_decodesOHLCAndPreviousClose(t *testing.T) {
	t.Parallel()

	var gotQuery string
	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		// Pre-market, then two regular-session bars, all on Tuesday 2026-09-22.
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"s":"ok",
			"t":[%d,%d,%d],
			"o":[229.0,230.0,231.0],
			"h":[229.5,230.9,231.8],
			"l":[228.2,229.7,230.4],
			"c":[229.4,230.5,231.4]
		}`,
			etUnix(2026, time.September, 22, 8, 0),
			etUnix(2026, time.September, 22, 10, 0),
			etUnix(2026, time.September, 22, 11, 0),
		)))
	})

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
	// These are Equity.US.AAPL/USD candles, not AAPLx candles. The series has to say
	// so, or the stats grid built from it ends up beside a token hero price with
	// nothing to explain why the two disagree.
	if series.Basis != PriceBasisUnderlying || series.BasisSymbol != "AAPL" {
		t.Fatalf("basis = %q/%q, want underlying/AAPL", series.Basis, series.BasisSymbol)
	}
	wantOpen := time.Date(2026, time.September, 22, 9, 30, 0, 0, marketcal.Location())
	if !series.RegularOpen.Equal(wantOpen) {
		t.Fatalf("regular open = %s, want %s", series.RegularOpen, wantOpen)
	}
}

func TestBenchmarks_Series_previousCloseIsThePriorRegularSessionClose(t *testing.T) {
	t.Parallel()

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Monday's last regular bar, then a Monday after-hours print, then Tuesday.
		// The after-hours print must not become "previous close": it is not a close.
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"s":"ok",
			"t":[%d,%d,%d,%d],
			"o":[225.0,226.5,229.0,231.0],
			"h":[226.0,227.9,229.5,231.8],
			"l":[224.0,226.4,228.2,230.4],
			"c":[226.5,227.8,229.4,231.4]
		}`,
			etUnix(2026, time.September, 21, 15, 55),
			etUnix(2026, time.September, 21, 18, 0),
			etUnix(2026, time.September, 22, 10, 0),
			etUnix(2026, time.September, 22, 11, 0),
		)))
	})

	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, tradingNoon)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want the 2 bars from the latest session", len(series.Points))
	}
	if series.PreviousCloseUsdcMicros == nil {
		t.Fatal("expected a previous close from the prior session")
	}
	if *series.PreviousCloseUsdcMicros != 226_500_000 {
		t.Fatalf("previous close = %d, want the 15:55 ET close 226500000, not the after-hours print", *series.PreviousCloseUsdcMicros)
	}
}

func TestBenchmarks_Series_1DWindowIsTheLastTradingSessionNotTheLastBar(t *testing.T) {
	t.Parallel()

	// Saturday. Pyth equity feeds keep republishing a frozen last price after the
	// bell and Benchmarks builds bars from published prices, so the payload really
	// does carry weekend bars. Anchoring the window on the last bar made those the
	// session: a run of identical closes drawn as a dead flat line, with Friday's
	// close as its baseline. The calendar knows Saturday is not a session.
	saturday := time.Date(2026, time.September, 26, 16, 0, 0, 0, time.UTC)

	var gotQuery url.Values
	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"s":"ok",
			"t":[%d,%d,%d,%d,%d],
			"c":[220.0,226.0,227.0,227.0,227.0]
		}`,
			etUnix(2026, time.September, 24, 15, 55), // Thursday's close
			etUnix(2026, time.September, 25, 10, 0),  // Friday, in session
			etUnix(2026, time.September, 25, 15, 0),  // Friday, in session
			etUnix(2026, time.September, 26, 9, 0),   // Saturday: frozen republish
			etUnix(2026, time.September, 26, 12, 0),  // Saturday: frozen republish
		)))
	})

	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, saturday)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want only Friday's two bars", len(series.Points))
	}
	for _, point := range series.Points {
		if point.Timestamp >= etUnix(2026, time.September, 26, 0, 0) {
			t.Fatalf("a Saturday bar at %d is in a 1D series", point.Timestamp)
		}
	}
	if series.PreviousCloseUsdcMicros == nil || *series.PreviousCloseUsdcMicros != 220_000_000 {
		t.Fatalf("previous close = %v, want Thursday's close", series.PreviousCloseUsdcMicros)
	}
	// The upstream window is bounded too, so the weekend bars are not even fetched
	// when the feed is well behaved.
	wantTo := etUnix(2026, time.September, 25, 20, 0)
	if got := gotQuery.Get("to"); got != fmt.Sprint(wantTo) {
		t.Fatalf("to = %s, want Friday's 20:00 ET post-close end %d", got, wantTo)
	}
}

func TestBenchmarks_Series_1DWindowSkipsAHoliday(t *testing.T) {
	t.Parallel()

	// Thanksgiving 2026 is Thursday 26 November: the exchange is shut, so the
	// session a 1D chart is about is Wednesday's.
	thanksgiving := time.Date(2026, time.November, 26, 17, 0, 0, 0, time.UTC)

	client := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"s":"ok",
			"t":[%d,%d,%d],
			"c":[240.0,243.0,244.0]
		}`,
			etUnix(2026, time.November, 24, 15, 55), // Tuesday's close
			etUnix(2026, time.November, 25, 10, 0),  // Wednesday, in session
			etUnix(2026, time.November, 26, 11, 0),  // Thanksgiving: frozen republish
		)))
	})

	series, err := client.Series(context.Background(), "AAPLx", ChartRange1D, thanksgiving)
	if err != nil {
		t.Fatalf("Series: %v", err)
	}
	if len(series.Points) != 1 {
		t.Fatalf("points = %d, want Wednesday's single bar", len(series.Points))
	}
	if series.Points[0].Timestamp != etUnix(2026, time.November, 25, 10, 0) {
		t.Fatalf("timestamp = %d, want the Wednesday bar", series.Points[0].Timestamp)
	}
	if series.PreviousCloseUsdcMicros == nil || *series.PreviousCloseUsdcMicros != 240_000_000 {
		t.Fatalf("previous close = %v, want Tuesday's close", series.PreviousCloseUsdcMicros)
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
		_, _ = w.Write([]byte(fmt.Sprintf(`{
			"s":"ok",
			"t":[%d,%d,%d],
			"c":[229.4,0,231.4]
		}`,
			etUnix(2026, time.September, 22, 10, 0),
			etUnix(2026, time.September, 22, 10, 30),
			etUnix(2026, time.September, 22, 11, 0),
		)))
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
