package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// eventColumns lists the events columns, with the name of each one's door
// and the display name of its user, in the order eventFields reads them.
const eventColumns = `events.id, events.at, events.door_id, doors.name, events.type, events.outcome, events.reason,
	events.user_id, coalesce(users.display_name, ''), events.rule_id, events.request_id, events.detail, events.open_ms,
	events.start_reason`

// eventSource is the events table joined to their doors and users.
const eventSource = ` from events join doors on doors.id = events.door_id left join users on users.id = events.user_id`

// eventFilter keeps the events an EventQuery selects: $1 its door, $2 its
// user, $3 its type, $4 its outcome, $5 its reason, $6 and $7 its range,
// $8 its start reason.
const eventFilter = ` where ($1::bigint = 0 or events.door_id = $1) and ($2::bigint = 0 or events.user_id = $2)
	and ($3::text = '' or events.type = $3) and ($4::text = '' or events.outcome = $4) and ($5::text = '' or events.reason = $5)
	and ($6::timestamptz is null or events.at >= $6) and ($7::timestamptz is null or events.at < $7)
	and ($8::text = '' or events.start_reason = $8)`

func eventFields(e *connector.StoredEvent) []any {
	return []any{&e.ID, &e.At, &e.DoorID, &e.DoorName, &e.Type, &e.Outcome, &e.Reason,
		&e.UserID, &e.UserName, &e.RuleID, &e.RequestID, &e.Detail, &e.OpenMS, &e.StartReason}
}

// AddEvent inserts e and returns its id.
func (s *Store) AddEvent(ctx context.Context, e connector.StoredEvent) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into events (at, door_id, type, outcome, reason, user_id, rule_id, request_id, detail, open_ms, start_reason)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning id`,
		e.At, e.DoorID, e.Type, e.Outcome, e.Reason, e.UserID, e.RuleID, e.RequestID, e.Detail, e.OpenMS, e.StartReason).Scan(&id)
	return id, err
}

// Events returns the page of events q selects, newest first, and how many
// it selects in all.
func (s *Store) Events(ctx context.Context, q connector.EventQuery) ([]connector.StoredEvent, int, error) {
	args := []any{q.DoorID, q.UserID, q.Type, q.Outcome, q.Reason, q.From, q.To, q.StartReason}
	var total int
	if err := s.pool.QueryRow(ctx, `select count(*) from events`+eventFilter, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `select `+eventColumns+eventSource+eventFilter+`
		order by events.at desc, events.id desc offset $9 limit $10`, append(args, q.Offset, q.Limit)...)
	if err != nil {
		return nil, 0, err
	}
	events, err := pgx.CollectRows(rows, scanned(eventFields))
	return events, total, err
}
