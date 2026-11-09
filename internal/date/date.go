// Package date holds calendar dates and weekdays as the whole application
// writes and reads them. A date is midnight UTC on the day it names,
// whatever zone the day is in; Of and StartOf convert between a date and
// the instants of its day in a zone.
package date

import "time"

// Layout is how a date is written: in fields, in the API, and by date
// inputs.
const Layout = "2006-01-02"

// Of is the date t falls on in zone, as midnight UTC on that date.
func Of(t time.Time, zone *time.Location) time.Time {
	local := t.In(zone)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// StartOf is the start of the date day in zone.
func StartOf(day time.Time, zone *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, zone)
}

// DayNames names the weekdays as fields and the API write them, Monday
// first; the name at index i is Weekday(i).
var DayNames = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// Weekday is the weekday at index i of DayNames.
func Weekday(i int) time.Weekday {
	return time.Weekday((i + 1) % 7)
}

// DayIndex is the index in DayNames of day, the inverse of Weekday.
func DayIndex(day time.Weekday) int {
	return (int(day) + 6) % 7
}
