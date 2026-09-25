package pyth

import (
	"context"
	"testing"
	"time"
)

// yahooDaySeries assembles a Yahoo 1D series for Tuesday 2026-09-22 as it would
// look at now: Monday's closing bar for the previous close, then whichever of the
// day's bars had printed by then.
func yahooDaySeries(t *testing.T, now time.Time, bars ...OHLCBar) AssetChartSeries {
	t.Helper()
	all := append([]OHLCBar{bar(etUnix(2026, time.September, 21, 15, 55), 226_500_000)}, bars...)
	var printed []OHLCBar
	for _, b := range all {
		if b.Timestamp <= now.Unix() {
			printed = append(printed, b)
		}
	}
	series := AssembleRangeSeries(ChartRange1D, now, printed)
	series.Range = ChartRange1D
	series.Source = ChartSourceYahoo
	series.Basis = PriceBasisUnderlying
	series.BasisSymbol = "AAPL"
	return series
}

func bar(timestamp, closeMicros int64) OHLCBar {
	return OHLCBar{Timestamp: timestamp, Open: closeMicros, High: closeMicros, Low: closeMicros, Close: closeMicros}
}

// tuesdayBars is a pre-market print, the regular session's first and last bars,
// and an after-hours print, all at different prices so a test can tell which one
// became the quote.
var tuesdayBars = []OHLCBar{
	bar(etUnix(2026, time.September, 22, 8, 0), 227_000_000),
	bar(etUnix(2026, time.September, 22, 9, 30), 228_000_000),
	bar(etUnix(2026, time.September, 22, 11, 55), 229_500_000),
	bar(etUnix(2026, time.September, 22, 15, 55), 231_400_000),
	bar(etUnix(2026, time.September, 22, 17, 30), 232_900_000),
}

func TestEquityQuoteFromDaySeries_afterTheBellIsTheClosingPrint(t *testing.T) {
	now := time.Date(2026, time.September, 22, 22, 0, 0, 0, time.UTC) // 18:00 ET
	quote, ok := equityQuoteFromDaySeries(yahooDaySeries(t, now, tuesdayBars...), now)
	if !ok {
		t.Fatal("expected an equity quote from the day series")
	}
	if quote.Source != QuoteSourceYahoo {
		t.Fatalf("source = %q, want yahoo: this is not a Pyth print", quote.Source)
	}
	// The 17:30 after-hours print is not what the equity feed would have shown.
	if quote.PriceUsdcMicros != 231_400_000 {
		t.Fatalf("price = %d, want the last regular-session close", quote.PriceUsdcMicros)
	}
	wantClose := time.Date(2026, time.September, 22, 20, 0, 0, 0, time.UTC)
	if quote.Status != QuoteStatusStale || !quote.PublishedAt.Equal(wantClose) {
		t.Fatalf("status %q at %s, want stale at the 16:00 ET bell (%s)", quote.Status, quote.PublishedAt, wantClose)
	}

	// A series cached at 15:58 and read after the bell never saw the close, so it
	// keeps its last bar's own time rather than claiming a closing print.
	cachedAt := time.Date(2026, time.September, 22, 19, 58, 0, 0, time.UTC)
	quote, _ = equityQuoteFromDaySeries(yahooDaySeries(t, cachedAt, tuesdayBars...), now)
	if want := time.Unix(etUnix(2026, time.September, 22, 15, 55), 0).UTC(); !quote.PublishedAt.Equal(want) || quote.Status != QuoteStatusStale {
		t.Fatalf("status %q at %s, want stale at the last bar's own time %s", quote.Status, quote.PublishedAt, want)
	}
}

func TestEquityQuoteFromDaySeries_duringTheSessionIsLiveOnlyWhileRecent(t *testing.T) {
	now := time.Date(2026, time.September, 22, 16, 0, 0, 0, time.UTC) // 12:00 ET
	quote, ok := equityQuoteFromDaySeries(yahooDaySeries(t, now, tuesdayBars...), now)
	if !ok {
		t.Fatal("expected an equity quote from the day series")
	}
	if quote.PriceUsdcMicros != 229_500_000 || quote.Status != QuoteStatusLive {
		t.Fatalf("quote = %+v, want the 11:55 bar, live", quote)
	}

	// The same series read twenty minutes later, as a cached series would be, is a
	// real price but not a live one.
	later := now.Add(20 * time.Minute)
	quote, _ = equityQuoteFromDaySeries(yahooDaySeries(t, now, tuesdayBars...), later)
	if quote.Status != QuoteStatusStale {
		t.Fatalf("status = %q twenty minutes after the bar, want stale", quote.Status)
	}
	if want := time.Unix(etUnix(2026, time.September, 22, 11, 55), 0).UTC(); !quote.PublishedAt.Equal(want) {
		t.Fatalf("publishedAt = %s, want the bar's own time %s", quote.PublishedAt, want)
	}
}

