package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// kindRows is how the Alerts page lists the kinds of alert, after its
// head.
var kindRows = [][]string{
	{"Alert", "Raised and cleared"},
	{"Door forced open forced_open", "A door opened while nothing unlocked it. It clears when the door closes."},
	{"Door held open held_open", "A door stood open for longer than its held-open time while it should have been locked. " +
		"It clears when the door closes or an open span of its timeline begins."},
}

// alertMailRows is how the Alerts page lists the alert mail settings,
// with the cell of each setting's value.
func alertMailRows(recipients, cleared, holdDown string) [][]string {
	return [][]string{
		{"Setting", "Value"},
		{"Recipients alert_recipients The addresses every enabled alert mails, separated by commas or new lines, such as ops@example.com. " +
			"Nothing is mailed while it is empty.", recipients},
		{"Mail when cleared alert_mail_cleared Mails the recipients once more when a condition an alert mailed about clears.", cleared},
		{"Quiet after clearing alert_hold_down_minutes Minutes a cleared condition must stay clear before it mails again when it comes back; " +
			"one that comes back sooner is followed without mail.", holdDown},
	}
}

// outboxHead heads the Alerts page's outbox.
var outboxHead = []string{"Queued", "Subject", "To", "Status", "Attempts", "Last error"}

func TestTheAlertsPageShowsTheKindsTheAlertMailSettingsAndAnEmptyOutbox(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := pageOf(b.Get("/admin/alerts"))

	want := testsupport.Page{
		Title: "Alerts · doorcode", Heading: "Alerts", Nav: mainNav("Site: Alerts", ""), Messages: []string{"You are signed in."},
		Notes: []string{"Mail is not set up, so alert mail waits in the outbox and fails after six attempts. " +
			"Give the mail server's host and the address mail is sent from on the Mail tab of the settings."},
		Values:  map[string]string{"alert_recipients": "", "alert_mail_cleared": "false", "alert_hold_down_minutes": "15"},
		Checked: []string{"alert_mail_cleared=true"},
		Tables: map[string][][]string{
			"Kinds of alert": kindRows,
			"Alert mail":     alertMailRows("", "On", "0 to 10080 minutes"),
			"Outbox":         {outboxHead, {"No alert mail yet."}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageAsksForEachAlertMailSettingWithItsControl(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/alerts").Body)

	keys := []string{"alert_recipients", "alert_mail_cleared", "alert_hold_down_minutes"}
	want := []any{
		map[string]string{"alert_recipients": "textarea", "alert_mail_cleared": "checkbox", "alert_hold_down_minutes": "number"},
		map[string]string{"alert_recipients": "ops@example.com, Front desk <desk@example.com>", "alert_mail_cleared": "", "alert_hold_down_minutes": ""},
		map[string]string{"alert_recipients": "", "alert_mail_cleared": "", "alert_hold_down_minutes": "0 to 10080 by 1"},
		map[string]string{"alert_recipients": "", "alert_mail_cleared": "", "alert_hold_down_minutes": "0 to 10080 minutes"},
	}
	got := []any{entriesOf(page.Inputs, keys...), entriesOf(page.Placeholders, keys...), entriesOf(page.Limits, keys...), entriesOf(page.Descriptions, keys...)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, placeholders, limits, descriptions (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageListsTheOutboxNewestFirstWithEachMessagesStatus(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "forced_open")
	k.inZone("Europe/Berlin")
	k.setting("alert_hold_down_minutes", "0")
	f.MailerStub.FailWith("421 Try again later")
	k.opens(false)
	for _, at := range []string{"2026-01-05T09:01:00Z", "2026-01-05T09:03:00Z", "2026-01-05T09:07:00Z", "2026-01-05T09:15:00Z", "2026-01-05T09:31:00Z"} {
		k.runAt(at)
	}
	f.MailerStub.FailWith("421 Busy")
	k.says("closed")
	f.MailerStub.Fail(nil)
	k.at("2026-01-05T09:31:30Z")
	k.opens(false)

	got := pageOf(k.b.Get("/admin/alerts")).Tables["Outbox"]

	to := "ops@example.com, desk@example.com"
	want := [][]string{
		outboxHead,
		{"Mon 5 Jan 2026 10:31:30", "Door forced open: Lobby", to, "Sent Mon 5 Jan 2026 10:31:30", "1", ""},
		{"Mon 5 Jan 2026 10:31:00", "Cleared: Door forced open: Lobby", to, "Waiting, next try Mon 5 Jan 2026 10:32:00", "1",
			"the mail server could not be reached or refused the message: 421 Busy"},
		{"Mon 5 Jan 2026 10:00:00", "Door forced open: Lobby", to, "Failed", "6", refused},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("outbox (-want +got):\n%s", diff)
	}
}

// inZone has the site's timezone be zone.
func (k *keypad) inZone(zone string) {
	k.t.Helper()
	k.setting("site_timezone", zone)
}

func TestSavingTheAlertsPageStoresTheKindsTickedAndTheAlertMailSettings(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	send(t, b, "PUT", "/api/v1/alerts/kinds/forced_open", `{"enabled":true}`, 200)

	saved := b.Post("/admin/alerts", "kind", "held_open", "alert_recipients", "ops@example.com\r\ndesk@example.com",
		"alert_mail_cleared", "false", "alert_hold_down_minutes", "30")
	shown := pageOf(b.Get(saved.Location))
	api := send(t, b, "GET", "/api/v1/alerts", "", 200).Body

	want := []any{303, "/admin/alerts", []string{"The alerts have been saved."}, []string{"kind=held_open"}, testsupport.JSON(`[false,true]`),
		testsupport.JSON(`["ops@example.com","desk@example.com"]`), false, 30.0}
	got := []any{saved.Status, saved.Location, shown.Messages, shown.Checked,
		[]any{testsupport.JSONAt(api, "/kinds/0/enabled"), testsupport.JSONAt(api, "/kinds/1/enabled")},
		testsupport.JSONAt(api, "/recipients"), testsupport.JSONAt(api, "/mail_cleared"), testsupport.JSONAt(api, "/hold_down_minutes")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("save, page after, kinds, recipients, mail cleared, hold-down (-want +got):\n%s", diff)
	}
}

func TestARefusedAlertsFormShowsWhatIsWrongAndStoresNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	refusedForm := b.Post("/admin/alerts", "kind", "held_open", "alert_recipients", "nobody", "alert_mail_cleared", "true",
		"alert_hold_down_minutes", "-5")
	page := pageOf(refusedForm)
	api := send(t, b, "GET", "/api/v1/alerts", "", 200).Body

	want := []any{422, map[string]string{
		"alert_recipients":        "Must be email addresses separated by commas or new lines, such as ops@example.com, Front desk <desk@example.com>.",
		"alert_hold_down_minutes": "Must be a whole number of minutes from 0 to 10080.",
	}, map[string]string{"alert_recipients": "nobody", "alert_mail_cleared": "false", "alert_hold_down_minutes": "-5"},
		false, testsupport.JSON(`[]`), 15.0}
	got := []any{refusedForm.Status, page.FieldErrors, page.Values,
		testsupport.JSONAt(api, "/kinds/1/enabled"), testsupport.JSONAt(api, "/recipients"), testsupport.JSONAt(api, "/hold_down_minutes")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, errors, values, held open enabled, recipients, hold-down (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageShowsAnAlertMailSettingTheEnvironmentGivesAsReadOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ALERT_RECIPIENTS"] = "ops@example.com"
	b := signedInAdmin(t, f)

	saved := b.Post("/admin/alerts", "kind", "forced_open", "alert_mail_cleared", "false", "alert_hold_down_minutes", "15")
	page := pageOf(b.Get("/admin/alerts"))

	note := "Set by DOORCODE_ALERT_RECIPIENTS in the environment; change it there."
	want := []any{303, []string{"Mail is not set up, so alert mail waits in the outbox and fails after six attempts. " +
		"Give the mail server's host and the address mail is sent from on the Mail tab of the settings.", note},
		alertMailRows("ops@example.com "+note, "On", "")[1], []string{"kind=forced_open"}}
	if diff := cmp.Diff(want, []any{saved.Status, page.Notes, page.Tables["Alert mail"][1], page.Checked}); diff != "" {
		t.Errorf("save, notes, recipients row, checked (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageFailsWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail func(s *testsupport.Store)
		send func(b *testsupport.Browser) testsupport.Response
	}{
		"showing the alerts": {(*testsupport.Store).FailAlerts, func(b *testsupport.Browser) testsupport.Response { return b.Get("/admin/alerts") }},
		"showing the settings": {(*testsupport.Store).FailSettings, func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/admin/alerts")
		}},
		"saving the kinds": {(*testsupport.Store).FailAlertWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/alerts", "kind", "held_open")
		}},
		"saving the settings": {(*testsupport.Store).FailSettingWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/alerts", "alert_hold_down_minutes", "20")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

			got := c.send(b)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheAlertsPageAndAPIFailWhileTheEnvironmentGivesAValueTheyCannotUse(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ variable, value, target string }{
		"the page, a hold-down that is no number": {"DOORCODE_ALERT_HOLD_DOWN_MINUTES", "soon", "/admin/alerts"},
		"the API, a hold-down that is no number":  {"DOORCODE_ALERT_HOLD_DOWN_MINUTES", "soon", "/api/v1/alerts"},
		"the page, a timezone nobody has":         {"DOORCODE_SITE_TIMEZONE", "Nowhere/Else", "/admin/alerts"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.EnvStub[c.variable] = c.value

			got := b.Get(c.target)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
