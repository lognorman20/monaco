package pyth

import (
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/marketcal"
)

func intPtr(v int64) *int64 { return &v }

// tuesdaySession is the regular cash session on Tuesday 2026-09-22, the same day
// the benchmarks tests use.
func tuesdaySession() (open, close time.Time) {
	session, ok := marketcal.SessionOn(time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC))
	if !ok {
		panic("2026-09-22 should be a trading day")
	}
	return session.RegularOpen, session.RegularClose
}

func TestSessionStats_foldsOnlyTheRegularSession(t *testing.T) {
	t.Parallel()

	// The 1D window spans the extended session on purpose, so the chart can draw
	// pre- and post-market. The Open cell is the 09:30 print, not the 04:00 one, and
	// a pre-market spike or an after-hours dump is not the day's high or low —
	// Robinhood's grid is the regular session's and ours has to mean the same thing.
	open, closeAt := tuesdaySession()
	series := AssetChartSeries{
		RegularOpen:  open,
		RegularClose: closeAt,
		Points: []ChartPoint{
			// 08:00 ET pre-market, with the session's extreme high.
			{Timestamp: etUnix(2026, time.September, 22, 8, 0), PriceUsdcMicros: 240_000_000, OpenUsdcMicros: 239_000_000, HighUsdcMicros: 241_000_000, LowUsdcMicros: 238_000_000},
			{Timestamp: etUnix(2026, time.September, 22, 9, 30), PriceUsdcMicros: 229_400_000, OpenUsdcMicros: 229_000_000, HighUsdcMicros: 229_500_000, LowUsdcMicros: 228_200_000},
			{Timestamp: etUnix(2026, time.September, 22, 12, 0), PriceUsdcMicros: 231_400_000, OpenUsdcMicros: 231_000_000, HighUsdcMicros: 231_800_000, LowUsdcMicros: 230_400_000},
			// 18:00 ET after-hours, with the session's extreme low.
			{Timestamp: etUnix(2026, time.September, 22, 18, 0), PriceUsdcMicros: 210_000_000, OpenUsdcMicros: 211_000_000, HighUsdcMicros: 212_000_000, LowUsdcMicros: 209_000_000},
		},
	}

	stats := SessionStats(series)
	if stats.OpenUsdcMicros == nil || *stats.OpenUsdcMicros != 229_000_000 {
		t.Fatalf("open = %v, want the 09:30 bar's open, not the pre-market print", stats.OpenUsdcMicros)
	}
	if stats.HighUsdcMicros == nil || *stats.HighUsdcMicros != 231_800_000 {
		t.Fatalf("high = %v, want the regular session's high, not the pre-market spike", stats.HighUsdcMicros)
	}
	if stats.LowUsdcMicros == nil || *stats.LowUsdcMicros != 228_200_000 {
		t.Fatalf("low = %v, want the regular session's low, not the after-hours dump", stats.LowUsdcMicros)
	}
}

func TestSessionStats_carriesTheBasisOfTheSeriesItFolded(t *testing.T) {
	t.Parallel()

	// These cells are NASDAQ dollars. The detail response puts them next to the
	// xStock's on-chain price, which trades at a premium, so a hero price above the
	// day's high is two instruments rather than a bug — but only if the grid says
	// which instrument it is about.
	stats := SessionStats(AssetChartSeries{
		Basis:       PriceBasisUnderlying,
		BasisSymbol: "AAPL",
		Points:      []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 229_000_000}},
	})
	if stats.Basis != PriceBasisUnderlying || stats.BasisSymbol != "AAPL" {
		t.Fatalf("basis = %q/%q, want underlying/AAPL", stats.Basis, stats.BasisSymbol)
	}
}

func TestAssetStats_hasFigures(t *testing.T) {
	t.Parallel()

	// A basis label alone is not a grid: a response with nothing sourced must still
	// be omitted whole.
	labelled := AssetStats{Basis: PriceBasisUnderlying, BasisSymbol: "AAPL"}
	if labelled.HasFigures() {
		t.Fatal("a grid with only a label in it has no figures")
	}
	if !(AssetStats{SpreadBps: new(int)}).HasFigures() {
		t.Fatal("a sourced spread is a figure")
	}
}

