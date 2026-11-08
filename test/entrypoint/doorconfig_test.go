package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// defaultConfigJSON is the effective configuration of a door that follows
// the site's defaults, as the API shows it.
const defaultConfigJSON = `{"strike_pulse_ms":3000,"keypad_idle_clear_ms":10000,"min_code_length":4,"max_code_length":32,` +
	`"submit_key":"#","reply_timeout_ms":3000,"telemetry_interval_s":60,"held_open_s":60,"timeline_refresh_hours":6,` +
	`"credential_rotation_days":30,"rate_limit_per_minute":30,"rate_limit_burst":10,"ntp_server":"pool.ntp.org","log_level":"info"}`

func TestADoorWithoutOverridesHasTheSitesDefaultConfiguration(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.Get("/api/v1/doors/1/config")

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"effective":` + defaultConfigJSON + `,"overrides":{}}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}

func TestADoorsOverridesReplaceTheDefaultsTheyNameAndEachOther(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"submit_key":"A"}}`, 200)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":"500","log_level":"debug"}}`)

	want := testsupport.JSON(`{"effective":{"strike_pulse_ms":500,"keypad_idle_clear_ms":10000,"min_code_length":4,"max_code_length":32,
		"submit_key":"#","reply_timeout_ms":3000,"telemetry_interval_s":60,"held_open_s":60,"timeline_refresh_hours":6,
		"credential_rotation_days":30,"rate_limit_per_minute":30,"rate_limit_burst":10,"ntp_server":"pool.ntp.org","log_level":"debug"},
		"overrides":{"strike_pulse_ms":"500","log_level":"debug"}}`)
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, testsupport.JSONAt(got.Body, "")}); diff != "" {
		t.Errorf("status, body (-want +got):\n%s", diff)
	}
}

func TestADoorsDefaultsFollowTheSettings(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_DOOR_NTP_SERVER"] = "time.example.com"
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	send(t, b, "PUT", "/api/v1/settings/door_min_code_length", `{"value":"6"}`, 200)

	body := b.Get("/api/v1/doors/1/config").Body

	got := []any{testsupport.JSONAt(body, "/effective/ntp_server"), testsupport.JSONAt(body, "/effective/min_code_length")}
	if diff := cmp.Diff([]any{"time.example.com", 6.0}, got); diff != "" {
		t.Errorf("time server, shortest code (-want +got):\n%s", diff)
	}
}

