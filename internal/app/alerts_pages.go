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
var alertSettingKeys = []string{settings.KeyAlertRecipients, settings.KeyAlertCleared, settings.KeyAlertHoldDown}

// alertsContent is the Alerts page's content: every kind of alert, the
// alert mail settings, each with what was entered for it and what is
// wrong with it when it was refused, a note while mail is not set up, and
// the newest messages of the outbox.
type alertsContent struct {
	Kinds    []alert.KindState
	Settings []settingRow
	MailNote string
	Outbox   []outboxRow
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
const mailNotSetUp = "Mail is not set up, so alert mail waits in the outbox and fails after six attempts. Give the mail server's host and the address mail is sent from on the Mail tab of the settings."

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
	content := alertsContent{Kinds: now.kinds, MailNote: a.mailNote(r.Context()), Outbox: []outboxRow{}}
	for _, section := range a.sectionsOf(all, settings.TabAlerts, entered, errs) {
		content.Settings = append(content.Settings, section.Rows...)
	}
	for _, m := range now.recent {
		content.Outbox = append(content.Outbox, outboxRowOf(m, zone))
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

// outboxRowOf is m as the Alerts page lists it, its times in zone.
func outboxRowOf(m connector.StoredMail, zone *time.Location) outboxRow {
	row := outboxRow{
		Queued: shownAt(zone, m.CreatedAt, secondLayout), Subject: m.Subject, To: strings.Join(m.To, ", "),
		Attempts: strconv.Itoa(m.Attempts), Error: m.LastError,
	}
	switch m.Status {
	case alert.Sent:
		row.Status = "Sent " + shownAt(zone, *m.SentAt, secondLayout)
	case alert.Pending:
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
