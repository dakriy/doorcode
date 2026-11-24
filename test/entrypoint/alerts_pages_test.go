package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// kindRows is how the Alerts tab of the settings page lists the kinds of
// alert.
var kindRows = [][]string{
	{"Alert", "Raised and cleared"},
	{"Door forced open forced_open", "A door opened while nothing unlocked it. It clears when the door closes."},
	{"Door held open held_open", "A door stood open for longer than its held-open time while it should have been locked. " +
		"It clears when the door closes or an open span of its timeline begins."},
	{"Door offline offline", "A door's controller has not been connected to the broker for the minutes of the unresponsive_minutes setting. " +
		"It clears once the controller connects again."},
	{"Door unresponsive unresponsive", "A door's controller has not been heard from for the minutes of the unresponsive_minutes setting " +
		"while the broker says it is connected. It clears once the server hears from it or it is shown offline."},
	{"Door configuration reverted config_reverted", "A door gave up the configuration it was last sent, because it could not come online with it, " +
		"and runs the one before. It clears once the door no longer shows the Reverted configuration badge: " +
		"it reports the configuration it was last sent, or is sent another."},
	{"Door restarting with its configuration config_restarting", "A door's controller started at least twice since the configuration " +
		"it was last sent went out, with no report naming that configuration. It clears once the door no longer shows the Restarting with configuration badge."},
	{"Door configuration mismatched config_mismatch", "A door's controller went on reporting a configuration other than the one it was last sent. " +
		"It clears once the door no longer shows the Mismatched configuration badge."},
	{"Door firmware update failed update_failed", "A door's firmware update failed: the door said it could not install it, " +
		"or it did not connect again with it in time. It clears once the door's firmware no longer shows Update failed: an update of it is under way again, it runs its target, or its target changes."},
	{"Certificate certificate", certificateAbout},
}

// alertMailRows is how the Alerts tab of the settings page lists the
// alert mail settings, with the cell of each setting's value and the
// recipients' source.
func alertMailRows(recipients, cleared, holdDown, source string) [][]string {
	return [][]string{
		{"Setting", "Value", "Source"},
		{"Recipients alert_recipients The addresses every enabled alert mails, separated by commas or new lines, such as ops@example.com. " +
			"Nothing is mailed while it is empty.", recipients, source},
		{"Mail when cleared alert_mail_cleared Mails the recipients once more when a condition an alert mailed about clears.", cleared, "default"},
		{"Quiet after clearing alert_hold_down_minutes Minutes a cleared condition must stay clear before it mails again when it comes back; " +
			"one that comes back sooner is followed without mail.", holdDown, "default"},
	}
}

// certificateCheckRows is how the Alerts tab of the settings page lists
// the certificate check settings, at their defaults.
var certificateCheckRows = [][]string{
	{"Setting", "Value", "Source"},
	{"Check certificates certificate_check_enabled Checks each endpoint again once the interval has passed since its last check. " +
		"Check now works while it is off.", "On", "default"},
	{"Check interval certificate_check_interval_hours Hours, from 1 to 720, between two checks of an endpoint.", "1 to 720 hours", "default"},
	{"Runway alert certificate_runway_days Days, from 1 to 3650: the certificate alert is raised when the earliest expiry along a chain " +
		"after its leaf, the trust anchor included, is fewer days away.", "1 to 3650 days", "default"},
	{"Leaf alert certificate_leaf_days Days, from 1 to 365: the certificate alert is raised when the leaf expires in fewer days.", "1 to 365 days", "default"},
	{"Unreachable alert certificate_unreachable_checks Checks, from 1 to 100: the certificate alert is raised when this many checks " +
		"in a row reach no server.", "1 to 100 checks", "default"},
}

// alertsTabPath is the address of the Alerts tab of the settings page.
const alertsTabPath = "/admin/settings?tab=alerts"

// mailNotSetUpNote is the note the Alerts page shows while mail is not set
// up.
const mailNotSetUpNote = "Mail is not set up, so no alert mail is sent. Alerts are still raised and listed here; their mail, " +
	"and the mail waiting in the outbox, goes out once mail is set up, using up no attempt meanwhile. " +
	"Give the mail server's host and the address mail is sent from on the Mail tab of the settings."

// raisedHead heads the Alerts page's table of the conditions raised now.
var raisedHead = []string{"Alert", "About", "Since", "Mail"}

// outboxHead heads the Alerts page's outbox.
var outboxHead = []string{"Queued", "Subject", "To", "Status", "Attempts", "Last error"}

