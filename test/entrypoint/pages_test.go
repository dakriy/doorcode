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

// brokerNote is what pages warn of while controllers would connect to the
// broker over TLS with no certificate to verify it against, as they do on
// a new site.
var brokerNote = "Controllers connect to the broker over TLS, but door_broker_ca_pem gives no CA certificate: " +
	"a controller could not verify the broker, so no provisioning file can be made."

// hostNote is what pages warn of while controllers are given no broker to
// connect to, as they are on a new site.
var hostNote = "door_broker_host is empty: a controller needs an address to reach the broker, so no provisioning file can be made."

// shownSetting is a setting as the settings page shows it.
type shownSetting struct {
	key, value, source string
}

// defaultSettings is every setting the settings page shows on a site
// where nothing is stored and the environment sets only the password
// hashing cost, in order.
var defaultSettings = []shownSetting{
	{"site_name", "doorcode", "default"}, {"site_timezone", "UTC", "default"}, {"admin_email", "admin@example.com", "default"},
	{"public_url", "", "default"},
	{"trusted_proxies", "", "default"},
	{"theme_accent", "#4f39f6", "default"}, {"theme_accent_dark", "", "default"},
	{"password_hash_memory_kib", "8", "environment, read-only"}, {"password_min_length", "12", "default"},
	{"new_user_expiry_days", "1826", "default"}, {"oidc_enabled", "false", "default"}, {"oidc_auto_sign_in", "false", "default"},
	{"oidc_issuer", "", "default"}, {"oidc_client_id", "", "default"}, {"oidc_client_secret", "", "default"},
	{"oidc_scopes", "openid email profile", "default"}, {"oidc_claim_user_id", "", "default"}, {"oidc_claim_email", "email", "default"},
	{"oidc_claim_first_name", "given_name", "default"}, {"oidc_claim_last_name", "family_name", "default"},
	{"oidc_claim_display_name", "name", "default"}, {"oidc_claim_entitlements", "", "default"},
	{"oidc_default_groups", "", "default"}, {"portal_door_pictures", "false", "default"},
	{"enrollment_username", "doorcode-enroll", "default"}, {"enrollment_password", "", "default"},
	{"telemetry_retention_days", "7", "default"}, {"telemetry_rollup_retention_days", "730", "default"},
	{"unresponsive_minutes", "5", "default"},
	{"device_url", "", "default"}, {"update_window_days", "daily", "default"}, {"update_window_start", "02:00", "default"},
	{"update_window_minutes", "120", "default"}, {"update_concurrency", "5", "default"}, {"update_timeout_minutes", "30", "default"},
	{"door_strike_pulse_ms", "3000", "default"}, {"door_held_open_s", "60", "default"},
	{"door_keypad_idle_clear_ms", "10000", "default"}, {"door_min_code_length", "4", "default"},
	{"door_max_code_length", "32", "default"}, {"door_reply_timeout_ms", "3000", "default"},
	{"door_telemetry_interval_s", "60", "default"}, {"door_timeline_refresh_hours", "6", "default"},
	{"door_credential_rotation_days", "30", "default"}, {"door_rate_limit_per_minute", "30", "default"},
	{"door_rate_limit_burst", "10", "default"}, {"door_log_level", "info", "default"}, {"door_ntp_server", "pool.ntp.org", "default"},
	{"door_broker_host", "", "default"}, {"door_broker_port", "8883", "default"}, {"door_broker_tls", "true", "default"},
	{"door_broker_ca_pem", "", "default"}, {"door_keepalive_s", "30", "default"}, {"door_reconnect_max_s", "60", "default"},
	{"door_wifi_ssid", "", "default"}, {"door_wifi_password", "", "default"}, {"door_wifi_roam_rssi", "-70", "default"},
	{"door_wifi_11b_disabled", "false", "default"},
	{"door_mesh_enabled", "false", "default"}, {"door_mesh_ssid", "", "default"}, {"door_mesh_password", "", "default"},
	{"door_mesh_min_router_rssi", "-75", "default"}, {"door_mesh_max_layer", "4", "default"},
	{"door_update_connect_timeout_s", "300", "default"}, {"door_accept_unsigned_firmware", "false", "default"},
}

