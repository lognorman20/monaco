package marketcal

import (
	"testing"
	"time"
)

func et(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02 15:04", value, exchangeLocation)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func TestStatusAt_sessionsAcrossARegularTradingDay(t *testing.T) {
	t.Parallel()

	// 2026-09-22 is an ordinary Tuesday.
	cases := []struct {
		at          string
		session     Session
		nextSession Session
		next        string
	}{
		{"2026-09-22 03:59", SessionClosed, SessionPreMarket, "2026-09-22 04:00"},
		{"2026-09-22 04:00", SessionPreMarket, SessionOpen, "2026-09-22 09:30"},
		{"2026-09-22 09:29", SessionPreMarket, SessionOpen, "2026-09-22 09:30"},
		{"2026-09-22 09:30", SessionOpen, SessionAfterHours, "2026-09-22 16:00"},
		{"2026-09-22 15:59", SessionOpen, SessionAfterHours, "2026-09-22 16:00"},
		{"2026-09-22 16:00", SessionAfterHours, SessionClosed, "2026-09-22 20:00"},
		{"2026-09-22 19:59", SessionAfterHours, SessionClosed, "2026-09-22 20:00"},
		{"2026-09-22 20:00", SessionClosed, SessionPreMarket, "2026-09-23 04:00"},
	}

	for _, tc := range cases {
		status := StatusAt(et(t, tc.at))
		if status.Session != tc.session {
			t.Fatalf("%s session = %q, want %q", tc.at, status.Session, tc.session)
		}
		if status.NextSession != tc.nextSession {
			t.Fatalf("%s next session = %q, want %q", tc.at, status.NextSession, tc.nextSession)
		}
		if want := et(t, tc.next).UTC(); !status.NextTransition.Equal(want) {
			t.Fatalf("%s next transition = %s, want %s", tc.at, status.NextTransition, want)
		}
		if status.NextTransition.Location() != time.UTC {
			t.Fatalf("%s next transition location = %s, want UTC", tc.at, status.NextTransition.Location())
		}
		if status.IsOpen != (tc.session == SessionOpen) {
			t.Fatalf("%s isOpen = %v", tc.at, status.IsOpen)
		}
		if status.AfterHours == status.IsOpen {
			t.Fatalf("%s afterHours must be the inverse of isOpen", tc.at)
		}
	}
}

func TestStatusAt_weekendRollsToMondayPreMarket(t *testing.T) {
	t.Parallel()

	status := StatusAt(et(t, "2026-09-19 12:00")) // Saturday
	if status.Session != SessionClosed {
		t.Fatalf("session = %q, want closed", status.Session)
	}
	if want := et(t, "2026-09-21 04:00").UTC(); !status.NextTransition.Equal(want) {
		t.Fatalf("next transition = %s, want %s", status.NextTransition, want)
	}
}

func TestStatusAt_holidayIsClosedAndNamed(t *testing.T) {
	t.Parallel()

	status := StatusAt(et(t, "2026-11-26 11:00")) // Thanksgiving
	if status.Session != SessionClosed {
		t.Fatalf("session = %q, want closed", status.Session)
	}
	if status.Holiday != "Thanksgiving Day" {
		t.Fatalf("holiday = %q, want Thanksgiving Day", status.Holiday)
	}
	// The half day after Thanksgiving is still a trading day.
	if want := et(t, "2026-11-27 04:00").UTC(); !status.NextTransition.Equal(want) {
		t.Fatalf("next transition = %s, want %s", status.NextTransition, want)
	}
}

func TestStatusAt_earlyCloseShortensRegularAndPostSessions(t *testing.T) {
	t.Parallel()

	// Friday after Thanksgiving 2026: regular session ends 13:00, post ends 17:00.
	atNoon := StatusAt(et(t, "2026-11-27 12:59"))
	if atNoon.Session != SessionOpen || !atNoon.EarlyClose {
		t.Fatalf("session = %q earlyClose = %v, want open + early close", atNoon.Session, atNoon.EarlyClose)
	}
	if want := et(t, "2026-11-27 13:00").UTC(); !atNoon.NextTransition.Equal(want) {
		t.Fatalf("next transition = %s, want %s", atNoon.NextTransition, want)
	}

	afterBell := StatusAt(et(t, "2026-11-27 13:00"))
	if afterBell.Session != SessionAfterHours {
		t.Fatalf("session = %q, want after_hours", afterBell.Session)
	}
	if want := et(t, "2026-11-27 17:00").UTC(); !afterBell.NextTransition.Equal(want) {
		t.Fatalf("next transition = %s, want %s", afterBell.NextTransition, want)
	}
}

