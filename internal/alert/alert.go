// Package alert follows conditions the server detects and mails the
// alert recipients about them. Each kind of alert is off, on, or set to
// mail; a condition is raised and resolved by what detects it, kept and
// listed unless its kind is off, and, when raised while its kind is set
// to mail, mails once when raised and, when the settings say so, once
// when resolved. A raise that cannot be mailed when it happens is owed and
// mailed once it can be, while it is still raised and its kind still set
// to mail.
// Mail waits in a durable outbox until Deliver hands it to the mail
// server, retrying a message that fails with a growing pause up to
// MaxAttempts, and trying nothing while mail is not set up.
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

// ErrUnknownState reports a name that names no state of a kind of alert.
var ErrUnknownState = errors.New("a kind of alert is off, on, or mail")

// State is what a kind of alert does with the conditions of it: Off
// raises none, On raises and lists them without mail, and Mail raises,
// lists, and mails them. A kind is On until it is set otherwise.
type State string

// The states of a kind of alert.
const (
	Off  State = "off"
	On   State = "on"
	Mail State = "mail"
)

// StateChoices is every state of a kind of alert, in the order pages
// offer them.
var StateChoices = []State{Off, On, Mail}

// ParseState is the state named name; ErrUnknownState when it names
// none.
func ParseState(name string) (State, error) {
	state := State(name)
	if !slices.Contains(StateChoices, state) {
		return "", ErrUnknownState
	}
	return state, nil
}

// Kind is a kind of alert: the Key it is stored and addressed by, its
// Name, and what raises and resolves it, About.
type Kind struct {
	Key   string
	Name  string
	About string
}

// The kinds of alert.
var (
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
		About: "A certificate check of the door broker or the device address found that what it served does not verify against door_broker_ca_pem, that its runway or its leaf has fewer days left than the certificate check settings allow, or that it reached no server for their number of checks in a row; each clears once a later check passes it, or without mail once the settings no longer give the endpoint. A verified chain whose certificates above the leaf's issuer, the anchor included, differ from those the endpoint served before mails once and becomes the shape compared with; another intermediate under the same certificates does not mail."}
)

// Kinds is every kind of alert, in display order.
var Kinds = []Kind{HeldOpen, Offline, Unresponsive, ConfigReverted, ConfigRestarting, ConfigMismatch, UpdateFailed, Certificate}

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

// Raise raises c at at, saying detail, unless it is raised already or
// its kind is off. While its kind is on, c is kept and listed but its
// raise is muted: neither it nor its clearing is ever mailed. While its
// kind is set to mail, it mails the recipients, except when it comes back
// less than the hold-down time after a raise that was mailed or held back
// cleared; while its mail cannot be sent, because no recipient is given
// or mail is not set up, the mail is owed and Deliver sends it once it
// can, if c is still raised. What stops it is logged.
func (s Service) Raise(ctx context.Context, c Condition, at time.Time, detail string) {
	s.followed(c, s.raise(ctx, c, at, detail))
}

// Resolve resolves c at at: when it was raised, it mails the recipients
// that it cleared if its raise was mailed, the settings mail cleared
// conditions, and its kind is still set to mail. An owed raise is no
// longer owed. What stops it is logged.
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
	m, err := s.mailing(ctx)
	if err != nil {
		return err
	}
	state := m.state(c.Kind)
	if state == Off {
		return nil
	}
	next := connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, Active: true, ChangedAt: at, Name: c.Name, Detail: detail}
	if state == On {
		next.Muted = true
		return s.Store.PutAlertCondition(ctx, next)
	}
	if held.Mailed && at.Before(held.ChangedAt.Add(m.policy.HoldDown)) {
		return s.Store.PutAlertCondition(ctx, next)
	}
	to := m.to(c.Kind)
	if len(to) == 0 || !s.mailSetUp(ctx) {
		next.Owed = true
		return s.Store.PutAlertCondition(ctx, next)
	}
	if err := s.queueRaised(ctx, c, to, at, detail, at); err != nil {
		return err
	}
	next.Mailed = true
	return s.Store.PutAlertCondition(ctx, next)
}

func (s Service) resolve(ctx context.Context, c Condition, at time.Time) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	held, _, err := s.Store.AlertCondition(ctx, c.Kind.Key, c.Subject)
	if err != nil || !held.Active {
		return err
	}
	m, err := s.mailing(ctx)
	if err != nil {
		return err
	}
	to := m.to(c.Kind)
	if held.Mailed && m.policy.MailCleared && len(to) > 0 {
		text := c.Kind.Name + " at " + c.Name + " cleared, " + Stamp(at) + ".\n\nIt was raised " + Stamp(held.ChangedAt) + ".\n" + footer(c.Kind)
		if err := s.queue(ctx, c.Kind, to, "Cleared: "+c.Kind.Name+": "+c.Name, text, at); err != nil {
			return err
		}
	}
	return s.Store.PutAlertCondition(ctx, clearedAt(held, c, at))
}