func TestSessionStats_fromCandles(t *testing.T) {
	t.Parallel()

	series := AssetChartSeries{
		PreviousCloseUsdcMicros: intPtr(226_500_000),
		Points: []ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 229_400_000, OpenUsdcMicros: 229_000_000, HighUsdcMicros: 229_500_000, LowUsdcMicros: 228_200_000},
			{Timestamp: 2, PriceUsdcMicros: 231_400_000, OpenUsdcMicros: 231_000_000, HighUsdcMicros: 231_800_000, LowUsdcMicros: 230_400_000},
		},
	}

	stats := SessionStats(series)
	if stats.OpenUsdcMicros == nil || *stats.OpenUsdcMicros != 229_000_000 {
		t.Fatalf("open = %v, want the first bar's open", stats.OpenUsdcMicros)
	}
	if stats.HighUsdcMicros == nil || *stats.HighUsdcMicros != 231_800_000 {
		t.Fatalf("high = %v, want 231800000", stats.HighUsdcMicros)
	}
	if stats.LowUsdcMicros == nil || *stats.LowUsdcMicros != 228_200_000 {
		t.Fatalf("low = %v, want 228200000", stats.LowUsdcMicros)
	}
	if stats.PreviousCloseUsdcMicros == nil || *stats.PreviousCloseUsdcMicros != 226_500_000 {
		t.Fatalf("previous close = %v", stats.PreviousCloseUsdcMicros)
	}
}

func TestSessionStats_closeOnlySeriesUsesCloses(t *testing.T) {
	t.Parallel()

	// The Hermes fallback has no candles. High and low are then the extremes of the
	// sampled closes, which is the most that source can honestly support.
	series := AssetChartSeries{Points: []ChartPoint{
		{Timestamp: 1, PriceUsdcMicros: 229_000_000},
		{Timestamp: 2, PriceUsdcMicros: 232_000_000},
		{Timestamp: 3, PriceUsdcMicros: 230_000_000},
	}}

	stats := SessionStats(series)
	if *stats.OpenUsdcMicros != 229_000_000 {
		t.Fatalf("open = %d", *stats.OpenUsdcMicros)
	}
	if *stats.HighUsdcMicros != 232_000_000 || *stats.LowUsdcMicros != 229_000_000 {
		t.Fatalf("high/low = %d/%d", *stats.HighUsdcMicros, *stats.LowUsdcMicros)
	}
	if stats.PreviousCloseUsdcMicros != nil {
		t.Fatal("no previous close was supplied, so none should be claimed")
	}
}

func TestSessionStats_emptySeriesClaimsNothing(t *testing.T) {
	t.Parallel()

	stats := SessionStats(AssetChartSeries{EmptyReason: EmptyReasonNoHistory})
	if stats.OpenUsdcMicros != nil || stats.HighUsdcMicros != nil || stats.LowUsdcMicros != nil {
		t.Fatalf("stats = %+v, want every cell absent", stats)
	}
}

func TestWeek52Range_usesTheWholeYearSeries(t *testing.T) {
	t.Parallel()

	series := AssetChartSeries{Points: []ChartPoint{
		{Timestamp: 1, PriceUsdcMicros: 164_000_000, HighUsdcMicros: 169_000_000, LowUsdcMicros: 163_000_000},
		{Timestamp: 2, PriceUsdcMicros: 260_000_000, HighUsdcMicros: 262_000_000, LowUsdcMicros: 259_000_000},
	}}

	high, low := Week52Range(series)
	if high == nil || *high != 262_000_000 {
		t.Fatalf("52w high = %v, want 262000000", high)
	}
	if low == nil || *low != 163_000_000 {
		t.Fatalf("52w low = %v, want 163000000", low)
	}
}

func TestWeek52Range_emptySeriesHasNoRange(t *testing.T) {
	t.Parallel()

	high, low := Week52Range(AssetChartSeries{})
	if high != nil || low != nil {
		t.Fatalf("high/low = %v/%v, want both absent for a symbol with no year of history", high, low)
	}
}

func TestConfToUSDCMicros_rejectsGarbageWithoutLosingTheMark(t *testing.T) {
	t.Parallel()

	if got := confToUSDCMicros("3000", -5); got != 30_000 {
		t.Fatalf("conf = %d, want 30000", got)
	}
	if got := confToUSDCMicros("", -5); got != 0 {
		t.Fatalf("missing conf = %d, want 0", got)
	}
	if got := confToUSDCMicros("not-a-number", -5); got != 0 {
		t.Fatalf("malformed conf = %d, want 0", got)
	}
}
