package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// logsSince is what f's log recorded after its first n lines.
func logsSince(f *testsupport.Factory, n int) []testsupport.LogEntry {
	return f.LogStub.Entries[n:]
}

func TestRunFailsToStart(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		want    string
	}{
		"when the environment sets an empty enrollment password": {
			arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = "" },
			want: "store the generated enrollment password: setting is read-only: " +
				"enrollment_password is set by environment variable DOORCODE_ENROLLMENT_PASSWORD",
		},
		"when a generated enrollment password cannot be stored": {
			arrange: func(f *testsupport.Factory) {
				delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")
				f.StoreStub.FailSettingWrites()
			},
			want: "store the generated enrollment password: stub: unavailable",
		},
		"when settings cannot be read": {
			arrange: func(f *testsupport.Factory) { f.StoreStub.FailSettings() },
			want:    "stub: unavailable",
		},
		"when the broker refuses the enrollment client": {
			arrange: func(f *testsupport.Factory) { f.BrokerStub.FailCreating() },
			want:    "create the enrollment client: stub: unavailable",
		},
		"when the broker refuses the subscription": {
			arrange: func(f *testsupport.Factory) { f.BrokerStub.FailSubscribing() },
			want:    "subscribe: stub: unavailable",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			f.EnvStub["DOORCODE_ENROLLMENT_PASSWORD"] = enrollPassword
			a := start(t, f)
			c.arrange(f)

			err := a.Run(t.Context())

			if diff := cmp.Diff(c.want, testsupport.ErrorText(err)); diff != "" {
				t.Errorf("error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunIgnoresMessagesItCannotUseAndLogsWhy(t *testing.T) {
	t.Parallel()
	v2 := testsupport.CBOR(map[string]any{"v": 2, "online": true})
	cases := map[string]struct {
		topic   string
		payload []byte
		want    string
	}{
		"an announcement without a hardware id": {"doorcode/enroll/", announcement, "the topic names no controller or door"},
		"a door that is not a number":           {"doorcode/doors/front/online", onlineAs("x", true), "the topic names no controller or door"},
		"door zero":                             {"doorcode/doors/0/online", onlineAs("x", true), "the topic names no controller or door"},
		"an announcement that is not CBOR":      {"doorcode/enroll/A1", []byte("hello"), "not a protocol message: unexpected EOF"},
		"an announcement of another version":    {"doorcode/enroll/A1", v2, "unsupported protocol version 2"},
		"a status without a version":            {"doorcode/doors/1/online", testsupport.CBOR(map[string]any{"online": true}), "unsupported protocol version 0"},
		"a door that does not exist":            {"doorcode/doors/7/online", onlineAs("x", true), "no controller is enrolled as door 7"},
		"a door no controller is enrolled as":   {"doorcode/doors/2/online", onlineAs("x", true), "no controller is enrolled as door 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b, _, _ := enrolledDoor(t, f)
			createDoor(t, b, frontDoor)
			logged := len(f.LogStub.Entries)
			f.BrokerStub.Inject(c.topic, c.payload)

			run(t, a)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{"topic": c.topic, "error": c.want}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunLogsWhenItCannotHandleAMessage(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		payload []byte
	}{
		"when doors cannot be read":                           {func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, onlineAs(firstUsername, true)},
		"when credentials cannot be read":                     {func(f *testsupport.Factory) { f.StoreStub.FailCredentials() }, onlineAs(firstUsername, true)},
		"when the first use cannot be stored":                 {func(f *testsupport.Factory) { f.StoreStub.FailCredentialWrites() }, onlineAs(secondUsername, true)},
		"when old credentials cannot be revoked":              {func(f *testsupport.Factory) { f.BrokerStub.FailDeleting() }, onlineAs(secondUsername, true)},
		"when the state cannot be stored":                     {func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, onlineAs(firstUsername, false)},
		"when the state of a connected door cannot be stored": {func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, onlineAs(firstUsername, true)},
		"when settings cannot be read":                        {func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, onlineAs(firstUsername, true)},
		"when the door's overrides cannot be read":            {func(f *testsupport.Factory) { f.StoreStub.FailOverrides() }, onlineAs(firstUsername, true)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b, _, _ := enrolledDoor(t, f)
			send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
			logged := len(f.LogStub.Entries)
			c.arrange(f)
			f.BrokerStub.Inject("doorcode/doors/1/online", c.payload)

			run(t, a)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message",
				Fields: map[string]string{"topic": "doorcode/doors/1/online", "error": "stub: unavailable"}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunLogsWhenADoorThatConnectsCannotBeSentItsTimeline(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _, _, _ := enrolledDoor(t, f)
	f.StoreStub.SeedSetting("door_credential_grace_hours", "0", testsupport.DefaultNow)
	logged := len(f.LogStub.Entries)
	f.BrokerStub.Inject("doorcode/doors/1/online", onlineAs(firstUsername, true))

	run(t, a)

	want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{
		"topic": "doorcode/doors/1/online", "error": "invalid value for door_credential_grace_hours: must be a whole number of hours from 1 to 8760",
	}}}
	if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestRunLogsWhenThePeriodicWorkFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser)
		at      string
		want    string
	}{
		"when credentials cannot be read": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailCredentials() },
			at:      "2026-01-05T09:01:00Z", want: "stub: unavailable",
		},
		"when a setting is invalid": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) {
				f.StoreStub.SeedSetting("door_credential_grace_hours", "0", testsupport.DefaultNow)
			},
			at: "2026-01-05T09:01:00Z", want: "invalid value for door_credential_grace_hours: must be a whole number of hours from 1 to 8760",
		},
		"when the rotation interval is invalid": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) {
				f.StoreStub.SeedSetting("door_credential_rotation_days", "3651", testsupport.DefaultNow)
			},
			// Both the rotation and the timeline refresh need a door's
			// configuration.
			at: "2026-01-05T09:01:00Z", want: "invalid value for door_credential_rotation_days: must be a whole number of days from 1 to 3650\n" +
				"invalid value for door_credential_rotation_days: must be a whole number of days from 1 to 3650",
		},
		"when settings cannot be read": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailSettings() },
			at:      "2026-01-05T09:01:00Z", want: "stub: unavailable",
		},
		"when expired sessions cannot be removed": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailSessions() },
			at:      "2026-01-05T09:01:00Z", want: "stub: unavailable",
		},
		"when schedules cannot be read": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailSchedules() },
			at:      "2026-01-05T09:01:00Z", want: "stub: unavailable",
		},
		"when doors cannot be read": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailDoors() },
			// Both the rotation and the timeline refresh read doors.
			at: "2026-01-05T09:01:00Z", want: "stub: unavailable\nstub: unavailable",
		},
		"when a timeline cannot be sent": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.BrokerStub.FailPublishing() },
			at:      "2026-01-05T15:00:00Z", want: "stub: unavailable",
		},
		"when a door's overrides cannot be read": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailOverrides() },
			at:      "2026-01-05T15:00:00Z", want: "stub: unavailable",
		},
		"when new credentials cannot be created": {
			arrange: func(_ *testing.T, f *testsupport.Factory, _ *testsupport.Browser) { f.BrokerStub.FailCreating() },
			at:      "2026-02-04T09:00:00Z", want: "stub: unavailable",
		},
		"when old credentials cannot be revoked": {
			arrange: func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser) {
				send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)
				f.BrokerStub.FailDeleting()
			},
			at: "2026-01-06T09:00:00Z", want: "stub: unavailable",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b, _, _ := enrolledDoor(t, f)
			c.arrange(t, f, b)
			logged := len(f.LogStub.Entries)
			f.ClockStub.Set(c.at)

			run(t, a)

			want := []testsupport.LogEntry{{Level: "error", Message: "periodic work failed", Fields: map[string]string{"error": c.want}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPeriodicWorkRunsAtMostOnceAMinute(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _, _, _ := enrolledDoor(t, f)
	f.StoreStub.FailCredentials()
	f.ClockStub.Set("2026-01-05T09:00:59Z")

	run(t, a)
	logged := len(f.LogStub.Entries)
	f.ClockStub.Set("2026-01-05T09:01:00Z")
	run(t, a)

	want := []testsupport.LogEntry{{Level: "error", Message: "periodic work failed", Fields: map[string]string{"error": "stub: unavailable"}}}
	if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

// runFails runs a and returns what it reports.
func runFails(t *testing.T, a *app.App) string {
	t.Helper()
	return testsupport.ErrorText(a.Run(t.Context()))
}

func TestFirstRunWithoutAnEnrollmentPasswordGeneratesOneAndLogsIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a := start(t, f)
	logged := len(f.LogStub.Entries)

	run(t, a)
	run(t, start(t, f))
	_, refused := f.BrokerStub.Connect("A1", enrollUser, "", "", nil)
	_, accepted := f.BrokerStub.Connect("A1", enrollUser, "IiMkJSYnKCkqKywtLi8wMTIz", "", nil)

	want := []any{
		[]testsupport.LogEntry{{Level: "info", Message: "generated the enrollment password; controllers enroll with these credentials, " +
			"and the enrollment_password setting changes the password",
			Fields: map[string]string{"username": enrollUser, "password": "IiMkJSYnKCkqKywtLi8wMTIz"}},
			{Level: "info", Message: "schema migrated"}},
		"stub: not authorized", "<nil>",
	}
	got := []any{logsSince(f, logged), testsupport.ErrorText(refused), testsupport.ErrorText(accepted)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("log of both starts, empty password, generated password (-want +got):\n%s", diff)
	}
}

func TestEnrollmentPasswordSavedInTheSettingsLetsRunStart(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"from-the-ui"}`, 200)
	a := start(t, f)

	got := runFails(t, a)

	if diff := cmp.Diff("<nil>", got); diff != "" {
		t.Errorf("run (-want +got):\n%s", diff)
	}
}

func TestRotationSettingsAcceptWholeNumbersWithinTheirRange(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"door_credential_rotation_days=0":    400,
		"door_credential_rotation_days=1":    200,
		"door_credential_rotation_days=3650": 200,
		"door_credential_rotation_days=3651": 400,
		"door_credential_grace_hours=0":      400,
		"door_credential_grace_hours=1":      200,
		"door_credential_grace_hours=8760":   200,
		"door_credential_grace_hours=8761":   400,
	}
	for setting, want := range cases {
		t.Run(setting, func(t *testing.T) {
			key, value := splitSetting(setting)
			got := signedInAdmin(t, testsupport.NewFactory()).PutJSON("/api/v1/settings/"+key, `{"value":"`+value+`"}`)

			if diff := cmp.Diff(want, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

// splitSetting splits key=value.
func splitSetting(s string) (string, string) {
	for i := range s {
		if s[i] == '=' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}
