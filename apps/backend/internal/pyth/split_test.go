package pyth

import (
	"context"
	"testing"
)

func TestWithCharts_fallsBackWhenHermesEmpty(t *testing.T) {
	t.Parallel()
	marks := NewFakeAssetPriceClient()
	charts := NewFakeAssetPriceClient()
	RegisterChartSeries(marks, "AAPLc", ChartRange1D, AssetChartSeries{
		Points: []ChartPoint{
			{Timestamp: 1, PriceUsdcMicros: 100},
			{Timestamp: 2, PriceUsdcMicros: 110},
		},
	})
	RegisterChartSeries(charts, "AAPLc", ChartRange1D, AssetChartSeries{EmptyReason: "price history unavailable"})

	client := WithCharts(marks, charts)
	series, err := client.ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want Chainlink fallback", len(series.Points))
	}
}

func TestWithCharts_keepsHermesWhenPresent(t *testing.T) {
	t.Parallel()
	marks := NewFakeAssetPriceClient()
	charts := NewFakeAssetPriceClient()
	RegisterChartSeries(marks, "AAPLc", ChartRange1D, AssetChartSeries{
		Points: []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 1}, {Timestamp: 2, PriceUsdcMicros: 2}},
	})
	RegisterChartSeries(charts, "AAPLc", ChartRange1D, AssetChartSeries{
		Points: []ChartPoint{{Timestamp: 9, PriceUsdcMicros: 9}, {Timestamp: 10, PriceUsdcMicros: 10}},
	})
	series, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if series.Points[0].PriceUsdcMicros != 9 {
		t.Fatalf("used fallback instead of Hermes: %+v", series.Points)
	}
}
