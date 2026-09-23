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

func TestParseChartRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want ChartRange
		ok   bool
	}{
		{raw: "", want: ChartRange1D, ok: true},
		{raw: "1D", want: ChartRange1D, ok: true},
		{raw: "1w", want: ChartRange1W, ok: true},
		{raw: "1M", want: ChartRange1M, ok: true},
		{raw: "3M", want: ChartRange3M, ok: true},
		{raw: "1y", want: ChartRange1Y, ok: true},
		{raw: " all ", want: ChartRangeAll, ok: true},
		{raw: "5Y", ok: false},
		{raw: "1H", ok: false},
	}
	for _, tc := range cases {
		got, err := ParseChartRange(tc.raw)
		if tc.ok {
			if err != nil {
				t.Fatalf("ParseChartRange(%q) err = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("ParseChartRange(%q) = %q, want %q", tc.raw, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Fatalf("ParseChartRange(%q) err = nil, want error", tc.raw)
		}
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestDayChange_isTheUnderlyingAgainstItsPreviousClose(t *testing.T) {
	t.Parallel()
	day := AssetChartSeries{
		Basis:                   PriceBasisUnderlying,
		PreviousCloseUsdcMicros: int64Ptr(200_000_000),
		Points: []ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 199_000_000},
			{Timestamp: 2, PriceUsdcMicros: 202_000_000},
		},
	}
	got := DayChange(day)
	if got == nil || *got != "0.010000" {
		t.Fatalf("DayChange = %v, want 0.010000", got)
	}
}

func TestDayChange_refusesWhatItCannotHonestlyCompute(t *testing.T) {
	t.Parallel()
	points := []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 202_000_000}}
	cases := map[string]AssetChartSeries{
		// Chainlink rounds are the token per token; a ratio against an equity close
		// would fold the multiplier into the move.
		"token basis":       {Basis: PriceBasisToken, PreviousCloseUsdcMicros: int64Ptr(200_000_000), Points: points},
		"no previous close": {Basis: PriceBasisUnderlying, Points: points},
		"empty series":      {Basis: PriceBasisUnderlying, PreviousCloseUsdcMicros: int64Ptr(200_000_000)},
		"zero close":        {Basis: PriceBasisUnderlying, PreviousCloseUsdcMicros: int64Ptr(0), Points: points},
	}
	for name, series := range cases {
		if got := DayChange(series); got != nil {
			t.Errorf("%s: DayChange = %q, want nil", name, *got)
		}
	}
}

func TestHermesClient_ChartSeries_noKeyAndNoSourceNeverCallsHermes(t *testing.T) {
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "")
	series, err := client.ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatalf("ChartSeries: %v", err)
	}
	if len(series.Points) != 0 || series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("series = %+v, want empty with the no-history reason", series)
	}
	if calls.Load() != 0 {
		t.Fatalf("hermes called %d times without a key", calls.Load())
	}
}

func TestHermesClient_ChartSeries_samplerNeverDrawsAFlatLineFromOnePrice(t *testing.T) {
	// Every historical sample fails. The old path then drew a two-point flat line
	// from the latest price and shipped it as the range's history.
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	RegisterFeedID("AAPLc", "feed-aapl")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/updates/price/latest" {
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-aapl","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, _ := client.ChartSeries(context.Background(), "AAPLc", ChartRange1W)
	if len(series.Points) != 0 {
		t.Fatalf("points = %+v, want none", series.Points)
	}
	if series.EmptyReason != EmptyReasonNoHistory {
		t.Fatalf("emptyReason = %q", series.EmptyReason)
	}
}

