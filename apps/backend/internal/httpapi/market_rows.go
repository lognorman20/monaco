package httpapi

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const (
	// catalogPriceBudget bounds the reads a catalog page costs, so a slow RPC or a
	// slow Pyth costs a missing price, change or sparkline, never a hung list.
	catalogPriceBudget = 4 * time.Second
	// rowDecorationBudget bounds the market figures on screens that are about
	// something else: a cabal's holdings (which the cabal screen polls) and the
	// Stocks tab's held and up-for-vote sections. Their own figures are the point;
	// the market's are decoration, and a slow Chainlink RPC or Benchmarks must not
	// hold the cabal screen for the full catalog budget.
	rowDecorationBudget = 2 * time.Second
	// dayChangeConcurrency bounds how many 1D series one page asks Pyth for at
	// once. They are cached for a minute and the spark warmer keeps the popular
	// ones hot, so a warm page costs none.
	dayChangeConcurrency = 4
	// maxRowSymbols caps how many symbols one response will decorate. A member in
	// many cabals must not turn one screen into an unbounded fan-out; the symbols
	// past the cap still ship as rows, just without market figures.
	maxRowSymbols = 40
)

// MarketRowSource builds the market half of a stock row: the token's Chainlink
// mark, the underlying's day change, and the day's closes a sparkline is drawn
// from.
//
// One type for every screen that lists a stock. The Stocks tab, the held and
// up-for-vote sections and a cabal's holdings all show the same instrument, so
// they read it the same way, from the same caches, under the same bounds. Before
// this only the catalog routes knew how, and a cabal's holdings showed a stock
// with no day move at all.
//
// Where each figure comes from:
//   - priceUsdcMicros is the Chainlink total-return mark, per token, the same
//     mark pot valuation uses;
//   - change24h and spark are both read from one Pyth 1D Benchmarks series of the
//     underlying equity, per share, so they are the same instrument over the same
//     window and each carries its basis;
//   - logoUrl is the image the issuer publishes in the token's own ERC-7572
//     contractURI metadata, only when it is https on the issuer's metadata host.
//
// Every dependency is optional. A nil client means that part of a row is absent,
// never an error: a list is worth showing without a sparkline and never worth
// failing over one.
type MarketRowSource struct {
	Catalog b20.Catalog
	// Marks serves the hero price (Chainlink, with Pyth charts in front of its
	// rounds). Only AssetMarks is read here.
	Marks pyth.AssetPriceClient
	// Charts is Pyth history alone. A row never falls back to Chainlink rounds for
	// its day move or its line: a token curve beside an equity change would be two
	// instruments under one row.
	Charts pyth.MarketDataClient
	// Logos resolves each token's logo from the issuer's on-chain ERC-7572
	// metadata (b20.NewContractLogos). Nil ships rows without logoUrl, and the app
	// draws the ticker tile.
	Logos b20.LogoSource
}

// LogoURLs resolves each asset's issuer logo, keyed by token address, under a
// bounded fan-out. Answers are cached for a day per token, so a warm page costs
// no chain reads; a token whose logo cannot be resolved is simply absent.
func (s *MarketRowSource) LogoURLs(ctx context.Context, assets []b20.Asset) map[string]string {
	out := make(map[string]string, len(assets))
	if s == nil || s.Logos == nil || len(assets) == 0 {
		return out
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, dayChangeConcurrency)
	)
	for _, asset := range assets {
		key := tokenKey(asset.TokenAddress)
		if key == "" {
			continue
		}
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if logo := s.Logos.LogoURL(ctx, key); logo != "" {
				mu.Lock()
				out[key] = logo
				mu.Unlock()
			}
		}(key)
	}
	wg.Wait()
	return out
}

// LookupAsset resolves one symbol in the catalog: an exact symbol match first,
// then the catalog's own resolution (which accepts a legacy AAPLx spelling).
func (s *MarketRowSource) LookupAsset(ctx context.Context, symbol string) (b20.Asset, bool, error) {
	if s == nil || s.Catalog == nil {
		return b20.Asset{}, false, nil
	}
	needle := strings.TrimSpace(symbol)
	if needle == "" {
		return b20.Asset{}, false, nil
	}
	page, err := s.Catalog.Search(ctx, needle, 25, 0)
	if err != nil {
		return b20.Asset{}, false, err
	}
	for _, asset := range page.Assets {
		if strings.EqualFold(strings.TrimSpace(asset.Symbol), needle) {
			return asset, true, nil
		}
	}
	addr, err := s.Catalog.ResolveTokenAddress(ctx, needle)
	if err != nil || strings.TrimSpace(addr) == "" {
		return b20.Asset{}, false, nil
	}
	return s.Catalog.LookupByAddress(ctx, addr)
}

// assetPriceSnapshot is one current Chainlink mark, with when it was struck.
type assetPriceSnapshot struct {
	PriceUsdcMicros int64
	Mark            pyth.AssetMark
}

// Prices loads current marks in one batched read, keyed by token address. A nil
// client or a failed fetch degrades to "no price" rather than failing the page.
func (s *MarketRowSource) Prices(ctx context.Context, assets []b20.Asset) map[string]assetPriceSnapshot {
	if s == nil || s.Marks == nil || len(assets) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(assets))
	for _, asset := range assets {
		symbols = append(symbols, asset.Symbol)
	}
	marks, err := s.Marks.AssetMarks(ctx, symbols)
	if err != nil {
		return nil
	}
	out := make(map[string]assetPriceSnapshot, len(assets))
	for _, asset := range assets {
		mark, ok := marks[asset.Symbol]
		if !ok || mark.PriceUsdcMicros <= 0 {
			continue
		}
		out[tokenKey(asset.TokenAddress)] = assetPriceSnapshot{PriceUsdcMicros: mark.PriceUsdcMicros, Mark: mark}
	}
	return out
}

