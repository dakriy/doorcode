package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// rangeFilter keeps the events an EventRange selects: $1 its type, $2 and
// $3 its range. The queries that read days and hours take its zone's name
// as $4.
const rangeFilter = ` where events.type = $1 and events.at >= $2 and events.at < $3`

// The queries EventStats runs, each over rangeFilter.
const (
	dayCounts = `select (events.at at time zone $4)::date as day, events.outcome, count(*) from events` + rangeFilter + `
		group by day, events.outcome order by day, events.outcome collate "C"`
	hourCounts = `select extract(dow from events.at at time zone $4)::int as weekday, extract(hour from events.at at time zone $4)::int as hour,
		count(*) from events` + rangeFilter + ` group by weekday, hour order by weekday, hour`
	reasonCounts = `select events.outcome, events.reason, count(*) from events` + rangeFilter + `
		group by events.outcome, events.reason order by events.outcome collate "C", events.reason collate "C"`
	doorCounts = `select events.door_id, events.outcome, count(*) from events` + rangeFilter + `
		group by events.door_id, events.outcome order by events.door_id, events.outcome collate "C"`
	userCount = `select count(distinct events.user_id) from events` + rangeFilter
)

// EventStats sums up the events q selects with one grouping query each.
func (s *Store) EventStats(ctx context.Context, q connector.EventRange) (connector.StoredEventStats, error) {
	zoned := []any{q.Type, q.From, q.To, q.Zone.String()}
	args := zoned[:3]
	var stats connector.StoredEventStats
	err := s.pool.QueryRow(ctx, userCount, args...).Scan(&stats.Users)
	if err == nil {
		stats.Days, err = grouped(ctx, s, dayCounts, zoned, func(c *connector.StoredDayCount) []any { return []any{&c.Day, &c.Outcome, &c.Count} })
	}
	if err == nil {
		stats.Hours, err = grouped(ctx, s, hourCounts, zoned, func(c *connector.StoredHourCount) []any { return []any{&c.Weekday, &c.Hour, &c.Count} })
	}
	if err == nil {
		stats.Reasons, err = grouped(ctx, s, reasonCounts, args, func(c *connector.StoredReasonCount) []any { return []any{&c.Outcome, &c.Reason, &c.Count} })
	}
	if err == nil {
		stats.Doors, err = grouped(ctx, s, doorCounts, args, func(c *connector.StoredDoorCount) []any { return []any{&c.DoorID, &c.Outcome, &c.Count} })
	}
	return stats, err
}

// grouped returns the rows query selects with args, each read by fields.
func grouped[T any](ctx context.Context, s *Store, query string, args []any, fields func(*T) []any) ([]T, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(fields))
}

// trendQuery sums up the rollups a TrendQuery selects: $1 and $2 its
// range, $3 its zone's name, $4 its unit.
const trendQuery = `select door_id, date_trunc($4, hour, $3) as start, count(*), sum(samples),
	sum(signal_mean * samples) / sum(samples), sum(latency_mean * samples) / sum(samples), sum(opens)
	from telemetry_rollups where hour >= $1 and hour < $2 group by door_id, start order by door_id, start`

// Trends sums up the rollups q selects with one grouping query.
func (s *Store) Trends(ctx context.Context, q connector.TrendQuery) ([]connector.StoredTrend, error) {
	return grouped(ctx, s, trendQuery, []any{q.From, q.To, q.Zone.String(), q.Unit}, func(t *connector.StoredTrend) []any {
		return []any{&t.DoorID, &t.Start, &t.Hours, &t.Samples, &t.Signal, &t.Latency, &t.Opens}
	})
}

// FirmwareVersions counts the doors that are not deleted by the firmware
// version their controller last reported.
func (s *Store) FirmwareVersions(ctx context.Context) ([]connector.StoredVersionCount, error) {
	return grouped(ctx, s, `select c.firmware_version, count(*) as doors from door_controllers c join doors on doors.id = c.door_id
		where doors.deleted_at is null group by c.firmware_version order by doors desc, c.firmware_version collate "C"`, nil,
		func(v *connector.StoredVersionCount) []any { return []any{&v.Version, &v.Doors} })
}
