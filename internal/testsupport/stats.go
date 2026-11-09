package testsupport

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// EventStats sums up the events q selects, as the database's grouping
// would.
func (s *Store) EventStats(_ context.Context, q connector.EventRange) (connector.StoredEventStats, error) {
	if err := s.usable(eventsAggregate); err != nil {
		return connector.StoredEventStats{}, err
	}
	days := map[connector.StoredDayCount]int{}
	hours := map[connector.StoredHourCount]int{}
	reasons := map[connector.StoredReasonCount]int{}
	doors := map[connector.StoredDoorCount]int{}
	users := map[int64]bool{}
	for _, e := range s.events {
		if e.Type != q.Type || !between(e.At, q.From, q.To) {
			continue
		}
		local := e.At.In(q.Zone)
		days[connector.StoredDayCount{Day: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC), Outcome: e.Outcome}]++
		hours[connector.StoredHourCount{Weekday: local.Weekday(), Hour: local.Hour()}]++
		reasons[connector.StoredReasonCount{Outcome: e.Outcome, Reason: e.Reason}]++
		doors[connector.StoredDoorCount{DoorID: e.DoorID, Outcome: e.Outcome}]++
		if e.UserID != nil {
			users[*e.UserID] = true
		}
	}
	return connector.StoredEventStats{
		Days: counted(days, func(c connector.StoredDayCount, n int) connector.StoredDayCount { c.Count = n; return c },
			func(a, b connector.StoredDayCount) int {
				return cmp.Or(a.Day.Compare(b.Day), cmp.Compare(a.Outcome, b.Outcome))
			}),
		Hours: counted(hours, func(c connector.StoredHourCount, n int) connector.StoredHourCount { c.Count = n; return c },
			func(a, b connector.StoredHourCount) int {
				return cmp.Or(cmp.Compare(a.Weekday, b.Weekday), cmp.Compare(a.Hour, b.Hour))
			}),
		Reasons: counted(reasons, func(c connector.StoredReasonCount, n int) connector.StoredReasonCount { c.Count = n; return c },
			func(a, b connector.StoredReasonCount) int {
				return cmp.Or(cmp.Compare(a.Outcome, b.Outcome), cmp.Compare(a.Reason, b.Reason))
			}),
		Doors: counted(doors, func(c connector.StoredDoorCount, n int) connector.StoredDoorCount { c.Count = n; return c },
			func(a, b connector.StoredDoorCount) int {
				return cmp.Or(cmp.Compare(a.DoorID, b.DoorID), cmp.Compare(a.Outcome, b.Outcome))
			}),
		Users: len(users),
	}, nil
}

// counted is every group of groups with its count set by withCount,
// ordered by order.
func counted[K comparable](groups map[K]int, withCount func(K, int) K, order func(a, b K) int) []K {
	out := []K{}
	for group, n := range groups {
		out = append(out, withCount(group, n))
	}
	slices.SortFunc(out, order)
	return out
}

// trendKey is a door and the start of the hour or day a trend sums up.
type trendKey struct {
	door  int64
	start int64
}

// Trends sums up the rollups q selects by door and by the hour or day of
// q.Zone they start in, as the database's grouping would.
func (s *Store) Trends(_ context.Context, q connector.TrendQuery) ([]connector.StoredTrend, error) {
	if err := s.usable(telemetryAggregate); err != nil {
		return nil, err
	}
	sums := map[trendKey]*connector.StoredTrend{}
	for _, r := range s.rollups {
		if !between(r.Hour, q.From, q.To) {
			continue
		}
		start := startOf(r.Hour, q.Zone, q.Unit)
		key := trendKey{r.DoorID, start.Unix()}
		if sums[key] == nil {
			sums[key] = &connector.StoredTrend{DoorID: r.DoorID, Start: start}
		}
		t := sums[key]
		t.Hours++
		t.Samples += r.Samples
		t.Signal += r.Signal.Mean * float64(r.Samples)
		t.Latency += r.Latency.Mean * float64(r.Samples)
		t.Opens += r.Opens
	}
	out := []connector.StoredTrend{}
	for _, t := range sums {
		t.Signal /= float64(t.Samples)
		t.Latency /= float64(t.Samples)
		out = append(out, *t)
	}
	slices.SortFunc(out, func(a, b connector.StoredTrend) int {
		return cmp.Or(cmp.Compare(a.DoorID, b.DoorID), a.Start.Compare(b.Start))
	})
	return out, nil
}

// startOf is the start of the hour, or of the day when unit says so, of
// zone that at falls in.
func startOf(at time.Time, zone *time.Location, unit string) time.Time {
	local := at.In(zone)
	if unit == connector.ByDay {
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	}
	into := time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
	return at.Add(-into)
}

// FirmwareVersions counts the doors that are not deleted by the firmware
// version their controller last reported, most doors first.
func (s *Store) FirmwareVersions(_ context.Context) ([]connector.StoredVersionCount, error) {
	if err := s.usable(doorsAggregate); err != nil {
		return nil, err
	}
	versions := map[string]int{}
	for _, d := range s.doors {
		if d.DeletedAt == nil && d.Controller != nil {
			versions[d.Controller.FirmwareVersion]++
		}
	}
	out := []connector.StoredVersionCount{}
	for version, n := range versions {
		out = append(out, connector.StoredVersionCount{Version: version, Doors: n})
	}
	slices.SortFunc(out, func(a, b connector.StoredVersionCount) int {
		return cmp.Or(cmp.Compare(b.Doors, a.Doors), cmp.Compare(a.Version, b.Version))
	})
	return out, nil
}