// settingRows is the section each setting is shown in, and its label and
// help text.
var settingRows = map[string]struct{ section, label, help string }{
	"site_name":     {"Site", "Site name", "Shown in the header and every page title."},
	"site_timezone": {"Site", "Timezone", "A zone of the tz database, such as Europe/Berlin; type to find one. Schedules, dates typed on pages, and every time shown are read in it."},
	"admin_email":   {"Site", "First administrator's email", "The account created on first start when there are no users."},
	"trusted_proxies": {"Proxies", "Trusted proxies", "IP addresses, CIDR ranges, and host names separated by commas, such as 10.0.0.0/8,proxy.example.com. " +
		"A host name is looked up by the periodic work, about once a minute, and trusts nobody until then."},
	"theme_accent":                    {"Theme", "Accent color", "A color as #rrggbb, such as #4f39f6."},
	"theme_accent_dark":               {"Theme", "Accent color in the dark theme", "A color as #rrggbb, or empty to use the accent color."},
	"password_hash_memory_kib":        {"Accounts and passwords", "Password hashing memory", "KiB of memory hashing one password takes, from 8 to 1048576. More is slower to guess."},
	"password_min_length":             {"Accounts and passwords", "Shortest password", "How many characters a password chosen in the portal has at least, from 8 to 128."},
	"new_user_expiry_days":            {"Accounts and passwords", "New account lifetime", "Days until the account of a user created by signing in at the provider expires."},
	"oidc_enabled":                    {"Single sign-on", "Single sign-on enabled", "Offers signing in at the provider set up here, beside signing in with a password."},
	"oidc_auto_sign_in":               {"Single sign-on", "Automatic sign-in", "Sends the sign-in page straight on to the provider and hides the password forms; add ?auto_sign_in=false to reach them."},
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
	"portal_door_pictures":            {"User portal", "Door pictures in the portal", "Shows each public door's primary picture in the portal; while it is off the portal shows none. The administration always shows them."},
	"enrollment_username":             {"Door enrollment", "Enrollment username", ""},
	"enrollment_password":             {"Door enrollment", "Enrollment password", "The password a new door controller signs in to the broker with, beside the enrollment username, until an administrator approves it and it gets credentials of its own. Enter both in the settings of each controller you install. When it is empty on first start, the server makes one up and writes it to its log."},
	"telemetry_retention_days":        {"Telemetry", "Reports kept", "Days each telemetry report is kept."},
	"telemetry_rollup_retention_days": {"Telemetry", "Hourly summaries kept", "Days the hourly summaries of the reports are kept."},
	"unresponsive_minutes":            {"Telemetry", "Unresponsive after", "Minutes a connected door may stay silent before it counts as unresponsive."},
	"public_url":                      {"Site", "Public address", "The https:// address browsers reach the server at, and single sign-on returns to; http:// only for development."},
	"device_url": {"Firmware updates", "Device address", "The https:// address of the server's device-facing entrypoint, which presents a certificate from the managed CA controllers trust, " +
		"the same CA as the broker's; doors download firmware from it. http:// only for development, since doors verify the server's certificate only over https. No update is sent to doors while it is empty."},
	"update_window_days":     {"Firmware updates", "Update days", "daily, or weekdays separated by commas such as sat,sun."},
	"update_window_start":    {"Firmware updates", "Update window opens", "A time of day in the site timezone."},
	"update_window_minutes":  {"Firmware updates", "Update window length", "Minutes."},
	"update_concurrency":     {"Firmware updates", "Doors updating at once", ""},
	"update_timeout_minutes": {"Firmware updates", "Update timeout", "Minutes a door may take to come back on its new version before the update fails."},
	"door_strike_pulse_ms": {"Door defaults", "Strike pulse",
		"How long the door's strike stays released each time it lets someone in. The door may open within this, plus the reply timeout, without counting as forced open. A whole number of milliseconds from 100 to 60000."},
	"door_keypad_idle_clear_ms": {"Door defaults", "Keypad idle clear",
		"How long a partly typed entry waits for the next key before the keypad discards it. A whole number of milliseconds from 1000 to 600000."},
	"door_min_code_length": {"Door defaults", "Shortest code",
		"The fewest digits a code may have; the door discards a shorter entry without asking the server. A whole number of digits from 1 to 16."},
	"door_max_code_length": {"Door defaults", "Longest code",
		"The longest entry the keypad takes, a keypad command included; the door takes no more keys once an entry is this long. A whole number of digits from 16 to 64."},
	"door_reply_timeout_ms": {"Door defaults", "Reply timeout",
		"How long the door waits for the server's answer to a code before denying it. A whole number of milliseconds from 100 to 60000."},
	"door_telemetry_interval_s": {"Door defaults", "Telemetry interval",
		"How often the door reports its health readings, such as its signal and uptime, to the server. A whole number of seconds from 5 to 86400."},
	"door_held_open_s": {"Door defaults", "Held open after",
		"How long a door may stand open, when it should be locked, before it counts as held open and an event is logged. A whole number of seconds from 5 to 86400."},
	"door_timeline_refresh_hours": {"Door defaults", "Timeline refresh",
		"How often the server sends the door its timeline of when to stay open and locked, even when nothing changed it. A whole number of hours from 1 to 24."},
	"door_credential_rotation_days": {"Door defaults", "Credential rotation",
		"How many days the door's broker credentials work before the server issues it new ones. A whole number of days from 1 to 3650."},
	"door_rate_limit_per_minute": {"Door defaults", "Code checks a minute",
		"How many codes a minute the server checks from the door once its burst is spent; an entry over the limit is denied without being looked at and logged as rate limited. A whole number of checks from 1 to 6000."},
	"door_rate_limit_burst": {"Door defaults", "Code checks in a burst",
		"How many codes the server checks from the door in quick succession before the limit a minute applies. A whole number of checks from 1 to 1000."},
	"door_ntp_server": {"Door defaults", "Time server",
		"The time server the door sets its clock from; the door keeps to its timeline by this clock. A host name of up to 253 letters, digits, dots, and hyphens."},
	"door_broker_host": {"Door defaults", "Broker host",
		"The broker the door connects to, from its next connection on; empty keeps the broker it uses now. A host name of up to 253 letters, digits, dots, and hyphens, an IP address, or nothing to keep the door's broker."},
	"door_broker_port": {"Door defaults", "Broker port",
		"The port of the broker the door connects to, from its next connection on. A port from 1 to 65535."},
	"door_broker_tls": {"Door defaults", "Broker over TLS",
		"Whether the door connects to the broker over TLS, checking the broker against the broker CA certificate. Turn it off for a development broker only. True or false."},
	"door_broker_ca_pem": {"Door defaults", "Broker CA certificate",
		"The certificate the door checks the broker against while it connects over TLS: the broker's CA's, or the broker's own. Empty keeps the one the door has. The PEM certificate of the broker's CA or of the broker itself, or nothing."},
	"door_wifi_ssid": {"Door defaults", "Wi-Fi network",
		"The Wi-Fi network the door joins; empty keeps the network it uses now. A door that cannot join a new network goes back to the last one that worked. A network name of up to 32 bytes, or nothing to keep the door's network."},
	"door_wifi_password": {"Door defaults", "Wi-Fi password",
		"The Wi-Fi network's password. It is stored encrypted and sent to nobody but the door. A password of up to 63 bytes."},
	"door_wifi_roam_rssi": {"Door defaults", "Roam below",
		"When the signal from its access point is weaker than this, the door moves to an access point of the same network at least 8 dB stronger, if it finds one. A whole number of dBm from -100 to 0."},
	"door_wifi_11b_disabled": {"Door defaults", "No 802.11b rates",
		"Whether the door keeps off the slow 802.11b rates, 1 to 11 Mbps, instead of falling back to them when packets are lost. It helps a door with a strong signal that still loses many packets on a crowded 2.4 GHz network, where the slow rates make things worse, and hurts a door with a weak signal at the edge of coverage, which needs those rates to stay connected. Leave it off unless the door's telemetry shows a good signal together with many TCP retransmits. Changing it makes the door restart its Wi-Fi, dropping off the network for a moment; a door in the mesh restarts. True or false."},
	"door_keepalive_s": {"Door defaults", "Keep-alive",
		"How often the door lets the broker know it is still there, from its next connection on; it shows offline once one and a half of these pass without a word from it. A whole number of seconds from 10 to 300."},
	"door_reconnect_max_s": {"Door defaults", "Longest reconnect pause",
		"The longest the door waits between attempts to connect again; it waits 1, 2, 4 seconds and so on up to this. A whole number of seconds from 5 to 600."},
	"door_mesh_enabled": {"Door defaults", "Mesh",
		"Whether the door may join or form a Wi-Fi mesh with other doors when its own access point's signal is poor. True or false."},
	"door_mesh_ssid": {"Door defaults", "Mesh network",
		"The mesh network's name, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open. A network name of up to 32 bytes, or nothing."},
	"door_mesh_password": {"Door defaults", "Mesh password",
		"The mesh network's password, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open. It is stored encrypted and sent to nobody but the doors. A password of 8 to 63 bytes, or nothing."},
	"door_mesh_min_router_rssi": {"Door defaults", "Mesh below",
		"When the strongest access point a door with the mesh on can see is weaker than this, the door joins the mesh through a neighboring door instead. A whole number of dBm from -100 to 0."},
	"door_mesh_max_layer": {"Door defaults", "Deepest mesh hop",
		"How many hops from the access point a door may sit in the mesh. A whole number of hops from 1 to 10."},
	"door_update_connect_timeout_s": {"Door defaults", "Revert new firmware after",
		"How long a door on newly installed firmware tries to connect before it goes back to its previous firmware. Keep it within the update timeout, after which the server counts the update failed. A whole number of seconds from 60 to 1800."},
	"door_accept_unsigned_firmware": {"Door defaults", "Unsigned firmware",
		"Whether the door installs firmware that has no valid signature. Leave it off except on bench and development boards. True or false."},
	"door_log_level": {"Door defaults", "Log level",
		"How much the door writes to its own log, from errors only to every detail. One of error, warn, info, debug."},
}

