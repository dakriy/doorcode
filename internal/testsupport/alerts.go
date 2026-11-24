package testsupport

import (
	"context"
	"maps"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// conditionKey is what an alert condition is stored under: its kind and
// its subject.
type conditionKey struct {
	kind, subject string
}

// AlertKinds returns whether each stored kind of alert is enabled.
func (s *Store) AlertKinds(context.Context) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(alertsAggregate); err != nil {
		return nil, err
	}
	return maps.Clone(s.alertKinds), nil
}

// SetAlertKind stores whether the kind of alert named kind is enabled.
func (s *Store) SetAlertKind(_ context.Context, kind string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(alertsAggregate, alertWrites); err != nil {
		return err
	}
	s.alertKinds[kind] = enabled
	return nil
}

// AlertCondition returns the condition of kind about subject.
func (s *Store) AlertCondition(_ context.Context, kind, subject string) (connector.StoredCondition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(alertsAggregate); err != nil {
		return connector.StoredCondition{}, false, err
	}
	c, held := s.conditions[conditionKey{kind, subject}]
	return c, held, nil
}

// PutAlertCondition stores c over the condition of its kind and subject.
func (s *Store) PutAlertCondition(_ context.Context, c connector.StoredCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(alertsAggregate, alertWrites); err != nil {
		return err
	}
	s.conditions[conditionKey{c.Kind, c.Subject}] = c
	return nil
}

// AddMail stores m in the outbox with the next id. It refuses nil
// recipients, which the database would hold as NULL.
func (s *Store) AddMail(_ context.Context, m connector.StoredMail) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(alertsAggregate, alertWrites); err != nil {
		return 0, err
	}
	if m.To == nil {
		return 0, ErrNull
	}
	s.nextMail++
	m.ID = s.nextMail
	s.outbox = append(s.outbox, clonedMail(m))
	return m.ID, nil
}

// MailWithStatus returns the outbox's messages whose status is status,
// oldest first.
func (s *Store) MailWithStatus(_ context.Context, status string) ([]connector.StoredMail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(alertsAggregate); err != nil {
		return nil, err
	}
	out := []connector.StoredMail{}
	for _, m := range s.outbox {
		if m.Status == status {
			out = append(out, clonedMail(m))
		}
	}
	return out, nil
}

// UpdateMail stores what sending m changed over the message with m.ID.
func (s *Store) UpdateMail(_ context.Context, m connector.StoredMail) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(alertsAggregate, alertWrites); err != nil {
		return false, err
	}
	for i, held := range s.outbox {
		if held.ID == m.ID {
			held.Status, held.Attempts, held.NextAttemptAt = m.Status, m.Attempts, m.NextAttemptAt
			held.LastAttemptAt, held.SentAt, held.LastError = cloned(m.LastAttemptAt), cloned(m.SentAt), m.LastError
			s.outbox[i] = held
			return true, nil
		}
	}
	return false, nil
}

// RecentMail returns at most limit of the outbox's messages, newest first.
func (s *Store) RecentMail(_ context.Context, limit int) ([]connector.StoredMail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(alertsAggregate); err != nil {
		return nil, err
	}
	out := []connector.StoredMail{}
	for _, m := range slices.Backward(s.outbox) {
		if len(out) == limit {
			break
		}
		out = append(out, clonedMail(m))
	}
	return out, nil
}

// clonedMail is a copy of m sharing nothing with it.
func clonedMail(m connector.StoredMail) connector.StoredMail {
	m.To = slices.Clone(m.To)
	m.LastAttemptAt, m.SentAt = cloned(m.LastAttemptAt), cloned(m.SentAt)
	return m
}

// FailAlerts makes the alerts methods report ErrUnavailable: the kinds of
// alert, the alert conditions, and the mail outbox.
func (s *Store) FailAlerts() {
	s.failing[alertsAggregate] = true
}

// FailAlertWrites makes the methods that change the alerts aggregate
// report ErrUnavailable, while the ones that read it still work.
func (s *Store) FailAlertWrites() {
	s.failing[alertWrites] = true
}
