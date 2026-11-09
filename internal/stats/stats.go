package stats

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/schedule"
)

// Store is what the dashboard's figures are read from.
type Store interface {
	Doors(ctx context.Context) ([]connector.StoredDoor, error)
	EventStats(ctx context.Context, q connector.EventRange) (connector.StoredEventStats, error)
	Trends(ctx context.Context, q connector.TrendQuery) ([]connector.StoredTrend, error)
	FirmwareVersions(ctx context.Context) ([]connector.StoredVersionCount, error)
}

// Service sums up the dashboard's figures.
type Service struct {
	Store Store
}

// hourlyDays is the longest range whose telemetry trends go an hour at a
// time; longer ones go a day at a time.
const hourlyDays = 7

// Entries is what happened when people entered codes at the doors over a
// range: the code checks the doors asked about, allowed and denied.
type Entries struct {
	Allowed int
	Denied  int
	// Users is how many people entered a code that is theirs, each
	// counted once.
	Users int
	// Days is every day of the range, oldest first.
	Days []Day
	// Weekdays is every weekday, Monday first, with its entries in each
	// hour of the day; Hours is each hour's entries over every weekday.
	Weekdays []Weekday
	Hours    [24]int
	// Reasons is why entries were denied, most often first.
	Reasons []Reason
	// Doors is every door that is not deleted and every deleted door
	// with entries, the most entries first and then by name.
	Doors []DoorEntries
}

// Total is how many entries there were.
func (e Entries) Total() int {
	return e.Allowed + e.Denied
}

// Day is how many entries were allowed and denied on Date, midnight UTC
// on the date in the site timezone.
type Day struct {
	Date    time.Time
	Allowed int
	Denied  int
}

// Total is how many entries there were on the day.
func (d Day) Total() int {
	return d.Allowed + d.Denied
}

// Weekday is how many entries fell in each hour of Day, and in all.
type Weekday struct {
	Day   time.Weekday
	Hours [24]int
	Total int
}

// Reason is how many entries were denied for Reason.
type Reason struct {
	Reason string
	Count  int
}

// DoorEntries is how many entries were allowed and denied at a door.
type DoorEntries struct {
	ID      int64
	Name    string
	Deleted bool
	Allowed int
	Denied  int
}

// Total is how many entries the door had.
func (d DoorEntries) Total() int {
	return d.Allowed + d.Denied
}

// Busiest is the n doors with the most entries, most first.
func (e Entries) Busiest(n int) []DoorEntries {
	return e.Doors[:min(n, len(e.Doors))]
}

// Quietest is up to n doors that are not deleted and not among the n
// busiest, the fewest entries first.
func (e Entries) Quietest(n int) []DoorEntries {
	out := []DoorEntries{}
	for _, d := range slices.Backward(e.Doors[min(n, len(e.Doors)):]) {
		if !d.Deleted && len(out) < n {
			out = append(out, d)
		}
	}
	return out
}

// Entries sums up the entries of r, its days and hours read in zone.
func (s Service) Entries(ctx context.Context, r Range, zone *time.Location) (Entries, error) {
	counted, err := s.Store.EventStats(ctx, connector.EventRange{Type: event.Check, From: r.From, To: r.To, Zone: zone})
	if err != nil {
		return Entries{}, err
	}
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return Entries{}, err
	}
	e := Entries{Users: counted.Users, Days: daysOf(r, counted.Days), Reasons: denials(counted.Reasons), Doors: doorEntries(doors, counted.Doors)}
	for _, d := range e.Days {
		e.Allowed += d.Allowed
		e.Denied += d.Denied
	}
	e.Weekdays, e.Hours = weekdays(counted.Hours)
	return e, nil
}

// daysOf is every day of r with its counts.
func daysOf(r Range, counts []connector.StoredDayCount) []Day {
	days := []Day{}
	for day := r.First; !day.After(r.Last); day = day.AddDate(0, 0, 1) {
		days = append(days, Day{Date: day})
	}
	for _, c := range counts {
		d := &days[int(c.Day.Sub(r.First).Hours()/24)]
		if c.Outcome == event.Allowed {
			d.Allowed = c.Count
		} else {
			d.Denied = c.Count
		}
	}
	return days
}

// weekdays is every weekday, Monday first, with counts, and each hour's
// counts over every weekday.
func weekdays(counts []connector.StoredHourCount) ([]Weekday, [24]int) {
	days := make([]Weekday, 7)
	var hours [24]int
	for i := range days {
		days[i].Day = schedule.Weekday(i)
	}
	for _, c := range counts {
		d := &days[schedule.DayIndex(c.Weekday)]
		d.Hours[c.Hour] = c.Count
		d.Total += c.Count
		hours[c.Hour] += c.Count
	}
	return days, hours
}