// doorGroups is each group of door configuration fields that the settings
// page and a door's page show under a heading, in order, with the fields
// it holds, in order.
var doorGroups = []struct {
	name   string
	fields []string
}{
	{"Keypad and strike", []string{"strike_pulse_ms", "held_open_s", "keypad_idle_clear_ms", "min_code_length", "max_code_length", "reply_timeout_ms"}},
	{"Server", []string{"telemetry_interval_s", "timeline_refresh_hours", "credential_rotation_days", "rate_limit_per_minute", "rate_limit_burst",
		"log_level", "ntp_server"}},
	{"Broker", []string{"broker_host", "broker_port", "broker_tls", "broker_ca_pem", "keepalive_s", "reconnect_max_s"}},
	{"Wi-Fi", []string{"wifi_ssid", "wifi_password", "wifi_roam_rssi", "wifi_11b_disabled"}},
	{"Mesh", []string{"mesh_enabled", "mesh_ssid", "mesh_password", "mesh_min_router_rssi", "mesh_max_layer"}},
	{"Firmware", []string{"update_connect_timeout_s", "accept_unsigned_firmware"}},
}

// groupStarted is the heading row a page shows before the setting key,
// the default of a door field, when the field is the first of its group,
// and whether it is.
func groupStarted(key string) ([]string, bool) {
	for _, g := range doorGroups {
		if "door_"+g.fields[0] == key {
			return []string{g.name}, true
		}
	}
	return nil, false
}

