package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// linkColumns lists the columns of a sample or rollup in the order
// linkFields reads them.
const linkColumns = `checks_sent, checks_lost, tcp_sent, tcp_retransmits, tcp_drops, wifi_drops, wifi_drop_reason, roams, bssid, channel`

func linkFields(c *connector.StoredLinkCounts, l *connector.StoredLink) []any {
	return []any{&c.ChecksSent, &c.ChecksLost, &c.TCPSent, &c.TCPRetransmits, &c.TCPDrops, &c.WifiDrops, &l.WifiDropReason, &c.Roams,
		&l.BSSID, &l.Channel}
}

// sampleColumns lists the telemetry_samples columns in the order
// sampleFields reads them.
const sampleColumns = `door_id, at, signal, latency_ms, uptime_s, free_bytes, firmware_version, reconnects, door_open, opens,
	mesh_layer, mesh_parent, mesh_children, config_digest, total_reconnects, total_opens, total_checks_sent, total_checks_lost,
	total_tcp_sent, total_tcp_retransmits, total_tcp_drops, total_wifi_drops, total_roams, ` + linkColumns

func sampleFields(s *connector.StoredSample) []any {
	t := &s.Totals
	fields := []any{&s.DoorID, &s.At, &s.Signal, &s.LatencyMS, &s.UptimeS, &s.FreeBytes, &s.FirmwareVersion, &s.Reconnects, &s.DoorOpen, &s.Opens,
		&s.MeshLayer, &s.MeshParent, &s.MeshChildren, &s.ConfigDigest, &t.Reconnects, &t.Opens, &t.ChecksSent, &t.ChecksLost,
		&t.TCPSent, &t.TCPRetransmits, &t.TCPDrops, &t.WifiDrops, &t.Roams}
	return append(fields, linkFields(&s.StoredLinkCounts, &s.StoredLink)...)
}

// rollupColumns lists the telemetry_rollups columns in the order
// rollupFields reads them.
const rollupColumns = `door_id, hour, samples, signal_mean, signal_min, signal_max, latency_samples, latency_mean, latency_min, latency_max,
	free_bytes_mean, free_bytes_min, free_bytes_max, opens, mesh_layer, mesh_children, ` + linkColumns

// latencyColumns is a rollup's latency spread as its columns hold it,
// each NULL for a rollup without one.
type latencyColumns struct {
	mean     *float64
	min, max *int64
}

// latencyColumnsOf is spread as its columns hold it.
func latencyColumnsOf(spread *connector.StoredSpread) latencyColumns {
	if spread == nil {
		return latencyColumns{}
	}
	return latencyColumns{mean: &spread.Mean, min: &spread.Min, max: &spread.Max}
}

func rollupFields(r *connector.StoredRollup, l *latencyColumns) []any {
	fields := []any{&r.DoorID, &r.Hour, &r.Samples, &r.Signal.Mean, &r.Signal.Min, &r.Signal.Max, &r.LatencySamples, &l.mean, &l.min, &l.max,
		&r.FreeBytes.Mean, &r.FreeBytes.Min, &r.FreeBytes.Max, &r.Opens, &r.MeshLayer, &r.MeshChildren}
	return append(fields, linkFields(&r.StoredLinkCounts, &r.StoredLink)...)
}

// scanRollup reads a rollup from row, its latency spread nil when its
// columns are NULL.
func scanRollup(row pgx.CollectableRow) (connector.StoredRollup, error) {
	var r connector.StoredRollup
	var l latencyColumns
	err := row.Scan(rollupFields(&r, &l)...)
	if l.mean != nil {
		r.Latency = &connector.StoredSpread{Mean: *l.mean, Min: *l.min, Max: *l.max}
	}
	return r, err
}

// AddSample inserts sample.
func (s *Store) AddSample(ctx context.Context, sample connector.StoredSample) error {
	_, err := s.pool.Exec(ctx, `insert into telemetry_samples (`+sampleColumns+`) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
		$14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33)`,
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
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
					$16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
				on conflict (door_id, hour) do update set samples = excluded.samples,
					signal_mean = excluded.signal_mean, signal_min = excluded.signal_min, signal_max = excluded.signal_max,
					latency_samples = excluded.latency_samples, latency_mean = excluded.latency_mean, latency_min = excluded.latency_min, latency_max = excluded.latency_max,
					free_bytes_mean = excluded.free_bytes_mean, free_bytes_min = excluded.free_bytes_min,
					free_bytes_max = excluded.free_bytes_max, opens = excluded.opens,
					mesh_layer = excluded.mesh_layer, mesh_children = excluded.mesh_children,
					checks_sent = excluded.checks_sent, checks_lost = excluded.checks_lost, tcp_sent = excluded.tcp_sent,
					tcp_retransmits = excluded.tcp_retransmits, tcp_drops = excluded.tcp_drops, wifi_drops = excluded.wifi_drops,
					wifi_drop_reason = excluded.wifi_drop_reason, roams = excluded.roams, bssid = excluded.bssid,
					channel = excluded.channel`, rollupArgs(r)...); err != nil {
				return err
			}
		}
		return nil
	})
}

// rollupArgs is r's columns, in the order of rollupColumns, to store.
func rollupArgs(r connector.StoredRollup) []any {
	l := latencyColumnsOf(r.Latency)
	return rollupFields(&r, &l)
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
	return pgx.CollectRows(rows, scanRollup)
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
