package httpapi

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

const (
	// sparkBudget bounds the whole sparkline fan-out for one page of rows.
	//
	// It only applies to a history client that cannot answer from memory. The
	// production chain implements pyth.CachedSeriesSource, so the list path is a
	// map lookup per row and never reaches this at all; the budget is the floor
	// under a client that has no cache of its own, and it is now a real one — the
	// chain honours a caller's deadline, so entering ChartSeries no longer means
	// waiting out its own 20s fetch timeout.
	//
	// A row whose series does not arrive is a row without a sparkline, which the
	// client draws correctly. It is not an error, and it is not worth a slow page.
	sparkBudget = 1200 * time.Millisecond
	// sparkConcurrency caps the simultaneous upstream reads a single cold page can
	// start, so one uncached page cannot spend the whole rate-limit allowance.
	sparkConcurrency = 6
	// symbolLookupBudget bounds resolving a set of symbols back to catalogue rows.
	symbolLookupBudget = 2 * time.Second
	// symbolLookupConcurrency caps that fan-out. Unbounded, a cabal with a dozen
	// holdings was a dozen simultaneous catalogue reads per poll tick per viewer.
	symbolLookupConcurrency = 4
	// maxRowSymbols caps how many symbols one response will decorate. A member in
	// many cabals must not turn one screen into an unbounded fan-out; the symbols
	// past the cap still ship as rows, just without market figures.
	maxRowSymbols = 40
)

// MarketRowSource builds the market half of a stock row: the current mark, the
// day change, and the day's closes a sparkline is drawn from.
//
// One type for every screen that lists a stock. The Stocks tab, the held/up-for-
// vote sections and a cabal's holdings all show the same instrument, so they read
// it the same way, from the same caches, with the same budgets. Before this, only
// the catalogue routes knew how, and the cabal screen showed a stock with no day
// change at all.
//
// Every field is optional. A nil client means that part of a row is simply
// absent, never an error: a list is worth showing without a sparkline, and never
// worth failing over one.
type MarketRowSource struct {
	Catalog xstocks.CatalogSearcher
	// Pyth backs the day series only; display marks come from Price.
	Pyth  pyth.AssetPriceClient
	Price jupiter.PriceClient
}

// LookupAsset resolves one symbol in the catalogue, through its symbol index.
//
// It used to go through Search, and Search walks every catalogue page. Resolving
// a ticker that way made each held symbol its own full crawl of a third-party
// API — twelve of them, at once, per poll tick, per viewer, on the cabal screen.
// The index is one crawl, shared, and a lookup against it is a map read.
func (s *MarketRowSource) LookupAsset(ctx context.Context, symbol string) (xstocks.CatalogAsset, bool, error) {
	if s == nil || s.Catalog == nil {
		return xstocks.CatalogAsset{}, false, nil
	}
	if strings.TrimSpace(symbol) == "" {
		return xstocks.CatalogAsset{}, false, nil
	}
	return s.Catalog.LookupBySymbol(ctx, symbol)
}

// Prices batches current USD marks for assets in one Jupiter Price API call
// instead of a per-asset round trip. A nil client or a failed fetch degrades to
// "no price" rather than erroring the whole response.
func (s *MarketRowSource) Prices(ctx context.Context, assets []xstocks.CatalogAsset) map[string]jupiter.TokenPrice {
	if s == nil || s.Price == nil {
		return nil
	}
	mints := make([]string, 0, len(assets))
	for _, asset := range assets {
		if mint := strings.TrimSpace(asset.SolanaMint); mint != "" {
			mints = append(mints, mint)
		}
	}
	prices, err := s.Price.Prices(ctx, mints)
	if err != nil {
		return nil
	}
	return prices
}

// rowSeries is one asset's day series with the instrument it is about recorded
// alongside it. The two are inseparable: a series and a basis it is not labelled
// with is how AAPL's shape ended up tinted by AAPLx's move.
type rowSeries struct {
	spark       []int64
	basis       string
	basisSymbol string
}

// Sparklines returns each asset's day series, keyed by Solana mint and already
// downsampled to what a row draws.
//
// Keyed by mint rather than symbol because that is what the price map is keyed
// by, and one lookup key per row is one chance to get it wrong.
//
// A history client that can answer from memory is read directly, with no
// goroutines and no budget, because there is nothing to wait for. Everything else
// goes through the bounded fan-out below.
//
// A pre-IPO token has no price history to draw, so its row is never asked for
// one: a cache miss would otherwise send a background fill upstream on every page.
func (s *MarketRowSource) Sparklines(ctx context.Context, assets []xstocks.CatalogAsset) map[string]rowSeries {
	if s == nil || s.Pyth == nil || len(assets) == 0 {
		return nil
	}
	charted := make([]xstocks.CatalogAsset, 0, len(assets))
	for _, asset := range assets {
		if asset.Normalize().Kind != xstocks.AssetKindPreIPO {
			charted = append(charted, asset)
		}
	}
	if cached, ok := s.Pyth.(pyth.CachedSeriesSource); ok {
		return s.cachedSparklines(ctx, cached, charted)
	}
	return s.fetchedSparklines(ctx, charted)
}

