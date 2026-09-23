package pyth

import (
	"context"
	"errors"
	"testing"
)

type stubCharts struct {
	series AssetChartSeries
	err    error
	calls  int
}

func (s *stubCharts) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	s.calls++
	return s.series, s.err
}

type stubMarks struct {
	stubCharts
}

func (s *stubMarks) AssetMark(ctx context.Context, symbol string) (AssetMark, error) {
	return AssetMark{}, nil
}

func (s *stubMarks) AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error) {
	return nil, nil
}

func twoPoints() []ChartPoint {
	return []ChartPoint{{Timestamp: 1, PriceUsdcMicros: 100}, {Timestamp: 2, PriceUsdcMicros: 101}}
}

// Pyth keeps precedence for anything it can draw: it is the underlying equity,
// which is what a stock chart should be about.
func TestWithCharts_pythWinsWhateverTheFallbackHas(t *testing.T) {
	t.Parallel()
	charts := &stubCharts{series: AssetChartSeries{Points: twoPoints(), Source: ChartSourceBenchmarks, Basis: PriceBasisUnderlying}}
	marks := &stubMarks{stubCharts{series: AssetChartSeries{
		Points: append(twoPoints(), ChartPoint{Timestamp: 3, PriceUsdcMicros: 102}),
		Source: ChartSourceChainlink,
	}}}
	got, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != ChartSourceBenchmarks {
		t.Fatalf("source = %q, want the Pyth series even though the fallback is longer", got.Source)
	}
	if marks.calls != 0 {
		t.Fatal("the fallback should not even be asked when Pyth can draw the range")
	}
}

func TestWithCharts_fallsThroughWhenPythHasNothing(t *testing.T) {
	t.Parallel()
	charts := &stubCharts{series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}}
	marks := &stubMarks{stubCharts{series: AssetChartSeries{Points: twoPoints(), Source: ChartSourceChainlink, Basis: PriceBasisToken}}}
	got, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != ChartSourceChainlink || len(got.Points) != 2 {
		t.Fatalf("series = %+v, want the Chainlink fallback", got)
	}
}

// Two empty answers are not equally useful: "Only on-chain since 5 Aug 2026"
// tells the reader something, "price history unavailable" does not.
func TestWithCharts_keepsTheEmptyAnswerThatExplainsItself(t *testing.T) {
	t.Parallel()
	charts := &stubCharts{series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}}
	marks := &stubMarks{stubCharts{series: AssetChartSeries{EmptyReason: "Only on-chain since 5 Aug 2026", Range: ChartRange1Y}}}
	got, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1Y)
	if err != nil {
		t.Fatal(err)
	}
	if got.EmptyReason != "Only on-chain since 5 Aug 2026" {
		t.Fatalf("reason = %q", got.EmptyReason)
	}
	if got.Range != ChartRange1Y {
		t.Fatalf("range = %q", got.Range)
	}
}

// A fallback that is down is not a fallback that is empty. The route says "could
// not load", the app offers Retry, and nobody is told this stock has no history.
func TestWithCharts_afallbackOutageIsAnError(t *testing.T) {
	t.Parallel()
	down := errors.New("rpc: over rate limit")
	charts := &stubCharts{series: AssetChartSeries{EmptyReason: EmptyReasonNoHistory}}
	marks := &stubMarks{stubCharts{err: down}}
	_, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if !errors.Is(err, down) {
		t.Fatalf("err = %v, want the outage", err)
	}
}

// And a Pyth outage is survivable as long as the fallback can draw.
func TestWithCharts_pythOutageIsCoveredByTheFallback(t *testing.T) {
	t.Parallel()
	charts := &stubCharts{err: errors.New("benchmarks: status 404")}
	marks := &stubMarks{stubCharts{series: AssetChartSeries{Points: twoPoints(), Source: ChartSourceChainlink}}}
	got, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != ChartSourceChainlink {
		t.Fatalf("series = %+v", got)
	}
}

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
	RegisterChartSeries(charts, "AAPLc", ChartRange1D, AssetChartSeries{EmptyReason: EmptyReasonNoHistory})

	client := WithCharts(marks, charts)
	series, err := client.ChartSeries(context.Background(), "AAPLc", ChartRange1D)
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Points) != 2 {
		t.Fatalf("points = %d, want Chainlink fallback", len(series.Points))
	}
}

func TestWithCharts_keepsThePythAnswerWhenTheFallbackHasNothing(t *testing.T) {
	t.Parallel()
	marks := NewFakeAssetPriceClient()
	charts := NewFakeAssetPriceClient()
	RegisterChartSeries(charts, "AAPLc", ChartRange1Y, AssetChartSeries{
		EmptyReason: EmptyReasonNoHistory,
		Range:       ChartRange1Y,
		Source:      ChartSourceBenchmarks,
	})
	series, err := WithCharts(marks, charts).ChartSeries(context.Background(), "AAPLc", ChartRange1Y)
	if err != nil {
		t.Fatal(err)
	}
	if series.Range != ChartRange1Y || series.Source != ChartSourceBenchmarks {
		t.Fatalf("series = %+v, want the chart client's own empty answer", series)
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
