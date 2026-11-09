package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestHomeAsksForSignIn(t *testing.T) {
	t.Parallel()
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/")

	want := testsupport.Response{Status: 303, Location: "/sign-in?next=%2F"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

// settingsNote is what the settings page says about door defaults.
var settingsNote = []string{"Settings starting with door_ are the defaults every door is configured with; a door's own page overrides " +
	"them for that door. Doors receive a changed default as soon as it is saved. A changed enrollment username or password takes " +
	"effect when the server next starts. Secret settings are stored encrypted and never shown; leave one blank to keep it, or " +
	"enter a new value to replace it."}

// shownSetting is a setting as the settings page shows it.
type shownSetting struct {
	key, value, source string
}

// defaultSettings is every setting the settings page shows on a site
// where nothing is stored and the environment sets only the password
// hashing cost, in order.
var defaultSettings = []shownSetting{
	{"site_name", "doorcode", "default"}, {"site_timezone", "UTC", "default"}, {"admin_email", "admin@example.com", "default"},
	{"password_hash_memory_kib", "8", "environment, read-only"}, {"password_min_length", "12", "default"},
	{"new_user_expiry_days", "1826", "default"}, {"oidc_enabled", "false", "default"}, {"oidc_auto_sign_in", "false", "default"},
	{"oidc_issuer", "", "default"}, {"oidc_client_id", "", "default"}, {"oidc_client_secret", "", "default"},
	{"oidc_scopes", "openid email profile", "default"}, {"oidc_claim_user_id", "", "default"}, {"oidc_claim_email", "email", "default"},
	{"oidc_claim_first_name", "given_name", "default"}, {"oidc_claim_last_name", "family_name", "default"},
	{"oidc_claim_display_name", "name", "default"}, {"oidc_claim_entitlements", "", "default"},
	{"oidc_entitlement_groups", "", "default"}, {"oidc_default_groups", "", "default"},
	{"enrollment_username", "doorcode-enroll", "default"}, {"enrollment_password", "", "default"},
	{"door_credential_grace_hours", "24", "default"},
	{"telemetry_retention_days", "7", "default"}, {"telemetry_rollup_retention_days", "730", "default"},
	{"unresponsive_minutes", "5", "default"},
	{"public_url", "", "default"}, {"update_window_days", "daily", "default"}, {"update_window_start", "02:00", "default"},
	{"update_window_minutes", "120", "default"}, {"update_concurrency", "5", "default"}, {"update_timeout_minutes", "30", "default"},
	{"door_strike_pulse_ms", "3000", "default"}, {"door_keypad_idle_clear_ms", "10000", "default"},
	{"door_min_code_length", "4", "default"}, {"door_max_code_length", "32", "default"}, {"door_submit_key", "#", "default"},
	{"door_reply_timeout_ms", "3000", "default"}, {"door_telemetry_interval_s", "60", "default"},
	{"door_held_open_s", "60", "default"}, {"door_timeline_refresh_hours", "6", "default"}, {"door_credential_rotation_days", "30", "default"},
	{"door_rate_limit_per_minute", "30", "default"}, {"door_rate_limit_burst", "10", "default"},
	{"door_ntp_server", "pool.ntp.org", "default"}, {"door_log_level", "info", "default"},
}

// secretSettings are the settings the settings page offers as password
// fields, which are left blank.
var secretSettings = map[string]bool{"oidc_client_secret": true, "enrollment_password": true}

// settingsPage is the settings page showing shown, where changed replaces
// the value and source of the settings it names, entered the value an
// input holds, and refused what is wrong with a value; a read-only
// setting shows its value in place of an input.
func settingsPage(shown []shownSetting, changed map[string]shownSetting, entered, refused map[string]string) testsupport.Page {
	page := testsupport.Page{
		Title: "Settings · doorcode", Heading: "Settings", Notes: settingsNote, Values: map[string]string{},
		Nav:    []string{"Home", "Dashboard", "Users", "Requests", "Groups", "Schedules", "Doors", "Zones", "Firmware", "Events", "Settings (current)", "API tokens", "API docs"},
		Tables: map[string][][]string{"Settings": {{"Setting", "Value", "Source", "Environment variable"}}},
	}
	for _, s := range shown {
		if c, ok := changed[s.key]; ok {
			s = c
		}
		cell := refused[s.key]
		if s.source == "environment, read-only" {
			cell = s.value
		} else if secretSettings[s.key] {
			// A secret's input is a password field, whose value is not read.
		} else if value, ok := entered[s.key]; ok {
			page.Values[s.key] = value
		} else {
			page.Values[s.key] = s.value
		}
		page.Tables["Settings"] = append(page.Tables["Settings"], []string{s.key, cell, s.source, "DOORCODE_" + upper(s.key)})
	}
	if refused != nil {
		page.FieldErrors = refused
	}
	return page
}

func TestAdministratorReachesTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "North Campus", "2026-01-02T08:00:00Z")
	b := signedInAdmin(t, f)
	b.Get("/")

	got := b.Get("/admin/settings")

	want := settingsPage(defaultSettings, map[string]shownSetting{"site_name": {"site_name", "North Campus", "database"}}, nil, nil)
	want.Title = "Settings · North Campus"
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestUserWithoutTheAdminRoleIsRefusedTheSettingsPage(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")
	f.StoreStub.RevokeRoles(adminEmail)

	got := b.Get("/admin/settings")

	want := testsupport.Page{
		Title:   "Not allowed · doorcode",
		Heading: "You do not have access to this page",
		Nav:     []string{"Home", "API tokens", "API docs"},
	}
	if diff := cmp.Diff(want, pageOf(got)); diff != "" {
		t.Errorf("page (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(403, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestHomeOffersOnlyWhatTheUserMayUse(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.RevokeRoles(adminEmail)

	got := pageOf(b.Get("/"))

	if diff := cmp.Diff(portalNav("Home"), got.Nav); diff != "" {
		t.Errorf("nav (-want +got):\n%s", diff)
	}
}

func TestPagesLoadTheirAssets(t *testing.T) {
	t.Parallel()
	h := start(t, testsupport.NewFactory()).Handler()
	assets := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Assets

	var got []string
	for _, asset := range assets {
		r := testsupport.Get(h, asset)
		got = append(got, r.ContentType)
	}

	want := []string{"text/css; charset=utf-8", "text/javascript; charset=utf-8"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("asset types (-want +got):\n%s", diff)
	}
}

func TestPageStoreFailureShowsARequestIDThatTheLogCarries(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		request func(t *testing.T, f *testsupport.Factory) testsupport.Response
		id      string
	}{
		"sign-in page without settings": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.Get(h, "/sign-in")
		}},
		"sign-in without users": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailUsers()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"sign-in of an unknown email without settings": {id: "2223242526272829", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", "nobody@example.com", "password", "x")
		}},
		"sign-in without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSessions()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"home without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSessions()
			return b.Get("/")
		}},
		"settings page without settings": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()
			return b.Get("/admin/settings")
		}},
		"tokens page without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Get("/account/tokens")
		}},
		"token creation without tokens, after drawing the secret": {id: "6263646566676869", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "ci", "expires_in_days", "30")
		}},
		"rejected token form without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "", "expires_in_days", "30")
		}},
		"token revocation without tokens": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens/1/revoke")
		}},
		"sign-out without sessions": {id: "4243444546474849", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSessions()
			return b.Post("/sign-out")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()

			got := c.request(t, f)

			want := testsupport.Page{Title: "Something went wrong", Heading: "Something went wrong", Messages: []string{c.id}}
			if diff := cmp.Diff(want, pageOf(got)); diff != "" {
				t.Errorf("page (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]any{500, htmlType}, []any{got.Status, got.ContentType}); diff != "" {
				t.Errorf("status and type (-want +got):\n%s", diff)
			}
			wantLog := testsupport.LogEntry{Level: "error", Message: "request failed",
				Fields: map[string]string{"request_id": c.id, "error": "stub: unavailable"}}
			if diff := cmp.Diff(wantLog, f.LogStub.Entries[len(f.LogStub.Entries)-1]); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}
