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
