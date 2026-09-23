package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/b20"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
)

const rowsAppleToken = "0xb200000000000000000000c2e324d24d7eecd1fb"

func newMarketRowSource(t *testing.T) (*MarketRowSource, pyth.AssetPriceClient) {
	t.Helper()
	charts := pyth.NewFakeAssetPriceClient()
	return &MarketRowSource{
		Catalog: b20.NewFakeCatalog(),
		Marks:   pyth.NewFakeAssetPriceClient(),
		Charts:  charts.(pyth.MarketDataClient),
	}, charts
}

// registerRowApple lists AAPLc with a Chainlink mark of $232.05.
func registerRowApple(t *testing.T, source *MarketRowSource) b20.Asset {
	t.Helper()
	asset := b20.Asset{Symbol: "AAPLc", Name: "Apple", TokenAddress: rowsAppleToken, Routable: true}
	b20.RegisterCatalogAsset(source.Catalog, asset)
	pyth.RegisterAssetMark(source.Marks, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 232_050_000})
	return asset
}

// underlyingDayCandles is a Benchmarks 1D series of AAPL: count five-minute bars
// rising from $226.50, after a $228 previous close.
func underlyingDayCandles(count int) pyth.AssetChartSeries {
	points := make([]pyth.ChartPoint, 0, count)
	for i := 0; i < count; i++ {
		points = append(points, pyth.ChartPoint{
			Timestamp:       int64(i * 300),
			PriceUsdcMicros: int64(226_500_000 + i*60_000),
		})
	}
	return pyth.AssetChartSeries{
		Points:                  points,
		Source:                  pyth.ChartSourceBenchmarks,
		Basis:                   pyth.PriceBasisUnderlying,
		BasisSymbol:             "AAPL",
		PreviousCloseUsdcMicros: int64Ptr(228_000_000),
	}
}

func TestMarketRowSource_enrichAttachesTheDaySeriesAndTheMark(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(78))

	rows := source.Enrich(context.Background(), []b20.Asset{asset})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	row := rows[0]
	if len(row.Spark) != pyth.DefaultSparkPoints {
		t.Fatalf("spark = %d points, want %d", len(row.Spark), pyth.DefaultSparkPoints)
	}
	if row.PriceUsdcMicros == nil || *row.PriceUsdcMicros != 232_050_000 {
		t.Fatalf("price = %v, want the Chainlink mark 232050000", row.PriceUsdcMicros)
	}
	if row.Change24h == nil {
		t.Fatal("change24h missing: the series carries a previous close")
	}
	if row.LogoURL != "" {
		t.Fatalf("logoUrl = %q with no logo source; nothing may be invented", row.LogoURL)
	}
}

// The line and the pill are the same instrument on Base: both are Pyth's
// underlying equity from one 1D series. The row says so for each, so the app can
// tell when they ever disagree.
func TestMarketRowSource_theRowNamesTheInstrumentBehindEachFigure(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(78))

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.SparkBasis != pyth.PriceBasisUnderlying || row.SparkBasisSymbol != "AAPL" {
		t.Fatalf("spark basis = %q/%q, want underlying/AAPL", row.SparkBasis, row.SparkBasisSymbol)
	}
	if row.Change24hBasis != pyth.PriceBasisUnderlying || row.Change24hBasisSymbol != "AAPL" {
		t.Fatalf("change basis = %q/%q, want underlying/AAPL", row.Change24hBasis, row.Change24hBasisSymbol)
	}
}

