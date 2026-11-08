package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// sampleColumns lists the telemetry_samples columns in the order
// sampleFields reads them.
const sampleColumns = `door_id, at, signal, latency_ms, uptime_s, free_bytes, firmware_version, reconnects, door_open, opens`

func sampleFields(s *connector.StoredSample) []any {
	return []any{&s.DoorID, &s.At, &s.Signal, &s.LatencyMS, &s.UptimeS, &s.FreeBytes, &s.FirmwareVersion, &s.Reconnects, &s.DoorOpen, &s.Opens}
}

// rollupColumns lists the telemetry_rollups columns in the order
// rollupFields reads them.
const rollupColumns = `door_id, hour, samples, signal_mean, signal_min, signal_max, latency_mean, latency_min, latency_max,
	free_bytes_mean, free_bytes_min, free_bytes_max, opens`

func rollupFields(r *connector.StoredRollup) []any {
	return []any{&r.DoorID, &r.Hour, &r.Samples, &r.Signal.Mean, &r.Signal.Min, &r.Signal.Max, &r.Latency.Mean, &r.Latency.Min, &r.Latency.Max,
		&r.FreeBytes.Mean, &r.FreeBytes.Min, &r.FreeBytes.Max, &r.Opens}
}

// AddSample inserts sample.
func (s *Store) AddSample(ctx context.Context, sample connector.StoredSample) error {
	_, err := s.pool.Exec(ctx, `insert into telemetry_samples (`+sampleColumns+`) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		sampleFields(&sample)...)
	return err
}

// Samples returns the samples of the door with doorID taken from from up
// to to, oldest first.
func (s *Store) Samples(ctx context.Context, doorID int64, from, to time.Time) ([]connector.StoredSample, error) {
	return s.samples(ctx, `select `+sampleColumns+` from telemetry_samples
		where door_id = $1 and at >= $2 and at < $3 order by at`, doorID, from, to)
}

// AllSamples returns the samples of every door taken from from up to to,
// by door and then by time.
func (s *Store) AllSamples(ctx context.Context, from, to time.Time) ([]connector.StoredSample, error) {
	return s.samples(ctx, `select `+sampleColumns+` from telemetry_samples
		where at >= $1 and at < $2 order by door_id, at`, from, to)
}

// samples returns the samples query selects with args.
func (s *Store) samples(ctx context.Context, query string, args ...any) ([]connector.StoredSample, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(sampleFields))
}

// FirstSampleSince returns when the earliest sample at or after since was
// taken.
func (s *Store) FirstSampleSince(ctx context.Context, since time.Time) (time.Time, bool, error) {
	var first *time.Time
	err := s.pool.QueryRow(ctx, `select min(at) from telemetry_samples where at >= $1`, since).Scan(&first)
	return deref(first), first != nil, err
}

// DeleteSamplesBefore removes the samples taken before before.
func (s *Store) DeleteSamplesBefore(ctx context.Context, before time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `delete from telemetry_samples where at < $1`, before)
	return int(tag.RowsAffected()), err
}

// PutRollups inserts rollups, each replacing the one of its door and hour.
func (s *Store) PutRollups(ctx context.Context, rollups []connector.StoredRollup) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range rollups {
			if _, err := tx.Exec(ctx, `insert into telemetry_rollups (`+rollupColumns+`)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
				on conflict (door_id, hour) do update set samples = excluded.samples,
					signal_mean = excluded.signal_mean, signal_min = excluded.signal_min, signal_max = excluded.signal_max,
					latency_mean = excluded.latency_mean, latency_min = excluded.latency_min, latency_max = excluded.latency_max,
					free_bytes_mean = excluded.free_bytes_mean, free_bytes_min = excluded.free_bytes_min,
					free_bytes_max = excluded.free_bytes_max, opens = excluded.opens`, rollupFields(&r)...); err != nil {
				return err
			}
		}
		return nil
	})
}

// NewestRollup returns the latest hour a rollup sums up.
func (s *Store) NewestRollup(ctx context.Context) (time.Time, bool, error) {
	var newest *time.Time
	err := s.pool.QueryRow(ctx, `select max(hour) from telemetry_rollups`).Scan(&newest)
	return deref(newest), newest != nil, err
}

// Rollups returns the rollups of the door with doorID whose hour starts
// from from up to to, oldest first.
func (s *Store) Rollups(ctx context.Context, doorID int64, from, to time.Time) ([]connector.StoredRollup, error) {
	rows, err := s.pool.Query(ctx, `select `+rollupColumns+` from telemetry_rollups
		where door_id = $1 and hour >= $2 and hour < $3 order by hour`, doorID, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(rollupFields))
}

// DeleteRollupsBefore removes the rollups whose hour starts before before.
func (s *Store) DeleteRollupsBefore(ctx context.Context, before time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `delete from telemetry_rollups where hour < $1`, before)
	return int(tag.RowsAffected()), err
}

// deref is the time t points to, the zero time when it is nil.
func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
