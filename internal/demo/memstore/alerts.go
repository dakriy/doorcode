package memstore

import (
	"context"
	"maps"
	"slices"

	"github.com/dakriy/doorcode/internal/connector"
)

// conditionKey is what an alert condition is kept under: its kind and its
// subject.
type conditionKey struct {
	kind, subject string
}

// AlertKinds returns whether each stored kind of alert is enabled.
func (s *Store) AlertKinds(_ context.Context) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.alertKinds), nil
}

// SetAlertKind stores whether the kind of alert named kind is enabled.
func (s *Store) SetAlertKind(_ context.Context, kind string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertKinds[kind] = enabled
	return nil
}

// AlertCondition returns the condition of kind about subject.
func (s *Store) AlertCondition(_ context.Context, kind, subject string) (connector.StoredCondition, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.conditions[conditionKey{kind, subject}]
	return c, ok, nil
}

// PutAlertCondition stores c over the condition of its kind and subject.
func (s *Store) PutAlertCondition(_ context.Context, c connector.StoredCondition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conditions[conditionKey{c.Kind, c.Subject}] = c
	return nil
}

// AddMail stores m in the outbox and returns its id.
func (s *Store) AddMail(_ context.Context, m connector.StoredMail) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.To == nil {
		return 0, errNil
	}
	m.ID = s.next("outbox")
	s.outbox = append(s.outbox, mailOut(m))
	return m.ID, nil
}

// MailWithStatus returns the outbox's messages whose status is status,
// oldest first.
func (s *Store) MailWithStatus(_ context.Context, status string) ([]connector.StoredMail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []connector.StoredMail{}
	for _, m := range s.outbox {
		if m.Status == status {
			out = append(out, mailOut(m))
		}
	}
	return out, nil
}

// UpdateMail stores what sending m changed over the message with m.ID.
func (s *Store) UpdateMail(_ context.Context, m connector.StoredMail) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, held := range s.outbox {
		if held.ID == m.ID {
			held.Status, held.Attempts, held.NextAttemptAt = m.Status, m.Attempts, m.NextAttemptAt
			held.LastAttemptAt, held.SentAt, held.LastError = clone(m.LastAttemptAt), clone(m.SentAt), m.LastError
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
	out := []connector.StoredMail{}
	for _, m := range slices.Backward(s.outbox) {
		if len(out) == limit {
			break
		}
		out = append(out, mailOut(m))
	}
	return out, nil
}

// mailOut is a copy of m sharing nothing with it.
func mailOut(m connector.StoredMail) connector.StoredMail {
	m.To = slices.Clone(m.To)
	m.LastAttemptAt, m.SentAt = clone(m.LastAttemptAt), clone(m.SentAt)
	return m
}