// A series that is not the underlying's is the token's own move, labelled as the
// token's — not nothing.
//
// This test used to assert the opposite, back when Pyth was expected to serve
// every equity and a token series could only mean something had gone sideways.
// Benchmarks' history endpoint 404s and our key is crypto-only, so for an equity
// Pyth answers nothing at all; refusing a token-basis change now means refusing
// every pill on the tab. The token's rounds are a real move of the thing the
// member actually holds, and saying whose move it is was always the rule that
// mattered. What is still forbidden is mixing: both ends of the ratio come from
// one series, so the pill and the line name the same instrument and SparkTint
// can still match them.
func TestMarketRowSource_aTokenSeriesIsTheTokensOwnDayMove(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	series := underlyingDayCandles(10)
	series.Basis = pyth.PriceBasisToken
	series.BasisSymbol = "AAPLc"
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, series)

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.Change24h == nil {
		t.Fatal("change24h missing: a token series carries a real move of the token")
	}
	if row.Change24hBasis != pyth.PriceBasisToken || row.Change24hBasisSymbol != "AAPLc" {
		t.Fatalf("change basis = %q/%q, want token/AAPLc", row.Change24hBasis, row.Change24hBasisSymbol)
	}
	if len(row.Spark) == 0 || row.SparkBasis != pyth.PriceBasisToken || row.SparkBasisSymbol != "AAPLc" {
		t.Fatalf("spark = %d points basis %q/%q, want a token-labelled line", len(row.Spark), row.SparkBasis, row.SparkBasisSymbol)
	}
	if row.Change24hBasis != row.SparkBasis {
		t.Fatalf("pill %q and line %q disagree about the instrument", row.Change24hBasis, row.SparkBasis)
	}
}

// The fall-through the Chainlink chart source exists for: Pyth has nothing for
// this symbol, and the row is drawn from the token's own feed instead of coming
// back blank. It is reached through Marks, the marks client with Pyth charts in
// front of its own rounds, which is the same series the chart route draws.
func TestMarketRowSource_aRowFallsThroughToTheTokensOwnFeed(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	// Nothing on Charts: Benchmarks could not answer, as it cannot for any equity
	// on a crypto-only key.
	series := underlyingDayCandles(40)
	series.Source = pyth.ChartSourceChainlink
	series.Basis = pyth.PriceBasisToken
	series.BasisSymbol = "AAPLc"
	pyth.RegisterChartSeries(source.Marks, "AAPLc", pyth.ChartRange1D, series)

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.Change24h == nil {
		t.Fatal("change24h missing: the token's own feed can answer when Pyth cannot")
	}
	if row.Change24hBasis != pyth.PriceBasisToken || row.Change24hBasisSymbol != "AAPLc" {
		t.Fatalf("change basis = %q/%q, want token/AAPLc", row.Change24hBasis, row.Change24hBasisSymbol)
	}
	if len(row.Spark) == 0 || row.SparkBasis != pyth.PriceBasisToken {
		t.Fatalf("spark = %d points basis %q, want a token-labelled line", len(row.Spark), row.SparkBasis)
	}
}

// Pyth still wins when it can answer: the fallback is a fallback, not a
// replacement. An equity move is what "AAPL is up 1.2% today" means, and it must
// not be displaced by the token's because the token's read happened to be warm.
func TestMarketRowSource_pythWinsOverTheTokensOwnFeed(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(78))
	token := underlyingDayCandles(40)
	token.Basis = pyth.PriceBasisToken
	token.BasisSymbol = "AAPLc"
	pyth.RegisterChartSeries(source.Marks, "AAPLc", pyth.ChartRange1D, token)

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.Change24hBasis != pyth.PriceBasisUnderlying || row.Change24hBasisSymbol != "AAPL" {
		t.Fatalf("change basis = %q/%q, want underlying/AAPL", row.Change24hBasis, row.Change24hBasisSymbol)
	}
	if row.SparkBasis != pyth.PriceBasisUnderlying {
		t.Fatalf("spark basis = %q, want underlying", row.SparkBasis)
	}
}

func TestMarketRowSource_symbolWithNoHistoryStillShipsARow(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	// Nothing registered: Benchmarks could not answer for this symbol.

	rows := source.Enrich(context.Background(), []b20.Asset{asset})
	if len(rows) != 1 || rows[0].Symbol != "AAPLc" {
		t.Fatalf("rows = %+v, want the AAPLc row", rows)
	}
	if rows[0].Spark != nil || rows[0].SparkBasis != "" || rows[0].Change24h != nil {
		t.Fatalf("row = %+v, want no line and no change", rows[0])
	}
	if rows[0].PriceUsdcMicros == nil {
		t.Fatal("a missing series must not cost the row its price")
	}
}

