// Package alert mails the alert recipients about conditions the server
// follows. Each kind of alert is enabled or not; a condition is raised and
// resolved by what detects it, and mails once when raised and, when the
// settings say so, once when resolved. Mail waits in a durable outbox
// until Deliver hands it to the mail server, retrying a message that
// fails with a growing pause up to MaxAttempts.
package alert

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/email"
	"github.com/dakriy/doorcode/internal/settings"
)

// ErrUnknownKind reports a key that names no kind of alert.
var ErrUnknownKind = errors.New("no kind of alert has this key")

// Kind is a kind of alert: the Key it is stored and addressed by, its
// Name, and what raises and resolves it, About.
type Kind struct {
	Key   string
	Name  string
	About string
}

// The kinds of alert.
var (
	ForcedOpen = Kind{Key: "forced_open", Name: "Door forced open",
		About: "A door opened while nothing unlocked it. It clears when the door closes."}
	HeldOpen = Kind{Key: "held_open", Name: "Door held open",
		About: "A door stood open for longer than its held-open time while it should have been locked. It clears when the door closes or an open span of its timeline begins."}
)

// Kinds is every kind of alert, in display order.
var Kinds = []Kind{ForcedOpen, HeldOpen}

// The states of a message in the outbox.
const (
	Pending = "pending"
	Sent    = "sent"
	Failed  = "failed"
)

// MaxAttempts is how many times a message is tried before it fails. The
// first retry waits firstRetry, each one after waits twice as long as
// the one before, and none waits longer than longestRetry.
const (
	MaxAttempts  = 6
	firstRetry   = time.Minute
	longestRetry = time.Hour
)

// RecentMail is how many messages of the outbox Recent lists.
const RecentMail = 50

// stampLayout is how a message writes an instant.
const stampLayout = "Mon 2 Jan 2006 15:04:05 UTC"

// Service raises and resolves alert conditions and delivers their mail.
type Service struct {
	Store    connector.AlertsStore
	Settings settings.Service
	Mail     email.Service
	Clock    connector.Clock
	Log      connector.Log
	// Queued is called after a message is put in the outbox.
	Queued func()
	// Lock makes each raise and resolve read and write its condition
	// alone, since doors are handled at once.
	Lock *sync.Mutex
}

// Condition is what an alert is raised about: its Kind, the Subject it
// concerns, such as one door, which tells it apart from the conditions of
// the same kind, and the Name its mail calls the subject by.
type Condition struct {
	Kind    Kind
	Subject string
	Name    string
}

// Raise raises c at at, saying detail: unless it is raised already, it
// mails the recipients while its kind is enabled, except when it was
// resolved less than the hold-down time before. What stops it is logged.
func (s Service) Raise(ctx context.Context, c Condition, at time.Time, detail string) {
	s.followed(c, s.raise(ctx, c, at, detail))
}

// Resolve resolves c at at: when it was raised, it mails the recipients
// that it cleared if its raise was mailed, the settings mail cleared
// conditions, and its kind is still enabled. What stops it is logged.
func (s Service) Resolve(ctx context.Context, c Condition, at time.Time) {
	s.followed(c, s.resolve(ctx, c, at))
}

// followed logs err, which stopped c being followed, when there is one.
func (s Service) followed(c Condition, err error) {
	if err == nil {
		return
	}
	s.Log.Error("alert condition not followed",
		connector.Field{Key: "kind", Value: c.Kind.Key},
		connector.Field{Key: "subject", Value: c.Subject},
		connector.Field{Key: "error", Value: err.Error()})
}

// raise and resolve read the kinds and the settings only once the
// condition's state says it changes, so a door closing with nothing
// raised costs one read.
func (s Service) raise(ctx context.Context, c Condition, at time.Time, detail string) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	held, _, err := s.Store.AlertCondition(ctx, c.Kind.Key, c.Subject)
	if err != nil || held.Active {
		return err
	}
	policy, to, err := s.mailing(ctx, c.Kind)
	if err != nil {
		return err
	}
	next := connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, Active: true, ChangedAt: at}
	quiet := at.Before(held.ChangedAt.Add(policy.HoldDown))
	if !quiet && len(to) > 0 {
		text := c.Kind.Name + " at " + c.Name + ", " + stamp(at) + ".\n\n" + detail + "\n" + footer(c.Kind)
		if err := s.queue(ctx, c.Kind, to, c.Kind.Name+": "+c.Name, text, at); err != nil {
			return err
		}
		next.Mailed = true
	}
	return s.Store.PutAlertCondition(ctx, next)
}

func (s Service) resolve(ctx context.Context, c Condition, at time.Time) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	held, _, err := s.Store.AlertCondition(ctx, c.Kind.Key, c.Subject)
	if err != nil || !held.Active {
		return err
	}
	policy, to, err := s.mailing(ctx, c.Kind)
	if err != nil {
		return err
	}
	if held.Mailed && policy.MailCleared && len(to) > 0 {
		text := c.Kind.Name + " at " + c.Name + " cleared, " + stamp(at) + ".\n\nIt was raised " + stamp(held.ChangedAt) + ".\n" + footer(c.Kind)
		if err := s.queue(ctx, c.Kind, to, "Cleared: "+c.Kind.Name+": "+c.Name, text, at); err != nil {
			return err
		}
	}
	return s.Store.PutAlertCondition(ctx, connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, ChangedAt: at})
}