// denials is why entries were denied, most often first and then by
// reason.
func denials(counts []connector.StoredReasonCount) []Reason {
	out := []Reason{}
	for _, c := range counts {
		if c.Outcome == event.Denied {
			out = append(out, Reason{Reason: c.Reason, Count: c.Count})
		}
	}
	slices.SortStableFunc(out, func(a, b Reason) int { return cmp.Compare(b.Count, a.Count) })
	return out
}

// doorEntries is every door of doors that is not deleted or has counts,
// with them, the most entries first and then by name.
func doorEntries(doors []connector.StoredDoor, counts []connector.StoredDoorCount) []DoorEntries {
	byID := map[int64]*DoorEntries{}
	for _, d := range doors {
		byID[d.ID] = &DoorEntries{ID: d.ID, Name: d.Name, Deleted: d.DeletedAt != nil}
	}
	for _, c := range counts {
		d := byID[c.DoorID]
		if c.Outcome == event.Allowed {
			d.Allowed = c.Count
		} else {
			d.Denied = c.Count
		}
	}
	out := []DoorEntries{}
	for _, d := range doors {
		entries := *byID[d.ID]
		if !entries.Deleted || entries.Total() > 0 {
			out = append(out, entries)
		}
	}
	slices.SortStableFunc(out, func(a, b DoorEntries) int {
		return cmp.Or(cmp.Compare(b.Total(), a.Total()), strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
	})
	return out
}

// Health is how the doors kept in touch over a range: Unit is the length
// of each step of their trends (connector.ByHour or ByDay), Hours how many
// whole hours of the range have ended, and Doors every door that is not
// deleted and has a controller, in id order.
type Health struct {
	Unit  string
	Hours int
	Doors []DoorHealth
}

// DoorHealth is how a door's controller kept in touch over a range: how
// many hours it reported in and how many reports it made, how often the
// door was opened, the mean signal strength and latency over every report
// (nil without reports), and those of each step.
type DoorHealth struct {
	ID      int64
	Name    string
	Hours   int
	Samples int
	Opens   int
	Signal  *float64
	Latency *float64
	Trend   []connector.StoredTrend
}

// Uptime is the share of h's ended hours in which d's controller reported,
// and whether any hour has ended.
func (h Health) Uptime(d DoorHealth) (float64, bool) {
	if h.Hours == 0 {
		return 0, false
	}
	return float64(d.Hours) / float64(h.Hours), true
}

// Health sums up the doors' telemetry over r when it is now, its steps
// read in zone. A door's uptime counts the hours in which its controller
// reported, from the hourly rollups of its telemetry: the server keeps no
// history of online and offline, and a report is what shows the door
// reached the server.
func (s Service) Health(ctx context.Context, r Range, now time.Time, zone *time.Location) (Health, error) {
	h := Health{Unit: connector.ByDay, Hours: endedHours(r, now)}
	if r.Days() <= hourlyDays {
		h.Unit = connector.ByHour
	}
	trends, err := s.Store.Trends(ctx, connector.TrendQuery{From: r.From, To: r.To, Zone: zone, Unit: h.Unit})
	if err != nil {
		return Health{}, err
	}
	doors, err := s.Store.Doors(ctx)
	if err != nil {
		return Health{}, err
	}
	for _, d := range doors {
		if door.Enrolled(d) {
			h.Doors = append(h.Doors, healthOf(d, trends))
		}
	}
	slices.SortFunc(h.Doors, func(a, b DoorHealth) int { return cmp.Compare(a.ID, b.ID) })
	return h, nil
}

// healthOf is d's health from its trends among trends.
func healthOf(d connector.StoredDoor, trends []connector.StoredTrend) DoorHealth {
	out := DoorHealth{ID: d.ID, Name: d.Name, Trend: []connector.StoredTrend{}}
	var signal, latency float64
	for _, t := range trends {
		if t.DoorID != d.ID {
			continue
		}
		out.Trend = append(out.Trend, t)
		out.Hours += t.Hours
		out.Samples += t.Samples
		out.Opens += t.Opens
		signal += t.Signal * float64(t.Samples)
		latency += t.Latency * float64(t.Samples)
	}
	if out.Samples > 0 {
		signal, latency = signal/float64(out.Samples), latency/float64(out.Samples)
		out.Signal, out.Latency = &signal, &latency
	}
	return out
}

// endedHours is how many of the hours that start in r have ended by now.
func endedHours(r Range, now time.Time) int {
	first := r.From.Truncate(time.Hour)
	if first.Before(r.From) {
		first = first.Add(time.Hour)
	}
	end := now.Truncate(time.Hour)
	if r.To.Before(end) {
		end = r.To
	}
	return max(0, int(end.Sub(first)/time.Hour))
}

// Versions is how many doors run each firmware version, as their
// controllers last reported, most doors first.
func (s Service) Versions(ctx context.Context) ([]connector.StoredVersionCount, error) {
	return s.Store.FirmwareVersions(ctx)
}