// clearedAt is held, the condition c, resolved at at: inactive, owing no
// mail, and Mailed unless its raise was owed or muted, so the hold-down
// follows only a raise the recipients were told of or one it held back.
func clearedAt(held connector.StoredCondition, c Condition, at time.Time) connector.StoredCondition {
	return connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, Mailed: !held.Owed && !held.Muted, ChangedAt: at, Name: c.Name}
}

// Notify mails the recipients about c at at, saying detail, while its
// kind is set to mail: once for each c, however often it is notified,
// since c names one occurrence of a one-off event and is never active.
// While its kind is set to mail but no recipient is given or mail is not
// set up, the mail is owed and Deliver sends it once it can; while its
// kind is on, c is kept and never mailed, and while it is off, c is not
// kept. What stops it is logged.
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
	m, err := s.mailing(ctx)
	if err != nil {
		return err
	}
	state := m.state(c.Kind)
	if state == Off {
		return nil
	}
	next := connector.StoredCondition{Kind: c.Kind.Key, Subject: c.Subject, ChangedAt: at, Name: c.Name, Detail: detail}
	to := m.to(c.Kind)
	if len(to) > 0 && s.mailSetUp(ctx) {
		if err := s.queueRaised(ctx, c, to, at, detail, at); err != nil {
			return err
		}
		next.Mailed = true
		return s.Store.PutAlertCondition(ctx, next)
	}
	next.Owed = state == Mail
	return s.Store.PutAlertCondition(ctx, next)
}

// queueRaised puts the mail raising c at raised, saying detail, to to in
// the outbox at at.
func (s Service) queueRaised(ctx context.Context, c Condition, to []string, raised time.Time, detail string, at time.Time) error {
	text := c.Kind.Name + " at " + c.Name + ", " + Stamp(raised) + ".\n\n" + detail + "\n" + footer(c.Kind)
	return s.queue(ctx, c.Kind, to, c.Kind.Name+": "+c.Name, text, at)
}

// release puts in the outbox at at the raise mail of each condition that
// owes it, while recipients are given, and stores it mailed. Only a kind
// set to mail owes mail.
func (s Service) release(ctx context.Context, at time.Time) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	owed, err := s.Store.OwedAlertConditions(ctx)
	if err != nil || len(owed) == 0 {
		return err
	}
	m, err := s.mailing(ctx)
	if err != nil {
		return err
	}
	for _, held := range owed {
		kind, _ := kindOf(held.Kind)
		to := m.to(kind)
		if len(to) == 0 {
			continue
		}
		err := s.queueRaised(ctx, Condition{Kind: kind, Subject: held.Subject, Name: held.Name}, to, held.ChangedAt, held.Detail, at)
		if err == nil {
			held.Owed, held.Mailed = false, true
			err = s.Store.PutAlertCondition(ctx, held)
		}
		if err != nil {
			return err
		}
	}
	return nil
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
	return s.Store.PutAlertCondition(ctx, clearedAt(held, c, at))
}

// mailing is whom alerts mail and when, and the state stored for each
// kind.
type mailing struct {
	policy settings.Alerts
	states map[string]string
}

// mailing reads whom alerts mail and when, and the state of each kind.
func (s Service) mailing(ctx context.Context) (mailing, error) {
	kinds, kindsErr := s.Store.AlertKinds(ctx)
	policy, policyErr := s.Settings.Alerts(ctx)
	return mailing{policy: policy, states: kinds}, errors.Join(kindsErr, policyErr)
}

// state is the state of kind: the one stored, else On.
func (m mailing) state(kind Kind) State {
	return stateOf(m.states, kind)
}

// stateOf is the state of kind among stored: the one stored for it, else
// On.
func stateOf(stored map[string]string, kind Kind) State {
	state, set := stored[kind.Key]
	if !set {
		return On
	}
	return State(state)
}

// to is the recipients mail about kind goes to, none unless kind is set
// to mail.
func (m mailing) to(kind Kind) []string {
	if m.state(kind) == Mail {
		return m.policy.Recipients
	}
	return nil
}

