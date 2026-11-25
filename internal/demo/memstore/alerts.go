package memstore

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/connector"
)

// conditionKey is what an alert condition is kept under: its kind and its
// subject.
type conditionKey struct {
	kind, subject string
}

// AlertKinds returns the state stored for each kind of alert.
func (s *Store) AlertKinds(_ context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.alertKinds), nil
}

// SetAlertKind stores state as the state of the kind of alert named kind.
func (s *Store) SetAlertKind(_ context.Context, kind, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertKinds[kind] = state
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

// ActiveAlertConditions returns every active condition by kind and
// subject.
func (s *Store) ActiveAlertConditions(_ context.Context) ([]connector.StoredCondition, error) {
	return s.conditionsWhere(func(c connector.StoredCondition) bool { return c.Active }), nil
}

// OwedAlertConditions returns every condition whose raise mail is owed,
// by kind and subject.
func (s *Store) OwedAlertConditions(_ context.Context) ([]connector.StoredCondition, error) {
	return s.conditionsWhere(func(c connector.StoredCondition) bool { return c.Owed }), nil
}

// conditionsWhere is the conditions kept that keep says to keep, by kind
// and subject.
func (s *Store) conditionsWhere(keep func(connector.StoredCondition) bool) []connector.StoredCondition {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []connector.StoredCondition{}
	for _, c := range s.conditions {
		if keep(c) {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b connector.StoredCondition) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Subject, b.Subject))
	})
	return out
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
