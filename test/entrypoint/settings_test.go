package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

const jsonType = "application/json"

// apiError is the API's answer refusing a request with status and message,
// written as it appears in the JSON body.
func apiError(status int, message string) testsupport.Response {
	return testsupport.Response{Status: status, ContentType: jsonType, Body: `{"error":"` + message + `"}` + "\n"}
}

// internalError is the API's answer to a request it could not handle,
// naming the request id its log line carries.
func internalError(requestID string) testsupport.Response {
	body := `{"error":"internal error","request_id":"` + requestID + `"}` + "\n"
	return testsupport.Response{Status: 500, ContentType: jsonType, Body: body}
}

// hashMemoryJSON is how the settings API shows the password hashing cost the
// stub environment sets.
const hashMemoryJSON = `{"key":"password_hash_memory_kib","value":"8","source":"environment","read_only":true,` +
	`"env_var":"DOORCODE_PASSWORD_HASH_MEMORY_KIB"}`

// themeSettingsJSON is how the settings API shows the theme colors when
// nothing sets them.
var themeSettingsJSON = defaultSetting("theme_accent", "#4f39f6") + `,` + defaultSetting("theme_accent_dark", "")

// signInSettingsJSON is how the settings API shows the settings of
// passwords, of new users, and of signing in through an identity provider when nothing sets
// them.
var signInSettingsJSON = defaultSetting("password_min_length", "12") + `,` +
	defaultSetting("new_user_expiry_days", "1826") + `,` +
	defaultSetting("oidc_enabled", "false") + `,` +
	defaultSetting("oidc_auto_sign_in", "false") + `,` +
	defaultSetting("oidc_issuer", "") + `,` +
	defaultSetting("oidc_client_id", "") + `,` +
	`{"key":"oidc_client_secret","value":"","source":"default","read_only":false,"env_var":"DOORCODE_OIDC_CLIENT_SECRET","secret":true},` +
	defaultSetting("oidc_scopes", "openid email profile") + `,` +
	defaultSetting("oidc_claim_user_id", "") + `,` +
	defaultSetting("oidc_claim_email", "email") + `,` +
	defaultSetting("oidc_claim_first_name", "given_name") + `,` +
	defaultSetting("oidc_claim_last_name", "family_name") + `,` +
	defaultSetting("oidc_claim_display_name", "name") + `,` +
	defaultSetting("oidc_claim_entitlements", "") + `,` +
	defaultSetting("oidc_default_groups", "") + `,` +
	defaultSetting("portal_door_pictures", "false")

// brokerSettingsJSON is how the settings API shows the broker settings
// and the door configuration defaults when nothing sets them.
var brokerSettingsJSON = `{"key":"enrollment_username","value":"doorcode-enroll","source":"default","read_only":false,` +
	`"env_var":"DOORCODE_ENROLLMENT_USERNAME"},` +
	`{"key":"enrollment_password","value":"","source":"default","read_only":false,"env_var":"DOORCODE_ENROLLMENT_PASSWORD","secret":true},` +
	defaultSetting("telemetry_retention_days", "7") + `,` +
	defaultSetting("telemetry_rollup_retention_days", "730") + `,` +
	defaultSetting("unresponsive_minutes", "5") + `,` +
	defaultSetting("public_url", "") + `,` +
	defaultSetting("update_window_days", "daily") + `,` +
	defaultSetting("update_window_start", "02:00") + `,` +
	defaultSetting("update_window_minutes", "120") + `,` +
	defaultSetting("update_concurrency", "5") + `,` +
	defaultSetting("update_timeout_minutes", "30") + `,` +
	defaultSetting("door_strike_pulse_ms", "3000") + `,` +
	defaultSetting("door_keypad_idle_clear_ms", "10000") + `,` +
	defaultSetting("door_min_code_length", "4") + `,` +
	defaultSetting("door_max_code_length", "32") + `,` +
	defaultSetting("door_submit_key", "#") + `,` +
	defaultSetting("door_reply_timeout_ms", "3000") + `,` +
	defaultSetting("door_telemetry_interval_s", "60") + `,` +
	defaultSetting("door_held_open_s", "60") + `,` +
	defaultSetting("door_timeline_refresh_hours", "6") + `,` +
	defaultSetting("door_credential_rotation_days", "30") + `,` +
	defaultSetting("door_rate_limit_per_minute", "30") + `,` +
	defaultSetting("door_rate_limit_burst", "10") + `,` +
	defaultSetting("door_ntp_server", "pool.ntp.org") + `,` +
	defaultSetting("door_log_level", "info")

// defaultSetting is how the settings API shows the setting named key
// holding its default value.
func defaultSetting(key, value string) string {
	return `{"key":"` + key + `","value":"` + value + `","source":"default","read_only":false,"env_var":"DOORCODE_` + upper(key) + `"}`
}

// upper is s with its ASCII letters upper-cased.
func upper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if 'a' <= c && c <= 'z' {
			out[i] = c - 'a' + 'A'
		}
	}
	return string(out)
}

