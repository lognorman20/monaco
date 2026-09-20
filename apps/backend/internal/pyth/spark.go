package pyth

// DefaultSparkPoints is how many closes a market row's sparkline is drawn from.
//
// A row's sparkline is 56pt wide, so about two dozen points is already more
// detail than the pixels can show. Sending the full 1D series instead would be
// tens of kilobytes per page of rows for a picture nobody can read.
const DefaultSparkPoints = 24

// SparkFromSeries reduces a chart series to at most points evenly spaced closes,
// keeping the real first and last so the drawn line starts and ends where the
// window does.
//
// Non-positive closes are dropped: a zero is a gap the upstream could not fill,
// and left in it would anchor the low of the window and flatten every real point
// against the top of the box. A series with fewer than two usable closes returns
// nil — the row then draws nothing, which is honest, rather than a flat line,
// which would read as "this stock did not move".
func SparkFromSeries(series AssetChartSeries, points int) []int64 {
	if points < 2 {
		points = DefaultSparkPoints
	}
	closes := make([]int64, 0, len(series.Points))
	for _, point := range series.Points {
		if point.PriceUsdcMicros > 0 {
			closes = append(closes, point.PriceUsdcMicros)
		}
	}
	if len(closes) < 2 {
		return nil
	}
	if len(closes) <= points {
		return closes
	}

	last := len(closes) - 1
	out := make([]int64, 0, points)
	for step := 0; step < points; step++ {
		index := int(float64(step)*float64(last)/float64(points-1) + 0.5)
		out = append(out, closes[index])
	}
	return out
}
