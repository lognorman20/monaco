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
	// Every one of these reads is served from the chart cache the detail screen
	// and the warmer already fill, so the warm path costs microseconds. The budget
	// is for the cold path: a list must never wait on Hermes. A row whose series
	// does not arrive in time is a row without a sparkline, which the client draws
	// correctly — it is not an error, and it is not worth a slow page.
	sparkBudget = 1200 * time.Millisecond
	// sparkConcurrency caps the simultaneous upstream reads a single cold page can
	// start, so one uncached page cannot spend the whole rate-limit allowance.
	sparkConcurrency = 6
	// symbolLookupBudget bounds resolving a set of symbols back to catalogue rows.
	symbolLookupBudget = 2 * time.Second
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

// LookupAsset resolves one symbol in the catalogue.
func (s *MarketRowSource) LookupAsset(ctx context.Context, symbol string) (xstocks.CatalogAsset, bool, error) {
	if s == nil || s.Catalog == nil {
		return xstocks.CatalogAsset{}, false, nil
	}
	page, err := s.Catalog.Search(ctx, symbol, 5, 0)
	if err != nil {
		return xstocks.CatalogAsset{}, false, err
	}
	needle := strings.ToUpper(strings.TrimSpace(symbol))
	for _, asset := range page.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Symbol), needle) {
			return asset, true, nil
		}
	}
	return xstocks.CatalogAsset{}, false, nil
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

// Sparklines returns each asset's day series, keyed by Solana mint and already
// downsampled to what a row draws.
//
// Keyed by mint rather than symbol because that is what the price map is keyed
// by, and one lookup key per row is one chance to get it wrong.
func (s *MarketRowSource) Sparklines(ctx context.Context, assets []xstocks.CatalogAsset) map[string][]int64 {
	if s == nil || s.Pyth == nil || len(assets) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, sparkBudget)
	defer cancel()

	out := make(map[string][]int64, len(assets))
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
			spark := pyth.SparkFromSeries(series, pyth.DefaultSparkPoints)
			if len(spark) == 0 {
				return
			}
			mu.Lock()
			out[mint] = spark
			mu.Unlock()
		}(mint, symbol)
	}
	wg.Wait()
	return out
}

// Enrich turns catalogue assets into list rows, reading the marks and the day
// series together: a page that waited for one and then the other would pay both
// budgets in a row for data neither needs from the other.
func (s *MarketRowSource) Enrich(ctx context.Context, assets []xstocks.CatalogAsset) []marketAssetResponse {
	var (
		prices map[string]jupiter.TokenPrice
		sparks map[string][]int64
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
	seen := map[string]bool{}
	for _, raw := range symbols {
		symbol := strings.TrimSpace(raw)
		key := strings.ToUpper(symbol)
		if symbol == "" || seen[key] {
			continue
		}
		seen[key] = true
		rows[key] = marketAssetResponse{Symbol: symbol, Name: symbol}

		wg.Add(1)
		go func(symbol, key string) {
			defer wg.Done()
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
