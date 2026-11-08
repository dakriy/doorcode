package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

const jsonType = "application/json"

// hashMemoryJSON is how the settings API shows the password hashing cost the
// stub environment sets.
const hashMemoryJSON = `{"key":"password_hash_memory_kib","value":"8","source":"environment","read_only":true,` +
	`"env_var":"DOORCODE_PASSWORD_HASH_MEMORY_KIB"}`

func TestSettingsListShowsDefaultsAndTheEnvironmentWhenNothingIsStored(t *testing.T) {
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/settings")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"settings":[` +
		`{"key":"site_name","value":"doorcode","source":"default","read_only":false,"env_var":"DOORCODE_SITE_NAME"},` +
		`{"key":"site_timezone","value":"UTC","source":"default","read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE"},` +
		`{"key":"admin_email","value":"admin@example.com","source":"default","read_only":false,"env_var":"DOORCODE_ADMIN_EMAIL"},` +
		hashMemoryJSON + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingsListShowsStoredAndEnvironmentValuesWithTheirSources(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Main Building", "2026-01-02T08:00:00Z")
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Europe/Berlin"

	got := signedInAdmin(t, f).Get("/api/v1/settings")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"settings":[` +
		`{"key":"site_name","value":"Main Building","source":"database","read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-01-02T08:00:00Z"},` +
		`{"key":"site_timezone","value":"Europe/Berlin","source":"environment","read_only":true,"env_var":"DOORCODE_SITE_TIMEZONE"},` +
		`{"key":"admin_email","value":"admin@example.com","source":"default","read_only":false,"env_var":"DOORCODE_ADMIN_EMAIL"},` +
		hashMemoryJSON + `]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingReadsTheStoredValue(t *testing.T) {
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
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"

	got := signedInAdmin(t, f).PutJSON("/api/v1/settings/site_name", `{"value":"Other"}`)

	want := testsupport.Response{Status: 409, ContentType: jsonType, Body: `{"error":"setting is read-only: ` +
		`site_name is set by environment variable DOORCODE_SITE_NAME"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestRejectedUpdateLeavesTheStoredValueInPlace(t *testing.T) {
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
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	delete(f.EnvStub, "DOORCODE_PASSWORD_HASH_MEMORY_KIB")

	got := b.PutJSON("/api/v1/settings/password_hash_memory_kib", `{"value":"7"}`)

	want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"invalid value for password_hash_memory_kib: ` +
		`must be a whole number of KiB from 8 to 1048576"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUnknownSettingIsNotFound(t *testing.T) {
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

			want := testsupport.Response{Status: 404, ContentType: jsonType, Body: `{"error":"unknown setting \"door_color\""}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateWithoutAStringValueIsABadRequest(t *testing.T) {
	bodies := map[string]string{
		"not json":     `site_name=x`,
		"no value":     `{}`,
		"number value": `{"value":3}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/site_name", body)

			want := testsupport.Response{Status: 400, ContentType: jsonType, Body: `{"error":"body must be a JSON object with a string \"value\""}` + "\n"}
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
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()

			got := request(b)

			want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
			wantLog := testsupport.LogEntry{Level: "error", Message: "request failed",
				Fields: map[string]string{"request_id": "4243444546474849", "error": "stub: unavailable"}}
			if diff := cmp.Diff(wantLog, f.LogStub.Entries[len(f.LogStub.Entries)-1]); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIAsksForSignIn(t *testing.T) {
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			got := request(testsupport.NewBrowser(start(t, testsupport.NewFactory()).Handler()))

			want := testsupport.Response{Status: 401, ContentType: jsonType, Body: `{"error":"sign in required"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIRefusesAUserWithoutTheAdminRole(t *testing.T) {
	for name, request := range settingsRequests {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.RevokeRoles(adminEmail)

			got := request(b)

			want := testsupport.Response{Status: 403, ContentType: jsonType, Body: `{"error":"the admin role is required"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSettingsAPIAnswersWithARequestIDWhenSessionsCannotBeRead(t *testing.T) {
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.StoreStub.FailSessions()

	got := b.Get("/api/v1/settings")

	want := testsupport.Response{Status: 500, ContentType: jsonType, Body: `{"error":"internal error","request_id":"4243444546474849"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
