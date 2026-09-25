package httpapi

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

// cachedSeriesClient is a history client shaped like the production chain: it
// answers a row's sparkline from memory only, and a miss is a miss.
type cachedSeriesClient struct {
	pyth.AssetPriceClient
	mu     sync.Mutex
	cached map[string]pyth.AssetChartSeries
	warms  atomic.Int32
}

func newCachedSeriesClient() *cachedSeriesClient {
	return &cachedSeriesClient{
		AssetPriceClient: pyth.NewFakeAssetPriceClient(),
		cached:           map[string]pyth.AssetChartSeries{},
	}
}

func (c *cachedSeriesClient) put(symbol string, series pyth.AssetChartSeries) {
	c.mu.Lock()
	c.cached[symbol] = series
	c.mu.Unlock()
}

func (c *cachedSeriesClient) CachedChartSeries(_ context.Context, symbol string, _ pyth.ChartRange) (pyth.AssetChartSeries, bool) {
	c.mu.Lock()
	series, ok := c.cached[symbol]
	c.mu.Unlock()
	if !ok {
		c.warms.Add(1)
	}
	return series, ok
}

// ChartSeries here is the path a row must never take. If a list row reaches it,
// this test fails rather than hangs.
func (c *cachedSeriesClient) ChartSeries(context.Context, string, pyth.ChartRange) (pyth.AssetChartSeries, error) {
	panic("a list row must not fetch a series upstream")
}

func underlyingDaySeries() pyth.AssetChartSeries {
	series := dayCandles(78)
	series.Basis = pyth.PriceBasisUnderlying
	series.BasisSymbol = "AAPL"
	return series
}

// The row path reads memory. The point of the whole exercise: a list route's
// sparkline budget is now unspendable rather than merely documented.
func TestMarketRowSource_aCachedClientNeverFetchesForARow(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	cached := newCachedSeriesClient()
	source.Pyth = cached
	asset := registerApple(t, source)
	cached.put("AAPLx", underlyingDaySeries())

	start := time.Now()
	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a cached page took %s", elapsed)
	}
	if len(rows) != 1 || len(rows[0].Spark) == 0 {
		t.Fatalf("rows = %+v, want one row with a sparkline", rows)
	}
}

// A cold symbol is a row without a line, right now, and a warm for next time.
func TestMarketRowSource_aColdSymbolIsARowWithoutALineNotAWait(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	cached := newCachedSeriesClient()
	source.Pyth = cached
	asset := registerApple(t, source)

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Spark != nil {
		t.Fatalf("spark = %v, want none on a cold symbol", rows[0].Spark)
	}
	if rows[0].PriceUsdcMicros == nil {
		t.Fatal("a cold series must not cost the row its price")
	}
	if cached.warms.Load() == 0 {
		t.Fatal("a miss should have asked the client to warm the symbol")
	}
}

// The fallback path — a history client with no cache — must respect the budget
// rather than run to the vendor's own timeout.
func TestMarketRowSource_anUncachedClientStillStopsAtTheBudget(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	pyth.RegisterChartSeries(source.Pyth, "AAPLx", pyth.ChartRange1D, dayCandles(78))
	pyth.RegisterChartSeriesDelay(source.Pyth, "AAPLx", pyth.ChartRange1D, 30*time.Second)

	start := time.Now()
	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("the page took %s; sparkBudget is %s", elapsed, sparkBudget)
	}
	if len(rows) != 1 || rows[0].Spark != nil {
		t.Fatalf("rows = %+v, want one row that gave up on its sparkline", rows)
	}
}

// countingCatalog records how many lookups a response makes at once, which is the
// figure that used to be "one per holding, all at the same time".
type countingCatalog struct {
	xstocks.CatalogSearcher
	mu      sync.Mutex
	live    int
	peak    int
	lookups int
}

func (c *countingCatalog) LookupBySymbol(ctx context.Context, symbol string) (xstocks.CatalogAsset, bool, error) {
	c.mu.Lock()
	c.live++
	c.lookups++
	if c.live > c.peak {
		c.peak = c.live
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.live--
		c.mu.Unlock()
	}()

	time.Sleep(20 * time.Millisecond)
	return c.CatalogSearcher.LookupBySymbol(ctx, symbol)
}

func (c *countingCatalog) peakConcurrency() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak
}

// A twelve-holding cabal used to be twelve simultaneous catalogue reads per poll
// tick per viewer. Sparklines had a cap; this did not.
func TestMarketRowSource_rowsForSymbolsBoundsItsFanOut(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	counting := &countingCatalog{CatalogSearcher: source.Catalog}
	registerApple(t, source)
	source.Catalog = counting

	symbols := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		symbols = append(symbols, "SYM"+string(rune('A'+i))+"x")
	}
	source.RowsForSymbols(context.Background(), symbols)

	if peak := counting.peakConcurrency(); peak > symbolLookupConcurrency {
		t.Fatalf("peak concurrent catalogue lookups = %d, want at most %d", peak, symbolLookupConcurrency)
	}
}

