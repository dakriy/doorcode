package testsupport

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// AddSample stores sample, refusing one of a door the stub does not hold.
func (s *Store) AddSample(_ context.Context, sample connector.StoredSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(telemetryAggregate, telemetryWrites); err != nil {
		return err
	}
	if s.doorByID(sample.DoorID) == nil {
		return ErrNoSuchReference
	}
	s.samples = append(s.samples, sample)
	return nil
}

// Samples returns the samples of the door with doorID taken from from up
// to to, oldest first.
func (s *Store) Samples(_ context.Context, doorID int64, from, to time.Time) ([]connector.StoredSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samplesWhere(func(sample connector.StoredSample) bool {
		return sample.DoorID == doorID && between(sample.At, from, to)
	})
}

// AllSamples returns the samples of every door taken from from up to to,
// by door and then by time.
func (s *Store) AllSamples(_ context.Context, from, to time.Time) ([]connector.StoredSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samplesWhere(func(sample connector.StoredSample) bool { return between(sample.At, from, to) })
}

// samplesWhere returns the samples keep keeps, by door and then by
// time.
func (s *Store) samplesWhere(keep func(connector.StoredSample) bool) ([]connector.StoredSample, error) {
	if err := s.writable(telemetryAggregate, sampleReads); err != nil {
		return nil, err
	}
	kept := []connector.StoredSample{}
	for _, sample := range s.samples {
		if keep(sample) {
			kept = append(kept, sample)
		}
	}
	slices.SortStableFunc(kept, func(a, b connector.StoredSample) int {
		return cmp.Or(cmp.Compare(a.DoorID, b.DoorID), a.At.Compare(b.At))
	})
	return kept, nil
}

// between reports whether at is at or after from and before to.
func between(at, from, to time.Time) bool {
	return !at.Before(from) && at.Before(to)
}

// FirstSampleSince returns when the earliest sample at or after since was
// taken.
func (s *Store) FirstSampleSince(_ context.Context, since time.Time) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(telemetryAggregate); err != nil {
		return time.Time{}, false, err
	}
	var first time.Time
	found := false
	for _, sample := range s.samples {
		if !sample.At.Before(since) && (!found || sample.At.Before(first)) {
			first, found = sample.At, true
		}
	}
	return first, found, nil
}

// DeleteSamplesBefore removes the samples taken before before.
func (s *Store) DeleteSamplesBefore(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(telemetryAggregate, telemetryWrites); err != nil {
		return 0, err
	}
	kept := s.samples[:0:0]
	for _, sample := range s.samples {
		if !sample.At.Before(before) {
			kept = append(kept, sample)
		}
	}
	removed := len(s.samples) - len(kept)
	s.samples = kept
	return removed, nil
}

// PutRollups stores rollups over those of the same door and hour,
// refusing one of a door the stub does not hold.
func (s *Store) PutRollups(_ context.Context, rollups []connector.StoredRollup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(telemetryAggregate, telemetryWrites); err != nil {
		return err
	}
	for _, r := range rollups {
		if s.doorByID(r.DoorID) == nil {
			return ErrNoSuchReference
		}
	}
	for _, r := range rollups {
		s.rollups = slices.DeleteFunc(s.rollups, func(held connector.StoredRollup) bool {
			return held.DoorID == r.DoorID && held.Hour.Equal(r.Hour)
		})
		s.rollups = append(s.rollups, r)
	}
	return nil
}

// NewestRollup returns the latest hour a rollup sums up.
func (s *Store) NewestRollup(context.Context) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(telemetryAggregate); err != nil {
		return time.Time{}, false, err
	}
	var newest time.Time
	for _, r := range s.rollups {
		if r.Hour.After(newest) {
			newest = r.Hour
		}
	}
	return newest, len(s.rollups) > 0, nil
}

// Rollups returns the rollups of the door with doorID whose hour starts
// from from up to to, oldest first.
func (s *Store) Rollups(_ context.Context, doorID int64, from, to time.Time) ([]connector.StoredRollup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(telemetryAggregate); err != nil {
		return nil, err
	}
	kept := []connector.StoredRollup{}
	for _, r := range s.rollups {
		if r.DoorID == doorID && between(r.Hour, from, to) {
			kept = append(kept, r)
		}
	}
	slices.SortFunc(kept, func(a, b connector.StoredRollup) int { return a.Hour.Compare(b.Hour) })
	return kept, nil
}

// DeleteRollupsBefore removes the rollups whose hour starts before before.
func (s *Store) DeleteRollupsBefore(_ context.Context, before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(telemetryAggregate, telemetryWrites); err != nil {
		return 0, err
	}
	held := len(s.rollups)
	s.rollups = slices.DeleteFunc(s.rollups, func(r connector.StoredRollup) bool { return r.Hour.Before(before) })
	return held - len(s.rollups), nil
}

// FailTelemetry makes the telemetry methods report ErrUnavailable.
func (s *Store) FailTelemetry() {
	s.failing[telemetryAggregate] = true
}

// FailSampleReads makes Samples and AllSamples report ErrUnavailable, while the rest of
// the telemetry history still works.
func (s *Store) FailSampleReads() {
	s.failing[sampleReads] = true
}

// FailTelemetryWrites makes the telemetry methods that change the history
// report ErrUnavailable, while it can still be read.
func (s *Store) FailTelemetryWrites() {
	s.failing[telemetryWrites] = true
}
