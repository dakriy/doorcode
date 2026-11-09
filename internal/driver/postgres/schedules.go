package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// scheduleColumns lists the schedules columns in the order scheduleFields
// reads them.
const scheduleColumns = `id, name, starts_on, ends_on, builtin`

func scheduleFields(sch *connector.StoredSchedule) []any {
	return []any{&sch.ID, &sch.Name, &sch.StartsOn, &sch.EndsOn, &sch.Builtin}
}

// Schedules returns every schedule in name order with its windows and
// exclusions.
func (s *Store) Schedules(ctx context.Context) ([]connector.StoredSchedule, error) {
	rows, err := s.pool.Query(ctx, `select `+scheduleColumns+` from schedules order by lower(name) collate "C", id`)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, scanned(scheduleFields))
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i], err = s.withLists(ctx, all[i]); err != nil {
			return nil, err
		}
	}
	return all, nil
}

// Schedule returns the schedule with id, its windows, and its exclusions.
func (s *Store) Schedule(ctx context.Context, id int64) (connector.StoredSchedule, bool, error) {
	var sch connector.StoredSchedule
	err := s.pool.QueryRow(ctx, `select `+scheduleColumns+` from schedules where id = $1`, id).Scan(scheduleFields(&sch)...)
	sch, ok, err := found(sch, err)
	if !ok {
		return sch, ok, err
	}
	sch, err = s.withLists(ctx, sch)
	return sch, err == nil, err
}

// withLists is sch with its windows and exclusions read.
func (s *Store) withLists(ctx context.Context, sch connector.StoredSchedule) (connector.StoredSchedule, error) {
	var err error
	if sch.Windows, err = s.windows(ctx, sch.ID); err != nil {
		return sch, err
	}
	sch.Excluded, err = s.exclusions(ctx, sch.ID)
	return sch, err
}

// windows returns the windows of the schedule with id in stored order.
func (s *Store) windows(ctx context.Context, id int64) ([]connector.StoredWindow, error) {
	rows, err := s.pool.Query(ctx, `
		select weekdays, starts_at, ends_at from schedule_windows where schedule_id = $1 order by position`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (connector.StoredWindow, error) {
		var w connector.StoredWindow
		err := row.Scan(&w.Days, &w.Start, &w.End)
		return w, err
	})
}

// exclusions returns the exclusions of the schedule with id in stored
// order.
func (s *Store) exclusions(ctx context.Context, id int64) ([]connector.StoredExclusion, error) {
	rows, err := s.pool.Query(ctx, `
		select first_day, last_day from schedule_exclusions where schedule_id = $1 order by position`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (connector.StoredExclusion, error) {
		var r connector.StoredExclusion
		err := row.Scan(&r.First, &r.Last)
		return r, err
	})
}

// CreateSchedule inserts sch, its windows, and its exclusions and returns
// its id.
func (s *Store) CreateSchedule(ctx context.Context, sch connector.StoredSchedule) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			insert into schedules (name, starts_on, ends_on) values ($1, $2, $3) returning id`,
			sch.Name, sch.StartsOn, sch.EndsOn).Scan(&id)
		if err != nil {
			return err
		}
		return insertLists(ctx, tx, id, sch)
	})
	return id, err
}

// UpdateSchedule stores sch over the schedule with sch.ID and replaces its
// windows and exclusions.
func (s *Store) UpdateSchedule(ctx context.Context, sch connector.StoredSchedule) (bool, error) {
	var updated bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		updated, err = replaceSchedule(ctx, tx, sch)
		return err
	})
	return updated, err
}

// replaceSchedule stores sch over the schedule with sch.ID and replaces its
// windows and exclusions within tx, reporting whether there is one.
func replaceSchedule(ctx context.Context, tx pgx.Tx, sch connector.StoredSchedule) (bool, error) {
	tag, err := tx.Exec(ctx, `
		update schedules set name = $2, starts_on = $3, ends_on = $4 where id = $1`,
		sch.ID, sch.Name, sch.StartsOn, sch.EndsOn)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `delete from schedule_windows where schedule_id = $1`, sch.ID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `delete from schedule_exclusions where schedule_id = $1`, sch.ID); err != nil {
		return false, err
	}
	return true, insertLists(ctx, tx, sch.ID, sch)
}

// insertLists inserts the windows and exclusions of sch as those of the
// schedule with id, in order.
func insertLists(ctx context.Context, tx pgx.Tx, id int64, sch connector.StoredSchedule) error {
	if err := insertWindows(ctx, tx, id, sch.Windows); err != nil {
		return err
	}
	return insertExclusions(ctx, tx, id, sch.Excluded)
}

// insertWindows inserts windows as the windows of the schedule with id, in
// order.
func insertWindows(ctx context.Context, tx pgx.Tx, id int64, windows []connector.StoredWindow) error {
	for i, w := range windows {
		_, err := tx.Exec(ctx, `
			insert into schedule_windows (schedule_id, position, weekdays, starts_at, ends_at) values ($1, $2, $3, $4, $5)`,
			id, i, w.Days, w.Start, w.End)
		if err != nil {
			return err
		}
	}
	return nil
}

// insertExclusions inserts exclusions as the exclusions of the schedule
// with id, in order.
func insertExclusions(ctx context.Context, tx pgx.Tx, id int64, exclusions []connector.StoredExclusion) error {
	for i, r := range exclusions {
		_, err := tx.Exec(ctx, `
			insert into schedule_exclusions (schedule_id, position, first_day, last_day) values ($1, $2, $3, $4)`,
			id, i, r.First, r.Last)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteSchedule removes the schedule with id; its windows and exclusions
// go with it.
func (s *Store) DeleteSchedule(ctx context.Context, id int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `delete from schedules where id = $1`, id)
	return tag.RowsAffected() == 1, err
}
