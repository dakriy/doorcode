package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// defaultConfigJSON is the effective configuration of a door that follows
// the site's defaults, as the API shows it.
const defaultConfigJSON = `{"strike_pulse_ms":3000,"keypad_idle_clear_ms":10000,"min_code_length":4,"max_code_length":32,` +
	`"reply_timeout_ms":3000,"telemetry_interval_s":60,"held_open_s":60,"timeline_refresh_hours":6,` +
	`"credential_rotation_days":30,"rate_limit_per_minute":30,"rate_limit_burst":10,"ntp_server":"pool.ntp.org","broker_host":"",` +
	`"broker_port":8883,"broker_tls":true,"broker_ca_pem":"","wifi_ssid":"","wifi_password":"","wifi_roam_rssi":-70,"wifi_11b_disabled":false,` +
	`"keepalive_s":30,"reconnect_max_s":60,"mesh_enabled":false,"mesh_ssid":"","mesh_password":"","mesh_min_router_rssi":-75,"mesh_max_layer":4,` +
	`"update_connect_timeout_s":300,"accept_unsigned_firmware":false,"log_level":"info"}`

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
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"reply_timeout_ms":"5000"}}`, 200)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":"500","log_level":"debug"}}`)

	effective := with(testsupport.JSON(defaultConfigJSON).(map[string]any), map[string]any{"strike_pulse_ms": 500.0, "log_level": "debug"})
	want := map[string]any{"effective": effective, "overrides": map[string]any{"strike_pulse_ms": "500", "log_level": "debug"}}
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
		"the retired submit key":   {"/api/v1/doors/1/config", `{"overrides":{"submit_key":"#"}}`, 400, "No door setting has this name."},
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
	long := testsupport.Repeat("a", 253)
	cases := map[string]struct {
		field, value string
		accepted     bool
	}{
		"the briefest strike pulse":       {"strike_pulse_ms", "100", true},
		"too brief a strike pulse":        {"strike_pulse_ms", "99", false},
		"the longest strike pulse":        {"strike_pulse_ms", "60000", true},
		"too long a strike pulse":         {"strike_pulse_ms", "60001", false},
		"a strike pulse of no number":     {"strike_pulse_ms", "soon", false},
		"the shortest idle clear":         {"keypad_idle_clear_ms", "1000", true},
		"too short an idle clear":         {"keypad_idle_clear_ms", "999", false},
		"the longest idle clear":          {"keypad_idle_clear_ms", "600000", true},
		"a shortest code of one":          {"min_code_length", "1", true},
		"a shortest code of none":         {"min_code_length", "0", false},
		"a shortest code of 16":           {"min_code_length", "16", true},
		"a shortest code of 17":           {"min_code_length", "17", false},
		"a longest code of 16":            {"max_code_length", "16", true},
		"a longest code of 15":            {"max_code_length", "15", false},
		"a longest code of 64":            {"max_code_length", "64", true},
		"a longest code of 65":            {"max_code_length", "65", false},
		"the shortest reply timeout":      {"reply_timeout_ms", "100", true},
		"too short a reply timeout":       {"reply_timeout_ms", "99", false},
		"the shortest telemetry":          {"telemetry_interval_s", "5", true},
		"too short a telemetry":           {"telemetry_interval_s", "4", false},
		"the longest telemetry":           {"telemetry_interval_s", "86400", true},
		"an hourly timeline refresh":      {"timeline_refresh_hours", "1", true},
		"a daily timeline refresh":        {"timeline_refresh_hours", "24", true},
		"too rare a timeline refresh":     {"timeline_refresh_hours", "25", false},
		"a daily rotation":                {"credential_rotation_days", "1", true},
		"too rare a rotation":             {"credential_rotation_days", "3651", false},
		"a check a minute":                {"rate_limit_per_minute", "1", true},
		"too many checks a minute":        {"rate_limit_per_minute", "6001", false},
		"a burst of one":                  {"rate_limit_burst", "1", true},
		"too large a burst":               {"rate_limit_burst", "1001", false},
		"a time server name":              {"ntp_server", "a0.z9-AZ", true},
		"the longest time server name":    {"ntp_server", long, true},
		"too long a time server name":     {"ntp_server", long + "a", false},
		"no time server":                  {"ntp_server", "", false},
		"a slash in a time server":        {"ntp_server", "a/b", false},
		"a colon in a time server":        {"ntp_server", "a:b", false},
		"a backtick in a time server":     {"ntp_server", "a`b", false},
		"a brace in a time server":        {"ntp_server", "a{b", false},
		"an at sign in a time server":     {"ntp_server", "a@b", false},
		"a bracket in a time server":      {"ntp_server", "a[b", false},
		"a broker host name":              {"broker_host", "mqtt.example.com", true},
		"a broker IPv4 address":           {"broker_host", "192.0.2.10", true},
		"a broker IPv6 address":           {"broker_host", "2001:db8::10", true},
		"no broker host":                  {"broker_host", "", true},
		"too long a broker host":          {"broker_host", long + "a", false},
		"a port with the broker host":     {"broker_host", "mqtt.example.com:8883", false},
		"a broker address":                {"broker_host", "mqtts://mqtt.example.com", false},
		"the lowest broker port":          {"broker_port", "1", true},
		"a broker port of none":           {"broker_port", "0", false},
		"the highest broker port":         {"broker_port", "65535", true},
		"too high a broker port":          {"broker_port", "65536", false},
		"a broker over TLS":               {"broker_tls", "true", true},
		"a broker without TLS":            {"broker_tls", "false", true},
		"a broker over TLS of yes":        {"broker_tls", "yes", false},
		"a broker CA certificate":         {"broker_ca_pem", testsupport.Certificate, true},
		"a broker CA chain":               {"broker_ca_pem", testsupport.Certificate + testsupport.Certificate, true},
		"a broker CA with CR LF":          {"broker_ca_pem", testsupport.CRLF(testsupport.Certificate), true},
		"no broker CA":                    {"broker_ca_pem", "", true},
		"a broker CA of white space":      {"broker_ca_pem", " \n", false},
		"a broker CA that is not PEM":     {"broker_ca_pem", "certificate", false},
		"a broker CA with more after it":  {"broker_ca_pem", testsupport.Certificate + "more", false},
		"a broker CA that is a key":       {"broker_ca_pem", testsupport.RetypedPEM(testsupport.Certificate, "PRIVATE KEY"), false},
		"a broker CA that cannot be read": {"broker_ca_pem", "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", false},
		"the longest Wi-Fi network":       {"wifi_ssid", testsupport.Repeat("n", 32), true},
		"too long a Wi-Fi network":        {"wifi_ssid", testsupport.Repeat("n", 33), false},
		"no Wi-Fi network":                {"wifi_ssid", "", true},
		"the longest Wi-Fi password":      {"wifi_password", testsupport.Repeat("p", 63), true},
		"too long a Wi-Fi password":       {"wifi_password", testsupport.Repeat("p", 64), false},
		"no Wi-Fi password":               {"wifi_password", "", true},
		"the weakest roaming signal":      {"wifi_roam_rssi", "-100", true},
		"too weak a roaming signal":       {"wifi_roam_rssi", "-101", false},
		"the strongest roaming signal":    {"wifi_roam_rssi", "0", true},
		"too strong a roaming signal":     {"wifi_roam_rssi", "1", false},
		"no 802.11b rates":                {"wifi_11b_disabled", "true", true},
		"802.11b rates":                   {"wifi_11b_disabled", "false", true},
		"802.11b rates of maybe":          {"wifi_11b_disabled", "maybe", false},
		"the shortest keep-alive":         {"keepalive_s", "10", true},
		"too short a keep-alive":          {"keepalive_s", "9", false},
		"the longest keep-alive":          {"keepalive_s", "300", true},
		"too long a keep-alive":           {"keepalive_s", "301", false},
		"the shortest reconnect pause":    {"reconnect_max_s", "5", true},
		"too short a reconnect pause":     {"reconnect_max_s", "4", false},
		"the longest reconnect pause":     {"reconnect_max_s", "600", true},
		"too long a reconnect pause":      {"reconnect_max_s", "601", false},
		"a mesh with no network":          {"mesh_enabled", "true", false},
		"no mesh":                         {"mesh_enabled", "false", true},
		"a mesh of maybe":                 {"mesh_enabled", "maybe", false},
		"the longest mesh network":        {"mesh_ssid", testsupport.Repeat("m", 32), true},
		"too long a mesh network":         {"mesh_ssid", testsupport.Repeat("m", 33), false},
		"no mesh password":                {"mesh_password", "", true},
		"too short a mesh password":       {"mesh_password", testsupport.Repeat("p", 7), false},
		"the shortest mesh password":      {"mesh_password", testsupport.Repeat("p", 8), true},
		"the longest mesh password":       {"mesh_password", testsupport.Repeat("p", 63), true},
		"too long a mesh password":        {"mesh_password", testsupport.Repeat("p", 64), false},
		"the weakest mesh signal":         {"mesh_min_router_rssi", "-100", true},
		"too weak a mesh signal":          {"mesh_min_router_rssi", "-101", false},
		"the strongest mesh signal":       {"mesh_min_router_rssi", "0", true},
		"too strong a mesh signal":        {"mesh_min_router_rssi", "1", false},
		"a mesh of one hop":               {"mesh_max_layer", "1", true},
		"a mesh of no hops":               {"mesh_max_layer", "0", false},
		"a mesh of ten hops":              {"mesh_max_layer", "10", true},
		"a mesh of eleven hops":           {"mesh_max_layer", "11", false},
		"the shortest firmware revert":    {"update_connect_timeout_s", "60", true},
		"too short a firmware revert":     {"update_connect_timeout_s", "59", false},
		"the longest firmware revert":     {"update_connect_timeout_s", "1800", true},
		"too long a firmware revert":      {"update_connect_timeout_s", "1801", false},
		"unsigned firmware":               {"accept_unsigned_firmware", "true", true},
		"only signed firmware":            {"accept_unsigned_firmware", "false", true},
		"unsigned firmware of one":        {"accept_unsigned_firmware", "1", false},
		"the debug log level":             {"log_level", "debug", true},
		"the error log level":             {"log_level", "error", true},
		"an unknown log level":            {"log_level", "trace", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)

			override := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"`+c.field+`":`+testsupport.Quoted(c.value)+`}}`)
			setting := b.PutJSON("/api/v1/settings/door_"+c.field, `{"value":`+testsupport.Quoted(c.value)+`}`)

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
		testsupport.JSONAt(b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"log_level":"loud"}}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"ntp_server":"a b"}}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/settings/door_log_level", `{"value":"loud"}`).Body, "/error"),
		testsupport.JSONAt(b.PutJSON("/api/v1/settings/door_ntp_server", `{"value":"a b"}`).Body, "/error"),
	}

	want := []any{
		"Enter one of error, warn, info, debug.",
		"Enter a host name of up to 253 letters, digits, dots, and hyphens.",
		"invalid value for door_log_level: must be one of error, warn, info, debug",
		"invalid value for door_ntp_server: must be a host name of up to 253 letters, digits, dots, and hyphens",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("errors (-want +got):\n%s", diff)
	}
}

// configTable is the configuration table of the page of a door that
// follows the site's defaults, but for the overrides and the values in
// effect changed names, by field, each marked overridden; the broker's
// host warns while it is empty, and its certificate while it is missing
// though the door connects over TLS.
func configTable(changed map[string][2]string) [][]string {
	rows := [][]string{{"Setting", "Site default", "This door", "In effect"}}
	tls := ""
	for _, f := range []struct{ name, label, about, value string }{
		{"strike_pulse_ms", "Strike pulse",
			"How long the door's strike stays released each time it lets someone in. The door may open within this, plus the reply timeout, without counting as forced open. A whole number of milliseconds from 100 to 60000.", "3000"},
		{"held_open_s", "Held open after",
			"How long a door may stand open, when it should be locked, before it counts as held open and an event is logged. A whole number of seconds from 5 to 86400.", "60"},
		{"keypad_idle_clear_ms", "Keypad idle clear",
			"How long a partly typed entry waits for the next key before the keypad discards it. A whole number of milliseconds from 1000 to 600000.", "10000"},
		{"min_code_length", "Shortest code",
			"The fewest digits a code may have; the door discards a shorter entry without asking the server. A whole number of digits from 1 to 16.", "4"},
		{"max_code_length", "Longest code",
			"The longest entry the keypad takes, a keypad command included; the door takes no more keys once an entry is this long. A whole number of digits from 16 to 64.", "32"},
		{"reply_timeout_ms", "Reply timeout",
			"How long the door waits for the server's answer to a code before denying it. A whole number of milliseconds from 100 to 60000.", "3000"},
		{"telemetry_interval_s", "Telemetry interval",
			"How often the door reports its health readings, such as its signal and uptime, to the server. A whole number of seconds from 5 to 86400.", "60"},
		{"timeline_refresh_hours", "Timeline refresh",
			"How often the server sends the door its timeline of when to stay open and locked, even when nothing changed it. A whole number of hours from 1 to 24.", "6"},
		{"credential_rotation_days", "Credential rotation",
			"How many days the door's broker credentials work before the server issues it new ones. A whole number of days from 1 to 3650.", "30"},
		{"rate_limit_per_minute", "Code checks a minute",
			"How many codes a minute the server checks from the door once its burst is spent; an entry over the limit is denied without being looked at and logged as rate limited. A whole number of checks from 1 to 6000.", "30"},
		{"rate_limit_burst", "Code checks in a burst",
			"How many codes the server checks from the door in quick succession before the limit a minute applies. A whole number of checks from 1 to 1000.", "10"},
		{"log_level", "Log level",
			"How much the door writes to its own log, from errors only to every detail. One of error, warn, info, debug.", "info"},
		{"ntp_server", "Time server",
			"The time server the door sets its clock from; the door keeps to its timeline by this clock. A host name of up to 253 letters, digits, dots, and hyphens.", "pool.ntp.org"},
		{"broker_host", "Broker host",
			"The broker the door connects to, from its next connection on; empty keeps the broker it uses now. A host name of up to 253 letters, digits, dots, and hyphens, an IP address, or nothing to keep the door's broker.", ""},
		{"broker_port", "Broker port",
			"The port of the broker the door connects to, from its next connection on. A port from 1 to 65535.", "8883"},
		{"broker_tls", "Broker over TLS",
			"Whether the door connects to the broker over TLS, checking the broker against the broker CA certificate. Turn it off for a development broker only. True or false.", "true"},
		{"broker_ca_pem", "Broker CA certificate",
			"The certificate the door checks the broker against while it connects over TLS: the broker's CA's, or the broker's own. Empty keeps the one the door has. The PEM certificate of the broker's CA or of the broker itself, or nothing.", ""},
		{"keepalive_s", "Keep-alive",
			"How often the door lets the broker know it is still there, from its next connection on; it shows offline once one and a half of these pass without a word from it. A whole number of seconds from 10 to 300.", "30"},
		{"reconnect_max_s", "Longest reconnect pause",
			"The longest the door waits between attempts to connect again; it waits 1, 2, 4 seconds and so on up to this. A whole number of seconds from 5 to 600.", "60"},
		{"wifi_ssid", "Wi-Fi network",
			"The Wi-Fi network the door joins; empty keeps the network it uses now. A door that cannot join a new network goes back to the last one that worked. A network name of up to 32 bytes, or nothing to keep the door's network.", ""},
		{"wifi_password", "Wi-Fi password",
			"The Wi-Fi network's password. It is stored encrypted and sent to nobody but the door. A password of up to 63 bytes.", ""},
		{"wifi_roam_rssi", "Roam below",
			"When the signal from its access point is weaker than this, the door moves to an access point of the same network at least 8 dB stronger, if it finds one. A whole number of dBm from -100 to 0.", "-70"},
		{"wifi_11b_disabled", "No 802.11b rates",
			"Whether the door keeps off the slow 802.11b rates, 1 to 11 Mbps, instead of falling back to them when packets are lost. It helps a door with a strong signal that still loses many packets on a crowded 2.4 GHz network, where the slow rates make things worse, and hurts a door with a weak signal at the edge of coverage, which needs those rates to stay connected. Leave it off unless the door's telemetry shows a good signal together with many TCP retransmits. Changing it makes the door restart its Wi-Fi, dropping off the network for a moment; a door in the mesh restarts. True or false.", "false"},
		{"mesh_enabled", "Mesh",
			"Whether the door may join or form a Wi-Fi mesh with other doors when its own access point's signal is poor. True or false.", "false"},
		{"mesh_ssid", "Mesh network",
			"The mesh network's name, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open. A network name of up to 32 bytes, or nothing.", ""},
		{"mesh_password", "Mesh password",
			"The mesh network's password, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open. It is stored encrypted and sent to nobody but the doors. A password of 8 to 63 bytes, or nothing.", ""},
		{"mesh_min_router_rssi", "Mesh below",
			"When the strongest access point a door with the mesh on can see is weaker than this, the door joins the mesh through a neighboring door instead. A whole number of dBm from -100 to 0.", "-75"},
		{"mesh_max_layer", "Deepest mesh hop",
			"How many hops from the access point a door may sit in the mesh. A whole number of hops from 1 to 10.", "4"},
		{"update_connect_timeout_s", "Revert new firmware after",
			"How long a door on newly installed firmware tries to connect before it goes back to its previous firmware. Keep it within the update timeout, after which the server counts the update failed. A whole number of seconds from 60 to 1800.", "300"},
		{"accept_unsigned_firmware", "Unsigned firmware",
			"Whether the door installs firmware that has no valid signature. Leave it off except on bench and development boards. True or false.", "false"},
	} {
		effective := f.value
		if c, ok := changed[f.name]; ok {
			effective = c[1] + " Overridden"
		}
		if f.name == "broker_tls" {
			tls = effective
		}
		if f.name == "broker_ca_pem" && tls == "true" && effective == "" {
			effective = brokerNote
		}
		if f.name == "broker_host" && effective == "" {
			effective = hostNote
		}
		if heading, starts := groupStarted("door_" + f.name); starts {
			rows = append(rows, heading)
		}
		rows = append(rows, []string{f.label + " " + f.about, f.value, "", effective})
	}
	return rows
}

func TestAnEnrolledDoorsPageShowsItsConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"log_level":"debug"}}`, 200)

	page := doorPage(t, b, "/admin/doors/1?tab=configuration")

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

	got := b.Post("/admin/doors/1/config", "strike_pulse_ms", " 500 ", "log_level", "", "reply_timeout_ms", "5000")
	page := pageOf(b.Get(got.Location))

	want := []any{303, "/admin/doors/1?tab=configuration", []string{"The door's configuration has been saved and sent to its controller."},
		configWith(map[string]any{"strike_pulse_ms": int64(500), "reply_timeout_ms": int64(5000)})}
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