// secretSettings are the settings the settings page offers as password
// fields, which are left blank.
var secretSettings = map[string]bool{"oidc_client_secret": true, "enrollment_password": true, "door_wifi_password": true, "door_mesh_password": true}

// settingChoices is what the settings page offers to choose from for each
// setting it asks for with a select: the zones the stub lists for the
// timezone, which is searchable.
var settingChoices = map[string][]string{
	"site_timezone":  {"America/New_York", "Asia/Tokyo", "Europe/Berlin", "UTC"},
	"door_log_level": {"error", "warn", "info", "debug"},
}

// yesNoSettings are the settings the settings page asks for with a
// checkbox.
var yesNoSettings = map[string]bool{
	"oidc_enabled": true, "oidc_auto_sign_in": true, "portal_door_pictures": true,
	"door_broker_tls": true, "door_wifi_11b_disabled": true, "door_mesh_enabled": true, "door_accept_unsigned_firmware": true,
}

// settingCell adds what page reads from the cell showing the setting s
// whose input holds value, and is the cell's text: a read-only setting's
// value and the note naming its variable, a secret's offer to clear it
// when it has a value, or what its input shows.
func settingCell(page *testsupport.Page, s shownSetting, value string) string {
	switch {
	case s.source == "environment, read-only":
		note := "Set by DOORCODE_" + testsupport.Upper(s.key) + " in the environment; change it there."
		page.Notes = append(page.Notes, note)
		return s.value + " " + note
	case secretSettings[s.key] && s.value != "":
		// A secret's input is a password field, whose value is not read.
		return "Clear"
	case secretSettings[s.key]:
		return ""
	}
	return settingInput(page, s.key, value)
}

