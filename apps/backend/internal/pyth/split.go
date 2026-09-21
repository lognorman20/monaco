package pyth

import "context"

type splitPrices struct {
	marks  AssetPriceClient
	charts AssetPriceClient
}

// WithCharts uses marks for AssetMark and charts for ChartSeries.
func WithCharts(marks, charts AssetPriceClient) AssetPriceClient {
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
		if err != nil {
			return AssetChartSeries{}, err
		}
		return series, nil
	}
	return fallback, nil
}