func TestSettingsListShowsDefaultsAndTheEnvironmentWhenNothingIsStored(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/settings")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"settings":[` +
		`{"key":"site_name","value":"doorcode","source":"default","read_only":false,"env_var":"DOORCODE_SITE_NAME"},` +
		`{"key":"site_timezone","value":"UTC","source":"default","read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE"},` +
		`{"key":"admin_email","value":"admin@example.com","source":"default","read_only":false,"env_var":"DOORCODE_ADMIN_EMAIL"},` +
		themeSettingsJSON + `,` + hashMemoryJSON + `,` + signInSettingsJSON + `,` + brokerSettingsJSON + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingsListShowsStoredAndEnvironmentValuesWithTheirSources(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Main Building", "2026-01-02T08:00:00Z")
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Europe/Berlin"

	got := signedInAdmin(t, f).Get("/api/v1/settings")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"settings":[` +
		`{"key":"site_name","value":"Main Building","source":"database","read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-01-02T08:00:00Z"},` +
		`{"key":"site_timezone","value":"Europe/Berlin","source":"environment","read_only":true,"env_var":"DOORCODE_SITE_TIMEZONE"},` +
		`{"key":"admin_email","value":"admin@example.com","source":"default","read_only":false,"env_var":"DOORCODE_ADMIN_EMAIL"},` +
		themeSettingsJSON + `,` + hashMemoryJSON + `,` + signInSettingsJSON + `,` + brokerSettingsJSON + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingReadsTheStoredValue(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_timezone", "America/Chicago", "2026-01-03T10:15:00Z")

	got := signedInAdmin(t, f).Get("/api/v1/settings/site_timezone")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"key":"site_timezone","value":"America/Chicago","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE","updated_at":"2026-01-03T10:15:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestEnvironmentValueWinsOverTheStoredValueAndIsReadOnly(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Stored Name", "2026-01-03T10:15:00Z")
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"

	got := signedInAdmin(t, f).Get("/api/v1/settings/site_name")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"key":"site_name","value":"Env Name","source":"environment",` +
		`"read_only":true,"env_var":"DOORCODE_SITE_NAME"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingASettingRespondsWithTheNewValueAndTheClockTime(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.ClockStub.Set("2026-02-10T14:30:00Z")
	b := signedInAdmin(t, f)

	got := b.PutJSON("/api/v1/settings/site_name", `{"value":"North Campus"}`)

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"key":"site_name","value":"North Campus","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-02-10T14:30:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatedSettingIsReadBackFromTheStore(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.PutJSON("/api/v1/settings/site_timezone", `{"value":"Asia/Tokyo"}`)

	got := b.Get("/api/v1/settings/site_timezone")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"key":"site_timezone","value":"Asia/Tokyo","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE","updated_at":"` + testsupport.DefaultNow + `"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingAnEnvironmentProvidedSettingIsRejected(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"

	got := signedInAdmin(t, f).PutJSON("/api/v1/settings/site_name", `{"value":"Other"}`)

	want := apiError(409, "setting is read-only: site_name is set by environment variable DOORCODE_SITE_NAME")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestRejectedUpdateLeavesTheStoredValueInPlace(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Stored Name", "2026-01-03T10:15:00Z")
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"
	b := signedInAdmin(t, f)
	b.PutJSON("/api/v1/settings/site_name", `{"value":"Other"}`)
	delete(f.EnvStub, "DOORCODE_SITE_NAME")

	got := b.Get("/api/v1/settings/site_name")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"key":"site_name","value":"Stored Name","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-01-03T10:15:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestPasswordHashingCostAcceptsWholeKiBWithinItsRange(t *testing.T) {
	t.Parallel()
	values := map[string]int{
		"7":       400,
		"8":       200,
		"1048576": 200,
		"1048577": 400,
		"-8":      400,
		"64 KiB":  400,
	}
	for value, want := range values {
		t.Run(value, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			delete(f.EnvStub, "DOORCODE_PASSWORD_HASH_MEMORY_KIB")

			got := b.PutJSON("/api/v1/settings/password_hash_memory_kib", `{"value":"`+value+`"}`)

			if diff := cmp.Diff(want, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestRejectedPasswordHashingCostSaysWhatItAccepts(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	delete(f.EnvStub, "DOORCODE_PASSWORD_HASH_MEMORY_KIB")

	got := b.PutJSON("/api/v1/settings/password_hash_memory_kib", `{"value":"7"}`)

	want := apiError(400, "invalid value for password_hash_memory_kib: must be a whole number of KiB from 8 to 1048576")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUnknownSettingIsNotFound(t *testing.T) {
	t.Parallel()
	requests := map[string]func(b *testsupport.Browser) testsupport.Response{
		"read": func(b *testsupport.Browser) testsupport.Response {
			return b.Get("/api/v1/settings/door_color")
		},
		"update": func(b *testsupport.Browser) testsupport.Response {
			return b.PutJSON("/api/v1/settings/door_color", `{"value":"red"}`)
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			got := request(signedInAdmin(t, testsupport.NewFactory()))

			want := apiError(404, `unknown setting \"door_color\"`)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateWithoutAStringValueIsABadRequest(t *testing.T) {
	t.Parallel()
	bodies := map[string]string{
		"not json":     `site_name=x`,
		"no value":     `{}`,
		"number value": `{"value":3}`,
		"both":         `{"value":"x","clear":true}`,
		"not clearing": `{"clear":false}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/site_name", body)

			want := apiError(400, `body must be a JSON object with either a string \"value\" or \"clear\": true`)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

// settingsRequests is one request to each settings API operation.
var settingsRequests = map[string]func(b *testsupport.Browser) testsupport.Response{
	"list": func(b *testsupport.Browser) testsupport.Response {
		return b.Get("/api/v1/settings")
	},
	"read": func(b *testsupport.Browser) testsupport.Response {
		return b.Get("/api/v1/settings/site_name")
	},
	"update": func(b *testsupport.Browser) testsupport.Response {
		return b.PutJSON("/api/v1/settings/site_name", `{"value":"x"}`)
	},
}

func TestSettingsStoreFailureAnswersWithARequestIDThatTheLogCarries(t *testing.T) {
	t.Parallel()
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()

			got := request(b)

			want := internalError("4243444546474849")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
			wantLog := testsupport.LogEntry{Level: "error", Message: "request failed",
				Fields: map[string]string{"request_id": "4243444546474849", "error": "stub: unavailable"}}
			if diff := cmp.Diff(wantLog, lastLog(f)); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIAsksForAToken(t *testing.T) {
	t.Parallel()
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			got := request(startBrowser(t, testsupport.NewFactory()))

			want := apiError(401, "a valid API token is required")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIRefusesAUserWithoutTheAdminRole(t *testing.T) {
	t.Parallel()
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.RevokeRoles(adminEmail)

			got := request(b)

			want := apiError(403, "the admin role is required")
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIAnswersWithARequestIDWhenSessionsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.FailSessions()

	got := b.Get("/api/v1/settings")

	want := internalError("4243444546474849")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSiteTimezoneAcceptsOnlyZonesOfTheTzDatabase(t *testing.T) {
	t.Parallel()
	values := map[string]int{
		"America/New_York": 200,
		"UTC":              200,
		"Mars/Olympus":     400,
		"":                 400,
		"Local":            400,
	}
	for value, want := range values {
		t.Run(value, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/site_timezone", `{"value":"`+value+`"}`)

			if diff := cmp.Diff(want, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestRejectedSiteTimezoneSaysWhatItAccepts(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/site_timezone", `{"value":"Mars/Olympus"}`)

	want := apiError(400, "invalid value for site_timezone: must be a time zone name from the tz database, such as UTC or Europe/Berlin")
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestPagesAreDrawnInTheAccentColorsTheSettingsChoose(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := adminSession(t, f)
	before := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Styles
	send(t, b, "PUT", "/api/v1/settings/theme_accent", `{"value":"#0F766E"}`, 200)
	light := testsupport.ParsePage(testsupport.Get(h, "/sign-in").Body).Styles
	send(t, b, "PUT", "/api/v1/settings/theme_accent_dark", `{"value":"#2dd4bf"}`, 200)

	got := []any{before, light, testsupport.ParsePage(b.Get("/admin/settings").Body).Styles}

	want := []any{
		[]string{":root { --accent: #4f39f6; }"},
		[]string{":root { --accent: #0f766e; }"},
		[]string{":root { --accent: #0f766e; --accent-dark: #2dd4bf; }"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("default, accent, both (-want +got):\n%s", diff)
	}
}

func TestAccentColorsAreColorsAsHexDigits(t *testing.T) {
	t.Parallel()
	values := map[string][]int{
		"#abcdef":  {200, 200},
		"#ABC123":  {200, 200},
		"":         {400, 200},
		"#abcde":   {400, 400},
		"#abcdeg":  {400, 400},
		"abcdef0":  {400, 400},
		"#abcdef0": {400, 400},
		"teal":     {400, 400},
	}
	for value, want := range values {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())

			got := []int{
				b.PutJSON("/api/v1/settings/theme_accent", `{"value":"`+value+`"}`).Status,
				b.PutJSON("/api/v1/settings/theme_accent_dark", `{"value":"`+value+`"}`).Status,
			}

			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("accent, dark accent (-want +got):\n%s", diff)
			}
		})
	}
}

func TestARefusedAccentColorSaysWhatItAccepts(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/theme_accent_dark", `{"value":"teal"}`)

	want := `{"error":"invalid value for theme_accent_dark: must be a color as #rrggbb, such as #4f39f6"}` + "\n"
	if diff := cmp.Diff([]any{400, want}, []any{got.Status, got.Body}); diff != "" {
		t.Errorf("status, body (-want +got):\n%s", diff)
	}
}
