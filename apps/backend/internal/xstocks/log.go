package xstocks

import "log/slog"

func logResolveMint(symbol, mint string, err error) {
	if err != nil {
		slog.Warn("xstocks resolve mint failed", "symbol", symbol, "err", err)
		return
	}
	slog.Info("xstocks resolve mint", "symbol", symbol, "mint", mint)
}

func logCatalogSearch(query string, count int, err error) {
	if err != nil {
		slog.Warn("xstocks catalog search failed", "query", query, "err", err)
		return
	}
	slog.Info("xstocks catalog search", "query", query, "results", count)
}