// settingInput adds what page reads from the input that holds value for
// the setting key, and is the text the input shows.
func settingInput(page *testsupport.Page, key, value string) string {
	switch {
	case yesNoSettings[key]:
		// The hidden no after the checkbox is what an unticked box sends.
		page.Values[key] = "false"
		if value == "true" {
			page.Checked = append(page.Checked, key+"=true")
		}
		return "On"
	case key == "theme_accent_dark":
		// Its color picker starts at the accent color while it is empty,
		// and the box beside it is ticked.
		page.Values[key] = value
		if value == "" {
			page.Values[key] = "#4f39f6"
			page.Checked = append(page.Checked, "clear="+key)
		}
		return "Use the accent color"
	case settingChoices[key] != nil:
		return optionsText(settingChoices[key], value)
	}
	page.Values[key] = value
	return ""
}

// optionsText is the text of a select offering choices with value chosen,
// which comes first when it is not among them.
func optionsText(choices []string, value string) string {
	text, listed := "", false
	for _, c := range choices {
		text = words(text, c)
		listed = listed || c == value
	}
	if !listed {
		return words(value, text)
	}
	return text
}

// words is a and b joined by a space, or the one that is not empty.
func words(a, b string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + " " + b
}

// settingWarning adds to page the warning the setting key shows while the
// inputs hold held, and is its text: the broker's certificate warns while
// it is missing though controllers connect over TLS, and the broker's host
// while it is empty.
func settingWarning(page *testsupport.Page, held map[string]string, key string) string {
	warning := ""
	switch {
	case key == "door_broker_host" && held[key] == "":
		warning = hostNote
	case key == "door_broker_ca_pem" && held["door_broker_tls"] == "true" && held[key] == "":
		warning = brokerNote
	default:
		return ""
	}
	page.Notes = append(page.Notes, warning)
	return warning
}

// settingsTabs is each tab of the settings page, in order.
var settingsTabs = []string{"Site setup", "Sign-in", "Look and feel", "Doors"}

// settingsTabOf is the tab of the settings page that shows each section.
var settingsTabOf = map[string]string{
	"Site": "Site setup", "Proxies": "Site setup",
	"Accounts and passwords": "Sign-in", "Single sign-on": "Sign-in", "Identity claims": "Sign-in",
	"Theme": "Look and feel", "User portal": "Look and feel",
	"Door enrollment": "Doors", "Telemetry": "Doors", "Firmware updates": "Doors", "Door defaults": "Doors",
}

// settingsTab is the tab of the settings page before its settings: the
// note, the main navigation and then the tabs, current marked, the
// searchable timezone on Site setup, and the upload of the sign-in page's
// picture on Look and feel.
func settingsTab(current string) testsupport.Page {
	page := testsupport.Page{
		Title: "Settings · doorcode", Heading: "Settings", Notes: []string{settingsNote}, Values: map[string]string{},
		Nav:    mainNav("Site: Settings", ""),
		Tables: map[string][][]string{},
	}
	for _, tab := range settingsTabs {
		if tab == current {
			tab += " (current)"
		}
		page.Nav = append(page.Nav, tab)
	}
	switch current {
	case "Site setup":
		page.Comboboxes = []string{"site_timezone"}
	case "Look and feel":
		page.Values["sign-in-image"] = ""
		page.Values["logo"] = ""
		page.Values["favicon"] = ""
	}
	return page
}

// settingsPage is the tab of the settings page showing the settings of
// shown on it, where changed replaces the value and source of the
// settings it names, entered the value an input holds, and refused what
// is wrong with a value; a read-only setting shows its value in place of
// an input, and a secret that has a value offers to clear it. The
// sections of single sign-on are open while it is enabled, and the
// broker's certificate warns while it is missing though controllers
// connect over TLS.
func settingsPage(tab string, shown []shownSetting, changed map[string]shownSetting, entered, refused map[string]string) testsupport.Page {
	page := settingsTab(tab)
	held := map[string]string{}
	for _, s := range shown {
		if c, ok := changed[s.key]; ok {
			s = c
		}
		row := settingRows[s.key]
		value := s.value
		if entered[s.key] != "" || refused[s.key] != "" {
			value = entered[s.key]
		}
		held[s.key] = value
		if settingsTabOf[row.section] != tab {
			continue
		}
		cell := words(words(settingCell(&page, s, value), refused[s.key]), settingWarning(&page, held, s.key))
		if s.key == "oidc_enabled" {
			page.Disclosures = map[string]bool{"Single sign-on": value == "true", "Identity claims": value == "true"}
		}
		setting := row.label + " " + s.key
		if row.help != "" {
			setting += " " + row.help
		}
		page.Tables[row.section] = withSettingRow(page.Tables[row.section], s.key, []string{setting, cell, s.source})
	}
	if refused != nil {
		page.FieldErrors = refused
	}
	return page
}