// threeOverrides overrides three of a door's settings, a secret among
// them.
const threeOverrides = `{"overrides":{"strike_pulse_ms":"500","log_level":"debug","wifi_password":"hunter2 now"}}`

// countingThree is nav with the Configuration tab counting three
// overrides.
func countingThree(nav []string) []string {
	out := []string{}
	for _, item := range nav {
		switch item {
		case "Configuration":
			item = "Configuration 3 overridden"
		case "Configuration (current)":
			item = "Configuration 3 overridden (current)"
		}
		out = append(out, item)
	}
	return out
}

func TestADoorsPageCountsTheSettingsItOverrides(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		overrides, tab string
		want           []any
	}{
		"none": {`{"overrides":{}}`, "configuration",
			[]any{doorNav("Configuration", true), []string{"Follows the site defaults", hostNote, brokerNote}}},
		"three": {threeOverrides, "configuration",
			[]any{countingThree(doorNav("Configuration", true)), []string{"3 of 30 settings overridden", hostNote, brokerNote}}},
		"three, from another tab": {threeOverrides, "details",
			[]any{countingThree(doorNav("Details", true)), []string{picturesNote,
				"Never in open mode: none of its zones has an open-mode schedule. Open-mode schedules are set on each zone's page.",
				"There are no zones yet. Create one on the zones page."}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/doors/1/config", c.overrides, 200)

			page := doorPage(t, b, "/admin/doors/1?tab="+c.tab)

			if diff := cmp.Diff(c.want, []any{page.Nav, page.Notes}); diff != "" {
				t.Errorf("navigation, notes (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageMarksEachSettingItOverrides(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/doors/1/config", threeOverrides, 200)

	rows := doorPage(t, b, "/admin/doors/1?tab=configuration").Tables["Configuration"]

	want := [][]string{{"500 Overridden"}, {"******** Overridden"}, {"300"}}
	got := [][]string{rows[strikePulseRow][3:], rows[wifiPasswordRow][3:], rows[connectTimeoutRow][3:]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("in effect of strike pulse, Wi-Fi password, connect timeout (-want +got):\n%s", diff)
	}
}

func TestUsingTheSiteDefaultsFromADoorsPageRemovesEveryOverrideAndSendsTheConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", threeOverrides, 200)
	asked := testsupport.Confirmations(b.Get("/admin/doors/1?tab=configuration").Body)["/admin/doors/1/config/defaults"]

	got := b.Post("/admin/doors/1/config/defaults")
	page := pageOf(b.Get(got.Location))

	want := []any{"Use site defaults: Return Lobby to the site defaults? Every setting overridden here is removed, secrets included, and its controller is sent the new configuration.",
		303, "/admin/doors/1?tab=configuration",
		[]string{"The door follows the site defaults again, and its configuration has been sent to its controller."},
		map[string]any{}, configWith(nil)}
	if diff := cmp.Diff(want, []any{asked, got.Status, got.Location, page.Messages,
		testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/overrides"), f.BrokerStub.RetainedDecoded(doorTopic("1", "config"))}); diff != "" {
		t.Errorf("question, status, location, messages, overrides, configuration sent (-want +got):\n%s", diff)
	}
}

func TestUsingTheSiteDefaultsNeedsTheManageDoorsRoleAndADoor(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		roles, target string
		status        int
	}{
		"a door commander":         {`["door-commander"]`, "/admin/doors/1/config/defaults", 403},
		"a door that is not there": {"", "/admin/doors/9/config/defaults", 404},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			send(t, b, "PUT", "/api/v1/doors/1/config", threeOverrides, 200)
			if c.roles != "" {
				holdingOnly(t, f, b, c.roles)
			}
			b.Get("/")

			got := b.Post(c.target)

			if diff := cmp.Diff([]any{c.status, "500"}, []any{got.Status, storedOverride(t, f, 1, "strike_pulse_ms")}); diff != "" {
				t.Errorf("status, strike pulse stored (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnEmptyObjectOfOverridesReturnsADoorToTheSiteDefaults(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)
	send(t, b, "PUT", "/api/v1/doors/1/config", threeOverrides, 200)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{}}`)

	want := testsupport.Response{Status: 200, ContentType: jsonType, Body: `{"effective":` + defaultConfigJSON + `,"overrides":{}}` + "\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
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

	got := b.Post("/admin/settings", "enrollment_username", "doorcode-enroll", "door_log_level", "debug", "door_ntp_server", "pool.ntp.org", "unknown", "x")
	page := pageOf(b.Get(got.Location))

	want := []any{303, "/admin/settings?tab=doors",
		[]string{"The settings have been saved. Doors receive changed configuration defaults right away."},
		settingsPage("Doors", defaultSettings, map[string]shownSetting{
			"door_log_level":      {"door_log_level", "debug", "database"},
			"enrollment_password": {"enrollment_password", "********", "environment, read-only"},
		}, nil, nil).Tables, topicsOf("config", "3", "2", "1")}
	if diff := cmp.Diff(want, []any{got.Status, got.Location, page.Messages, page.Tables, f.BrokerStub.Topics(sent, "config")}); diff != "" {
		t.Errorf("status, location, messages, settings, configurations sent (-want +got):\n%s", diff)
	}
}

func TestARefusedSettingsFormShowsWhatIsWrongOnItsTabAndStoresNothing(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		tab              string
		form             []string
		entered, refused map[string]string
	}{
		"on Site setup": {
			tab: "Site setup", form: []string{"site_name", "North Campus", "site_timezone", "Mars/Base"},
			entered: map[string]string{"site_name": "North Campus", "site_timezone": "Mars/Base"},
			refused: map[string]string{"site_timezone": "Must be a time zone name from the tz database, such as UTC or Europe/Berlin."},
		},
		"on Doors": {
			tab: "Doors", form: []string{"door_log_level", "debug", "door_rate_limit_burst", "0"},
			entered: map[string]string{"door_log_level": "debug", "door_rate_limit_burst": "0"},
			refused: map[string]string{"door_rate_limit_burst": "Must be a whole number of checks from 1 to 1000."},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			b.Get("/")

			got := b.Post("/admin/settings", c.form...)

			page := pageOf(got)
			want := settingsPage(c.tab, defaultSettings, nil, c.entered, c.refused)
			stored := testsupport.JSONAt(b.Get("/api/v1/settings/"+c.form[0]).Body, "/source")
			if diff := cmp.Diff([]any{422, want, "default"}, []any{got.Status, page, stored}); diff != "" {
				t.Errorf("status, page, source of the valid value (-want +got):\n%s", diff)
			}
		})
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
