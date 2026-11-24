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

// AlertKinds returns whether each stored kind of alert is enabled.
func (s *Store) AlertKinds(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `select kind, enabled from alert_kinds`)
	if err != nil {
		return nil, err
	}
	kinds := map[string]bool{}
	var kind string
	var enabled bool
	_, err = pgx.ForEachRow(rows, []any{&kind, &enabled}, func() error {
		kinds[kind] = enabled
		return nil
	})
	return kinds, err
}

// SetAlertKind stores whether the kind of alert named kind is enabled.
func (s *Store) SetAlertKind(ctx context.Context, kind string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `insert into alert_kinds (kind, enabled) values ($1, $2)
		on conflict (kind) do update set enabled = excluded.enabled`, kind, enabled)
	return err
}

// AlertCondition returns the condition of kind about subject.
func (s *Store) AlertCondition(ctx context.Context, kind, subject string) (connector.StoredCondition, bool, error) {
	var c connector.StoredCondition
	err := s.pool.QueryRow(ctx, `select kind, subject, active, mailed, changed_at from alert_conditions where kind = $1 and subject = $2`,
		kind, subject).Scan(&c.Kind, &c.Subject, &c.Active, &c.Mailed, &c.ChangedAt)
	return found(c, err)
}

// PutAlertCondition stores c over the condition of its kind and subject.
func (s *Store) PutAlertCondition(ctx context.Context, c connector.StoredCondition) error {
	_, err := s.pool.Exec(ctx, `insert into alert_conditions (kind, subject, active, mailed, changed_at) values ($1, $2, $3, $4, $5)
		on conflict (kind, subject) do update set active = excluded.active, mailed = excluded.mailed, changed_at = excluded.changed_at`,
		c.Kind, c.Subject, c.Active, c.Mailed, c.ChangedAt)
	return err
}

// ActiveAlertConditions returns every active condition by kind and
// subject.
func (s *Store) ActiveAlertConditions(ctx context.Context) ([]connector.StoredCondition, error) {
	rows, err := s.pool.Query(ctx, `select kind, subject, active, mailed, changed_at from alert_conditions
		where active order by kind collate "C", subject collate "C"`)
	if err != nil {
		return nil, err
	}
	out := []connector.StoredCondition{}
	var c connector.StoredCondition
	_, err = pgx.ForEachRow(rows, []any{&c.Kind, &c.Subject, &c.Active, &c.Mailed, &c.ChangedAt}, func() error {
		out = append(out, c)
		return nil
	})
	return out, err
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
