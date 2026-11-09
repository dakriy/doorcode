package memstore

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// rollupKey is the door and hour a rollup sums up.
type rollupKey struct {
	doorID int64
	hour   int64
}

// keyOf is the key r is stored under.
func keyOf(r connector.StoredRollup) rollupKey {
	return rollupKey{r.DoorID, r.Hour.Unix()}
}

// AddSample stores sample.
func (s *Store) AddSample(_ context.Context, sample connector.StoredSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.doors[sample.DoorID]; !ok {
		return errNoReference
	}
	s.samples = append(s.samples, sample)
	return nil
}

// samplesWhere returns the samples keep holds for, by door id and then
// time.
func (s *Store) samplesWhere(keep func(connector.StoredSample) bool) []connector.StoredSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := []connector.StoredSample{}
	for _, sample := range s.samples {
		if keep(sample) {
			samples = append(samples, sample)
		}
	}
	slices.SortStableFunc(samples, func(x, y connector.StoredSample) int {
		if c := cmp.Compare(x.DoorID, y.DoorID); c != 0 {
			return c
		}
		return x.At.Compare(y.At)
	})
	return samples
}

// Samples returns the door's samples from from up to to, oldest first.
func (s *Store) Samples(_ context.Context, doorID int64, from, to time.Time) ([]connector.StoredSample, error) {
	return s.samplesWhere(func(x connector.StoredSample) bool { return x.DoorID == doorID && within(x.At, from, to) }), nil
}

// AllSamples returns every door's samples from from up to to, by door and
// then time.
func (s *Store) AllSamples(_ context.Context, from, to time.Time) ([]connector.StoredSample, error) {
	return s.samplesWhere(func(x connector.StoredSample) bool { return within(x.At, from, to) }), nil
}

// FirstSampleSince returns when the earliest sample at or after since was
// taken.
func (s *Store) FirstSampleSince(_ context.Context, since time.Time) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first time.Time
	found := false
	for _, sample := range s.samples {
		if !sample.At.Before(since) && (!found || sample.At.Before(first)) {
			first = sample.At
			found = true
		}
	}
	return first, found, nil
}

// DeleteSamplesBefore removes the samples taken before before.
func (s *Store) DeleteSamplesBefore(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := []connector.StoredSample{}
	for _, sample := range s.samples {
		if !sample.At.Before(before) {
			kept = append(kept, sample)
		}
	}
	removed := len(s.samples) - len(kept)
	s.samples = kept
	return removed, nil
}

// PutRollups stores rollups, each replacing the one of its door and hour.
func (s *Store) PutRollups(_ context.Context, rollups []connector.StoredRollup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range rollups {
		if _, ok := s.doors[r.DoorID]; !ok {
			return errNoReference
		}
	}
	for _, r := range rollups {
		s.rollups[keyOf(r)] = r
	}
	return nil
}

// NewestRollup returns the latest hour a rollup sums up.
func (s *Store) NewestRollup(_ context.Context) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var newest time.Time
	found := false
	for _, r := range s.rollups {
		if !found || r.Hour.After(newest) {
			newest = r.Hour
			found = true
		}
	}
	return newest, found, nil
}

// Rollups returns the door's rollups whose hour starts from from up to to,
// oldest first.
func (s *Store) Rollups(_ context.Context, doorID int64, from, to time.Time) ([]connector.StoredRollup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rollups := []connector.StoredRollup{}
	for _, r := range s.rollups {
		if r.DoorID == doorID && within(r.Hour, from, to) {
			rollups = append(rollups, r)
		}
	}
	slices.SortFunc(rollups, func(x, y connector.StoredRollup) int { return x.Hour.Compare(y.Hour) })
	return rollups, nil
}

// DeleteRollupsBefore removes the rollups whose hour starts before before.
func (s *Store) DeleteRollupsBefore(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for key, r := range s.rollups {
		if r.Hour.Before(before) {
			delete(s.rollups, key)
			removed++
		}
	}
	return removed, nil
}

// startIn is the start of the hour or day of zone, as unit says, that t
// falls in.
func startIn(t time.Time, zone *time.Location, unit string) time.Time {
	local := t.In(zone)
	if unit == connector.ByHour {
		return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, zone)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
}

// trendSums is what Trends adds up for one door and start: the hours, the
// reports, the signal and latency means weighted by reports, and the
// opens.
type trendSums struct {
	trend           connector.StoredTrend
	signal, latency float64
}

// Trends sums up the rollups q selects by door and start.
func (s *Store) Trends(_ context.Context, q connector.TrendQuery) ([]connector.StoredTrend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sums := map[rollupKey]*trendSums{}
	for _, r := range s.rollups {
		if !within(r.Hour, q.From, q.To) {
			continue
		}
		start := startIn(r.Hour, q.Zone, q.Unit)
		key := rollupKey{r.DoorID, start.Unix()}
		sum, ok := sums[key]
		if !ok {
			sum = &trendSums{trend: connector.StoredTrend{DoorID: r.DoorID, Start: start}}
			sums[key] = sum
		}
		sum.trend.Hours++
		sum.trend.Samples += r.Samples
		sum.trend.Opens += r.Opens
		sum.signal += r.Signal.Mean * float64(r.Samples)
		sum.latency += r.Latency.Mean * float64(r.Samples)
	}
	trends := []connector.StoredTrend{}
	for _, sum := range sums {
		t := sum.trend
		if t.Samples > 0 {
			t.Signal = sum.signal / float64(t.Samples)
			t.Latency = sum.latency / float64(t.Samples)
		}
		trends = append(trends, t)
	}
	slices.SortFunc(trends, func(x, y connector.StoredTrend) int {
		if c := cmp.Compare(x.DoorID, y.DoorID); c != 0 {
			return c
		}
		return x.Start.Compare(y.Start)
	})
	return trends, nil
}