// withSettingRow is table, a section's table on the settings page, with
// the row showing the setting key after it: the table's head first when
// it has none, and the heading of the group key starts before the row.
func withSettingRow(table [][]string, key string, row []string) [][]string {
	if table == nil {
		table = [][]string{{"Setting", "Value", "Source"}}
	}
	if heading, starts := groupStarted(key); starts {
		table = append(table, heading)
	}
	return append(table, row)
}

func TestEachTabOfTheSettingsPageShowsOnlyItsSections(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ path, tab string }{
		"no tab":         {"/admin/settings", "Site setup"},
		"an unknown tab": {"/admin/settings?tab=billing", "Site setup"},
		"site":           {"/admin/settings?tab=site", "Site setup"},
		"sign-in":        {"/admin/settings?tab=sign-in", "Sign-in"},
		"look":           {"/admin/settings?tab=look", "Look and feel"},
		"doors":          {"/admin/settings?tab=doors", "Doors"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			f.StoreStub.SeedSetting("site_name", "North Campus", "2026-01-02T08:00:00Z")
			b := signedInAdmin(t, f)
			b.Get("/")

			got := b.Get(c.path)

			want := settingsPage(c.tab, defaultSettings, map[string]shownSetting{"site_name": {"site_name", "North Campus", "database"}}, nil, nil)
			want.Title = "Settings · North Campus"
			if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
				t.Errorf("status, page (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheSettingsPageTabsAreLinksToTheirAddresses(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	nav := testsupport.ParsePage(b.Get("/admin/settings?tab=doors").Body).NavTargets

	want := []string{"/admin/settings?tab=site", "/admin/settings?tab=sign-in", "/admin/settings?tab=look", "/admin/settings?tab=doors"}
	if diff := cmp.Diff(want, nav[len(nav)-len(want):]); diff != "" {
		t.Errorf("tab addresses (-want +got):\n%s", diff)
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
		Nav:     []string{"Portal", "API tokens"},
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
		"sign-in page without settings": {id: "565758595a5b5c5d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.Get(h, "/sign-in")
		}},
		"favicon without settings": {id: "565758595a5b5c5d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.Get(h, "/favicon")
		}},
		"sign-in without users": {id: "565758595a5b5c5d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailUsers()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"sign-in of an unknown email without settings": {id: "565758595a5b5c5d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSettings()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", "nobody@example.com", "password", "x")
		}},
		"sign-in without sessions": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			h := start(t, f).Handler()
			f.StoreStub.FailSessions()
			return testsupport.NewBrowser(h).Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
		}},
		"home without sessions": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSessions()
			return b.Get("/")
		}},
		"settings page without settings": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailSettings()
			return b.Get("/admin/settings")
		}},
		"tokens page without tokens": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Get("/account/tokens")
		}},
		"token creation without tokens, after drawing the secret": {id: "969798999a9b9c9d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "ci", "expires_in_days", "30")
		}},
		"rejected token form without tokens": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens", "name", "", "expires_in_days", "30")
		}},
		"token revocation without tokens": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
			b := signedInAdmin(t, f)
			f.StoreStub.FailTokens()
			return b.Post("/account/tokens/1/revoke")
		}},
		"sign-out without sessions": {id: "767778797a7b7c7d", request: func(t *testing.T, f *testsupport.Factory) testsupport.Response {
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
				Fields: map[string]string{"request_id": c.id, "error": "stub: unavailable", "client": testsupport.ClientAddress}}
			if diff := cmp.Diff(wantLog, lastLog(f)); diff != "" {
				t.Errorf("last log line (-want +got):\n%s", diff)
			}
		})
	}
}
