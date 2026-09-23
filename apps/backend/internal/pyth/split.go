package pyth

import "context"

type splitPrices struct {
	marks  AssetPriceClient
	charts ChartSeriesClient
}

// WithCharts uses marks for AssetMark and charts for ChartSeries, falling back to
// the marks client's own history when the chart client cannot draw the range.
//
// The order is deliberate and stays that way: Pyth serves the underlying equity,
// which is what a stock chart should be about, so it wins any range it can
// actually serve. The marks client's Chainlink rounds are the token itself and
// come second — but they are on-chain, need no API key, and are today the only
// source that answers at all for equities on our key.
func WithCharts(marks AssetPriceClient, charts ChartSeriesClient) AssetPriceClient {
	if charts == nil {
		return marks
	}
	return &splitPrices{marks: marks, charts: charts}
}

func (s *splitPrices) AssetMark(ctx context.Context, symbol string) (AssetMark, error) {
	return s.marks.AssetMark(ctx, symbol)
}

func (s *splitPrices) AssetMarks(ctx context.Context, symbols []string) (map[string]AssetMark, error) {
	return s.marks.AssetMarks(ctx, symbols)
}

// ChartSeries picks the series a caller should draw, and when neither source can
// draw one, the better explanation of why.
func (s *splitPrices) ChartSeries(ctx context.Context, symbol string, chartRange ChartRange) (AssetChartSeries, error) {
	series, err := s.charts.ChartSeries(ctx, symbol, chartRange)
	if err == nil && len(series.Points) >= 2 {
		return series, nil
	}
	if s.marks == nil {
		if err != nil {
			return AssetChartSeries{}, err
		}
		return series, nil
	}

	fallback, markErr := s.marks.ChartSeries(ctx, symbol, chartRange)
	if markErr != nil {
		// The fallback is the only source that could have drawn this range, and it
		// is down rather than empty. Report that: "could not load, retry" is true,
		// "no price history" is not.
		if err != nil {
			return AssetChartSeries{}, err
		}
		return AssetChartSeries{}, markErr
	}
	if len(fallback.Points) > len(series.Points) {
		return fallback, nil
	}
	if err != nil {
		// The chart client failed and the fallback has nothing better. Its answer,
		// empty but explained, still beats a 500.
		return withRange(fallback, chartRange), nil
	}
	if len(series.Points) == 0 && explainsItself(fallback) {
		// Both are empty. Keep the answer that names its reason ("only on-chain
		// since 5 Aug 2026") over the generic one.
		return withRange(fallback, chartRange), nil
	}
	return series, nil
}

// explainsItself reports whether a series carries a reason more useful than the
// catch-all.
func explainsItself(series AssetChartSeries) bool {
	return series.EmptyReason != "" && series.EmptyReason != EmptyReasonNoHistory
}

func withRange(series AssetChartSeries, chartRange ChartRange) AssetChartSeries {
	if series.Range == "" {
		series.Range = chartRange
	}
	return series
}
