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

// EmptyReasonNoHistory is the single empty-series reason the app renders.
const EmptyReasonNoHistory = "price history unavailable"

// ParseChartRange validates a chart range query param. An empty value means 1D, so
// a build that only ever sends 1D, 1W or 1M keeps working unchanged.
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
	// regularOpen and regularClose bound the regular cash session inside the
	// window, for the ranges that have one (1D). The chart still draws the whole
	// extended session; only the stats grid folds over these.
	regularOpen  time.Time
	regularClose time.Time
	// previousCloseAt is the closing bell the previous close is taken from: the
	// close of the last bar that opens strictly before it (bars are stamped with
	// their open time, so the bar stamped at the bell is after-hours). Zero means
	// "the last bar before from", which is what every range but 1D wants; 1D wants
	// the previous regular session's closing bar, not whatever after-hours print
	// happened to be last before 04:00 ET.
	previousCloseAt time.Time
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
		return dayWindow(now)
	}
}

// dayWindow is the 1D window: the most recent trading session, taken from the
// exchange calendar rather than from the bars that came back.
//
// Inferring it from the data was wrong at the root. Pyth equity feeds keep
// republishing a frozen last price after the bell — `isFrozenEquityMark` and
// QuoteStatusStale exist precisely to detect that — and Benchmarks builds bars
// from published prices, so a Saturday request would get Saturday bars, anchor the
// window on Saturday, and draw a flat line of identical closes with Friday's close
// as its baseline. The calendar knows Saturday is not a session; the data does not.
func dayWindow(now time.Time) chartRangeWindow {
	session, found := marketcal.LastTradingSession(now)
	if !found {
		// No session inside the calendar's scan horizon. Degrade to a rolling day
		// rather than to no chart at all.
		return chartRangeWindow{
			from:       now.AddDate(0, 0, -1),
			fetchFrom:  now.AddDate(0, 0, -7),
			to:         now,
			resolution: "5",
		}
	}

	to := session.PostCloseEnd
	if now.Before(to) {
		to = now
	}
	window := chartRangeWindow{
		from: session.PreMarketOpen,
		// Reach back far enough to clear a long holiday weekend, so the previous
		// session's closing bar is always in the payload.
		fetchFrom:    session.PreMarketOpen.AddDate(0, 0, -7),
		to:           to,
		resolution:   "5",
		regularOpen:  session.RegularOpen,
		regularClose: session.RegularClose,
	}
	if previous, ok := marketcal.PreviousTradingSession(session.Day); ok {
		window.previousCloseAt = previous.RegularClose
	}
	return window
}

// allRangeYears bounds ALL. The B20 tokens themselves are new; five years of the
// underlying equity is as much history as the chart can honestly claim to be about
// the thing the user can buy.
const allRangeYears = 5