func TestTheAlertsPageShowsNothingRaisedNothingToCheckAnEmptyOutboxAndWhereTheSettingsAre(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := pageOf(b.Get("/admin/alerts"))

	want := testsupport.Page{
		Title: "Alerts · doorcode", Heading: "Alerts", Nav: mainNav("Site: Alerts", ""), Messages: []string{"You are signed in."},
		Notes: []string{mailNotSetUpNote, nothingToCheck}, LinkTargets: map[string]string{"Alert settings": alertsTabPath},
		Tables: map[string][][]string{
			"Raised now":         {raisedHead, {"Nothing is raised."}},
			"Certificate checks": {certificatesHead},
			"Outbox":             {outboxHead, {"No alert mail yet."}},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestTheAlertsTabOfTheSettingsPageShowsTheKindsAndTheAlertSettings(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := pageOf(b.Get(alertsTabPath))

	want := settingsTab("Alerts")
	want.Messages = []string{"You are signed in."}
	want.Values = map[string]string{"kind": "", "alert_recipients": "", "alert_mail_cleared": "false", "alert_hold_down_minutes": "15",
		"certificate_check_enabled": "false", "certificate_check_interval_hours": "24",
		"certificate_runway_days": "548", "certificate_leaf_days": "14", "certificate_unreachable_checks": "3"}
	want.Checked = []string{"alert_mail_cleared=true"}
	want.Tables = map[string][][]string{
		"Kinds of alert":    kindRows,
		"Alert mail":        alertMailRows("", "On", "0 to 10080 minutes", "default"),
		"Certificate check": certificateCheckRows,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
}

func TestTheAlertsTabAsksForEachAlertMailSettingWithItsControl(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get(alertsTabPath).Body)

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

func TestTheAlertsTabAsksForEachCertificateCheckSettingWithItsControl(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get(alertsTabPath).Body)

	keys := []string{"certificate_check_enabled", "certificate_check_interval_hours", "certificate_runway_days", "certificate_leaf_days", "certificate_unreachable_checks"}
	want := []any{
		map[string]string{
			"certificate_check_enabled": "checkbox", "certificate_check_interval_hours": "number", "certificate_runway_days": "number",
			"certificate_leaf_days": "number", "certificate_unreachable_checks": "number",
		},
		map[string]string{
			"certificate_check_enabled": "", "certificate_check_interval_hours": "1 to 720 by 1", "certificate_runway_days": "1 to 3650 by 1",
			"certificate_leaf_days": "1 to 365 by 1", "certificate_unreachable_checks": "1 to 100 by 1",
		},
	}
	got := []any{entriesOf(page.Inputs, keys...), entriesOf(page.Limits, keys...)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, limits (-want +got):\n%s", diff)
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

func TestSavingTheAlertsTabStoresTheKindsTickedAndTheAlertMailSettings(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	send(t, b, "PUT", "/api/v1/alerts/kinds/forced_open", `{"enabled":true}`, 200)

	saved := b.Post("/admin/settings", "kind", "", "kind", "held_open", "alert_recipients", "ops@example.com\r\ndesk@example.com",
		"alert_mail_cleared", "false", "alert_hold_down_minutes", "30")
	shown := pageOf(b.Get(saved.Location))
	api := send(t, b, "GET", "/api/v1/alerts", "", 200).Body

	want := []any{303, alertsTabPath, []string{"The settings have been saved. Doors receive changed configuration defaults right away."}, []string{"kind=held_open"}, testsupport.JSON(`[false,true]`),
		testsupport.JSON(`["ops@example.com","desk@example.com"]`), false, 30.0}
	got := []any{saved.Status, saved.Location, shown.Messages, shown.Checked,
		[]any{testsupport.JSONAt(api, "/kinds/0/enabled"), testsupport.JSONAt(api, "/kinds/1/enabled")},
		testsupport.JSONAt(api, "/recipients"), testsupport.JSONAt(api, "/mail_cleared"), testsupport.JSONAt(api, "/hold_down_minutes")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("save, page after, kinds, recipients, mail cleared, hold-down (-want +got):\n%s", diff)
	}
}

func TestARefusedAlertsTabShowsWhatIsWrongAndStoresNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	refusedForm := b.Post("/admin/settings", "kind", "", "kind", "held_open", "alert_recipients", "nobody", "alert_mail_cleared", "true",
		"alert_hold_down_minutes", "-5")
	page := pageOf(refusedForm)
	api := send(t, b, "GET", "/api/v1/alerts", "", 200).Body

	want := []any{422, map[string]string{
		"alert_recipients":        "Must be email addresses separated by commas or new lines, such as ops@example.com, Front desk <desk@example.com>.",
		"alert_hold_down_minutes": "Must be a whole number of minutes from 0 to 10080.",
	}, map[string]string{"kind": "", "alert_recipients": "nobody", "alert_mail_cleared": "false", "alert_hold_down_minutes": "-5",
		"certificate_check_enabled": "false", "certificate_check_interval_hours": "24",
		"certificate_runway_days": "548", "certificate_leaf_days": "14", "certificate_unreachable_checks": "3"},
		false, testsupport.JSON(`[]`), 15.0}
	got := []any{refusedForm.Status, page.FieldErrors, page.Values,
		testsupport.JSONAt(api, "/kinds/1/enabled"), testsupport.JSONAt(api, "/recipients"), testsupport.JSONAt(api, "/hold_down_minutes")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, errors, values, held open enabled, recipients, hold-down (-want +got):\n%s", diff)
	}
}

func TestTheAlertsTabShowsAnAlertMailSettingTheEnvironmentGivesAsReadOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ALERT_RECIPIENTS"] = "ops@example.com"
	b := signedInAdmin(t, f)

	saved := b.Post("/admin/settings", "kind", "", "kind", "forced_open", "alert_mail_cleared", "false", "alert_hold_down_minutes", "15")
	page := pageOf(b.Get(alertsTabPath))

	note := "Set by DOORCODE_ALERT_RECIPIENTS in the environment; change it there."
	want := []any{303, []string{settingsNote, note},
		alertMailRows("ops@example.com "+note, "On", "", "environment, read-only")[1], []string{"kind=forced_open"}}
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
		"showing the kinds on the settings page": {(*testsupport.Store).FailAlerts, func(b *testsupport.Browser) testsupport.Response {
			return b.Get(alertsTabPath)
		}},
		"saving the kinds": {(*testsupport.Store).FailAlertWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/settings", "kind", "held_open")
		}},
		"saving the settings and the kinds": {(*testsupport.Store).FailSettingWrites, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/settings", "kind", "", "alert_hold_down_minutes", "20")
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
