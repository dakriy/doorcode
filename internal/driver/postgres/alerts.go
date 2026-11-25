package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
)

// mailColumns lists the mail_outbox columns in the order mailFields reads
// them.
const mailColumns = `id, kind, subject, body, recipients, status, attempts, created_at, next_attempt_at,
	last_attempt_at, sent_at, last_error`

func mailFields(m *connector.StoredMail) []any {
	return []any{&m.ID, &m.Kind, &m.Subject, &m.Text, &m.To, &m.Status, &m.Attempts, &m.CreatedAt, &m.NextAttemptAt,
		&m.LastAttemptAt, &m.SentAt, &m.LastError}
}

// AlertKinds returns the state stored for each kind of alert.
func (s *Store) AlertKinds(ctx context.Context) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `select kind, state from alert_kinds`)
	if err != nil {
		return nil, err
	}
	kinds := map[string]string{}
	var kind, state string
	_, err = pgx.ForEachRow(rows, []any{&kind, &state}, func() error {
		kinds[kind] = state
		return nil
	})
	return kinds, err
}

// SetAlertKind stores state as the state of the kind of alert named kind.
func (s *Store) SetAlertKind(ctx context.Context, kind, state string) error {
	_, err := s.pool.Exec(ctx, `insert into alert_kinds (kind, state) values ($1, $2)
		on conflict (kind) do update set state = excluded.state`, kind, state)
	return err
}

// conditionColumns lists the alert_conditions columns in the order
// conditionFields reads them.
const conditionColumns = `kind, subject, active, mailed, owed, muted, changed_at, name, detail`

func conditionFields(c *connector.StoredCondition) []any {
	return []any{&c.Kind, &c.Subject, &c.Active, &c.Mailed, &c.Owed, &c.Muted, &c.ChangedAt, &c.Name, &c.Detail}
}

// AlertCondition returns the condition of kind about subject.
func (s *Store) AlertCondition(ctx context.Context, kind, subject string) (connector.StoredCondition, bool, error) {
	var c connector.StoredCondition
	err := s.pool.QueryRow(ctx, `select `+conditionColumns+` from alert_conditions where kind = $1 and subject = $2`,
		kind, subject).Scan(conditionFields(&c)...)
	return found(c, err)
}

// PutAlertCondition stores c over the condition of its kind and subject.
func (s *Store) PutAlertCondition(ctx context.Context, c connector.StoredCondition) error {
	_, err := s.pool.Exec(ctx, `insert into alert_conditions (`+conditionColumns+`) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (kind, subject) do update set active = excluded.active, mailed = excluded.mailed, owed = excluded.owed,
			muted = excluded.muted, changed_at = excluded.changed_at, name = excluded.name, detail = excluded.detail`,
		c.Kind, c.Subject, c.Active, c.Mailed, c.Owed, c.Muted, c.ChangedAt, c.Name, c.Detail)
	return err
}

// ActiveAlertConditions returns every active condition by kind and
// subject.
func (s *Store) ActiveAlertConditions(ctx context.Context) ([]connector.StoredCondition, error) {
	return s.conditionsWhere(ctx, "active")
}

// OwedAlertConditions returns every condition whose raise mail is owed,
// by kind and subject.
func (s *Store) OwedAlertConditions(ctx context.Context) ([]connector.StoredCondition, error) {
	return s.conditionsWhere(ctx, "owed")
}

// conditionsWhere returns the conditions whose boolean column column is
// true, by kind and subject.
func (s *Store) conditionsWhere(ctx context.Context, column string) ([]connector.StoredCondition, error) {
	rows, err := s.pool.Query(ctx, `select `+conditionColumns+` from alert_conditions
		where `+column+` order by kind collate "C", subject collate "C"`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(conditionFields))
}

// AddMail stores m in the outbox and returns its id.
func (s *Store) AddMail(ctx context.Context, m connector.StoredMail) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		insert into mail_outbox (kind, subject, body, recipients, status, attempts, created_at, next_attempt_at,
			last_attempt_at, sent_at, last_error)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) returning id`,
		m.Kind, m.Subject, m.Text, m.To, m.Status, m.Attempts, m.CreatedAt, m.NextAttemptAt,
		m.LastAttemptAt, m.SentAt, m.LastError).Scan(&id)
	return id, err
}

// MailWithStatus returns the outbox's messages whose status is status,
// oldest first.
func (s *Store) MailWithStatus(ctx context.Context, status string) ([]connector.StoredMail, error) {
	rows, err := s.pool.Query(ctx, `select `+mailColumns+` from mail_outbox where status = $1 order by id`, status)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(mailFields))
}

// UpdateMail stores what sending m changed over the message with m.ID.
func (s *Store) UpdateMail(ctx context.Context, m connector.StoredMail) (bool, error) {
	tag, err := s.pool.Exec(ctx, `update mail_outbox set status = $2, attempts = $3, next_attempt_at = $4,
		last_attempt_at = $5, sent_at = $6, last_error = $7 where id = $1`,
		m.ID, m.Status, m.Attempts, m.NextAttemptAt, m.LastAttemptAt, m.SentAt, m.LastError)
	return tag.RowsAffected() == 1, err
}

// RecentMail returns at most limit of the outbox's messages, newest first.
func (s *Store) RecentMail(ctx context.Context, limit int) ([]connector.StoredMail, error) {
	rows, err := s.pool.Query(ctx, `select `+mailColumns+` from mail_outbox order by id desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanned(mailFields))
}
