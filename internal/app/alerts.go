package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/alert"
	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

// deliveryRetry is how long the delivery waits to look at the outbox
// again after it could not read or update it.
const deliveryRetry = time.Minute

// beside starts work on a goroutine of its own, beside the rest of Run,
// returning the function that closes work's stop channel and waits for
// work to return.
func beside(work func(stop <-chan struct{})) func() {
	stop := make(chan struct{})
	var working sync.WaitGroup
	working.Go(func() { work(stop) })
	return func() {
		close(stop)
		working.Wait()
	}
}

// deliver delivers the outbox's mail whenever it is due: at once when a
// message is queued, else when the earliest one waiting is due again,
// until stop closes.
func (a *App) deliver(ctx context.Context, stop <-chan struct{}) {
	for {
		a.deliverIfDue(ctx)
		var due <-chan time.Time
		if a.nextDelivery != nil {
			due = a.clock.Alarm(*a.nextDelivery)
		}
		select {
		case <-stop:
			a.deliverIfDue(ctx)
			return
		case <-a.wake:
		case <-due:
		}
	}
}

// wakeDelivery tells the delivery a message was queued.
func (a *App) wakeDelivery() {
	a.queued.Store(true)
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// deliverIfDue tries the outbox's mail when its turn has come, a message
// was queued since it last did, or the earliest message waiting is due,
// and sets the next turn: when the earliest message still waiting is due,
// none while nothing waits, and a while later when the outbox could not
// be read or updated, which is logged.
func (a *App) deliverIfDue(ctx context.Context) {
	now := a.clock.Now()
	queued := a.queued.Swap(false)
	if !queued && (a.nextDelivery == nil || now.Before(*a.nextDelivery)) {
		return
	}
	next, err := a.alerts.Deliver(ctx)
	if err != nil {
		a.log.Error("mail delivery failed", connector.Field{Key: "error", Value: err.Error()})
		retry := now.Add(deliveryRetry)
		next = &retry
	}
	a.nextDelivery = next
}

type alertKindView struct {
	Key     string `json:"key" doc:"The kind's key."`
	Name    string `json:"name" doc:"The kind's name, which its mail's subject starts with."`
	About   string `json:"about" doc:"What raises and clears it."`
	Enabled bool   `json:"enabled" doc:"True when it mails the recipients; every kind is disabled until an administrator enables it."`
}

type alertKindUpdate struct {
	Enabled *bool `json:"enabled" doc:"Whether the kind mails the recipients."`
}

type outboxView struct {
	ID            int64      `json:"id" doc:"The message's id."`
	Kind          string     `json:"kind" doc:"The key of the kind of alert it is about."`
	Subject       string     `json:"subject" doc:"The message's subject."`
	Text          string     `json:"text" doc:"The message's text."`
	To            []string   `json:"to" doc:"The addresses it goes to."`
	Status        string     `json:"status" doc:"pending while it waits to be sent, sent once the mail server took it, or failed once it was tried six times."`
	Attempts      int        `json:"attempts" doc:"How many times it was tried."`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was queued."`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty" doc:"When it is tried next; only while it is pending."`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty" doc:"When it was last tried; absent before its first attempt."`
	SentAt        *time.Time `json:"sent_at,omitempty" doc:"When the mail server took it; absent until it did."`
	LastError     string     `json:"last_error,omitempty" doc:"What its latest failed attempt reported; absent once it is sent."`
}

type alertsView struct {
	Kinds           []alertKindView `json:"kinds" doc:"Every kind of alert, with whether it is enabled."`
	Recipients      []string        `json:"recipients" doc:"The addresses every enabled kind mails, from the setting alert_recipients."`
	MailCleared     bool            `json:"mail_cleared" doc:"True when a condition that was mailed about mails again once it clears, from the setting alert_mail_cleared."`
	HoldDownMinutes int             `json:"hold_down_minutes" doc:"Minutes a cleared condition must stay clear before it mails again, from the setting alert_hold_down_minutes."`
	Outbox          []outboxView    `json:"outbox" doc:"The newest messages of the outbox, at most 50, newest first."`
}

// alertKindParam is the path parameter naming a kind of alert.
var alertKindParam = openapi.Param{Name: "kind", In: "path", Description: "The kind's key.", Example: alert.HeldOpen.Key}

func (a *App) alertRoutes() []route {
	admin := holding(auth.RoleAdmin)
	enabled := true
	return []route{
		pageRoute(http.MethodGet, alertsPath, admin, a.alertsPage),
		pageRoute(http.MethodPost, alertsPath, admin, a.saveAlertsFromForm),
		apiRoute(http.MethodGet, "/alerts", admin, a.getAlerts, apiDoc{
			id: "getAlerts", summary: "Read the kinds of alert, the recipients, and the outbox", tag: "alerts",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every kind of alert, whom and when they mail, and the newest messages of the outbox. The settings API changes the recipients and when they are mailed.", Body: alertsView{}}},
		}),
		apiRoute(http.MethodPut, "/alerts/kinds/{kind}", admin, a.updateAlertKind, apiDoc{
			id: "updateAlertKind", summary: "Enable or disable a kind of alert", tag: "alerts",
			params:  []openapi.Param{alertKindParam},
			request: alertKindUpdate{Enabled: &enabled},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The kind with whether it is enabled now.", Body: alertKindView{}},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object with enabled true or false.", Body: errorBody{}},
				{Status: http.StatusNotFound, Description: "No kind of alert has this key.", Body: errorBody{}},
			},
		}),
	}
}

