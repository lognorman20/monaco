package pyth

import "log/slog"

func logFeedLookup(symbol string, feedID string, err error) {
	if err != nil {
		slog.Warn("pyth feed lookup failed", "symbol", symbol, "err", err)
		return
	}
	slog.Info("pyth feed lookup", "symbol", symbol, "feed_id", feedID)
}

func logLatestPrice(feedID string, err error) {
	if err != nil {
		slog.Warn("pyth latest price failed", "feed_id", feedID, "err", err)
		return
	}
	slog.Info("pyth latest price", "feed_id", feedID, "ok", true)
}

func logHistoricalPrice(symbol, feedID string, atUnix int64, err error) {
	if err != nil {
		slog.Warn("pyth historical price failed", "symbol", symbol, "feed_id", feedID, "timestamp", atUnix, "err", err)
	}
}

func logChartSeries(symbol string, chartRange ChartRange, requested, failed, pointCount int) {
	slog.Info("pyth chart series",
		"symbol", symbol,
		"range", chartRange,
		"requested_samples", requested,
		"failed_samples", failed,
		"point_count", pointCount,
	)
}

// logSeriesSource records a one-call history source failing over to the Hermes
// sampler. Charts keep working either way, so this log is the only place the
// switch is visible.
func logSeriesSource(symbol string, chartRange ChartRange, err error) {
	slog.Warn("pyth series source failed, falling back to hermes samples",
		"symbol", symbol,
		"range", chartRange,
		"err", err,
	)
}

func logReferenceQuotes(symbol string, equityStatus, tokenStatus QuoteStatus) {
	slog.Info("pyth reference quotes", "symbol", symbol, "equity", equityStatus, "token", tokenStatus)
}