// Past the cap a row still ships — the member really holds it — it just carries
// no market figures.
func TestMarketRowSource_rowsForSymbolsCapsHowManySymbolsItResolves(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	counting := &countingCatalog{CatalogSearcher: source.Catalog}
	source.Catalog = counting

	symbols := make([]string, 0, maxRowSymbols+10)
	for i := 0; i < maxRowSymbols+10; i++ {
		symbols = append(symbols, "SYM"+string(rune('A'+i%26))+string(rune('a'+i/26))+"x")
	}
	rows := source.RowsForSymbols(context.Background(), symbols)

	if len(rows) != len(symbols) {
		t.Fatalf("rows = %d, want one per symbol (%d)", len(rows), len(symbols))
	}
	counting.mu.Lock()
	lookups := counting.lookups
	counting.mu.Unlock()
	if lookups > maxRowSymbols {
		t.Fatalf("catalogue lookups = %d, want at most %d", lookups, maxRowSymbols)
	}
}

// Issue #336: the row has a logo on it. The field existed and was set by nothing,
// so every row in the app fell back to its ticker tile.
func TestMarketRowSource_theRowCarriesTheCatalogueLogo(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple xStock",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
		LogoURL:    "https://xstocks-metadata.backed.fi/logos/tokens/AAPLx.png",
	}
	xstocks.RegisterCatalogAsset(source.Catalog, asset)

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if rows[0].LogoURL != asset.LogoURL {
		t.Fatalf("logoUrl = %q, want the catalogue logo", rows[0].LogoURL)
	}
}

// A row mixes two instruments — Jupiter's token move and Pyth's equity series —
// and must say so, or the app tints Apple's shape by AAPLx's day.
func TestMarketRowSource_theRowNamesTheInstrumentBehindEachFigure(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	cached := newCachedSeriesClient()
	source.Pyth = cached
	asset := registerApple(t, source)
	change := "0.0124"
	jupiter.RegisterPrice(source.Price, jupiter.AAPLxMint, jupiter.TokenPrice{
		PriceUsdcMicros: 232_050_000,
		Change24h:       &change,
	})
	cached.put("AAPLx", underlyingDaySeries())

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	row := rows[0]

	if row.SparkBasis != pyth.PriceBasisUnderlying || row.SparkBasisSymbol != "AAPL" {
		t.Fatalf("spark basis = %q/%q, want underlying/AAPL", row.SparkBasis, row.SparkBasisSymbol)
	}
	if row.ChangeBasis != pyth.PriceBasisToken || row.ChangeBasisSymbol != "AAPLx" {
		t.Fatalf("change basis = %q/%q, want token/AAPLx", row.ChangeBasis, row.ChangeBasisSymbol)
	}
	if row.SparkBasis == row.ChangeBasis {
		t.Fatal("the whole point is that these two are different instruments")
	}
}

// A row with no day change has nothing to attribute, so it claims nothing.
func TestMarketRowSource_noChangeMeansNoChangeBasis(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if rows[0].ChangeBasis != "" {
		t.Fatalf("changeBasis = %q, want empty when there is no change", rows[0].ChangeBasis)
	}
}

// A pre-IPO token has no price history, so its row neither draws a line nor sends
// a fill upstream for one. Its price still arrives.
func TestMarketRowSource_aPreIPORowIsNeverChartedButStillPriced(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	cached := newCachedSeriesClient()
	source.Pyth = cached
	const mint = "TSPXcLV76s6V2zDiZQ18kBfcbnjaE2ZzNT3ga2Pd99v"
	asset := xstocks.CatalogAsset{
		Symbol:     "tSpaceX",
		Name:       "T-SpaceX",
		SolanaMint: mint,
		Kind:       xstocks.AssetKindPreIPO,
		Decimals:   9,
	}
	jupiter.RegisterPrice(source.Price, mint, jupiter.TokenPrice{PriceUsdcMicros: 562_000_000})

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Spark != nil {
		t.Fatalf("spark = %v, want none for a pre-IPO token", rows[0].Spark)
	}
	if warms := cached.warms.Load(); warms != 0 {
		t.Fatalf("chart reads = %d, want 0 for a pre-IPO token", warms)
	}
	if rows[0].PriceUsdcMicros == nil || *rows[0].PriceUsdcMicros != 562_000_000 {
		t.Fatalf("price = %v, want 562000000", rows[0].PriceUsdcMicros)
	}
	if rows[0].Kind != string(xstocks.AssetKindPreIPO) || rows[0].TokenDecimals != 9 {
		t.Fatalf("kind/decimals = %q/%d, want pre_ipo/9", rows[0].Kind, rows[0].TokenDecimals)
	}
}
