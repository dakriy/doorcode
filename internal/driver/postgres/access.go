package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// ruleColumns lists the access_rules columns, and the ids of each rule's
// zones, in the order ruleFields reads them.
const ruleColumns = `id, group_id, schedule_id,
	array(select zone_id from access_rule_zones where rule_id = access_rules.id order by zone_id)`

func ruleFields(r *connector.StoredRule) []any {
	return []any{&r.ID, &r.GroupID, &r.ScheduleID, &r.ZoneIDs}
}

// Rules returns every rule in id order.
func (s *Store) Rules(ctx context.Context) ([]connector.StoredRule, error) {
	rows, err := s.pool.Query(ctx, `select `+ruleColumns+` from access_rules order by id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(ruleFields))
}

// Rule returns the rule with id.
func (s *Store) Rule(ctx context.Context, id int64) (connector.StoredRule, bool, error) {
	var r connector.StoredRule
	err := s.pool.QueryRow(ctx, `select `+ruleColumns+` from access_rules where id = $1`, id).Scan(ruleFields(&r)...)
	return found(r, err)
}

// CreateRule inserts r with its zones and returns its id.
func (s *Store) CreateRule(ctx context.Context, r connector.StoredRule) (int64, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `insert into access_rules (group_id, schedule_id) values ($1, $2) returning id`,
			r.GroupID, r.ScheduleID).Scan(&r.ID)
		if err != nil {
			return err
		}
		return linkRule(ctx, tx, r)
	})
	return r.ID, err
}

// UpdateRule stores r over the rule with r.ID and replaces its zones.
func (s *Store) UpdateRule(ctx context.Context, r connector.StoredRule) (bool, error) {
	var updated bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update access_rules set group_id = $2, schedule_id = $3 where id = $1`,
			r.ID, r.GroupID, r.ScheduleID)
		updated = tag.RowsAffected() == 1
		if err != nil || !updated {
			return err
		}
		return linkRule(ctx, tx, r)
	})
	return updated, err
}

// linkRule replaces the zones of the rule with r.ID within tx.
func linkRule(ctx context.Context, tx pgx.Tx, r connector.StoredRule) error {
	_, err := tx.Exec(ctx, `delete from access_rule_zones where rule_id = $1`, r.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `insert into access_rule_zones (rule_id, zone_id) select $1, unnest($2::bigint[])`, r.ID, r.ZoneIDs)
	return err
}

// DeleteRule removes the rule with id; its zones go with it.
func (s *Store) DeleteRule(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from access_rules where id = $1`, id)
	return tag.RowsAffected() == 1, err
}

// overrideColumns lists the overrides columns, and the display name of
// each one's creator, in the order overrideFields reads them.
const overrideColumns = `overrides.id, overrides.door_id, overrides.kind, overrides.starts_at, overrides.ends_at,
	overrides.reason, overrides.created_by, users.display_name, overrides.created_at`

// overrideSource is the overrides table joined to the users who created
// them.
const overrideSource = ` from overrides join users on users.id = overrides.created_by`

func overrideFields(o *connector.StoredOverride) []any {
	return []any{&o.ID, &o.DoorID, &o.Kind, &o.Start, &o.End, &o.Reason, &o.CreatedBy, &o.CreatorName, &o.CreatedAt}
}

// DoorOverrides returns the door's overrides that end after endsAfter, by
// start and then id.
func (s *Store) DoorOverrides(ctx context.Context, doorID int64, endsAfter time.Time) ([]connector.StoredOverride, error) {
	rows, err := s.pool.Query(ctx, `select `+overrideColumns+overrideSource+`
		where overrides.door_id = $1 and overrides.ends_at > $2 order by overrides.starts_at, overrides.id`, doorID, endsAfter)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(overrideFields))
}

// Override returns the override with id.
func (s *Store) Override(ctx context.Context, id int64) (connector.StoredOverride, bool, error) {
	var o connector.StoredOverride
	err := s.pool.QueryRow(ctx, `select `+overrideColumns+overrideSource+` where overrides.id = $1`, id).Scan(overrideFields(&o)...)
	return found(o, err)
}

// CreateOverride inserts o and returns its id.
func (s *Store) CreateOverride(ctx context.Context, o connector.StoredOverride) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into overrides (door_id, kind, starts_at, ends_at, reason, created_by, created_at)
		values ($1, $2, $3, $4, $5, $6, $7) returning id`,
		o.DoorID, o.Kind, o.Start, o.End, o.Reason, o.CreatedBy, o.CreatedAt).Scan(&id)
	return id, err
}

// UpdateOverride stores o's kind, start, end, and reason over the override
// with o.ID.
func (s *Store) UpdateOverride(ctx context.Context, o connector.StoredOverride) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update overrides set kind = $2, starts_at = $3, ends_at = $4, reason = $5 where id = $1`,
		o.ID, o.Kind, o.Start, o.End, o.Reason)
	return tag.RowsAffected() == 1, err
}

// DeleteOverride removes the override with id.
func (s *Store) DeleteOverride(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from overrides where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