// mailSetUp reports whether the mail settings give a server and a
// sender. Settings that cannot be used otherwise count as set up, so
// their error is what each delivery attempt reports.
func (s Service) mailSetUp(ctx context.Context) bool {
	_, err := s.Settings.Mail(ctx)
	return !errors.Is(err, settings.ErrMailNotConfigured)
}

// footer ends every message about kind, saying why it was sent.
func footer(kind Kind) string {
	return "\nThis mail goes to every alert recipient while the alert " + kind.Name + " is set to mail on the Alerts tab of the settings.\n"
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

// Deliver does nothing while mail is not set up, so no message uses up
// an attempt. Otherwise it queues the raise mail the conditions owe,
// while recipients are given, then tries each
// pending message of the outbox whose next attempt is due, through the
// mail settings as they are now, and returns when the earliest message
// still pending is due next, nil when none is, with what stopped the
// owed mail being queued.
func (s Service) Deliver(ctx context.Context) (*time.Time, error) {
	site, siteErr := s.Settings.Mail(ctx)
	if errors.Is(siteErr, settings.ErrMailNotConfigured) {
		return nil, nil
	}
	now := s.Clock.Now()
	releaseErr := s.release(ctx, now)
	pending, err := s.Store.MailWithStatus(ctx, Pending)
	if err != nil {
		return nil, err
	}
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
	return next, releaseErr
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

// KindState is a kind of alert and its State.
type KindState struct {
	Kind
	State State
}

// States is every kind of alert, in display order, with its state.
func (s Service) States(ctx context.Context) ([]KindState, error) {
	stored, err := s.Store.AlertKinds(ctx)
	if err != nil {
		return nil, err
	}
	states := make([]KindState, 0, len(Kinds))
	for _, kind := range Kinds {
		states = append(states, KindState{Kind: kind, State: stateOf(stored, kind)})
	}
	return states, nil
}

// SetState stores state, by name, as the state of the kind of alert whose
// key is key, and returns the kind with it; ErrUnknownKind when no kind
// has the key, ErrUnknownState when state names none. A kind set off has
// every condition of it cleared, and one set on has the mail its
// conditions owe muted, both without mail, so a kind later set to mail
// mails only what is raised from then on.
func (s Service) SetState(ctx context.Context, key, state string) (KindState, error) {
	kind, known := kindOf(key)
	if !known {
		return KindState{}, fmt.Errorf("%w: %s", ErrUnknownKind, key)
	}
	chosen, err := ParseState(state)
	if err != nil {
		return KindState{}, err
	}
	s.Lock.Lock()
	defer s.Lock.Unlock()
	if chosen != Mail {
		if err := s.quiet(ctx, kind, chosen); err != nil {
			return KindState{}, err
		}
	}
	return KindState{Kind: kind, State: chosen}, s.Store.SetAlertKind(ctx, key, state)
}

// quiet clears every condition of kind, now set to state, when state is
// Off, and mutes the mail they owe when it is On: a raised condition
// owing it is muted, and a one-off event owing it is never mailed.
func (s Service) quiet(ctx context.Context, kind Kind, state State) error {
	held, err := s.Held(ctx)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	for _, c := range held {
		if c.Kind != kind.Key || (state == On && !c.Owed) {
			continue
		}
		next := connector.StoredCondition{Kind: c.Kind, Subject: c.Subject, ChangedAt: now, Name: c.Name}
		if state == On {
			next = c
			next.Owed, next.Muted = false, c.Active
		}
		if err := s.Store.PutAlertCondition(ctx, next); err != nil {
			return err
		}
	}
	return nil
}

// kindOf is the kind of alert whose key is key, and whether there is one.
func kindOf(key string) (Kind, bool) {
	i := slices.IndexFunc(Kinds, func(k Kind) bool { return k.Key == key })
	if i < 0 {
		return Kind{}, false
	}
	return Kinds[i], true
}

// Held is the conditions raised now, by kind and subject, then the
// one-off events whose mail is owed.
func (s Service) Held(ctx context.Context) ([]connector.StoredCondition, error) {
	active, activeErr := s.Store.ActiveAlertConditions(ctx)
	owed, owedErr := s.Store.OwedAlertConditions(ctx)
	for _, c := range owed {
		if !c.Active {
			active = append(active, c)
		}
	}
	return active, errors.Join(activeErr, owedErr)
}

// KindName is the name of the kind of alert whose key is key.
func KindName(key string) string {
	kind, _ := kindOf(key)
	return kind.Name
}

// Recent is the RecentMail newest messages of the outbox, newest first.
func (s Service) Recent(ctx context.Context) ([]connector.StoredMail, error) {
	return s.Store.RecentMail(ctx, RecentMail)
}