func TestMarketRowSource_anEmptyOrSinglePointSeriesDrawsNothing(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, pyth.AssetChartSeries{
		Basis:  pyth.PriceBasisUnderlying,
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 226_500_000}},
	})

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.Spark != nil {
		t.Fatalf("spark = %v, want none from one close", row.Spark)
	}
}

func TestMarketRowSource_noChartsClientIsARowWithoutALine(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	source.Charts = nil

	rows := source.Enrich(context.Background(), []b20.Asset{asset})
	if len(rows) != 1 || rows[0].Spark != nil || rows[0].Change24h != nil {
		t.Fatalf("rows = %+v, want one row with no line and no change", rows)
	}
	if rows[0].PriceUsdcMicros == nil {
		t.Fatal("price should still be marked when only the history source is missing")
	}
}

func TestMarketRowSource_noMarksClientStillDrawsTheLine(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(78))
	source.Marks = nil

	row := source.Enrich(context.Background(), []b20.Asset{asset})[0]
	if row.PriceUsdcMicros != nil {
		t.Fatalf("price = %v, want none", row.PriceUsdcMicros)
	}
	if len(row.Spark) == 0 {
		t.Fatal("the line should survive a missing marks client")
	}
}

// A slow Benchmarks costs the row its line, never the page its budget.
func TestMarketRowSource_aSlowSeriesStopsAtTheBudget(t *testing.T) {
	t.Parallel()
	source, charts := newMarketRowSource(t)
	asset := registerRowApple(t, source)
	pyth.RegisterChartSeries(charts, "AAPLc", pyth.ChartRange1D, underlyingDayCandles(78))
	pyth.RegisterChartSeriesDelay(charts, "AAPLc", pyth.ChartRange1D, 30*time.Second)

	start := time.Now()
	rows := source.enrich(context.Background(), []b20.Asset{asset}, 100*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the page took %s against a 100ms budget", elapsed)
	}
	if len(rows) != 1 || rows[0].Spark != nil {
		t.Fatalf("rows = %+v, want one row that gave up on its line", rows)
	}
	if rows[0].PriceUsdcMicros == nil {
		t.Fatal("a slow series must not cost the row its price")
	}
}

func TestMarketRowSource_rowsForSymbolsKeepsASymbolTheCatalogCannotResolve(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	registerRowApple(t, source)

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLc", "GONEc"})
	if row, ok := rows["AAPLC"]; !ok || row.PriceUsdcMicros == nil {
		t.Fatalf("rows = %+v, want a priced row for AAPLc", rows)
	}
	gone, ok := rows["GONEC"]
	if !ok {
		t.Fatal("a symbol the catalog does not know must still ship: the cabal really holds it")
	}
	if gone.Symbol != "GONEc" || gone.PriceUsdcMicros != nil || gone.Spark != nil {
		t.Fatalf("gone = %+v, want the symbol and no figures", gone)
	}
}

func TestMarketRowSource_rowsForSymbolsIsCaseInsensitiveAndDeduplicates(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	registerRowApple(t, source)

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLc", "aaplc", "  ", "AAPLc"})
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
}

// A legacy spelling resolves to the catalog's asset, and the answer lands under
// the key the caller asked with.
func TestMarketRowSource_rowsForSymbolsAnswersUnderTheCallersSpelling(t *testing.T) {
	t.Parallel()
	charts := pyth.NewFakeAssetPriceClient()
	source := &MarketRowSource{
		Catalog: b20.NewPinnedCatalog(),
		Marks:   pyth.NewFakeAssetPriceClient(),
		Charts:  charts.(pyth.MarketDataClient),
	}
	pyth.RegisterAssetMark(source.Marks, "AAPLc", pyth.AssetMark{PriceUsdcMicros: 232_050_000})

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLx"})
	row, ok := rows["AAPLX"]
	if !ok || row.Symbol != "AAPLc" || row.PriceUsdcMicros == nil {
		t.Fatalf("rows = %+v, want AAPLc's priced row under AAPLX", rows)
	}
}

