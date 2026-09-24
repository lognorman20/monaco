package marketcal

import (
	"fmt"
	"sync"
	"time"
)

// This is the NYSE/Nasdaq holiday rule set. Both exchanges publish the same list,
// so one table serves every US-listed equity an xStock tracks.
//
// Observance: a holiday on a Saturday moves back to the preceding Friday, one on a
// Sunday moves forward to the following Monday. New Year's Day is the exception —
// when January 1 is a Saturday the exchange does not close the December 31 before it.

type holidayRule struct {
	name string
	// on returns the holiday's actual date in a year, before observance is applied.
	on func(year int) time.Time
	// noSaturdayRollback marks a holiday that is simply not observed when it lands on
	// a Saturday, instead of moving to the Friday before.
	noSaturdayRollback bool
	// fixed marks a date-based holiday, which is the only kind that needs observance
	// shifting; the "nth weekday of month" ones always land on a weekday already.
	fixed bool
}

var holidayRules = []holidayRule{
	{name: "New Year's Day", fixed: true, noSaturdayRollback: true, on: func(y int) time.Time {
		return date(y, time.January, 1)
	}},
	{name: "Martin Luther King, Jr. Day", on: func(y int) time.Time {
		return nthWeekday(y, time.January, time.Monday, 3)
	}},
	{name: "Washington's Birthday", on: func(y int) time.Time {
		return nthWeekday(y, time.February, time.Monday, 3)
	}},
	{name: "Good Friday", on: func(y int) time.Time {
		return easterSunday(y).AddDate(0, 0, -2)
	}},
	{name: "Memorial Day", on: func(y int) time.Time {
		return lastWeekday(y, time.May, time.Monday)
	}},
	{name: "Juneteenth National Independence Day", fixed: true, on: func(y int) time.Time {
		return date(y, time.June, 19)
	}},
	{name: "Independence Day", fixed: true, on: func(y int) time.Time {
		return date(y, time.July, 4)
	}},
	{name: "Labor Day", on: func(y int) time.Time {
		return nthWeekday(y, time.September, time.Monday, 1)
	}},
	{name: "Thanksgiving Day", on: func(y int) time.Time {
		return nthWeekday(y, time.November, time.Thursday, 4)
	}},
	{name: "Christmas Day", fixed: true, on: func(y int) time.Time {
		return date(y, time.December, 25)
	}},
}

// juneteenthFirstObserved is the first year the exchange closed for Juneteenth.
// Charting a 1Y or ALL range crosses that boundary, so the table has to know it.
const juneteenthFirstObserved = 2022

var (
	holidayCacheMu sync.RWMutex
	holidayCache   = map[int]map[string]string{}
)

func holidayName(local time.Time) (string, bool) {
	table := holidaysForYear(local.Year())
	name, ok := table[dayKey(local)]
	return name, ok
}

func holidaysForYear(year int) map[string]string {
	holidayCacheMu.RLock()
	table, ok := holidayCache[year]
	holidayCacheMu.RUnlock()
	if ok {
		return table
	}

	table = buildHolidays(year)
	holidayCacheMu.Lock()
	holidayCache[year] = table
	holidayCacheMu.Unlock()
	return table
}

func buildHolidays(year int) map[string]string {
	table := make(map[string]string, len(holidayRules)+1)
	for _, rule := range holidayRules {
		if rule.name == "Juneteenth National Independence Day" && year < juneteenthFirstObserved {
			continue
		}
		observed, skipped := observe(rule)(year)
		if skipped {
			continue
		}
		table[dayKey(observed)] = rule.name
	}
	// A New Year's Day on a Sunday is observed on January 2, which belongs to the
	// same year. One on a Saturday is not observed at all, so nothing leaks into
	// the previous December.
	return table
}

func observe(rule holidayRule) func(year int) (time.Time, bool) {
	return func(year int) (time.Time, bool) {
		actual := rule.on(year)
		if !rule.fixed {
			return actual, false
		}
		switch actual.Weekday() {
		case time.Saturday:
			if rule.noSaturdayRollback {
				return time.Time{}, true
			}
			return actual.AddDate(0, 0, -1), false
		case time.Sunday:
			return actual.AddDate(0, 0, 1), false
		default:
			return actual, false
		}
	}
}

// isEarlyClose reports the 13:00 ET sessions: the Friday after Thanksgiving, July 3
// when Independence Day itself is a weekday, and Christmas Eve when it is a trading
// day in its own right.
func isEarlyClose(local time.Time) bool {
	year := local.Year()

	if sameDay(local, nthWeekday(year, time.November, time.Thursday, 4).AddDate(0, 0, 1)) {
		return true
	}

	july4 := date(year, time.July, 4)
	july3 := date(year, time.July, 3)
	if sameDay(local, july3) && isWeekday(july3) && isWeekday(july4) {
		return true
	}

	christmasEve := date(year, time.December, 24)
	if sameDay(local, christmasEve) && isWeekday(christmasEve) {
		if _, isHoliday := holidaysForYear(year)[dayKey(christmasEve)]; !isHoliday {
			return true
		}
	}
	return false
}

// easterSunday is the anonymous Gregorian computus. Good Friday is the only
// NYSE holiday that moves with the lunar calendar.
func easterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := ((h + l - 7*m + 114) % 31) + 1
	return date(year, time.Month(month), day)
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, exchangeLocation)
}

func nthWeekday(year int, month time.Month, weekday time.Weekday, n int) time.Time {
	first := date(year, month, 1)
	offset := (int(weekday) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, offset+7*(n-1))
}

func lastWeekday(year int, month time.Month, weekday time.Weekday) time.Time {
	// Day 0 of the next month is the last day of this one.
	last := date(year, month+1, 0)
	offset := (int(last.Weekday()) - int(weekday) + 7) % 7
	return last.AddDate(0, 0, -offset)
}

func isWeekday(day time.Time) bool {
	return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday
}

func sameDay(a, b time.Time) bool {
	return dayKey(a) == dayKey(b)
}

func dayKey(day time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", day.Year(), int(day.Month()), day.Day())
}
