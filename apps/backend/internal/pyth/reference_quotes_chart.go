package pyth

import (
	"context"
	"log/slog"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/marketcal"
)

// WithEquityQuoteFallback fills an unavailable equity leg from the underlying's
// day chart.
//
// Our Pyth key is not entitled to US equity feeds, so the equity leg came back
// unavailable on every asset screen and the card read "This feed is not part of
// our plan", right under a chart that was drawing the very same equity from Yahoo.
// The last regular-session bar of that day series is a real price for the
// underlying, and it goes out labelled as Yahoo's so nothing reads it as a Pyth
// print. Only the equity leg is touched, and only when Pyth had nothing: a Pyth
// equity quote that did come back always wins, and the token leg is whatever the
// primary said.
//
// charts should be the same chart client the asset screen reads, so the day
// series is the one already in its cache and a poll costs a memory read rather
// than a vendor call.
func WithEquityQuoteFallback(primary ReferenceQuoteClient, charts AssetPriceClient) ReferenceQuoteClient {
	if primary == nil || charts == nil {
		return primary
	}
	return &equityQuoteFallback{primary: primary, charts: charts, now: time.Now}
}

type equityQuoteFallback struct {
	primary ReferenceQuoteClient
	charts  AssetPriceClient
	now     func() time.Time
}

// ReferenceQuotes implements ReferenceQuoteClient.
func (q *equityQuoteFallback) ReferenceQuotes(ctx context.Context, symbol string) (ReferenceQuotes, error) {
	quotes, err := q.primary.ReferenceQuotes(ctx, symbol)
	if err != nil || quotes.Equity.Status != QuoteStatusUnavailable {
		return quotes, err
	}
	series, err := q.charts.ChartSeries(ctx, symbol, ChartRange1D)
	if err != nil {
		return quotes, nil
	}
	equity, ok := equityQuoteFromDaySeries(series, q.now().UTC())
	if !ok {
		return quotes, nil
	}
	logEquityQuoteFallback(symbol, quotes.Equity.Reason, equity)
	quotes.Equity = equity
	return quotes, nil
}

// equityQuoteFromDaySeries reads the equity leg off a 1D series of the underlying.
//
// It follows what a Pyth equity feed would have said, because the app already
// knows how to word that. The price is the regular session's, never a pre- or
// post-market print, since that is what the equity feed publishes. Once the
// series runs past the closing bell the quote is the closing print, stale and
// stamped at the bell, which the card reads as "Closed 4:00 PM". Before the
// session has a bar, it is the previous session's close. During the session it
// is live only if its bar is as recent as a live Pyth quote would have to be.
func equityQuoteFromDaySeries(series AssetChartSeries, now time.Time) (ReferenceQuote, bool) {
	source, ok := equityQuoteSourceFor(series)
	if !ok {
		return ReferenceQuote{}, false
	}
	last, found := latestPoint(regularSessionPoints(series))
	if !found {
		return previousCloseQuote(series, source)
	}
	if last.PriceUsdcMicros <= 0 {
		return ReferenceQuote{}, false
	}
	quote := ReferenceQuote{
		Source:          source,
		Status:          QuoteStatusStale,
		PriceUsdcMicros: last.PriceUsdcMicros,
		PublishedAt:     time.Unix(last.Timestamp, 0).UTC(),
	}
	if regularSessionComplete(series) {
		quote.PublishedAt = series.RegularClose.UTC()
		return quote, true
	}
	if now.Sub(quote.PublishedAt) <= ReferenceQuoteMaxAge {
		quote.Status = QuoteStatusLive
	}
	return quote, true
}

// equityQuoteSourceFor accepts only a Yahoo series of the underlying. Yahoo is the
// one history source we have that prices the equity without a Pyth entitlement,
// which is exactly what the primary leg is missing. A Jupiter series is the token,
// not the stock, and a Benchmarks or sampler series is Pyth's own equity feed,
// which the primary leg already reports on directly.
func equityQuoteSourceFor(series AssetChartSeries) (QuoteSource, bool) {
	if series.Basis != PriceBasisUnderlying || series.Source != ChartSourceYahoo {
		return "", false
	}
	return QuoteSourceYahoo, true
}

// previousCloseQuote is the equity leg before the session has printed: the last
// regular close, stamped at the previous session's bell when the calendar knows it.
func previousCloseQuote(series AssetChartSeries, source QuoteSource) (ReferenceQuote, bool) {
	if series.PreviousCloseUsdcMicros == nil || *series.PreviousCloseUsdcMicros <= 0 {
		return ReferenceQuote{}, false
	}
	quote := ReferenceQuote{
		Source:          source,
		Status:          QuoteStatusStale,
		PriceUsdcMicros: *series.PreviousCloseUsdcMicros,
	}
	if !series.RegularOpen.IsZero() {
		if previous, ok := marketcal.PreviousTradingSession(series.RegularOpen); ok {
			quote.PublishedAt = previous.RegularClose
		}
	}
	return quote, true
}

// regularSessionComplete reports whether the series reaches past the closing bell,
// which is the only proof it holds the session's last regular bar. A series cached
// a few minutes before the close does not, and stamping its last bar at 16:00
// would claim a closing print it never saw.
func regularSessionComplete(series AssetChartSeries) bool {
	if series.RegularClose.IsZero() {
		return false
	}
	closeUnix := series.RegularClose.Unix()
	for _, point := range series.Points {
		if point.Timestamp >= closeUnix {
			return true
		}
	}
	return false
}

func latestPoint(points []ChartPoint) (ChartPoint, bool) {
	var latest ChartPoint
	found := false
	for _, point := range points {
		if !found || point.Timestamp > latest.Timestamp {
			latest, found = point, true
		}
	}
	return latest, found
}

func logEquityQuoteFallback(symbol, pythReason string, quote ReferenceQuote) {
	slog.Debug("pyth reference equity quote from day chart",
		"symbol", symbol,
		"source", quote.Source,
		"status", quote.Status,
		"pyth_reason", pythReason,
	)
}