func TestEquityQuoteFromDaySeries_beforeTheOpenIsThePreviousClose(t *testing.T) {
	now := time.Date(2026, time.September, 22, 12, 30, 0, 0, time.UTC) // 08:30 ET
	quote, ok := equityQuoteFromDaySeries(yahooDaySeries(t, now, tuesdayBars...), now)
	if !ok {
		t.Fatal("expected the previous close before the session has printed")
	}
	// The 08:00 pre-market print is not a regular-session price.
	if quote.PriceUsdcMicros != 226_500_000 {
		t.Fatalf("price = %d, want Monday's close", quote.PriceUsdcMicros)
	}
	mondayClose := time.Date(2026, time.September, 21, 20, 0, 0, 0, time.UTC)
	if quote.Status != QuoteStatusStale || !quote.PublishedAt.Equal(mondayClose) {
		t.Fatalf("status %q at %s, want stale at Monday's bell", quote.Status, quote.PublishedAt)
	}
}

func TestEquityQuoteFromDaySeries_refusesAnythingButYahoosUnderlying(t *testing.T) {
	now := time.Date(2026, time.September, 22, 22, 0, 0, 0, time.UTC)

	token := yahooDaySeries(t, now, tuesdayBars...)
	token.Source, token.Basis = ChartSourceJupiter, PriceBasisToken
	if _, ok := equityQuoteFromDaySeries(token, now); ok {
		t.Fatal("a Jupiter series is the token, and must never become the stock's price")
	}

	empty := AssetChartSeries{Source: ChartSourceYahoo, Basis: PriceBasisUnderlying, EmptyReason: EmptyReasonNoHistory}
	if _, ok := equityQuoteFromDaySeries(empty, now); ok {
		t.Fatal("an empty series has no price to give")
	}
}

func TestWithEquityQuoteFallback_fillsAnUnentitledEquityLegFromTheDayChart(t *testing.T) {
	now := time.Date(2026, time.September, 22, 22, 0, 0, 0, time.UTC)
	primary := NewFakeReferenceQuoteClient()
	token := ReferenceQuote{Source: QuoteSourcePythCrypto, Status: QuoteStatusLive, PriceUsdcMicros: 232_050_000}
	RegisterReferenceQuotes(primary, "AAPLx", ReferenceQuotes{
		Symbol: "AAPLx",
		Equity: unavailableQuote(QuoteSourcePythEquity, QuoteReasonNotEntitled),
		Token:  token,
	})
	charts := NewFakeAssetPriceClient()
	RegisterChartSeries(charts, "AAPLx", ChartRange1D, yahooDaySeries(t, now, tuesdayBars...))

	client := WithEquityQuoteFallback(primary, charts).(*equityQuoteFallback)
	client.now = func() time.Time { return now }

	quotes, err := client.ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Source != QuoteSourceYahoo || quotes.Equity.PriceUsdcMicros != 231_400_000 {
		t.Fatalf("equity = %+v, want Yahoo's closing print", quotes.Equity)
	}
	if quotes.Token != token {
		t.Fatalf("token = %+v, want the primary's token leg untouched", quotes.Token)
	}
	if quotes.PremiumBps() == nil {
		t.Fatal("with both legs priced the card has a premium to show")
	}
}

func TestWithEquityQuoteFallback_aPythEquityQuoteAlwaysWins(t *testing.T) {
	primary := NewFakeReferenceQuoteClient()
	equity := ReferenceQuote{Source: QuoteSourcePythEquity, Status: QuoteStatusLive, PriceUsdcMicros: 231_000_000}
	RegisterReferenceQuotes(primary, "AAPLx", ReferenceQuotes{Symbol: "AAPLx", Equity: equity})
	charts := NewFakeAssetPriceClient()

	quotes, err := WithEquityQuoteFallback(primary, charts).ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity != equity {
		t.Fatalf("equity = %+v, want the Pyth quote", quotes.Equity)
	}
	if got := ChartSeriesCallCount(charts, "AAPLx", ChartRange1D); got != 0 {
		t.Fatalf("day chart read %d times, want none when Pyth answered", got)
	}
}

func TestWithEquityQuoteFallback_leavesTheLegUnavailableWithoutAYahooSeries(t *testing.T) {
	primary := NewFakeReferenceQuoteClient()
	charts := NewFakeAssetPriceClient()
	// No series registered: the chart client answers with an empty one.
	quotes, err := WithEquityQuoteFallback(primary, charts).ReferenceQuotes(context.Background(), "AAPLx")
	if err != nil {
		t.Fatalf("ReferenceQuotes: %v", err)
	}
	if quotes.Equity.Status != QuoteStatusUnavailable || quotes.Equity.Reason != QuoteReasonNoFeed {
		t.Fatalf("equity = %+v, want the primary's unavailable leg and its reason", quotes.Equity)
	}
	if WithEquityQuoteFallback(primary, nil) != primary {
		t.Fatal("with no chart client the primary is returned as is")
	}
}