// rowDay is what a row takes from one asset's 1D series: the day move and the
// line, each with the instrument it is about. They are kept together because a
// line and a change that are not labelled is how a row ends up drawing one
// instrument and tinting it by another.
type rowDay struct {
	change      *string
	spark       []int64
	basis       string
	basisSymbol string
}

// DaySeries reads each asset's 1D Pyth series once, under a bounded fan-out, and
// reduces it to a day change and a sparkline. Keyed by token address, like the
// prices. An asset whose series is late, empty or unusable simply has neither.
func (s *MarketRowSource) DaySeries(ctx context.Context, assets []b20.Asset) map[string]rowDay {
	out := make(map[string]rowDay, len(assets))
	if s == nil || s.Charts == nil || len(assets) == 0 {
		return out
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, dayChangeConcurrency)
	)
	for _, asset := range assets {
		key := tokenKey(asset.TokenAddress)
		symbol := strings.TrimSpace(asset.Symbol)
		if key == "" || symbol == "" {
			continue
		}
		wg.Add(1)
		go func(key, symbol string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			series, ok := s.Charts.DaySeries(ctx, symbol)
			if !ok {
				return
			}
			day, ok := rowDayFrom(series)
			if !ok {
				return
			}
			mu.Lock()
			out[key] = day
			mu.Unlock()
		}(key, symbol)
	}
	wg.Wait()
	return out
}

func rowDayFrom(series pyth.AssetChartSeries) (rowDay, bool) {
	day := rowDay{change: pyth.DayChange(series)}
	if spark := pyth.SparkFromSeries(series, pyth.DefaultSparkPoints); len(spark) > 1 {
		day.spark = spark
		day.basis = series.Basis
		day.basisSymbol = series.BasisSymbol
	}
	if day.change == nil && day.spark == nil {
		return rowDay{}, false
	}
	return day, true
}

// Enrich turns catalog assets into list rows under the catalog budget.
func (s *MarketRowSource) Enrich(ctx context.Context, assets []b20.Asset) []marketAssetResponse {
	return s.enrich(ctx, assets, catalogPriceBudget)
}

// enrich reads the marks, the day series and the logos together: a page that
// waited for one and then the next would pay each budget in a row for data none
// needs from the others.
func (s *MarketRowSource) enrich(ctx context.Context, assets []b20.Asset, budget time.Duration) []marketAssetResponse {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	var (
		prices map[string]assetPriceSnapshot
		days   map[string]rowDay
		logos  map[string]string
		wg     sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		prices = s.Prices(ctx, assets)
	}()
	go func() {
		defer wg.Done()
		days = s.DaySeries(ctx, assets)
	}()
	go func() {
		defer wg.Done()
		logos = s.LogoURLs(ctx, assets)
	}()
	wg.Wait()

	out := make([]marketAssetResponse, 0, len(assets))
	for _, asset := range assets {
		row := marketAssetResponseFor(asset, prices, days)
		row.LogoURL = logos[tokenKey(asset.TokenAddress)]
		out = append(out, row)
	}
	return out
}

// RowsForSymbols resolves each symbol in the catalog and decorates it, keyed by
// upper-cased symbol, under the decoration budget.
//
// A symbol that cannot be resolved still gets a row (symbol and name only), so a
// position the member really holds never disappears from their own screen because
// the catalog does not know it.
func (s *MarketRowSource) RowsForSymbols(ctx context.Context, symbols []string) map[string]marketAssetResponse {
	rows := make(map[string]marketAssetResponse, len(symbols))
	if s == nil || len(symbols) == 0 {
		return rows
	}

	var (
		resolved []b20.Asset
		// askedAs is the key each resolved asset was asked for by, so the answer
		// lands under the caller's spelling even when the catalog resolved a legacy
		// one (AAPLx) to its own (AAPLc).
		askedAs []string
	)
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
			continue
		}
		lookups++
		asset, found, err := s.LookupAsset(ctx, symbol)
		if err != nil || !found {
			continue
		}
		resolved = append(resolved, asset)
		askedAs = append(askedAs, key)
	}
	if len(resolved) == 0 {
		return rows
	}

	for i, row := range s.enrich(ctx, resolved, rowDecorationBudget) {
		rows[askedAs[i]] = row
	}
	return rows
}

func tokenKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func marketAssetResponseFor(asset b20.Asset, prices map[string]assetPriceSnapshot, days map[string]rowDay) marketAssetResponse {
	resp := marketAssetResponse{
		Symbol:       asset.Symbol,
		Name:         asset.Name,
		TokenAddress: asset.TokenAddress,
		Routable:     asset.Routable || strings.TrimSpace(asset.TokenAddress) != "",
	}
	key := tokenKey(asset.TokenAddress)
	if price, ok := prices[key]; ok && price.PriceUsdcMicros > 0 {
		resp.PriceUsdcMicros = &price.PriceUsdcMicros
	}
	if day, ok := days[key]; ok {
		resp.Change24h, resp.Change24hBasis, resp.Change24hBasisSymbol = dayChangeFields(asset.Symbol, day.change)
		if len(day.spark) > 1 {
			resp.Spark = day.spark
			resp.SparkBasis = day.basis
			resp.SparkBasisSymbol = day.basisSymbol
		}
	}
	return resp
}
