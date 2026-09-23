// Package marketcal answers which session the US cash equities market is in and
// when that changes.
//
// The app needs this on every stock surface (the session chip on the Stocks tab
// and the asset screen), so it has to be cheap, deterministic and correct at a
// date boundary.
// Hermes' market_hours.is_open only says open or not, says nothing about
// pre-market or when the next session starts, and costs a network call — so the
// schedule is computed here from the NYSE/Nasdaq calendar instead.
//
// Everything in and out of this package is UTC; America/New_York is an internal
// detail used to apply the exchange's own rules.
package marketcal

import (
	"fmt"
	"time"
	// The calendar is wrong without a tz database, and container images routinely
	// ship without one. Embed it rather than depend on the host.
	_ "time/tzdata"
)

// Session is one window of the US equities trading day.
type Session string

const (
	// SessionPreMarket is 04:00–09:30 ET on a trading day.
	SessionPreMarket Session = "pre_market"
	// SessionOpen is the regular cash session, 09:30–16:00 ET (13:00 on an early close).
	SessionOpen Session = "open"
	// SessionAfterHours is 16:00–20:00 ET (13:00–17:00 on an early close).
	SessionAfterHours Session = "after_hours"
	// SessionClosed is every other moment: overnight, weekends and holidays.
	SessionClosed Session = "closed"
)

// Status is the market's state at one instant, with the next boundary.
type Status struct {
	// Session is the window the market is in right now.
	Session Session
	// IsOpen is true only during the regular cash session.
	IsOpen bool
	// AfterHours is true whenever the regular session is not running — pre-market,
	// after-hours and closed all count, because that is when the underlying's
	// regular-session price is not being made, while the B20 token can still be
	// swapped in its pools on Base.
	AfterHours bool
	// NextSession is the session that begins at NextTransition.
	NextSession Session
	// NextTransition is when Session changes, in UTC.
	NextTransition time.Time
	// AsOf is the instant this status describes, in UTC.
	AsOf time.Time
	// Holiday names the exchange holiday when today is closed for one; empty otherwise.
	Holiday string
	// EarlyClose is true when today's regular session ends at 13:00 ET.
	EarlyClose bool
}

const (
	preMarketOpenMinutes  = 4 * 60    // 04:00 ET
	regularOpenMinutes    = 9*60 + 30 // 09:30 ET
	regularCloseMinutes   = 16 * 60   // 16:00 ET
	earlyCloseMinutes     = 13 * 60   // 13:00 ET
	postCloseEndMinutes   = 20 * 60   // 20:00 ET
	earlyPostCloseMinutes = 17 * 60   // 17:00 ET
	maxForwardScanDays    = 10        // longest run of non-trading days plus slack
	exchangeTimeZoneName  = "America/New_York"
)

var exchangeLocation = mustLoadExchangeLocation()

func mustLoadExchangeLocation() *time.Location {
	loc, err := time.LoadLocation(exchangeTimeZoneName)
	if err != nil {
		// With time/tzdata embedded this cannot fail; a panic here means the build
		// dropped the embed, and a silently wrong market clock is worse than a crash.
		panic(fmt.Sprintf("marketcal: load %s: %v", exchangeTimeZoneName, err))
	}
	return loc
}

// StatusAt reports the market status at an instant. The instant may be in any
// location; the result is always UTC.
func StatusAt(at time.Time) Status {
	local := at.In(exchangeLocation)
	day := scheduleFor(local)

	status := Status{
		AsOf:       at.UTC(),
		Session:    SessionClosed,
		Holiday:    day.holiday,
		EarlyClose: day.earlyClose,
	}

	if day.trading {
		switch {
		case local.Before(day.preMarketOpen):
			status.Session = SessionClosed
			status.NextSession = SessionPreMarket
			status.NextTransition = day.preMarketOpen.UTC()
			return finish(status)
		case local.Before(day.regularOpen):
			status.Session = SessionPreMarket
			status.NextSession = SessionOpen
			status.NextTransition = day.regularOpen.UTC()
			return finish(status)
		case local.Before(day.regularClose):
			status.Session = SessionOpen
			status.NextSession = SessionAfterHours
			status.NextTransition = day.regularClose.UTC()
			return finish(status)
		case local.Before(day.postCloseEnd):
			status.Session = SessionAfterHours
			status.NextSession = SessionClosed
			status.NextTransition = day.postCloseEnd.UTC()
			return finish(status)
		}
	}

	// Overnight, a weekend or a holiday: the next thing that happens is the next
	// trading day's pre-market bell.
	next, found := nextTradingDay(local)
	status.Session = SessionClosed
	status.NextSession = SessionPreMarket
	if found {
		status.NextTransition = next.preMarketOpen.UTC()
	}
	return finish(status)
}

func finish(status Status) Status {
	status.IsOpen = status.Session == SessionOpen
	status.AfterHours = !status.IsOpen
	return status
}

// Now reports the market status at the current instant.
func Now() Status { return StatusAt(time.Now()) }

// daySchedule is one calendar day's session boundaries in exchange-local time.
type daySchedule struct {
	trading       bool
	holiday       string
	earlyClose    bool
	preMarketOpen time.Time
	regularOpen   time.Time
	regularClose  time.Time
	postCloseEnd  time.Time
}

