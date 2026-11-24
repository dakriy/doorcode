package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/alert"
	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// alertsPath is the Alerts page's address.
const alertsPath = "/admin/alerts"

// alertSettingKeys are the settings the Alerts page shows and saves.
var alertSettingKeys = settings.KeysOn(settings.TabAlerts)

// alertsContent is the Alerts page's content: every kind of alert, the
// sections of the alert mail and certificate check settings, each
// setting with what was entered for it and what is wrong with it when it
// was refused, a note while mail is not set up, the conditions raised and
// the events owed mail, each endpoint's latest certificate check, and the
// newest messages of the outbox.
type alertsContent struct {
	Kinds        []alert.KindState
	Sections     []settingSection
	MailNote     string
	Raised       []raisedRow
	Certificates []certificateRow
	Outbox       []outboxRow
}

// raisedRow is a condition raised, or an event owed mail, as the Alerts
// page lists it: its kind's name, what it is about, since when in the
// site timezone, and what became of its mail.
type raisedRow struct {
	Alert string
	About string
	Since string
	Mail  string
}

// What the Alerts page says of a raised condition's mail.
const (
	raisedMailed = "Mailed"
	raisedOwed   = "Waiting: mailed once it can be"
	raisedOff    = "Not mailed: this alert is off"
	raisedQuiet  = "Not mailed: raised again within the hold-down"
)

// raisedRowOf is c as the Alerts page lists it, its time in zone, owed
// mail waiting only while c's kind is among enabled.
func raisedRowOf(c connector.StoredCondition, zone *time.Location, enabled map[string]bool) raisedRow {
	row := raisedRow{Alert: alert.KindName(c.Kind), About: c.Name, Since: shownAt(zone, c.ChangedAt, secondLayout), Mail: raisedQuiet}
	off := c.Owed && !enabled[c.Kind]
	switch {
	case c.Mailed:
		row.Mail = raisedMailed
	case off:
		row.Mail = raisedOff
	case c.Owed:
		row.Mail = raisedOwed
	}
	return row
}

// outboxRow is a message of the outbox as the Alerts page lists it, its
// times in the site timezone.
type outboxRow struct {
	Queued   string
	Subject  string
	To       string
	Status   string
	Attempts string
	Error    string
}

// mailNotSetUp is what the Alerts page says while mail is not set up.
const mailNotSetUp = "Mail is not set up, so no alert mail is sent. Alerts are still raised and listed here; their mail, and the mail waiting in the outbox, goes out once mail is set up, using up no attempt meanwhile. Give the mail server's host and the address mail is sent from on the Mail tab of the settings."

// waitingForMail is the status of a pending message while mail is not set
// up.
const waitingForMail = "Waiting for mail to be set up"

func (a *App) alertsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showAlerts(w, r, http.StatusOK, viewer, nil, nil)
}

// showAlerts shows the Alerts page with status, the alert mail settings'
// inputs holding entered, or each setting's value where entered has none.
func (a *App) showAlerts(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, entered, errs map[string]string) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	now, err := a.readAlerts(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	held, heldErr := a.alerts.Held(r.Context())
	checks, err := a.certificates.Results(r.Context())
	if err = errors.Join(heldErr, err); err != nil {
		a.pageError(w, r, err)
		return
	}
	content := alertsContent{
		Kinds: now.kinds, Sections: a.sectionsOf(all, settings.TabAlerts, entered, errs), MailNote: a.mailNote(r.Context()),
		Outbox: []outboxRow{},
	}
	enabled := map[string]bool{}
	for _, k := range now.kinds {
		enabled[k.Key] = k.Enabled
	}
	for _, c := range held {
		content.Raised = append(content.Raised, raisedRowOf(c, zone, enabled))
	}
	for _, c := range checks {
		content.Certificates = append(content.Certificates, certificateRowOf(c, zone))
	}
	for _, m := range now.recent {
		content.Outbox = append(content.Outbox, outboxRowOf(m, zone, content.MailNote == ""))
	}
	a.render(w, r, status, alertsTemplate, "Alerts", &viewer, content)
}

// mailNote is what the Alerts page says of the mail settings: that mail
// is not set up, when it is not.
func (a *App) mailNote(ctx context.Context) string {
	if _, err := a.settings.Mail(ctx); errors.Is(err, settings.ErrMailNotConfigured) {
		return mailNotSetUp
	}
	return ""
}

// outboxRowOf is m as the Alerts page lists it, its times in zone, a
// pending message waiting for mail to be set up unless setUp.
func outboxRowOf(m connector.StoredMail, zone *time.Location, setUp bool) outboxRow {
	row := outboxRow{
		Queued: shownAt(zone, m.CreatedAt, secondLayout), Subject: m.Subject, To: strings.Join(m.To, ", "),
		Attempts: strconv.Itoa(m.Attempts), Error: m.LastError,
	}
	sent, pending := m.Status == alert.Sent, m.Status == alert.Pending
	held := pending && !setUp
	switch {
	case sent:
		row.Status = "Sent " + shownAt(zone, *m.SentAt, secondLayout)
	case held:
		row.Status = waitingForMail
	case pending:
		row.Status = "Waiting, next try " + shownAt(zone, m.NextAttemptAt, secondLayout)
	default:
		row.Status = "Failed"
	}
	return row
}

// saveAlertsFromForm stores the alert mail settings the form changed, or
// none when one is refused, and then which kinds of alert are enabled:
// those the form ticks. It goes back to the Alerts page, or shows it with
// the values refused.
func (a *App) saveAlertsFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_ = r.ParseForm()
	entered := map[string]string{}
	for _, key := range alertSettingKeys {
		if r.PostForm.Has(key) {
			entered[key] = r.PostForm.Get(key)
		}
	}
	_, err := a.settings.UpdateAll(r.Context(), entered, nil)
	if errs, rejected := fieldErrors(err); rejected {
		a.showAlerts(w, r, http.StatusUnprocessableEntity, viewer, entered, errs)
		return
	}
	if err == nil {
		err = a.enableTicked(r.Context(), r.PostForm["kind"])
	}
	a.wakeDelivery()
	a.changed(w, r, err, alertsPath, "alerts-saved")
}

// enableTicked enables the kinds of alert whose keys are ticked and
// disables the others.
func (a *App) enableTicked(ctx context.Context, ticked []string) error {
	for _, kind := range alert.Kinds {
		if _, err := a.alerts.Enable(ctx, kind.Key, slices.Contains(ticked, kind.Key)); err != nil {
			return err
		}
	}
	return nil
}