func TestADoorsConfigurationRefusesWhatItCannotHold(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target, body string
		status       int
		want         string
	}{
		"an unknown field":         {"/api/v1/doors/1/config", `{"overrides":{"strike":"500"}}`, 400, "No door setting has this name."},
		"a value out of range":     {"/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":"50"}}`, 400, "Enter a whole number of milliseconds from 100 to 60000."},
		"no overrides":             {"/api/v1/doors/1/config", `{}`, 400, `body must be a JSON object with an object "overrides" of strings`},
		"overrides not strings":    {"/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":500}}`, 400, `body must be a JSON object with an object "overrides" of strings`},
		"a door that is not there": {"/api/v1/doors/9/config", `{"overrides":{}}`, 404, "no door has this id"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			got := b.PutJSON(c.target, c.body)

			if diff := cmp.Diff([]any{c.status, c.want}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadingTheConfigurationOfADoorThatIsNotThereIsNotFound(t *testing.T) {
	t.Parallel()
	got := signedInAdmin(t, testsupport.NewFactory()).Get("/api/v1/doors/9/config")

	if diff := cmp.Diff([]any{404, "no door has this id"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
		t.Errorf("status, error (-want +got):\n%s", diff)
	}
}

func TestADoorsConfigurationAnswersWithARequestIDWhenItCannotBeWorkedOut(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange      func(f *testsupport.Factory)
		method, body string
	}{
		"when settings cannot be read": {func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, "GET", ""},
		"when a default is invalid": {func(f *testsupport.Factory) {
			f.StoreStub.SeedSetting("door_strike_pulse_ms", "5", testsupport.DefaultNow)
		}, "GET", ""},
		"when overrides cannot be stored": {func(f *testsupport.Factory) { f.StoreStub.FailDoorWrites() }, "PUT", `{"overrides":{}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			c.arrange(f)

			got := b.Send(c.method, "/api/v1/doors/1/config", c.body)

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEachDoorSettingAcceptsOnlyWhatADoorCanRunWith(t *testing.T) {
	t.Parallel()
	long := repeat("a", 253)
	cases := map[string]struct {
		field, value string
		accepted     bool
	}{
		"the briefest strike pulse":    {"strike_pulse_ms", "100", true},
		"too brief a strike pulse":     {"strike_pulse_ms", "99", false},
		"the longest strike pulse":     {"strike_pulse_ms", "60000", true},
		"too long a strike pulse":      {"strike_pulse_ms", "60001", false},
		"a strike pulse of no number":  {"strike_pulse_ms", "soon", false},
		"the shortest idle clear":      {"keypad_idle_clear_ms", "1000", true},
		"too short an idle clear":      {"keypad_idle_clear_ms", "999", false},
		"the longest idle clear":       {"keypad_idle_clear_ms", "600000", true},
		"a shortest code of one":       {"min_code_length", "1", true},
		"a shortest code of none":      {"min_code_length", "0", false},
		"a shortest code of 16":        {"min_code_length", "16", true},
		"a shortest code of 17":        {"min_code_length", "17", false},
		"a longest code of 16":         {"max_code_length", "16", true},
		"a longest code of 15":         {"max_code_length", "15", false},
		"a longest code of 64":         {"max_code_length", "64", true},
		"a longest code of 65":         {"max_code_length", "65", false},
		"the hash submit key":          {"submit_key", "#", true},
		"the D submit key":             {"submit_key", "D", true},
		"the star submit key":          {"submit_key", "*", false},
		"the shortest reply timeout":   {"reply_timeout_ms", "100", true},
		"too short a reply timeout":    {"reply_timeout_ms", "99", false},
		"the shortest telemetry":       {"telemetry_interval_s", "5", true},
		"too short a telemetry":        {"telemetry_interval_s", "4", false},
		"the longest telemetry":        {"telemetry_interval_s", "86400", true},
		"an hourly timeline refresh":   {"timeline_refresh_hours", "1", true},
		"a daily timeline refresh":     {"timeline_refresh_hours", "24", true},
		"too rare a timeline refresh":  {"timeline_refresh_hours", "25", false},
		"a daily rotation":             {"credential_rotation_days", "1", true},
		"too rare a rotation":          {"credential_rotation_days", "3651", false},
		"a check a minute":             {"rate_limit_per_minute", "1", true},
		"too many checks a minute":     {"rate_limit_per_minute", "6001", false},
		"a burst of one":               {"rate_limit_burst", "1", true},
		"too large a burst":            {"rate_limit_burst", "1001", false},
		"a time server name":           {"ntp_server", "a0.z9-AZ", true},
		"the longest time server name": {"ntp_server", long, true},
		"too long a time server name":  {"ntp_server", long + "a", false},
		"no time server":               {"ntp_server", "", false},
		"a slash in a time server":     {"ntp_server", "a/b", false},
		"a colon in a time server":     {"ntp_server", "a:b", false},
		"a backtick in a time server":  {"ntp_server", "a`b", false},
		"a brace in a time server":     {"ntp_server", "a{b", false},
		"an at sign in a time server":  {"ntp_server", "a@b", false},
		"a bracket in a time server":   {"ntp_server", "a[b", false},
		"the debug log level":          {"log_level", "debug", true},
		"the error log level":          {"log_level", "error", true},
		"an unknown log level":         {"log_level", "trace", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			override := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"`+c.field+`":"`+c.value+`"}}`)
			setting := b.PutJSON("/api/v1/settings/door_"+c.field, `{"value":"`+c.value+`"}`)

			want := []bool{c.accepted, c.accepted}
			if diff := cmp.Diff(want, []bool{override.Status == 200, setting.Status == 200}); diff != "" {
				t.Errorf("override, default accepted (-want +got):\n%s\n%s\n%s", diff, override.Body, setting.Body)
			}
		})
	}
}

func TestARefusedDoorSettingSaysWhatItTakes(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := []any{
		testsupport.JSONAt(b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"submit_key":"*"}}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"ntp_server":"a b"}}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/settings/door_submit_key", `{"value":"*"}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/settings/door_ntp_server", `{"value":"a b"}`).Body, "/error"),
	}

	want := []any{
		"Enter one of #, A, B, C, D.",
		"Enter a host name of up to 253 letters, digits, dots, and hyphens.",
		"invalid value for door_submit_key: must be one of #, A, B, C, D",
		"invalid value for door_ntp_server: must be a host name of up to 253 letters, digits, dots, and hyphens",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("errors (-want +got):\n%s", diff)
	}
}

// configTable is the configuration table of the page of a door that
// follows the site's defaults, but for the overrides and the values in
// effect changed names, by field.
func configTable(changed map[string][2]string) [][]string {
	rows := [][]string{{"Setting", "Site default", "This door", "In effect"}}
	for _, f := range []struct{ name, label, hint, value string }{
		{"strike_pulse_ms", "Strike pulse", "a whole number of milliseconds from 100 to 60000", "3000"},
		{"keypad_idle_clear_ms", "Keypad idle clear", "a whole number of milliseconds from 1000 to 600000", "10000"},
		{"min_code_length", "Shortest code", "a whole number of digits from 1 to 16", "4"},
		{"max_code_length", "Longest code", "a whole number of digits from 16 to 64", "32"},
		{"submit_key", "Submit key", "one of #, A, B, C, D", "#"},
		{"reply_timeout_ms", "Reply timeout", "a whole number of milliseconds from 100 to 60000", "3000"},
		{"telemetry_interval_s", "Telemetry interval", "a whole number of seconds from 5 to 86400", "60"},
		{"held_open_s", "Held open after", "a whole number of seconds from 5 to 86400", "60"},
		{"timeline_refresh_hours", "Timeline refresh", "a whole number of hours from 1 to 24", "6"},
		{"credential_rotation_days", "Credential rotation", "a whole number of days from 1 to 3650", "30"},
		{"rate_limit_per_minute", "Code checks a minute", "a whole number of checks from 1 to 6000", "30"},
		{"rate_limit_burst", "Code checks in a burst", "a whole number of checks from 1 to 1000", "10"},
		{"ntp_server", "Time server", "a host name of up to 253 letters, digits, dots, and hyphens", "pool.ntp.org"},
		{"log_level", "Log level", "one of error, warn, info, debug", "info"},
	} {
		effective := f.value
		if c, ok := changed[f.name]; ok {
			effective = c[1]
		}
		rows = append(rows, []string{f.label + " " + f.hint, f.value, "", effective})
	}
	return rows
}

func TestAnEnrolledDoorsPageShowsItsConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"log_level":"debug"}}`, 200)

	page := doorPage(t, b, "/admin/doors/1")

	want := []any{configTable(map[string][2]string{"log_level": {"debug", "debug"}}), "debug", ""}
	got := []any{page.Tables["Configuration"], page.Values["log_level"], page.Values["strike_pulse_ms"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configuration, log level, strike pulse (-want +got):\n%s", diff)
	}
}

func TestSavingADoorsConfigurationFromItsPageSendsIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	b.Get("/")

	got := b.Post("/admin/doors/1/config", "strike_pulse_ms", " 500 ", "log_level", "", "submit_key", "A")
	page := pageOf(b.Get(got.Location))

	want := []any{303, "/admin/doors/1", []string{"The door's configuration has been saved and sent to its controller."},
		configWith(map[string]any{"strike_pulse_ms": int64(500), "submit_key": "A"})}
	if diff := cmp.Diff(want, []any{got.Status, got.Location, page.Messages, f.BrokerStub.RetainedDecoded(doorTopic("1", "config"))}); diff != "" {
		t.Errorf("status, location, messages, configuration sent (-want +got):\n%s", diff)
	}
}

func TestARefusedConfigurationFromADoorsPageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	b.Get("/")

	response := b.Post("/admin/doors/1/config", "strike_pulse_ms", "50", "log_level", "debug")

	page := pageOf(response)
	want := []any{422, map[string]string{"strike_pulse_ms": "Enter a whole number of milliseconds from 100 to 60000."}, "50", "debug", "info"}
	got := []any{response.Status, page.FieldErrors, page.Values["strike_pulse_ms"], page.Values["log_level"], testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/effective/log_level")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, field errors, strike pulse, log level, log level stored (-want +got):\n%s", diff)
	}
}

func TestSavingTheConfigurationOfADoorThatIsNotThereIsNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Post("/admin/doors/9/config", "log_level", "debug")

	if diff := cmp.Diff(404, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestADoorsPageFailsWhenADefaultIsInvalid(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	f.StoreStub.SeedSetting("door_log_level", "loud", testsupport.DefaultNow)

	got := b.Get("/admin/doors/1")

	if diff := cmp.Diff(500, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestSavingSettingsFromTheSettingsPageStoresOnlyWhatChanged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	b.Get("/")
	sent := len(f.BrokerStub.Published)

	got := b.Post("/admin/settings", "site_name", "North Campus", "admin_email", "admin@example.com", "door_log_level", "debug", "unknown", "x")
	page := pageOf(b.Get(got.Location))

	want := []any{303, "/admin/settings",
		[]string{"The settings have been saved. Doors receive changed configuration defaults right away."},
		settingsPage(defaultSettings, map[string]shownSetting{
			"site_name":           {"site_name", "North Campus", "database"},
			"door_log_level":      {"door_log_level", "debug", "database"},
			"enrollment_password": {"enrollment_password", enrollPassword, "environment, read-only"},
		}, nil, nil).Tables, topicsOf("config", "3", "2", "1")}
	if diff := cmp.Diff(want, []any{got.Status, got.Location, page.Messages, page.Tables, f.BrokerStub.Topics(sent, "config")}); diff != "" {
		t.Errorf("status, location, messages, settings, configurations sent (-want +got):\n%s", diff)
	}
}

func TestARefusedSettingsFormSaysWhatIsWrongAndStoresNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")

	got := b.Post("/admin/settings", "site_name", "North Campus", "site_timezone", "Mars/Base", "door_rate_limit_burst", "0",
		"password_hash_memory_kib", "16")

	page := pageOf(got)
	want := settingsPage(defaultSettings, nil, map[string]string{"site_name": "North Campus", "site_timezone": "Mars/Base", "door_rate_limit_burst": "0"},
		map[string]string{
			"site_timezone":         "Must be a time zone name from the tz database, such as UTC or Europe/Berlin.",
			"door_rate_limit_burst": "Must be a whole number of checks from 1 to 1000.",
		})
	stored := testsupport.JSONAt(b.Get("/api/v1/settings/site_name").Body, "/source")
	if diff := cmp.Diff([]any{422, want, "default"}, []any{got.Status, page, stored}); diff != "" {
		t.Errorf("status, page, site name source (-want +got):\n%s", diff)
	}
}

func TestSettingsPageAnswersWithARequestIDWhenTheStoreFails(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *testsupport.Store){
		"when settings cannot be read":   (*testsupport.Store).FailSettings,
		"when settings cannot be stored": (*testsupport.Store).FailSettingWrites,
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			fail(f.StoreStub)

			got := b.Post("/admin/settings", "site_name", "North Campus")

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