// mailing is whom alerts mail and when, and the recipients mail about
// kind goes to, none while kind is disabled.
func (s Service) mailing(ctx context.Context, kind Kind) (settings.Alerts, []string, error) {
	kinds, kindsErr := s.Store.AlertKinds(ctx)
	policy, policyErr := s.Settings.Alerts(ctx)
	var to []string
	if kinds[kind.Key] {
		to = policy.Recipients
	}
	return policy, to, errors.Join(kindsErr, policyErr)
}

// footer ends every message about kind, saying why it was sent.
func footer(kind Kind) string {
	return "\nThis mail goes to every alert recipient while the alert " + kind.Name + " is enabled on the Alerts page.\n"
}

// stamp is t as a message writes it.
func stamp(t time.Time) string {
	return t.UTC().Format(stampLayout)
}

// queue puts a message about kind to to in the outbox at at, due at once.
func (s Service) queue(ctx context.Context, kind Kind, to []string, subject, text string, at time.Time) error {
	_, err := s.Store.AddMail(ctx, connector.StoredMail{
		Kind: kind.Key, Subject: subject, Text: text, To: to, Status: Pending, CreatedAt: at, NextAttemptAt: at,
	})
	if err != nil {
		return err
	}
	s.Queued()
	return nil
}

// Deliver tries each pending message of the outbox whose next attempt is
// due, through the mail settings as they are now, and returns when the
// earliest message still pending is due next, nil when none is.
func (s Service) Deliver(ctx context.Context) (*time.Time, error) {
	pending, err := s.Store.MailWithStatus(ctx, Pending)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	site, siteErr := s.Settings.Mail(ctx)
	var next *time.Time
	for _, m := range pending {
		if !now.Before(m.NextAttemptAt) {
			m = s.attempt(ctx, m, site, siteErr, now)
			if _, err := s.Store.UpdateMail(ctx, m); err != nil {
				return nil, err
			}
		}
		if m.Status == Pending {
			next = earliest(next, m.NextAttemptAt)
		}
	}
	return next, nil
}

// earliest is the earlier of next and at, at when next is nil.
func earliest(next *time.Time, at time.Time) *time.Time {
	if next == nil || at.Before(*next) {
		return &at
	}
	return next
}

// attempt is m after trying to send it at now through site, or failing
// with siteErr when the mail settings could not be read: sent, pending
// until its next attempt is due, or failed once it was tried MaxAttempts
// times.
func (s Service) attempt(ctx context.Context, m connector.StoredMail, site settings.Mail, siteErr error, now time.Time) connector.StoredMail {
	m.Attempts++
	m.LastAttemptAt = &now
	err := siteErr
	if err == nil {
		var to []*mail.Address
		for _, address := range m.To {
			to = append(to, &mail.Address{Address: address})
		}
		err = s.Mail.Send(ctx, site, email.Message{To: to, Subject: m.Subject, Text: m.Text})
	}
	if err == nil {
		m.Status, m.SentAt, m.LastError = Sent, &now, ""
		return m
	}
	m.LastError = err.Error()
	if m.Attempts >= MaxAttempts {
		m.Status = Failed
		return m
	}
	m.NextAttemptAt = now.Add(min(firstRetry<<(m.Attempts-1), longestRetry))
	return m
}

// KindState is a kind of alert and whether it is Enabled.
type KindState struct {
	Kind
	Enabled bool
}

// States is every kind of alert, in display order, with whether it is
// enabled; a kind is disabled until it is enabled.
func (s Service) States(ctx context.Context) ([]KindState, error) {
	stored, err := s.Store.AlertKinds(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]KindState, 0, len(Kinds))
	for _, kind := range Kinds {
		states = append(states, KindState{Kind: kind, Enabled: stored[kind.Key]})
	}
	return states, nil
}

// Enable stores whether the kind of alert whose key is key is enabled,
// and returns it; ErrUnknownKind when no kind has the key.
func (s Service) Enable(ctx context.Context, key string, enabled bool) (KindState, error) {
	i := slices.IndexFunc(Kinds, func(k Kind) bool { return k.Key == key })
	if i < 0 {
		return KindState{}, fmt.Errorf("%w: %s", ErrUnknownKind, key)
	}
	if err := s.Store.SetAlertKind(ctx, key, enabled); err != nil {
		return KindState{}, err
	}
	return KindState{Kind: Kinds[i], Enabled: enabled}, nil
}

// Recent is the RecentMail newest messages of the outbox, newest first.
func (s Service) Recent(ctx context.Context) ([]connector.StoredMail, error) {
	return s.Store.RecentMail(ctx, RecentMail)
}
