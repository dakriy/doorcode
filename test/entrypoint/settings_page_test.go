package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// inputsOf is the kind of input each of keys has on page.
func inputsOf(page testsupport.Page, keys ...string) map[string]string {
	kinds := map[string]string{}
	for _, key := range keys {
		kinds[key] = page.Inputs[key]
	}
	return kinds
}

func TestTheSettingsPageAsksForEachSettingWithAnInputOfItsKind(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/settings").Body)

	want := []any{
		map[string]string{
			"site_name": "text", "site_timezone": "select", "theme_accent": "color", "theme_accent_dark": "color",
			"password_min_length": "number", "oidc_enabled": "checkbox", "oidc_auto_sign_in": "checkbox",
			"update_window_start": "time", "update_window_minutes": "number", "door_strike_pulse_ms": "number",
			"door_submit_key": "select", "door_log_level": "select", "door_ntp_server": "text",
		},
		[]string{"America/New_York", "Asia/Tokyo", "Europe/Berlin", "UTC (selected)"},
		[]string{"error", "warn", "info (selected)", "debug"},
		[]string{"site_timezone"},
	}
	got := []any{
		inputsOf(page, "site_name", "site_timezone", "theme_accent", "theme_accent_dark", "password_min_length", "oidc_enabled",
			"oidc_auto_sign_in", "update_window_start", "update_window_minutes", "door_strike_pulse_ms", "door_submit_key",
			"door_log_level", "door_ntp_server"),
		page.Options["site_timezone"], page.Options["door_log_level"], page.Comboboxes,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, timezones offered, log levels offered, searchable selects (-want +got):\n%s", diff)
	}
}

func TestASiteTimezoneTheZonesDoNotListIsOfferedFirst(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_timezone", "Asia/Kolkata", testsupport.DefaultNow)
	b := signedInAdmin(t, f)

	got := testsupport.ParsePage(b.Get("/admin/settings").Body).Options["site_timezone"]

	want := []string{"Asia/Kolkata (selected)", "America/New_York", "Asia/Tokyo", "Europe/Berlin", "UTC"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("timezones offered (-want +got):\n%s", diff)
	}
}

func TestTheSingleSignOnSectionsFoldAwayWhileItIsOff(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	off := testsupport.ParsePage(b.Get("/admin/settings").Body).Disclosures
	send(t, b, "PUT", "/api/v1/settings/oidc_enabled", `{"value":"true"}`, 200)
	on := testsupport.ParsePage(b.Get("/admin/settings").Body).Disclosures

	want := []any{
		map[string]bool{"Single sign-on": false, "Identity claims": false},
		map[string]bool{"Single sign-on": true, "Identity claims": true},
	}
	if diff := cmp.Diff(want, []any{off, on}); diff != "" {
		t.Errorf("sections open while off, while on (-want +got):\n%s", diff)
	}
}

func TestTickingAndUntickingAYesOrNoSettingStoresIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	ticked := b.Post("/admin/settings", "oidc_auto_sign_in", "true", "oidc_auto_sign_in", "false")
	afterTicking := f.StoreStub.StoredSetting("oidc_auto_sign_in")
	unticked := b.Post("/admin/settings", "oidc_auto_sign_in", "false")

	want := []any{303, "true", 303, "false"}
	if diff := cmp.Diff(want, []any{ticked.Status, afterTicking, unticked.Status, f.StoreStub.StoredSetting("oidc_auto_sign_in")}); diff != "" {
		t.Errorf("ticked status and stored, unticked status and stored (-want +got):\n%s", diff)
	}
}

func TestTheDarkAccentColorFollowsTheAccentWhileItsBoxIsTicked(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)

	b.Post("/admin/settings", "theme_accent_dark", "#112233")
	picked := f.StoreStub.StoredSetting("theme_accent_dark")
	b.Post("/admin/settings", "clear", "theme_accent_dark", "theme_accent_dark", "#112233")

	want := []any{"#112233", "", []string{"clear=theme_accent_dark"}}
	got := []any{picked, f.StoreStub.StoredSetting("theme_accent_dark"), testsupport.ParsePage(b.Get("/admin/settings").Body).Checked}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stored when picked, stored when ticked, ticked boxes (-want +got):\n%s", diff)
	}
}