func scheduleFor(local time.Time) daySchedule {
	year, month, dayOfMonth := local.Date()
	midnight := time.Date(year, month, dayOfMonth, 0, 0, 0, 0, exchangeLocation)

	weekday := midnight.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return daySchedule{}
	}
	if name, ok := holidayName(midnight); ok {
		return daySchedule{holiday: name}
	}

	early := isEarlyClose(midnight)
	closeMinutes := regularCloseMinutes
	postMinutes := postCloseEndMinutes
	if early {
		closeMinutes = earlyCloseMinutes
		postMinutes = earlyPostCloseMinutes
	}
	return daySchedule{
		trading:       true,
		earlyClose:    early,
		preMarketOpen: atMinutes(midnight, preMarketOpenMinutes),
		regularOpen:   atMinutes(midnight, regularOpenMinutes),
		regularClose:  atMinutes(midnight, closeMinutes),
		postCloseEnd:  atMinutes(midnight, postMinutes),
	}
}

// atMinutes builds an instant at a wall-clock time on the day midnight belongs to.
// The bells are wall-clock facts — the exchange opens at 09:30 ET whatever the UTC
// offset is that week — so they are built from calendar fields, not from an offset
// added to midnight.
func atMinutes(midnight time.Time, minutes int) time.Time {
	return time.Date(
		midnight.Year(), midnight.Month(), midnight.Day(),
		minutes/60, minutes%60, 0, 0, exchangeLocation,
	)
}

func nextTradingDay(local time.Time) (daySchedule, bool) {
	cursor := local
	for i := 0; i < maxForwardScanDays; i++ {
		cursor = cursor.AddDate(0, 0, 1)
		midnight := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 0, 0, 0, 0, exchangeLocation)
		if day := scheduleFor(midnight); day.trading {
			return day, true
		}
	}
	return daySchedule{}, false
}

func previousTradingDay(local time.Time) (daySchedule, bool) {
	cursor := local
	for i := 0; i < maxForwardScanDays; i++ {
		cursor = cursor.AddDate(0, 0, -1)
		midnight := time.Date(cursor.Year(), cursor.Month(), cursor.Day(), 0, 0, 0, 0, exchangeLocation)
		if day := scheduleFor(midnight); day.trading {
			return day, true
		}
	}
	return daySchedule{}, false
}

// TradingSession is one trading day's boundaries, in UTC. Callers that need to
// bound a window on the exchange's own schedule — a 1D chart, or the open/high/low
// the stats grid folds — take it from here rather than inferring a session from
// whatever bars a vendor happened to publish. Pyth equity feeds keep republishing a
// frozen last price after the bell, so "the data stopped" is not a reliable signal
// that the session did.
type TradingSession struct {
	// Day is midnight at the exchange on this session's calendar date, in UTC.
	Day time.Time
	// PreMarketOpen is 04:00 ET, the first instant of the extended session.
	PreMarketOpen time.Time
	// RegularOpen is 09:30 ET: the print Robinhood's Open cell shows.
	RegularOpen time.Time
	// RegularClose is 16:00 ET, or 13:00 ET on a half day.
	RegularClose time.Time
	// PostCloseEnd is 20:00 ET (17:00 on a half day), the last instant of the
	// extended session.
	PostCloseEnd time.Time
	EarlyClose   bool
}

func sessionFrom(day daySchedule) TradingSession {
	midnight := time.Date(
		day.regularOpen.Year(), day.regularOpen.Month(), day.regularOpen.Day(),
		0, 0, 0, 0, exchangeLocation,
	)
	return TradingSession{
		Day:           midnight.UTC(),
		PreMarketOpen: day.preMarketOpen.UTC(),
		RegularOpen:   day.regularOpen.UTC(),
		RegularClose:  day.regularClose.UTC(),
		PostCloseEnd:  day.postCloseEnd.UTC(),
		EarlyClose:    day.earlyClose,
	}
}

// SessionOn returns the session held on at's exchange-local calendar date. The
// second result is false on a weekend or an exchange holiday.
func SessionOn(at time.Time) (TradingSession, bool) {
	day := scheduleFor(at.In(exchangeLocation))
	if !day.trading {
		return TradingSession{}, false
	}
	return sessionFrom(day), true
}

// LastTradingSession returns the most recent session at or before at: today's once
// the exchange has reached pre-market, and otherwise the previous trading day's.
// On a Saturday it is Friday's; on a holiday it is the session before the holiday.
func LastTradingSession(at time.Time) (TradingSession, bool) {
	local := at.In(exchangeLocation)
	if day := scheduleFor(local); day.trading && !local.Before(day.preMarketOpen) {
		return sessionFrom(day), true
	}
	if day, found := previousTradingDay(local); found {
		return sessionFrom(day), true
	}
	return TradingSession{}, false
}

// PreviousTradingSession returns the last session strictly before at's
// exchange-local calendar date — the one whose close is "previous close".
func PreviousTradingSession(at time.Time) (TradingSession, bool) {
	day, found := previousTradingDay(at.In(exchangeLocation))
	if !found {
		return TradingSession{}, false
	}
	return sessionFrom(day), true
}

// IsTradingDay reports whether the exchange holds a session on the day containing at.
func IsTradingDay(at time.Time) bool {
	return scheduleFor(at.In(exchangeLocation)).trading
}

// Location returns the exchange's time zone. Callers that bucket a series by
// trading day need the same day boundary this package uses.
func Location() *time.Location { return exchangeLocation }
