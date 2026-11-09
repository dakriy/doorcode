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

// settingsNote is what the settings page says about the environment and
// secrets.
var settingsNote = "Every setting can also be given by an environment variable, DOORCODE_ followed by its key in capitals; " +
	"a setting the environment gives is read-only here. Secret settings are stored encrypted and never shown: leave one blank " +
	"to keep it, enter a new value to replace it, or tick Clear to remove it."

// shownSetting is a setting as the settings page shows it.
type shownSetting struct {
	key, value, source string
}

// defaultSettings is every setting the settings page shows on a site
// where nothing is stored and the environment sets only the password
// hashing cost, in order.
var defaultSettings = []shownSetting{
	{"site_name", "doorcode", "default"}, {"site_timezone", "UTC", "default"}, {"admin_email", "admin@example.com", "default"},
	{"theme_accent", "#4f39f6", "default"}, {"theme_accent_dark", "", "default"},
	{"password_hash_memory_kib", "8", "environment, read-only"}, {"password_min_length", "12", "default"},
	{"new_user_expiry_days", "1826", "default"}, {"oidc_enabled", "false", "default"}, {"oidc_auto_sign_in", "false", "default"},
	{"oidc_issuer", "", "default"}, {"oidc_client_id", "", "default"}, {"oidc_client_secret", "", "default"},
	{"oidc_scopes", "openid email profile", "default"}, {"oidc_claim_user_id", "", "default"}, {"oidc_claim_email", "email", "default"},
	{"oidc_claim_first_name", "given_name", "default"}, {"oidc_claim_last_name", "family_name", "default"},
	{"oidc_claim_display_name", "name", "default"}, {"oidc_claim_entitlements", "", "default"},
	{"oidc_default_groups", "", "default"},
	{"enrollment_username", "doorcode-enroll", "default"}, {"enrollment_password", "", "default"},
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

// settingRows is the section each setting is shown in, and its label and
// help text.
var settingRows = map[string]struct{ section, label, help string }{
	"site_name":                       {"Site", "Site name", "Shown in the header and every page title."},
	"site_timezone":                   {"Site", "Timezone", "A tz database name such as Europe/Berlin. Schedules, dates typed on pages, and every time shown are read in it."},
	"admin_email":                     {"Site", "First administrator's email", "The account created on first start when there are no users."},
	"theme_accent":                    {"Theme", "Accent color", "A color as #rrggbb, such as #4f39f6."},
	"theme_accent_dark":               {"Theme", "Accent color in the dark theme", "A color as #rrggbb, or empty to use the accent color."},
	"password_hash_memory_kib":        {"Accounts and passwords", "Password hashing memory", "KiB of memory hashing one password takes, from 8 to 1048576. More is slower to guess."},
	"password_min_length":             {"Accounts and passwords", "Shortest password", "How many characters a password chosen in the portal has at least, from 8 to 128."},
	"new_user_expiry_days":            {"Accounts and passwords", "New account lifetime", "Days until the account of a user created by signing in at the provider expires."},
	"oidc_enabled":                    {"Single sign-on", "Single sign-on enabled", "true or false."},
	"oidc_auto_sign_in":               {"Single sign-on", "Automatic sign-in", "true sends the sign-in page straight on to the provider and hides the password forms; add ?auto_sign_in=false to reach them."},
	"oidc_issuer":                     {"Single sign-on", "Issuer", "The provider's https:// address, exactly as it names itself."},
	"oidc_client_id":                  {"Single sign-on", "Client id", "The id the site is registered with at the provider."},
	"oidc_client_secret":              {"Single sign-on", "Client secret", "The secret the site is registered with, if the provider gave one."},
	"oidc_scopes":                     {"Single sign-on", "Scopes", "Scopes asked for, separated by spaces, openid among them."},
	"oidc_claim_user_id":              {"Identity claims", "User id claim", "The claim holding the user id people are matched by first."},
	"oidc_claim_email":                {"Identity claims", "Email claim", "Required."},
	"oidc_claim_first_name":           {"Identity claims", "First name claim", ""},
	"oidc_claim_last_name":            {"Identity claims", "Last name claim", ""},
	"oidc_claim_display_name":         {"Identity claims", "Display name claim", ""},
	"oidc_claim_entitlements":         {"Identity claims", "Entitlements claim", "The claim listing the slugs of someone's groups. A sign-in that carries it makes them a member of exactly the groups it names, ignoring slugs no group has; one without it leaves their groups alone. While it is set, new users join no default or pre-assigned groups."},
	"oidc_default_groups":             {"Identity claims", "Groups new users join", "Group slugs separated by commas. New users join these, and the groups whose pre-assigned list holds their user id, only while the entitlements claim is empty."},
	"enrollment_username":             {"Door enrollment", "Enrollment username", ""},
	"enrollment_password":             {"Door enrollment", "Enrollment password", "Generated and logged on first start when empty."},
	"telemetry_retention_days":        {"Telemetry", "Reports kept", "Days each telemetry report is kept."},
	"telemetry_rollup_retention_days": {"Telemetry", "Hourly summaries kept", "Days the hourly summaries of the reports are kept."},
	"unresponsive_minutes":            {"Telemetry", "Unresponsive after", "Minutes a connected door may stay silent before it counts as unresponsive."},
	"public_url":                      {"Firmware updates", "Public address", "The https:// address doors and browsers reach the server at. Nothing is sent to doors to download while it is empty."},
	"update_window_days":              {"Firmware updates", "Update days", "daily, or weekdays separated by commas such as sat,sun."},
	"update_window_start":             {"Firmware updates", "Update window opens", "HH:MM in the site timezone."},
	"update_window_minutes":           {"Firmware updates", "Update window length", "Minutes."},
	"update_concurrency":              {"Firmware updates", "Doors updating at once", ""},
	"update_timeout_minutes":          {"Firmware updates", "Update timeout", "Minutes a door may take to come back on its new version before the update fails."},
	"door_strike_pulse_ms":            {"Door defaults", "Strike pulse", "A whole number of milliseconds from 100 to 60000."},
	"door_keypad_idle_clear_ms":       {"Door defaults", "Keypad idle clear", "A whole number of milliseconds from 1000 to 600000."},
	"door_min_code_length":            {"Door defaults", "Shortest code", "A whole number of digits from 1 to 16."},
	"door_max_code_length":            {"Door defaults", "Longest code", "A whole number of digits from 16 to 64."},
	"door_submit_key":                 {"Door defaults", "Submit key", "One of #, A, B, C, D."},
	"door_reply_timeout_ms":           {"Door defaults", "Reply timeout", "A whole number of milliseconds from 100 to 60000."},
	"door_telemetry_interval_s":       {"Door defaults", "Telemetry interval", "A whole number of seconds from 5 to 86400."},
	"door_held_open_s":                {"Door defaults", "Held open after", "A whole number of seconds from 5 to 86400."},
	"door_timeline_refresh_hours":     {"Door defaults", "Timeline refresh", "A whole number of hours from 1 to 24."},
	"door_credential_rotation_days":   {"Door defaults", "Credential rotation", "A whole number of days from 1 to 3650."},
	"door_rate_limit_per_minute":      {"Door defaults", "Code checks a minute", "A whole number of checks from 1 to 6000."},
	"door_rate_limit_burst":           {"Door defaults", "Code checks in a burst", "A whole number of checks from 1 to 1000."},
	"door_ntp_server":                 {"Door defaults", "Time server", "A host name of up to 253 letters, digits, dots, and hyphens."},
	"door_log_level":                  {"Door defaults", "Log level", "One of error, warn, info, debug."},
}

// secretSettings are the settings the settings page offers as password
// fields, which are left blank.
var secretSettings = map[string]bool{"oidc_client_secret": true, "enrollment_password": true}

// settingsPage is the settings page showing shown, where changed replaces
// the value and source of the settings it names, entered the value an
// input holds, and refused what is wrong with a value; a read-only
// setting shows its value in place of an input, and a secret that has a
// value offers to clear it.
func settingsPage(shown []shownSetting, changed map[string]shownSetting, entered, refused map[string]string) testsupport.Page {
	page := testsupport.Page{
		Title: "Settings · doorcode", Heading: "Settings", Notes: []string{settingsNote}, Values: map[string]string{"picture": ""},
		Nav:    []string{"Home", "Dashboard", "Users", "Requests", "Groups", "Schedules", "Doors", "Zones", "Firmware", "Events", "Settings (current)", "API tokens", "API docs"},
		Tables: map[string][][]string{},
	}
	for _, s := range shown {
		if c, ok := changed[s.key]; ok {
			s = c
		}
		row := settingRows[s.key]
		cell := refused[s.key]
		switch {
		case s.source == "environment, read-only":
			note := "Set by DOORCODE_" + upper(s.key) + " in the environment; change it there."
			cell = s.value + " " + note
			page.Notes = append(page.Notes, note)
		case secretSettings[s.key] && s.value != "":
			// A secret's input is a password field, whose value is not read.
			cell = "Clear"
		case secretSettings[s.key]:
		case entered[s.key] != "" || refused[s.key] != "":
			page.Values[s.key] = entered[s.key]
		default:
			page.Values[s.key] = s.value
		}
		setting := row.label + " " + s.key
		if row.help != "" {
			setting += " " + row.help
		}
		if page.Tables[row.section] == nil {
			page.Tables[row.section] = [][]string{{"Setting", "Value", "Source"}}
		}
		page.Tables[row.section] = append(page.Tables[row.section], []string{setting, cell, s.source})
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
			if diff := cmp.Diff(wantLog, lastLog(f)); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}
