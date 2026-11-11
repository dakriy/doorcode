package telemetry

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// rollUp sums up the reports of every hour that has ended since the
// newest rollup, door by door.
func (s Service) rollUp(ctx context.Context) error {
	newest, rolled, err := s.Store.NewestRollup(ctx)
	if err != nil {
		return err
	}
	since := time.Time{}
	if rolled {
		since = newest.Add(time.Hour)
	}
	first, found, err := s.Store.FirstSampleSince(ctx, since)
	if err != nil || !found {
		return err
	}
	from, to := first.Truncate(time.Hour), s.Clock.Now().Truncate(time.Hour)
	if !from.Before(to) {
		return nil
	}
	samples, err := s.Store.AllSamples(ctx, from, to)
	if err != nil {
		return err
	}
	return s.Store.PutRollups(ctx, Hourly(samples))
}

// Hourly sums up samples, ordered by door and then by time, an hour of a
// door at a time.
func Hourly(samples []connector.StoredSample) []connector.StoredRollup {
	var hours [][]connector.StoredSample
	for _, sample := range samples {
		last := len(hours) - 1
		if last < 0 || !sameHour(hours[last][0], sample) {
			hours = append(hours, nil)
			last++
		}
		hours[last] = append(hours[last], sample)
	}
	out := []connector.StoredRollup{}
	for _, hour := range hours {
		out = append(out, RollUp(hour))
	}
	return out
}

// RollUp sums up hour, at least one report of one door in one hour,
// oldest first: the spread of their signal strength, latency, and free
// memory, how often the door was opened and how its links fared in all,
// and the mesh layer and children and the Wi-Fi link of the last report.
func RollUp(hour []connector.StoredSample) connector.StoredRollup {
	counts := connector.StoredCounts{}
	for _, sample := range hour {
		counts = Added(counts, sample.StoredCounts)
	}
	last := hour[len(hour)-1]
	return connector.StoredRollup{
		DoorID: hour[0].DoorID, Hour: hour[0].At.Truncate(time.Hour), Samples: len(hour), Opens: counts.Opens,
		MeshLayer: last.MeshLayer, MeshChildren: last.MeshChildren, StoredLinkCounts: counts.StoredLinkCounts, StoredLink: last.StoredLink,
		Signal:    spread(hour, func(s connector.StoredSample) int64 { return int64(s.Signal) }),
		Latency:   spread(hour, func(s connector.StoredSample) int64 { return int64(s.LatencyMS) }),
		FreeBytes: spread(hour, func(s connector.StoredSample) int64 { return s.FreeBytes }),
	}
}

// sameHour reports whether a and b are reports of one door in one hour.
func sameHour(a, b connector.StoredSample) bool {
	return a.DoorID == b.DoorID && a.At.Truncate(time.Hour).Equal(b.At.Truncate(time.Hour))
}

// spread is how the value of each of samples, at least one, ranged.
func spread(samples []connector.StoredSample, value func(connector.StoredSample) int64) connector.StoredSpread {
	first := value(samples[0])
	out := connector.StoredSpread{Min: first, Max: first}
	var sum int64
	for _, sample := range samples {
		v := value(sample)
		sum += v
		out.Min, out.Max = min(out.Min, v), max(out.Max, v)
	}
	out.Mean = float64(sum) / float64(len(samples))
	return out
}

// purge removes the reports and the rollups older than current keeps
// them.
func (s Service) purge(ctx context.Context, current settings.Current) error {
	now := s.Clock.Now()
	if _, err := s.Store.DeleteSamplesBefore(ctx, now.Add(-current.TelemetryRetention)); err != nil {
		return err
	}
	_, err := s.Store.DeleteRollupsBefore(ctx, now.Add(-current.RollupRetention))
	return err
}

// Samples returns the reports of the door whose id is doorID from from up
// to to, oldest first.
func (s Service) Samples(ctx context.Context, doorID int64, from, to time.Time) ([]connector.StoredSample, error) {
	if _, err := s.Doors.Door(ctx, doorID); err != nil {
		return nil, err
	}
	return s.Store.Samples(ctx, doorID, from, to)
}

// Rollups returns the hourly rollups of the reports of the door whose id
// is doorID for the hours that start from from up to to, oldest first.
func (s Service) Rollups(ctx context.Context, doorID int64, from, to time.Time) ([]connector.StoredRollup, error) {
	if _, err := s.Doors.Door(ctx, doorID); err != nil {
		return nil, err
	}
	return s.Store.Rollups(ctx, doorID, from, to)
}