func kindViewOf(k alert.KindState) alertKindView {
	return alertKindView{Key: k.Key, Name: k.Name, About: k.About, Enabled: k.Enabled}
}

func outboxViewOf(m connector.StoredMail) outboxView {
	v := outboxView{
		ID: m.ID, Kind: m.Kind, Subject: m.Subject, Text: m.Text, To: m.To, Status: m.Status, Attempts: m.Attempts,
		CreatedAt: m.CreatedAt.UTC(), LastAttemptAt: utc(m.LastAttemptAt), SentAt: utc(m.SentAt), LastError: m.LastError,
	}
	if m.Status == alert.Pending {
		v.NextAttemptAt = utc(&m.NextAttemptAt)
	}
	return v
}

// alertsNow is every kind of alert, whom and when they mail, and the
// newest messages of the outbox.
type alertsNow struct {
	kinds  []alert.KindState
	policy settings.Alerts
	recent []connector.StoredMail
}

// readAlerts reads every kind of alert, whom and when they mail, and the
// newest messages of the outbox.
func (a *App) readAlerts(ctx context.Context) (alertsNow, error) {
	kinds, err := a.alerts.States(ctx)
	if err != nil {
		return alertsNow{}, err
	}
	policy, err := a.settings.Alerts(ctx)
	if err != nil {
		return alertsNow{}, err
	}
	recent, err := a.alerts.Recent(ctx)
	return alertsNow{kinds: kinds, policy: policy, recent: recent}, err
}

func (a *App) getAlerts(w http.ResponseWriter, r *http.Request, _ auth.User) {
	now, err := a.readAlerts(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	view := alertsView{
		Kinds: []alertKindView{}, Recipients: now.policy.Recipients, MailCleared: now.policy.MailCleared,
		HoldDownMinutes: int(now.policy.HoldDown / time.Minute), Outbox: []outboxView{},
	}
	for _, k := range now.kinds {
		view.Kinds = append(view.Kinds, kindViewOf(k))
	}
	for _, m := range now.recent {
		view.Outbox = append(view.Outbox, outboxViewOf(m))
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *App) updateAlertKind(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body alertKindUpdate
	if err := decodeJSON(w, r, &body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "body must be a JSON object with enabled true or false")
		return
	}
	k, err := a.alerts.Enable(r.Context(), r.PathValue("kind"), *body.Enabled)
	if err != nil {
		a.refuse(w, r, err, []refusalStatus{{alert.ErrUnknownKind, http.StatusNotFound}})
		return
	}
	writeJSON(w, http.StatusOK, kindViewOf(k))
}
