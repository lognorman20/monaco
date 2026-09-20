package pyth

// The stats grid on the asset screen. Every cell here is derived from data the app
// really has — Benchmarks candles, the Pyth confidence interval, the Jupiter spread
// probe. Market cap, P/E and dividend yield have no source behind xStocks, so they
// are not in this struct at all: a grid with two honest cells missing beats a grid
// with two invented ones in it.

// AssetStats is the derived stats grid for one symbol. Every field is a pointer
// because "we could not source this" and "this is zero" are different answers.
type AssetStats struct {
	OpenUsdcMicros          *int64
	HighUsdcMicros          *int64
	LowUsdcMicros           *int64
	PreviousCloseUsdcMicros *int64
	Week52HighUsdcMicros    *int64
	Week52LowUsdcMicros     *int64
	// ConfUsdcMicros is Pyth's confidence interval on the latest mark.
	ConfUsdcMicros *int64
	// SpreadBps is the round-trip cost implied by the Jupiter probes.
	SpreadBps *int
}

// SessionStats folds a day series into open/high/low/previous close. The series is
// expected to be the 1D range: its first bar opens the session, and its previous
// close is the baseline the day's change is measured against.
//
// A Hermes fallback series carries no candles, so only close prices are available;
// high and low are then the extremes of those closes, which is the most the source
// can honestly support.
func SessionStats(series AssetChartSeries) AssetStats {
	stats := AssetStats{PreviousCloseUsdcMicros: series.PreviousCloseUsdcMicros}
	if len(series.Points) == 0 {
		return stats
	}

	first := series.Points[0]
	open := first.OpenUsdcMicros
	if open <= 0 {
		open = first.PriceUsdcMicros
	}
	if open > 0 {
		stats.OpenUsdcMicros = &open
	}

	high, low := seriesExtremes(series)
	if high > 0 {
		stats.HighUsdcMicros = &high
	}
	if low > 0 {
		stats.LowUsdcMicros = &low
	}
	return stats
}

// Week52Range folds a year series into its high and low. Both are nil when the
// series is empty, which is what a symbol listed last month honestly looks like.
func Week52Range(series AssetChartSeries) (high, low *int64) {
	if len(series.Points) == 0 {
		return nil, nil
	}
	maxima, minima := seriesExtremes(series)
	if maxima <= 0 || minima <= 0 {
		return nil, nil
	}
	return &maxima, &minima
}

func seriesExtremes(series AssetChartSeries) (high, low int64) {
	for _, point := range series.Points {
		for _, candidate := range []int64{point.PriceUsdcMicros, point.HighUsdcMicros, point.LowUsdcMicros} {
			if candidate <= 0 {
				continue
			}
			if candidate > high {
				high = candidate
			}
			if low == 0 || candidate < low {
				low = candidate
			}
		}
	}
	return high, low
}