func TestHermesClient_ChartSeries_samplerLabelsItsBasisAndShipsNoPreviousClose(t *testing.T) {
	resetEquityDeniedForTest()
	ClearFeedRegistry()
	RegisterFeedID("SPCXc", "feed-spcx")
	var asked atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v2/updates/price/") {
			asked.Store(r.URL.Query().Get("ids[]"))
			_, _ = w.Write([]byte(`{"parsed":[{"id":"feed-spcx","price":{"price":"18500000","expo":-5,"publish_time":1714746101}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewHermesClientWithHTTP(server.URL, server.Client(), "test-pyth-key")
	series, _ := client.ChartSeries(context.Background(), "SPCXc", ChartRange1W)
	if series.Source != ChartSourceHermes || series.Range != ChartRange1W {
		t.Fatalf("source/range = %q/%q", series.Source, series.Range)
	}
	if series.Basis != PriceBasisUnderlying || series.BasisSymbol != "SPCX" {
		t.Fatalf("basis = %q/%q, want underlying/SPCX", series.Basis, series.BasisSymbol)
	}
	if series.PreviousCloseUsdcMicros != nil {
		t.Fatal("the sampler must not ship its own first point as a previous close")
	}
	if got, _ := asked.Load().(string); got != "0xfeed-spcx" {
		t.Fatalf("asked for feed %q", got)
	}
}

func TestChartCache_dayLivesAMinuteAndEmptyNeverOutlivesAReal(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC)
	cache := newChartCache(func() time.Time { return now })
	real := AssetChartSeries{Points: []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 1}}}

	cache.set("AAPLc", ChartRange1D, real)
	cache.set("AAPLc", ChartRange1Y, real)
	cache.set("NEWc", ChartRange1Y, AssetChartSeries{EmptyReason: EmptyReasonNoHistory})

	now = now.Add(ChartCacheDayTTL + time.Second)
	if _, ok := cache.get("AAPLc", ChartRange1D); ok {
		t.Fatal("a 1D series must expire after a minute")
	}
	if _, ok := cache.get("aaplc", ChartRange1Y); !ok {
		t.Fatal("a 1Y series must still be cached, whatever the symbol's case")
	}
	now = now.Add(ChartCacheEmptyTTL)
	if _, ok := cache.get("NEWc", ChartRange1Y); ok {
		t.Fatal("an empty answer must expire on the empty TTL")
	}
	if _, ok := cache.get("AAPLc", ChartRange1Y); !ok {
		t.Fatal("a real 1Y series outlives an empty one")
	}
}

func TestChartCache_isKeyedOnTheFeedNotTheSpelling(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC)
	cache := newChartCache(func() time.Time { return now })
	cache.set("AAPLc", ChartRange1D, AssetChartSeries{Points: []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 1}}})

	if _, ok := cache.get("AAPLx", ChartRange1D); !ok {
		t.Fatal("AAPLx is the same Equity.US.AAPL/USD feed and should share the entry")
	}
	// Upper-case C is part of a ticker, not the token suffix: Equity.US.AAPLC/USD.
	if _, ok := cache.get("AAPLC", ChartRange1D); ok {
		t.Fatal("AAPLC is a different feed and must not read AAPLc's series")
	}
}

func TestHermesClient_ChartSeries_servesRepeatsFromTheCache(t *testing.T) {
	ClearFeedRegistry()
	now := time.Now().UTC()
	var sourceCalls atomic.Int32
	source := benchmarksServer(t, func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		_, _ = w.Write([]byte(`{"s":"ok","t":[` + itoa(now.AddDate(0, 0, -2).Unix()) + `,` + itoa(now.AddDate(0, 0, -1).Unix()) + `],"c":[229.4,231.4]}`))
	})
	client := NewHermesClientWithHTTP("http://127.0.0.1:1", nil, "").WithSeriesSource(source)
	for i := 0; i < 3; i++ {
		series, _ := client.ChartSeries(context.Background(), "AAPLc", ChartRange1Y)
		if len(series.Points) != 2 {
			t.Fatalf("points = %d", len(series.Points))
		}
	}
	if sourceCalls.Load() != 1 {
		t.Fatalf("source called %d times, want 1", sourceCalls.Load())
	}
}
