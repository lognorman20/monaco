package marketcal

import (
	"testing"
	"time"
)

func TestLastTradingSession_onANonTradingNow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		now  time.Time
		want time.Time // the session's calendar date, at the exchange
	}{
		{
			// A 1D chart asked for on a Saturday is about Friday's session. Nothing in
			// the bars says so — an equity feed keeps republishing its frozen last
			// price all weekend — so the calendar has to.
			name: "saturday resolves to friday",
			now:  time.Date(2026, time.September, 26, 12, 0, 0, 0, exchangeLocation),
			want: date(2026, time.September, 25),
		},
		{
			name: "sunday resolves to friday",
			now:  time.Date(2026, time.September, 27, 20, 0, 0, 0, exchangeLocation),
			want: date(2026, time.September, 25),
		},
		{
			name: "thanksgiving resolves to the wednesday before",
			now:  time.Date(2026, time.November, 26, 12, 0, 0, 0, exchangeLocation),
			want: date(2026, time.November, 25),
		},
		{
			// Good Friday shuts the exchange, so the Thursday before is the session —
			// not the Monday after, and not the holiday itself.
			name: "good friday resolves to maundy thursday",
			now:  time.Date(2026, time.April, 3, 11, 0, 0, 0, exchangeLocation),
			want: date(2026, time.April, 2),
		},
		{
			// 02:00 ET, before the 04:00 pre-market bell: today has not started
			// trading yet, so the session being looked at is still yesterday's.
			name: "overnight before pre-market resolves to the previous day",
			now:  time.Date(2026, time.September, 22, 2, 0, 0, 0, exchangeLocation),
			want: date(2026, time.September, 21),
		},
		{
			name: "inside pre-market resolves to today",
			now:  time.Date(2026, time.September, 22, 5, 0, 0, 0, exchangeLocation),
			want: date(2026, time.September, 22),
		},
		{
			name: "after the bell resolves to today",
			now:  time.Date(2026, time.September, 22, 18, 0, 0, 0, exchangeLocation),
			want: date(2026, time.September, 22),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			session, ok := LastTradingSession(tc.now)
			if !ok {
				t.Fatal("expected a session inside the scan horizon")
			}
			if got := session.Day.In(exchangeLocation); !sameDay(got, tc.want) {
				t.Fatalf("session day = %s, want %s", got.Format("2006-01-02"), tc.want.Format("2006-01-02"))
			}
			if session.RegularOpen.Location() != time.UTC {
				t.Fatal("session boundaries must cross the package boundary in UTC")
			}
			local := session.RegularOpen.In(exchangeLocation)
			if local.Hour() != 9 || local.Minute() != 30 {
				t.Fatalf("regular open = %s, want 09:30 ET", local.Format("15:04"))
			}
			if !session.PreMarketOpen.Before(session.RegularOpen) ||
				!session.RegularOpen.Before(session.RegularClose) ||
				!session.RegularClose.Before(session.PostCloseEnd) {
				t.Fatalf("session boundaries are out of order: %+v", session)
			}
		})
	}
}

func TestPreviousTradingSession_skipsTheWeekendAndTheHoliday(t *testing.T) {
	t.Parallel()

	// The previous close a Monday chart measures against is Friday's, not Sunday's.
	session, ok := PreviousTradingSession(date(2026, time.September, 28))
	if !ok {
		t.Fatal("expected a previous session")
	}
	if got := session.RegularClose.In(exchangeLocation); !sameDay(got, date(2026, time.September, 25)) {
		t.Fatalf("previous close day = %s, want 2026-09-25", got.Format("2006-01-02"))
	}

	// The Friday after Thanksgiving closes at 13:00 ET, so the previous close the
	// Monday after it draws is a 13:00 print, not a 16:00 one.
	halfDay, ok := PreviousTradingSession(date(2026, time.November, 30))
	if !ok {
		t.Fatal("expected a previous session")
	}
	if !halfDay.EarlyClose {
		t.Fatal("2026-11-27 is a half day")
	}
	if got := halfDay.RegularClose.In(exchangeLocation); got.Hour() != 13 {
		t.Fatalf("half-day close = %s, want 13:00 ET", got.Format("15:04"))
	}
}

func TestSessionOn_holidayAndWeekendHaveNoSession(t *testing.T) {
	t.Parallel()

	if _, ok := SessionOn(date(2026, time.November, 26)); ok {
		t.Fatal("Thanksgiving is not a session")
	}
	if _, ok := SessionOn(date(2026, time.September, 26)); ok {
		t.Fatal("Saturday is not a session")
	}
	if _, ok := SessionOn(date(2026, time.September, 25)); !ok {
		t.Fatal("Friday 2026-09-25 is a session")
	}
}
