package pyth

import "time"

// OHLCBar is one candle from a history source, already converted to USDC micros.
//
// It is the vocabulary every history source hands back, not Pyth's own shape: a
// source is responsible for talking to its vendor and for the arithmetic that turns
// a vendor price into micros, and for nothing else. Windowing a run of bars into a
// series — which ones fall inside the range, which one supplies the previous close,
// how many survive the point cap — is the same problem whoever fetched them, and
// lives once in `AssembleRangeSeries`.
type OHLCBar struct {
	// Timestamp is the bar's opening instant, in Unix seconds.
	Timestamp int64
	Open      int64
	High      int64
	Low       int64
	Close     int64
}

// AssembleRangeSeries turns a source's bars into the series for one range: it picks
// the window from the exchange calendar, keeps the bars inside it, takes the
// previous close from the last bar before it, and thins the rest to the point cap.
//
// Bars may be passed in any order and may reach further back than the window; a
// source is expected to over-fetch so the previous close is in the payload.
func AssembleRangeSeries(chartRange ChartRange, now time.Time, bars []OHLCBar) AssetChartSeries {
	return assembleSeries(bars, chartWindow(chartRange, now.UTC()))
}

// RangeFetchWindow is the span a source should ask its vendor for to serve a range:
// `from` reaches back past the window so the previous close is included, and `to`
// is the last instant the chart draws.
func RangeFetchWindow(chartRange ChartRange, now time.Time) (from, to time.Time) {
	window := chartWindow(chartRange, now.UTC())
	return window.fetchFrom, window.to
}
