package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestSettingsListShowsEveryDefaultWhenNothingIsStored(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/api/v1/settings")

	want := testsupport.Response{Status: 200, Body: `{"settings":[` +
		`{"key":"site_name","value":"doorcode","source":"default","read_only":false,"env_var":"DOORCODE_SITE_NAME"},` +
		`{"key":"site_timezone","value":"UTC","source":"default","read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE"}]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingsListShowsStoredAndEnvironmentValuesWithTheirSources(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Main Building", "2026-01-02T08:00:00Z")
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Europe/Berlin"

	got := testsupport.Get(start(t, f).Handler(), "/api/v1/settings")

	want := testsupport.Response{Status: 200, Body: `{"settings":[` +
		`{"key":"site_name","value":"Main Building","source":"database","read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-01-02T08:00:00Z"},` +
		`{"key":"site_timezone","value":"Europe/Berlin","source":"environment","read_only":true,"env_var":"DOORCODE_SITE_TIMEZONE"}]}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestSettingReadsTheStoredValue(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_timezone", "America/Chicago", "2026-01-03T10:15:00Z")

	got := testsupport.Get(start(t, f).Handler(), "/api/v1/settings/site_timezone")

	want := testsupport.Response{Status: 200, Body: `{"key":"site_timezone","value":"America/Chicago","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE","updated_at":"2026-01-03T10:15:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestEnvironmentValueWinsOverTheStoredValueAndIsReadOnly(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Stored Name", "2026-01-03T10:15:00Z")
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"

	got := testsupport.Get(start(t, f).Handler(), "/api/v1/settings/site_name")

	want := testsupport.Response{Status: 200, Body: `{"key":"site_name","value":"Env Name","source":"environment",` +
		`"read_only":true,"env_var":"DOORCODE_SITE_NAME"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingASettingRespondsWithTheNewValueAndTheClockTime(t *testing.T) {
	f := testsupport.NewFactory()
	f.ClockStub.Set("2026-02-10T14:30:00Z")

	got := testsupport.PutJSON(start(t, f).Handler(), "/api/v1/settings/site_name", `{"value":"North Campus"}`)

	want := testsupport.Response{Status: 200, Body: `{"key":"site_name","value":"North Campus","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-02-10T14:30:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatedSettingIsReadBackFromTheStore(t *testing.T) {
	h := start(t, testsupport.NewFactory()).Handler()
	testsupport.PutJSON(h, "/api/v1/settings/site_timezone", `{"value":"Asia/Tokyo"}`)

	got := testsupport.Get(h, "/api/v1/settings/site_timezone")

	want := testsupport.Response{Status: 200, Body: `{"key":"site_timezone","value":"Asia/Tokyo","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_TIMEZONE","updated_at":"` + testsupport.DefaultNow + `"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUpdatingAnEnvironmentProvidedSettingIsRejected(t *testing.T) {
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"

	got := testsupport.PutJSON(start(t, f).Handler(), "/api/v1/settings/site_name", `{"value":"Other"}`)

	want := testsupport.Response{Status: 409, Body: `{"error":"setting is read-only: ` +
		`site_name is set by environment variable DOORCODE_SITE_NAME"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestRejectedUpdateLeavesTheStoredValueInPlace(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("site_name", "Stored Name", "2026-01-03T10:15:00Z")
	f.EnvStub["DOORCODE_SITE_NAME"] = "Env Name"
	h := start(t, f).Handler()
	testsupport.PutJSON(h, "/api/v1/settings/site_name", `{"value":"Other"}`)
	delete(f.EnvStub, "DOORCODE_SITE_NAME")

	got := testsupport.Get(h, "/api/v1/settings/site_name")

	want := testsupport.Response{Status: 200, Body: `{"key":"site_name","value":"Stored Name","source":"database",` +
		`"read_only":false,"env_var":"DOORCODE_SITE_NAME","updated_at":"2026-01-03T10:15:00Z"}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestUnknownSettingIsNotFound(t *testing.T) {
	requests := map[string]func(h testsupport.Handler) testsupport.Response{
		"read": func(h testsupport.Handler) testsupport.Response {
			return testsupport.Get(h, "/api/v1/settings/door_color")
		},
		"update": func(h testsupport.Handler) testsupport.Response {
			return testsupport.PutJSON(h, "/api/v1/settings/door_color", `{"value":"red"}`)
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			got := request(start(t, testsupport.NewFactory()).Handler())

			want := testsupport.Response{Status: 404, Body: `{"error":"unknown setting \"door_color\""}` + "\n"}
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
			got := testsupport.PutJSON(start(t, testsupport.NewFactory()).Handler(), "/api/v1/settings/site_name", body)

			want := testsupport.Response{Status: 400, Body: `{"error":"body must be a JSON object with a string \"value\""}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFailureAnswersWithARequestIDThatTheLogCarries(t *testing.T) {
	requests := map[string]func(h testsupport.Handler) testsupport.Response{
		"list": func(h testsupport.Handler) testsupport.Response {
			return testsupport.Get(h, "/api/v1/settings")
		},
		"read": func(h testsupport.Handler) testsupport.Response {
			return testsupport.Get(h, "/api/v1/settings/site_name")
		},
		"update": func(h testsupport.Handler) testsupport.Response {
			return testsupport.PutJSON(h, "/api/v1/settings/site_name", `{"value":"x"}`)
		},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			h := start(t, f).Handler()
			f.StoreStub.Fail()

			got := request(h)

			want := testsupport.Response{Status: 500, Body: `{"error":"internal error","request_id":"0001020304050607"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
			wantLog := []testsupport.LogEntry{
				{Level: "info", Message: "schema migrated"},
				{Level: "error", Message: "request failed",
					Fields: map[string]string{"request_id": "0001020304050607", "error": "stub: unavailable"}},
			}
			if diff := cmp.Diff(wantLog, f.LogStub.Entries); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}