func TestStatusAt_christmasEveIsAHalfDay(t *testing.T) {
	t.Parallel()

	status := StatusAt(et(t, "2026-12-24 14:00")) // Thursday
	if status.Session != SessionAfterHours || !status.EarlyClose {
		t.Fatalf("session = %q earlyClose = %v, want after_hours + early close", status.Session, status.EarlyClose)
	}
}

func TestStatusAt_utcInputProducesUTCOutput(t *testing.T) {
	t.Parallel()

	// 14:00 UTC on 2026-09-22 is 10:00 ET — the regular session.
	status := StatusAt(time.Date(2026, time.September, 22, 14, 0, 0, 0, time.UTC))
	if !status.IsOpen {
		t.Fatalf("session = %q, want open", status.Session)
	}
	if status.AsOf.Location() != time.UTC {
		t.Fatalf("asOf location = %s, want UTC", status.AsOf.Location())
	}
}

func TestStatusAt_daylightSavingBoundaryKeepsLocalBells(t *testing.T) {
	t.Parallel()

	// 2026-03-08 is the US DST switch. The Monday after still opens at 09:30 ET,
	// which is 13:30 UTC rather than the 14:30 UTC of the week before.
	before := StatusAt(time.Date(2026, time.March, 6, 14, 30, 0, 0, time.UTC))
	after := StatusAt(time.Date(2026, time.March, 9, 13, 30, 0, 0, time.UTC))
	if !before.IsOpen {
		t.Fatalf("pre-DST session = %q, want open", before.Session)
	}
	if !after.IsOpen {
		t.Fatalf("post-DST session = %q, want open", after.Session)
	}
}

func TestHolidays_matchThePublishedNyseCalendar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		day  string
		name string
	}{
		{"2025-01-01", "New Year's Day"},
		{"2025-01-20", "Martin Luther King, Jr. Day"},
		{"2025-02-17", "Washington's Birthday"},
		{"2025-04-18", "Good Friday"},
		{"2025-05-26", "Memorial Day"},
		{"2025-06-19", "Juneteenth National Independence Day"},
		{"2025-07-04", "Independence Day"},
		{"2025-09-01", "Labor Day"},
		{"2025-11-27", "Thanksgiving Day"},
		{"2025-12-25", "Christmas Day"},
		{"2026-01-01", "New Year's Day"},
		{"2026-01-19", "Martin Luther King, Jr. Day"},
		{"2026-02-16", "Washington's Birthday"},
		{"2026-04-03", "Good Friday"},
		{"2026-05-25", "Memorial Day"},
		{"2026-06-19", "Juneteenth National Independence Day"},
		{"2026-07-03", "Independence Day"}, // July 4 is a Saturday, observed Friday
		{"2026-09-07", "Labor Day"},
		{"2026-11-26", "Thanksgiving Day"},
		{"2026-12-25", "Christmas Day"},
		{"2027-12-24", "Christmas Day"}, // December 25 is a Saturday
	}

	for _, tc := range cases {
		day, err := time.ParseInLocation("2006-01-02", tc.day, exchangeLocation)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.day, err)
		}
		name, ok := holidayName(day)
		if !ok {
			t.Fatalf("%s: expected holiday %q, got none", tc.day, tc.name)
		}
		if name != tc.name {
			t.Fatalf("%s holiday = %q, want %q", tc.day, name, tc.name)
		}
		if IsTradingDay(day) {
			t.Fatalf("%s should not be a trading day", tc.day)
		}
	}
}

func TestHolidays_newYearsDayOnSaturdayIsNotObserved(t *testing.T) {
	t.Parallel()

	// January 1 2022 fell on a Saturday; the exchange traded a full day on
	// December 31 2021 rather than closing for it.
	dec31, err := time.ParseInLocation("2006-01-02", "2021-12-31", exchangeLocation)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if name, ok := holidayName(dec31); ok {
		t.Fatalf("2021-12-31 marked as %q, want a trading day", name)
	}
	if !IsTradingDay(dec31) {
		t.Fatalf("2021-12-31 should be a trading day")
	}
}

func TestHolidays_juneteenthNotObservedBefore2022(t *testing.T) {
	t.Parallel()

	day, err := time.ParseInLocation("2006-01-02", "2021-06-18", exchangeLocation)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if name, ok := holidayName(day); ok {
		t.Fatalf("2021-06-18 marked as %q, want a trading day", name)
	}
}

func TestEarlyClose_julyThirdOnlyWhenIndependenceDayIsAWeekday(t *testing.T) {
	t.Parallel()

	// 2025: July 4 is a Friday, so July 3 (Thursday) is a half day.
	if !isEarlyClose(date(2025, time.July, 3)) {
		t.Fatalf("2025-07-03 should be an early close")
	}
	// 2026: July 4 is a Saturday, observed Friday July 3 — a full closure, not a half day.
	if isEarlyClose(date(2026, time.July, 3)) {
		t.Fatalf("2026-07-03 is the observed holiday, not an early close")
	}
}
