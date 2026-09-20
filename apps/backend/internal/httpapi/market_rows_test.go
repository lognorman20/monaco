package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/monaco/monaco/apps/backend/internal/jupiter"
	"github.com/monaco/monaco/apps/backend/internal/pyth"
	"github.com/monaco/monaco/apps/backend/internal/xstocks"
)

func newMarketRowSource(t *testing.T) *MarketRowSource {
	t.Helper()
	return &MarketRowSource{
		Catalog: xstocks.NewFakeCatalogSearcher(),
		Pyth:    pyth.NewFakeAssetPriceClient(),
		Price:   jupiter.NewFakePriceClient(),
	}
}

func registerApple(t *testing.T, source *MarketRowSource) xstocks.CatalogAsset {
	t.Helper()
	asset := xstocks.CatalogAsset{
		Symbol:     "AAPLx",
		Name:       "Apple",
		SolanaMint: jupiter.AAPLxMint,
		Routable:   true,
	}
	xstocks.RegisterCatalogAsset(source.Catalog, asset)
	jupiter.RegisterPrice(source.Price, jupiter.AAPLxMint, jupiter.TokenPrice{PriceUsdcMicros: 232_050_000})
	return asset
}

func dayCandles(count int) pyth.AssetChartSeries {
	points := make([]pyth.ChartPoint, 0, count)
	for i := 0; i < count; i++ {
		points = append(points, pyth.ChartPoint{
			Timestamp:       int64(i * 300),
			PriceUsdcMicros: int64(226_500_000 + i*60_000),
		})
	}
	return pyth.AssetChartSeries{Points: points}
}

func TestMarketRowSource_enrichAttachesTheDaySeries(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	pyth.RegisterChartSeries(source.Pyth, "AAPLx", pyth.ChartRange1D, dayCandles(78))

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if len(rows[0].Spark) != pyth.DefaultSparkPoints {
		t.Fatalf("spark = %d points, want %d", len(rows[0].Spark), pyth.DefaultSparkPoints)
	}
	if rows[0].PriceUsdcMicros == nil || *rows[0].PriceUsdcMicros != 232_050_000 {
		t.Fatalf("price = %v, want 232050000", rows[0].PriceUsdcMicros)
	}
}

func TestMarketRowSource_symbolWithNoHistoryStillShipsARow(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	// No chart registered: the fake answers "no history", which is what a newly
	// listed xStock really looks like.

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Spark != nil {
		t.Fatalf("spark = %v, want none", rows[0].Spark)
	}
	if rows[0].Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", rows[0].Symbol)
	}
}

func TestMarketRowSource_aFlatSeriesIsNotASeries(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	// One usable close: nothing to draw a line between.
	pyth.RegisterChartSeries(source.Pyth, "AAPLx", pyth.ChartRange1D, pyth.AssetChartSeries{
		Points: []pyth.ChartPoint{{Timestamp: 1, PriceUsdcMicros: 226_500_000}},
	})

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if rows[0].Spark != nil {
		t.Fatalf("spark = %v, want none", rows[0].Spark)
	}
}

func TestMarketRowSource_noPythClientIsARowWithoutASparkline(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	source.Pyth = nil

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if len(rows) != 1 || rows[0].Spark != nil {
		t.Fatalf("rows = %+v, want one row with no spark", rows)
	}
	if rows[0].PriceUsdcMicros == nil {
		t.Fatal("price should still be marked when only the history source is missing")
	}
}

func TestMarketRowSource_noPriceClientStillDrawsTheSparkline(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	asset := registerApple(t, source)
	pyth.RegisterChartSeries(source.Pyth, "AAPLx", pyth.ChartRange1D, dayCandles(78))
	source.Price = nil

	rows := source.Enrich(context.Background(), []xstocks.CatalogAsset{asset})
	if rows[0].PriceUsdcMicros != nil {
		t.Fatalf("price = %v, want none", rows[0].PriceUsdcMicros)
	}
	if len(rows[0].Spark) == 0 {
		t.Fatal("spark should survive a missing price client")
	}
}

func TestMarketRowSource_rowsForSymbolsKeepsASymbolTheCatalogueCannotResolve(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	registerApple(t, source)

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLx", "GONEx"})
	if _, ok := rows["AAPLX"]; !ok {
		t.Fatalf("rows = %+v, want a row for AAPLx", rows)
	}
	gone, ok := rows["GONEX"]
	if !ok {
		t.Fatal("a symbol the catalogue does not know must still ship: the cabal really holds it")
	}
	if gone.Symbol != "GONEx" || gone.PriceUsdcMicros != nil {
		t.Fatalf("gone = %+v, want the symbol and no figures", gone)
	}
}

func TestMarketRowSource_rowsForSymbolsIsCaseInsensitiveAndDeduplicates(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	registerApple(t, source)

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLx", "aaplx", "  ", "AAPLx"})
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one", rows)
	}
}

func TestMarketRowSource_noSymbolsIsNoWork(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	if rows := source.RowsForSymbols(context.Background(), nil); len(rows) != 0 {
		t.Fatalf("rows = %+v, want empty", rows)
	}
}

func TestMarketRowSource_aCatalogueFailureIsNotAFailedPage(t *testing.T) {
	t.Parallel()
	source := newMarketRowSource(t)
	xstocks.RegisterCatalogSearchError(source.Catalog, errors.New("catalogue down"))

	rows := source.RowsForSymbols(context.Background(), []string{"AAPLx"})
	row, ok := rows["AAPLX"]
	if !ok {
		t.Fatalf("rows = %+v, want the symbol to survive", rows)
	}
	if row.Symbol != "AAPLx" {
		t.Fatalf("symbol = %q, want AAPLx", row.Symbol)
	}
}

func TestMarketRowSource_nilSourceAnswersEmpty(t *testing.T) {
	t.Parallel()
	var source *MarketRowSource
	if rows := source.RowsForSymbols(context.Background(), []string{"AAPLx"}); len(rows) != 0 {
		t.Fatalf("rows = %+v, want empty", rows)
	}
	if sparks := source.Sparklines(context.Background(), []xstocks.CatalogAsset{{Symbol: "AAPLx"}}); sparks != nil {
		t.Fatalf("sparks = %v, want nil", sparks)
	}
}