// cachedSparklines reads every row's series out of the client's cache. A miss is
// a row without a sparkline now and a background fill for the next request; this
// path never blocks on a vendor.
func (s *MarketRowSource) cachedSparklines(
	ctx context.Context,
	cached pyth.CachedSeriesSource,
	assets []xstocks.CatalogAsset,
) map[string]rowSeries {
	out := make(map[string]rowSeries, len(assets))
	for _, asset := range assets {
		mint := strings.TrimSpace(asset.SolanaMint)
		symbol := strings.TrimSpace(asset.Symbol)
		if mint == "" || symbol == "" {
			continue
		}
		series, ok := cached.CachedChartSeries(ctx, symbol, pyth.ChartRange1D)
		if !ok {
			continue
		}
		if row, ok := rowSeriesFrom(series); ok {
			out[mint] = row
		}
	}
	return out
}

// fetchedSparklines is the fallback for a history client with no cache of its
// own: a bounded fan-out under a real deadline.
func (s *MarketRowSource) fetchedSparklines(ctx context.Context, assets []xstocks.CatalogAsset) map[string]rowSeries {
	ctx, cancel := context.WithTimeout(ctx, sparkBudget)
	defer cancel()

	out := make(map[string]rowSeries, len(assets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, sparkConcurrency)

	for _, asset := range assets {
		mint := strings.TrimSpace(asset.SolanaMint)
		symbol := strings.TrimSpace(asset.Symbol)
		if mint == "" || symbol == "" {
			continue
		}
		wg.Add(1)
		go func(mint, symbol string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			series, err := s.Pyth.ChartSeries(ctx, symbol, pyth.ChartRange1D)
			if err != nil {
				return
			}
			row, ok := rowSeriesFrom(series)
			if !ok {
				return
			}
			mu.Lock()
			out[mint] = row
			mu.Unlock()
		}(mint, symbol)
	}
	wg.Wait()
	return out
}

func rowSeriesFrom(series pyth.AssetChartSeries) (rowSeries, bool) {
	spark := pyth.SparkFromSeries(series, pyth.DefaultSparkPoints)
	if len(spark) == 0 {
		return rowSeries{}, false
	}
	return rowSeries{spark: spark, basis: series.Basis, basisSymbol: series.BasisSymbol}, true
}

// Enrich turns catalogue assets into list rows, reading the marks and the day
// series together: a page that waited for one and then the other would pay both
// budgets in a row for data neither needs from the other.
func (s *MarketRowSource) Enrich(ctx context.Context, assets []xstocks.CatalogAsset) []marketAssetResponse {
	var (
		prices map[string]jupiter.TokenPrice
		sparks map[string]rowSeries
		wg     sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		prices = s.Prices(ctx, assets)
	}()
	go func() {
		defer wg.Done()
		sparks = s.Sparklines(ctx, assets)
	}()
	wg.Wait()

	out := make([]marketAssetResponse, 0, len(assets))
	for _, asset := range assets {
		out = append(out, marketAssetResponseFor(asset, prices, sparks))
	}
	return out
}

// RowsForSymbols resolves each symbol in the catalogue and decorates it, keyed by
// upper-cased symbol.
//
// A symbol that cannot be resolved still gets a row — symbol and name only — so a
// position the member really holds never disappears from their own screen because
// the catalogue was slow.
func (s *MarketRowSource) RowsForSymbols(ctx context.Context, symbols []string) map[string]marketAssetResponse {
	rows := make(map[string]marketAssetResponse, len(symbols))
	if s == nil || len(symbols) == 0 {
		return rows
	}

	lookupCtx, cancel := context.WithTimeout(ctx, symbolLookupBudget)
	defer cancel()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		resolved []xstocks.CatalogAsset
	)
	slots := make(chan struct{}, symbolLookupConcurrency)
	seen := map[string]bool{}
	lookups := 0
	for _, raw := range symbols {
		symbol := strings.TrimSpace(raw)
		key := strings.ToUpper(symbol)
		if symbol == "" || seen[key] {
			continue
		}
		seen[key] = true
		rows[key] = marketAssetResponse{Symbol: symbol, Name: symbol}
		if lookups >= maxRowSymbols {
			// Past the cap the row still ships; it just carries no market figures.
			// An unbounded fan-out is the thing a screen cannot survive.
			continue
		}
		lookups++

		wg.Add(1)
		go func(symbol, key string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-lookupCtx.Done():
				return
			}
			asset, found, err := s.LookupAsset(lookupCtx, symbol)
			if err != nil || !found {
				return
			}
			mu.Lock()
			resolved = append(resolved, asset)
			mu.Unlock()
		}(symbol, key)
	}
	wg.Wait()

	for _, row := range s.Enrich(ctx, resolved) {
		rows[strings.ToUpper(strings.TrimSpace(row.Symbol))] = row
	}
	return rows
}