func TestMarketRowSource_noSymbolsIsNoWork(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	if rows := source.RowsForSymbols(context.Background(), nil); len(rows) != 0 {
		t.Fatalf("rows = %+v, want empty", rows)
	}
}

// failingCatalog is a catalog whose every read errors.
type failingCatalog struct{ b20.Catalog }

func (failingCatalog) Search(context.Context, string, int, int) (b20.SearchPage, error) {
	return b20.SearchPage{}, errors.New("catalog down")
}

func (failingCatalog) ResolveTokenAddress(context.Context, string) (string, error) {
	return "", errors.New("catalog down")
}

func TestMarketRowSource_aCatalogFailureIsNotAFailedPage(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	source.Catalog = failingCatalog{source.Catalog}

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLc"})
	row, ok := rows["AAPLC"]
	if !ok || row.Symbol != "AAPLc" {
		t.Fatalf("rows = %+v, want the symbol to survive", rows)
	}
}

// countingCatalog records how many symbols a response resolves.
type countingCatalog struct {
	b20.Catalog
	mu      sync.Mutex
	lookups int
}

func (c *countingCatalog) Search(ctx context.Context, query string, limit, offset int) (b20.SearchPage, error) {
	c.mu.Lock()
	c.lookups++
	c.mu.Unlock()
	return c.Catalog.Search(ctx, query, limit, offset)
}

// Past the cap a row still ships (the member really holds it); it just carries
// no market figures.
func TestMarketRowSource_rowsForSymbolsCapsHowManySymbolsItResolves(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	counting := &countingCatalog{Catalog: source.Catalog}
	source.Catalog = counting

	symbols := make([]string, 0, maxRowSymbols+10)
	for i := 0; i < maxRowSymbols+10; i++ {
		symbols = append(symbols, fmt.Sprintf("SYM%02dc", i))
	}
	rows := source.RowsForSymbols(context.Background(), symbols)
	if len(rows) != len(symbols) {
		t.Fatalf("rows = %d, want one per symbol (%d)", len(rows), len(symbols))
	}
	counting.mu.Lock()
	lookups := counting.lookups
	counting.mu.Unlock()
	if lookups > maxRowSymbols {
		t.Fatalf("catalog lookups = %d, want at most %d", lookups, maxRowSymbols)
	}
}

func TestMarketRowSource_nilSourceAnswersEmpty(t *testing.T) {
	t.Parallel()
	var source *MarketRowSource
	if rows := source.RowsForSymbols(context.Background(), []string{"AAPLc"}); len(rows) != 0 {
		t.Fatalf("rows = %+v, want empty", rows)
	}
	if days := source.DaySeries(context.Background(), []b20.Asset{{Symbol: "AAPLc", TokenAddress: rowsAppleToken}}); len(days) != 0 {
		t.Fatalf("days = %v, want none", days)
	}
}

// stubLogos answers from a map keyed by token address.
type stubLogos map[string]string

func (s stubLogos) LogoURL(_ context.Context, token string) string { return s[token] }

// The row carries the issuer's own logo when the token's metadata published one,
// and nothing when it did not.
func TestMarketRowSource_theRowCarriesTheIssuersLogo(t *testing.T) {
	t.Parallel()
	source, _ := newMarketRowSource(t)
	apple := registerRowApple(t, source)
	other := b20.Asset{Symbol: "SPCXc", Name: "SpaceX", TokenAddress: "0xb2000000000000000000007b9fcbd005511acbd5", Routable: true}
	b20.RegisterCatalogAsset(source.Catalog, other)
	const logo = "https://metadata.coinbase.com/equity_icons/aapl.png"
	source.Logos = stubLogos{rowsAppleToken: logo}

	rows := source.Enrich(context.Background(), []b20.Asset{apple, other})
	if rows[0].LogoURL != logo {
		t.Fatalf("AAPLc logoUrl = %q, want the issuer's", rows[0].LogoURL)
	}
	if rows[1].LogoURL != "" {
		t.Fatalf("SPCXc logoUrl = %q, want none when the metadata has none", rows[1].LogoURL)
	}
}
