package pyth

import (
	"fmt"
	"strings"
	"time"

	"github.com/monaco/monaco/apps/backend/internal/marketcal"
)

// ChartRange is a supported asset chart window.
type ChartRange string

const (
	ChartRange1D  ChartRange = "1D"
	ChartRange1W  ChartRange = "1W"
	ChartRange1M  ChartRange = "1M"
	ChartRange3M  ChartRange = "3M"
	ChartRange1Y  ChartRange = "1Y"
	ChartRangeAll ChartRange = "ALL"
)

// ChartRanges lists every supported range, shortest first.
var ChartRanges = []ChartRange{
	ChartRange1D, ChartRange1W, ChartRange1M, ChartRange3M, ChartRange1Y, ChartRangeAll,
}

// Chart series sources, reported so a caller can tell a dense Benchmarks series
// from the sparse Hermes fallback.
const (
	ChartSourceBenchmarks = "benchmarks"
	ChartSourceHermes     = "hermes"
)

// EmptyReasonNoHistory is the single empty-series reason the app renders.
const EmptyReasonNoHistory = "price history unavailable"

// ParseChartRange validates a chart range query param. An empty value means 1D, so
// the app on main — which only ever sends 1D, 1W or 1M — keeps working unchanged.
func ParseChartRange(raw string) (ChartRange, error) {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	if normalized == "" {
		return ChartRange1D, nil
	}
	for _, candidate := range ChartRanges {
		if normalized == string(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("invalid chart range %q", raw)
}

// chartRangeWindow is what one range asks the history source for.
type chartRangeWindow struct {
	// from is the first instant the chart draws.
	from time.Time
	// fetchFrom reaches further back than from so the bar before the window can
	// supply the previous close.
	fetchFrom time.Time
	to        time.Time
	// resolution is the TradingView shim's bar size.
	resolution string
	// lastSessionOnly narrows the window to the most recent trading session once
	// the bars come back. 1D means "today's session", and after the close, over a
	// weekend or on a holiday that session is not the last 24 hours.
	lastSessionOnly bool
}

func chartWindow(chartRange ChartRange, now time.Time) chartRangeWindow {
	now = now.UTC()
	switch chartRange {
	case ChartRange1W:
		return chartRangeWindow{
			from:       now.AddDate(0, 0, -7),
			fetchFrom:  now.AddDate(0, 0, -11),
			to:         now,
			resolution: "30",
		}
	case ChartRange1M:
		return chartRangeWindow{
			from:       now.AddDate(0, -1, 0),
			fetchFrom:  now.AddDate(0, -1, -5),
			to:         now,
			resolution: "60",
		}
	case ChartRange3M:
		return chartRangeWindow{
			from:       now.AddDate(0, -3, 0),
			fetchFrom:  now.AddDate(0, -3, -7),
			to:         now,
			resolution: "D",
		}
	case ChartRange1Y:
		return chartRangeWindow{
			from:       now.AddDate(-1, 0, 0),
			fetchFrom:  now.AddDate(-1, 0, -10),
			to:         now,
			resolution: "D",
		}
	case ChartRangeAll:
		return chartRangeWindow{
			from:       now.AddDate(-allRangeYears, 0, 0),
			fetchFrom:  now.AddDate(-allRangeYears, 0, -30),
			to:         now,
			resolution: "W",
		}
	default:
		// 1D. Reach back far enough to clear a long holiday weekend so the most
		// recent session is always in the payload.
		return chartRangeWindow{
			from:            now.AddDate(0, 0, -1),
			fetchFrom:       now.AddDate(0, 0, -7),
			to:              now,
			resolution:      "5",
			lastSessionOnly: true,
		}
	}
}

// allRangeYears bounds ALL. xStocks themselves are months old; five years of the
// underlying equity is as much history as the chart can honestly claim to be about
// the thing the user can buy.
const allRangeYears = 5

func exchangeLocation() *time.Location { return marketcal.Location() }
