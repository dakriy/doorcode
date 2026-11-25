package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// nothingRaised is the Alerts page's table of the conditions raised now
// while none is.
var nothingRaised = [][]string{raisedHead, {"Nothing is raised."}}

func TestADoorHeldOpenWhileItsAlertIsOffShowsHeldOpenAndRaisesNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f)
	k.setKind("held_open", "off")

	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")

	held := testsupport.Samples(scrapeAs(t, k.a.Handler(), k.b), "doorcode_door_held_open")
	want := []any{nothingRaised, []any{}, "Online Held open", true, []any{"held open", "opened"}, []string{`doorcode_door_held_open{door="Lobby",door_id="1"} 1`}}
	got := []any{k.raised(), subjects(t, f), k.badges(), k.state("/held_open"), k.reasons(), held}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("raised, subjects, badges, held open, reasons, metric (-want +got):\n%s", diff)
	}
}

func TestSettingAKindOffClearsWhatItRaisedWithoutMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")

	k.setKind("held_open", "off")
	raised := k.raised()
	k.at("2026-01-05T09:01:40Z")
	k.says("closed")

	if diff := cmp.Diff([]any{nothingRaised, []any{heldSubject}}, []any{raised, subjects(t, f)}); diff != "" {
		t.Errorf("raised once off, subjects after the door closed (-want +got):\n%s", diff)
	}
}

func TestADoorAlreadyHeldOpenIsRaisedAtTheNextSweepOnceItsAlertIsOn(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f)
	k.setKind("held_open", "off")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	k.setKind("held_open", "on")

	k.sweepAt("2026-01-05T09:02:00Z")

	if diff := cmp.Diff([]string{"Door held open", "Lobby", "Mon 5 Jan 2026 09:02:00", muted}, k.raisedAs("Door held open")); diff != "" {
		t.Errorf("raised (-want +got):\n%s", diff)
	}
}

func TestAConditionRaisedWhileOnIsNeverMailedNorItsClearingAndHoldsNothingDown(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f)
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	k.setKind("held_open", "mail")
	k.at("2026-01-05T09:01:40Z")
	k.says("closed")
	quiet := subjects(t, f)

	k.holdsOpen("2026-01-05T09:05:00Z", "2026-01-05T09:06:00Z")

	want := []any{[]any{}, [][]any{{recipients, heldSubject, heldText("Mon 5 Jan 2026 09:06:00 UTC")}}}
	if diff := cmp.Diff(want, []any{quiet, mailed(t, f)}); diff != "" {
		t.Errorf("subjects before the second raise, mail after it (-want +got):\n%s", diff)
	}
}

func TestSettingAKindOnDropsTheMailItsConditionsOwe(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f, "held_open")
	k.setting("alert_recipients", "")
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	waiting := k.raisedAs("Door held open")[3]
	k.at("2026-01-05T09:01:30Z")

	k.setKind("held_open", "on")
	k.setting("alert_recipients", "ops@example.com")
	k.setKind("held_open", "mail")
	k.runAt("2026-01-05T09:02:00Z")

	want := []any{"Waiting: mailed once it can be", []any{}, muted}
	if diff := cmp.Diff(want, []any{waiting, subjects(t, f), k.raisedAs("Door held open")[3]}); diff != "" {
		t.Errorf("mail while owed, subjects, mail once on and back (-want +got):\n%s", diff)
	}
}

func TestAnAlertsTabNamingAStateThereIsNotIsRefusedShowingWhatWasChosenAndStoresNothing(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	refused := b.Post("/admin/settings", "alert_kind.held_open", "loud", "alert_kind.offline", "off", "alert_hold_down_minutes", "20")
	page := testsupport.ParsePage(refused.Body)
	api := send(t, b, "GET", "/api/v1/alerts", "", 200).Body

	want := []any{422, map[string]string{"alert_kind.held_open": "Must be off, on, or mail."},
		[]string{"loud (selected)", "off", "on", "mail"}, []string{"off (selected)", "on", "mail"}, "20",
		testsupport.JSON(`["on","on"]`), 15.0}
	got := []any{refused.Status, page.FieldErrors, page.Options["alert_kind.held_open"], page.Options["alert_kind.offline"], page.Values["alert_hold_down_minutes"],
		[]any{testsupport.JSONAt(api, "/kinds/0/state"), testsupport.JSONAt(api, "/kinds/1/state")}, testsupport.JSONAt(api, "/hold_down_minutes")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, errors, held open shown, offline shown, hold-down shown, kinds stored, hold-down stored (-want +got):\n%s", diff)
	}
}

func TestASettingsSaveWhoseKindsCannotBeStoredStillSendsTheDoorsTheSettingsItStored(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	before := k.sentDigest()
	f.StoreStub.FailAlertWrites()

	saved := k.b.Post("/admin/settings", "door_strike_pulse_ms", "5000", "alert_kind.held_open", "mail")

	if diff := cmp.Diff([]any{500, true}, []any{saved.Status, k.sentDigest() != before}); diff != "" {
		t.Errorf("status, configuration sent again (-want +got):\n%s", diff)
	}
}

func TestSettingAKindOffThatCannotClearItsConditionsFailsAndLeavesItAsItWas(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f)
	k.holdsOpen("2026-01-05T09:00:00Z", "2026-01-05T09:01:00Z")
	f.StoreStub.FailAlertWrites()

	refused := k.b.Send("PUT", "/api/v1/alerts/kinds/held_open", `{"state":"off"}`).Status
	f.StoreStub.Recover()

	state := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/alerts", "", 200).Body, "/kinds/0/state")
	want := []any{500, "on", []string{"Door held open", "Lobby", "Mon 5 Jan 2026 09:01:00", muted}}
	if diff := cmp.Diff(want, []any{refused, state, k.raisedAs("Door held open")}); diff != "" {
		t.Errorf("status, state, raised (-want +got):\n%s", diff)
	}
}

func TestSettingAKindOffClearsOnlyItsOwnConditions(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := alertSite(t, f)
	k.runs("2026-01-05T09:01:00Z", otherDigest)
	k.runs("2026-01-05T09:02:00Z", otherDigest)
	k.at("2026-01-05T09:02:00Z")
	k.opens(true)
	k.sweepAt("2026-01-05T09:03:00Z")
	before := len(k.raised())

	k.setKind("held_open", "off")

	want := []any{3, [][]string{raisedHead, {"Door configuration mismatched", "Lobby", "Mon 5 Jan 2026 09:03:00", muted}}}
	if diff := cmp.Diff(want, []any{before, k.raised()}); diff != "" {
		t.Errorf("rows before, raised after (-want +got):\n%s", diff)
	}
}
