package pyth

import (
	"reflect"
	"testing"
)

func seriesOf(prices ...int64) AssetChartSeries {
	points := make([]ChartPoint, 0, len(prices))
	for i, price := range prices {
		points = append(points, ChartPoint{Timestamp: int64(i), PriceUsdcMicros: price})
	}
	return AssetChartSeries{Points: points}
}

func TestSparkFromSeries_emptySeriesHasNothingToDraw(t *testing.T) {
	t.Parallel()
	if got := SparkFromSeries(AssetChartSeries{}, DefaultSparkPoints); got != nil {
		t.Fatalf("spark = %v, want nil", got)
	}
}

func TestSparkFromSeries_singlePointHasNothingToDraw(t *testing.T) {
	t.Parallel()
	if got := SparkFromSeries(seriesOf(100), DefaultSparkPoints); got != nil {
		t.Fatalf("spark = %v, want nil", got)
	}
}

func TestSparkFromSeries_dropsGapsRatherThanDrawingThemAsAFloor(t *testing.T) {
	t.Parallel()
	got := SparkFromSeries(seriesOf(0, 100, 0, 200, 0), DefaultSparkPoints)
	want := []int64{100, 200}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spark = %v, want %v", got, want)
	}
}

func TestSparkFromSeries_oneUsableCloseIsNotASeries(t *testing.T) {
	t.Parallel()
	if got := SparkFromSeries(seriesOf(0, 0, 100), DefaultSparkPoints); got != nil {
		t.Fatalf("spark = %v, want nil", got)
	}
}

func TestSparkFromSeries_shortSeriesIsSentWhole(t *testing.T) {
	t.Parallel()
	got := SparkFromSeries(seriesOf(1, 2, 3), DefaultSparkPoints)
	if !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("spark = %v, want [1 2 3]", got)
	}
}

func TestSparkFromSeries_denseSeriesIsDownsampledKeepingBothEnds(t *testing.T) {
	t.Parallel()
	prices := make([]int64, 0, 500)
	for i := 1; i <= 500; i++ {
		prices = append(prices, int64(i))
	}
	got := SparkFromSeries(seriesOf(prices...), DefaultSparkPoints)
	if len(got) != DefaultSparkPoints {
		t.Fatalf("len = %d, want %d", len(got), DefaultSparkPoints)
	}
	if got[0] != 1 {
		t.Fatalf("first = %d, want 1", got[0])
	}
	if got[len(got)-1] != 500 {
		t.Fatalf("last = %d, want 500", got[len(got)-1])
	}
}

func TestSparkFromSeries_nonsensePointCountFallsBackToTheDefault(t *testing.T) {
	t.Parallel()
	prices := make([]int64, 0, 100)
	for i := 1; i <= 100; i++ {
		prices = append(prices, int64(i))
	}
	got := SparkFromSeries(seriesOf(prices...), 0)
	if len(got) != DefaultSparkPoints {
		t.Fatalf("len = %d, want %d", len(got), DefaultSparkPoints)
	}
}
