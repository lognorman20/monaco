package pyth

// The stats grid on the asset screen. Every cell here is derived from data the app
// really has about the underlying equity — Benchmarks candles and the Pyth
// confidence interval. The Kyber spread is about the token and lives on the
// liquidity strip and the stock-vs-token card instead. Market cap, P/E and dividend yield have no source behind B20 tokens, so they
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
	// ConfUsdcMicros is Pyth's confidence interval on the latest equity price.
	ConfUsdcMicros *int64
	// Basis names the instrument the candle-derived cells describe, and BasisSymbol
	// names it in full. Every history source we have serves the underlying equity,
	// so these cells are Apple on NASDAQ, per share, while the hero price is the
	// AAPLc token's Chainlink total-return mark, per token. The two differ by the
	// token's multiplier and by market hours, and without this label a current
	// price above the day's high reads as a bug rather than as two instruments.
	Basis       string
	BasisSymbol string
}

// HasFigures reports whether any cell was sourced. A grid with nothing in it is
// omitted whole rather than shipped as an object of nulls.
func (s AssetStats) HasFigures() bool {
	return s.OpenUsdcMicros != nil ||
		s.HighUsdcMicros != nil ||
		s.LowUsdcMicros != nil ||
		s.PreviousCloseUsdcMicros != nil ||
		s.Week52HighUsdcMicros != nil ||
		s.Week52LowUsdcMicros != nil ||
		s.ConfUsdcMicros != nil
}

// SessionStats folds a day series into open/high/low/previous close.
//
// It folds only the regular cash session. The 1D window deliberately spans the
// extended session so the chart can draw pre- and post-market, but Robinhood's Open
// cell is the 09:30 print, not the 04:00 one, and its High/Low are the regular
// session's — a pre-market spike is not the day's high. RegularOpen/RegularClose on
// the series carry the exchange's own boundaries; a series without them (a range
// that has no session, or a source that did not record one) folds whole, which is
// the most that series can support.
func SessionStats(series AssetChartSeries) AssetStats {
	stats := AssetStats{
		PreviousCloseUsdcMicros: series.PreviousCloseUsdcMicros,
		Basis:                   series.Basis,
		BasisSymbol:             series.BasisSymbol,
	}
	points := regularSessionPoints(series)
	if len(points) == 0 {
		return stats
	}

	first := points[0]
	open := first.OpenUsdcMicros
	if open <= 0 {
		open = first.PriceUsdcMicros
	}
	if open > 0 {
		stats.OpenUsdcMicros = &open
	}

	high, low := pointExtremes(points)
	if high > 0 {
		stats.HighUsdcMicros = &high
	}
	if low > 0 {
		stats.LowUsdcMicros = &low
	}
	return stats
}

// regularSessionPoints narrows a series to the bars inside the regular cash
// session. A bar is kept when it opens at or after the bell and before the close,
// so the 16:00 bar itself — which belongs to the after-hours window — is excluded.
func regularSessionPoints(series AssetChartSeries) []ChartPoint {
	if series.RegularOpen.IsZero() || series.RegularClose.IsZero() {
		return series.Points
	}
	from := series.RegularOpen.Unix()
	to := series.RegularClose.Unix()
	out := make([]ChartPoint, 0, len(series.Points))
	for _, point := range series.Points {
		if point.Timestamp < from || point.Timestamp >= to {
			continue
		}
		out = append(out, point)
	}
	return out
}

// Week52Range folds a year series into its high and low. Both are nil when the
// series is empty, which is what a symbol listed last month honestly looks like.
func Week52Range(series AssetChartSeries) (high, low *int64) {
	if len(series.Points) == 0 {
		return nil, nil
	}
	maxima, minima := pointExtremes(series.Points)
	if maxima <= 0 || minima <= 0 {
		return nil, nil
	}
	return &maxima, &minima
}

func pointExtremes(points []ChartPoint) (high, low int64) {
	for _, point := range points {
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
