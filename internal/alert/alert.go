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
	"strconv"
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
	Offline = Kind{Key: "offline", Name: "Door offline",
		About: "A door's controller has not been connected to the broker for the minutes of the unresponsive_minutes setting. It clears once the controller connects again."}
	Unresponsive = Kind{Key: "unresponsive", Name: "Door unresponsive",
		About: "A door's controller has not been heard from for the minutes of the unresponsive_minutes setting while the broker says it is connected. It clears once the server hears from it or it is shown offline."}
	ConfigReverted = Kind{Key: "config_reverted", Name: "Door configuration reverted",
		About: "A door gave up the configuration it was last sent, because it could not come online with it, and runs the one before. It clears once the door no longer shows the Reverted configuration badge: it reports the configuration it was last sent, or is sent another."}
	ConfigRestarting = Kind{Key: "config_restarting", Name: "Door restarting with its configuration",
		About: "A door's controller started at least twice since the configuration it was last sent went out, with no report naming that configuration. It clears once the door no longer shows the Restarting with configuration badge."}
	ConfigMismatch = Kind{Key: "config_mismatch", Name: "Door configuration mismatched",
		About: "A door's controller went on reporting a configuration other than the one it was last sent. It clears once the door no longer shows the Mismatched configuration badge."}
	UpdateFailed = Kind{Key: "update_failed", Name: "Door firmware update failed",
		About: "A door's firmware update failed: the door said it could not install it, or it did not connect again with it in time. It clears once the door's firmware no longer shows Update failed: an update of it is under way again, it runs its target, or its target changes."}
	Certificate = Kind{Key: "certificate", Name: "Certificate",
		About: "A certificate check of the door broker or the device address found that what it served does not verify against door_broker_ca_pem, that its runway or its leaf has fewer days left than the certificate check settings allow, or that it reached no server for their number of checks in a row; each clears once a later check passes it, or without mail once the settings no longer give the endpoint. A verified chain of another shape than the one the endpoint served before mails once and becomes the shape compared with."}
)

// Kinds is every kind of alert, in display order.
var Kinds = []Kind{ForcedOpen, HeldOpen, Offline, Unresponsive, ConfigReverted, ConfigRestarting, ConfigMismatch, UpdateFailed, Certificate}

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

// DoorCondition is the condition of kind about d.
func DoorCondition(kind Kind, d connector.StoredDoor) Condition {
	return Condition{Kind: kind, Subject: "door " + strconv.FormatInt(d.ID, 10), Name: d.Name}
}

// Raised is the conditions that were active when it was read, by kind
// and subject.
type Raised map[conditionKey]bool

// conditionKey is what tells conditions apart: their kind's key and
// their subject.
type conditionKey struct {
	kind, subject string
}

// Raised reads which conditions are active now.
func (s Service) Raised(ctx context.Context) (Raised, error) {
	active, err := s.Store.ActiveAlertConditions(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the alert conditions raised: %w", err)
	}
	r := Raised{}
	for _, c := range active {
		r[conditionKey{c.Kind, c.Subject}] = true
	}
	return r, nil
}

// Follow keeps c as holds says at at, as r saw it: it raises c, saying
// detail, when it holds and was not active, and resolves it when it does
// not hold and was, so a condition that stays as it was costs no store
// work. What stops it is logged.
func (s Service) Follow(ctx context.Context, r Raised, c Condition, holds bool, at time.Time, detail string) {
	active := r[conditionKey{c.Kind.Key, c.Subject}]
	if holds && !active {
		s.Raise(ctx, c, at, detail)
	}
	if active && !holds {
		s.Resolve(ctx, c, at)
	}
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
		if err := s.queueRaised(ctx, c, to, at, detail); err != nil {
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
		text := c.Kind.Name + " at " + c.Name + " cleared, " + Stamp(at) + ".\n\nIt was raised " + Stamp(held.ChangedAt) + ".\n" + footer(c.Kind)
		if err := s.queue(ctx, c.Kind, to, "Cleared: "+c.Kind.Name+": "+c.Name, text, at); err != nil {
			return err
		}
	}
	return s.Store.PutAlertCondition(ctx, connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, ChangedAt: at})
}

// Notify mails the recipients about c at at, saying detail, while its
// kind is enabled: once for each c, however often it is notified, since
// c names one occurrence of a one-off event and is never active. What
// stops it is logged.
func (s Service) Notify(ctx context.Context, c Condition, at time.Time, detail string) {
	s.followed(c, s.notify(ctx, c, at, detail))
}

func (s Service) notify(ctx context.Context, c Condition, at time.Time, detail string) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	_, known, err := s.Store.AlertCondition(ctx, c.Kind.Key, c.Subject)
	if err != nil || known {
		return err
	}
	_, to, err := s.mailing(ctx, c.Kind)
	if err != nil {
		return err
	}
	next := connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, ChangedAt: at}
	if len(to) > 0 {
		if err := s.queueRaised(ctx, c, to, at, detail); err != nil {
			return err
		}
		next.Mailed = true
	}
	return s.Store.PutAlertCondition(ctx, next)
}

// queueRaised puts the mail raising c at at, saying detail, to to in the
// outbox.
func (s Service) queueRaised(ctx context.Context, c Condition, to []string, at time.Time, detail string) error {
	text := c.Kind.Name + " at " + c.Name + ", " + Stamp(at) + ".\n\n" + detail + "\n" + footer(c.Kind)
	return s.queue(ctx, c.Kind, to, c.Kind.Name+": "+c.Name, text, at)
}

// Withdraw resolves c at at without mail, for a condition whose subject
// is no longer watched, so nothing can say it cleared. What stops it is
// logged and returned.
func (s Service) Withdraw(ctx context.Context, c Condition, at time.Time) error {
	err := s.withdraw(ctx, c, at)
	s.followed(c, err)
	return err
}

func (s Service) withdraw(ctx context.Context, c Condition, at time.Time) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	held, _, err := s.Store.AlertCondition(ctx, c.Kind.Key, c.Subject)
	if err != nil || !held.Active {
		return err
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

// Stamp is t as alert mail writes it.
func Stamp(t time.Time) string {
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
