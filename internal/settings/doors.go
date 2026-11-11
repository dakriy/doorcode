package settings

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
)

// doorPrefix starts the key of every setting that is a door configuration
// default; the rest of the key is the field's name.
const doorPrefix = "door_"

// brokerHint is what a broker host must be: empty tells the door to keep
// the broker it connects to.
var brokerHint = fmt.Sprintf("%s, an IP address, or nothing to keep the door's broker", check.HostHint)

// caHint is what the broker's CA certificate must be.
const caHint = "the PEM certificate of the broker's CA or of the broker itself, or nothing"

// ConnectTimeoutField names the field of how long a controller on new
// firmware tries to connect before it goes back to its old image, and
// KeyConnectTimeout is the key of its default.
const (
	ConnectTimeoutField = "update_connect_timeout_s"
	KeyConnectTimeout   = "door_update_connect_timeout_s"
)

// BrokerHostField and BrokerCAField name the fields of the broker a
// controller connects to and the certificate it verifies the broker
// against, and KeyBrokerHost, KeyBrokerCA, and KeyBrokerTLS are the keys of
// the defaults of them and of whether controllers connect over TLS.
const (
	BrokerHostField = "broker_host"
	BrokerCAField   = "broker_ca_pem"
	KeyBrokerHost   = "door_broker_host"
	KeyBrokerCA     = "door_broker_ca_pem"
	KeyBrokerTLS    = "door_broker_tls"
)

// NoBrokerHost is what pages warn of, and why no provisioning file is
// made, while controllers are given no broker to connect to.
const NoBrokerHost = "door_broker_host is empty: a controller needs an address to reach the broker, so no provisioning file can be made."

// UnverifiedBroker is what pages warn of, and why no provisioning file is
// made, while controllers connect to the broker over TLS with no
// certificate to verify it against.
const UnverifiedBroker = "Controllers connect to the broker over TLS, but door_broker_ca_pem gives no CA certificate: a controller could not verify the broker, so no provisioning file can be made."

// MeshEnabledField names the field of whether a door may join or form a
// mesh, and KeyMeshEnabled, KeyMeshSSID, and KeyMeshPassword are the keys
// of the defaults of it and of the mesh's network name and password.
const (
	MeshEnabledField  = "mesh_enabled"
	meshSSIDField     = "mesh_ssid"
	meshPasswordField = "mesh_password"
	KeyMeshEnabled    = "door_mesh_enabled"
	KeyMeshSSID       = "door_mesh_ssid"
	KeyMeshPassword   = "door_mesh_password"
)

// meshFields is the fields a mesh that is on needs set: whether it is on,
// its network name, and its password.
var meshFields = []string{MeshEnabledField, meshSSIDField, meshPasswordField}

// minMeshPassword is the fewest characters a mesh password has; Mask is
// as long, so a masked password counts as one.
const minMeshPassword = 8

// Why a mesh that is on is left open.
const (
	noMeshSSID        = "with the mesh on, no network name leaves the mesh soft-AP open"
	shortMeshPassword = "with the mesh on, a password of fewer than 8 characters, or none, leaves the mesh soft-AP open"
)

// meshProblems is what leaves a mesh open while enabled is true, with ssid
// as its network name and password as its password, each as a problem
// with the field named prefix and the field's name: no network name, and
// a password shorter than minMeshPassword. It is empty while the mesh is
// off.
func meshProblems(enabled bool, ssid, password, prefix string) check.List {
	if !enabled {
		return nil
	}
	var found check.List
	found.Add(prefix+meshSSIDField, check.Required(ssid, noMeshSSID))
	if len(password) < minMeshPassword {
		found.Add(prefix+meshPasswordField, shortMeshPassword)
	}
	return found
}

// asSentences is found with each message a sentence, as a field error.
func asSentences(found check.List) check.List {
	var out check.List
	for _, p := range found {
		out.Add(p.Field, sentence(p.Message))
	}
	return out
}

// MeshWarning is what a page warns of controllers whose mesh is on when
// enabled, the value of a mesh setting, is true, with ssid as its network
// name and password, perhaps masked, as its password: that they are sent
// the mesh off, and why; empty when the mesh is off or set.
func MeshWarning(enabled, ssid, password string) string {
	return meshWarning(meshProblems(enabled == Yes, ssid, password, ""))
}

// meshProblemsOf is meshProblems of the mesh whose fields have the
// values value gives by field name, a yes-no's as a setting holds it.
func meshProblemsOf(value func(name string) string, prefix string) check.List {
	return meshProblems(value(MeshEnabledField) == Yes, value(meshSSIDField), value(meshPasswordField), prefix)
}

// meshWarning is what a page warns of a mesh that found leaves open: that
// controllers are sent it off, and why; empty when found is.
func meshWarning(found check.List) string {
	if len(found) == 0 {
		return ""
	}
	return "Controllers are sent mesh_enabled false: " + found[0].Message + "."
}

// DoorConfig is the configuration a door runs with. Durations are whole
// numbers of the unit their name ends in.
type DoorConfig struct {
	StrikePulseMS          int
	KeypadIdleClearMS      int
	MinCodeLength          int
	MaxCodeLength          int
	ReplyTimeoutMS         int
	TelemetryIntervalS     int
	HeldOpenS              int
	TimelineRefreshHours   int
	CredentialRotationDays int
	RateLimitPerMinute     int
	RateLimitBurst         int
	NTPServer              string
	BrokerHost             string
	BrokerPort             int
	BrokerTLS              bool
	BrokerCAPEM            string
	WifiSSID               string
	WifiPassword           string
	WifiRoamRSSI           int
	Wifi11bDisabled        bool
	KeepaliveS             int
	ReconnectMaxS          int
	MeshEnabled            bool
	MeshSSID               string
	MeshPassword           string
	MeshMinRouterRSSI      int
	MeshMaxLayer           int
	UpdateConnectTimeoutS  int
	AcceptUnsignedFirmware bool
	LogLevel               string
}

// Masked is c as the API shows it: each secret masked when it has a
// value, as DoorField.Secret marks them.
func (c DoorConfig) Masked() DoorConfig {
	c.WifiPassword = masked(c.WifiPassword)
	c.MeshPassword = masked(c.MeshPassword)
	return c
}

// masked is a secret's value as it is shown: Mask when it has one.
func masked(value string) string {
	if value == "" {
		return ""
	}
	return Mask
}

// Unprovisionable is why no provisioning file can be made with c, each
// what a page warns of: no broker host, and TLS with no certificate to
// verify the broker against; empty when one can.
func (c DoorConfig) Unprovisionable() []string {
	var problems []string
	for _, warning := range []string{HostWarning(c.BrokerHost), BrokerWarning(strconv.FormatBool(c.BrokerTLS), c.BrokerCAPEM)} {
		if warning != "" {
			problems = append(problems, warning)
		}
	}
	return problems
}

// RotateAfter is how long a door's credentials work before the server
// issues new ones.
func (c DoorConfig) RotateAfter() time.Duration {
	return time.Duration(c.CredentialRotationDays) * 24 * time.Hour
}

// HeldOpen is how long the door may stay open before it is held open.
func (c DoorConfig) HeldOpen() time.Duration {
	return time.Duration(c.HeldOpenS) * time.Second
}

// StrikeWindow is how long after the server lets someone in, or tells the
// door to open, the door may open without being forced: its strike pulse,
// and its reply timeout for the time the news takes to come and go.
func (c DoorConfig) StrikeWindow() time.Duration {
	return time.Duration(c.StrikePulseMS+c.ReplyTimeoutMS) * time.Millisecond
}

// TimelineRefresh is how often the server sends a door its timeline again
// when nothing changed it.
func (c DoorConfig) TimelineRefresh() time.Duration {
	return time.Duration(c.TimelineRefreshHours) * time.Hour
}

// DoorField is one field of a door's configuration. Its site default is
// the setting whose key is "door_" and its name; a door may override it.
// Help says what the field does, in a sentence or two, and Hint what
// values it takes, to follow "must be" or "Enter".
type DoorField struct {
	Name  string
	Label string
	Help  string
	Hint  string
	// Group is the name of the group of fields pages show it in.
	Group string
	// Input is how a page asks for the field's value.
	Input Input
	// Secret fields are stored encrypted, as defaults and as a door's
	// overrides, and shown masked; only the configuration a door is sent
	// carries them in the clear.
	Secret bool
	// fallback is the default's default, and set parses a value into a
	// configuration, reporting why it cannot.
	fallback string
	set      func(c *DoorConfig, value string) error
}

// DoorFields is every field of a door's configuration, in display order,
// group by group, each knowing its group. The ranges of the code lengths
// meet but do not overlap, so the shortest code a door takes is never
// longer than the longest.
var DoorFields = grouped(
	doorGroup{"Keypad and strike", []DoorField{
		wholeField("strike_pulse_ms", "Strike pulse",
			"How long the door's strike stays released each time it lets someone in. The door may open within this, plus the reply timeout, without counting as forced open.",
			"3000", 100, 60000, "milliseconds", func(c *DoorConfig) *int { return &c.StrikePulseMS }),
		wholeField("held_open_s", "Held open after",
			"How long a door may stand open, when it should be locked, before it counts as held open and an event is logged.",
			"60", 5, 86400, "seconds", func(c *DoorConfig) *int { return &c.HeldOpenS }),
		wholeField("keypad_idle_clear_ms", "Keypad idle clear",
			"How long a partly typed entry waits for the next key before the keypad discards it.",
			"10000", 1000, 600000, "milliseconds", func(c *DoorConfig) *int { return &c.KeypadIdleClearMS }),
		wholeField("min_code_length", "Shortest code",
			"The fewest digits a code may have; the door discards a shorter entry without asking the server.",
			"4", 1, 16, "digits", func(c *DoorConfig) *int { return &c.MinCodeLength }),
		wholeField("max_code_length", "Longest code",
			"The longest entry the keypad takes, a keypad command included; the door takes no more keys once an entry is this long.",
			"32", 16, 64, "digits", func(c *DoorConfig) *int { return &c.MaxCodeLength }),
		wholeField("reply_timeout_ms", "Reply timeout",
			"How long the door waits for the server's answer to a code before denying it.",
			"3000", 100, 60000, "milliseconds", func(c *DoorConfig) *int { return &c.ReplyTimeoutMS }),
	}},
	doorGroup{"Server", []DoorField{
		wholeField("telemetry_interval_s", "Telemetry interval",
			"How often the door reports its health readings, such as its signal and uptime, to the server.",
			"60", 5, 86400, "seconds", func(c *DoorConfig) *int { return &c.TelemetryIntervalS }),
		wholeField("timeline_refresh_hours", "Timeline refresh",
			"How often the server sends the door its timeline of when to stay open and locked, even when nothing changed it.",
			"6", 1, 24, "hours", func(c *DoorConfig) *int { return &c.TimelineRefreshHours }),
		wholeField("credential_rotation_days", "Credential rotation",
			"How many days the door's broker credentials work before the server issues it new ones.",
			"30", 1, 3650, "days", func(c *DoorConfig) *int { return &c.CredentialRotationDays }),
		wholeField("rate_limit_per_minute", "Code checks a minute",
			"How many codes a minute the server checks from the door once its burst is spent; an entry over the limit is denied without being looked at and logged as rate limited.",
			"30", 1, 6000, "checks", func(c *DoorConfig) *int { return &c.RateLimitPerMinute }),
		wholeField("rate_limit_burst", "Code checks in a burst",
			"How many codes the server checks from the door in quick succession before the limit a minute applies.",
			"10", 1, 1000, "checks", func(c *DoorConfig) *int { return &c.RateLimitBurst }),
		choiceField("log_level", "Log level", "How much the door writes to its own log, from errors only to every detail.",
			"info", []string{"error", "warn", "info", "debug"}, func(c *DoorConfig) *string { return &c.LogLevel }),
		{Name: "ntp_server", Label: "Time server", Help: "The time server the door sets its clock from; the door keeps to its timeline by this clock.",
			Hint: check.HostHint, fallback: "pool.ntp.org", set: setHost},
	}},
	doorGroup{"Broker", []DoorField{
		{Name: "broker_host", Label: "Broker host", Help: "The broker the door connects to, from its next connection on; empty keeps the broker it uses now.",
			Hint: brokerHint, fallback: "", set: setBrokerHost},
		numberField("broker_port", "Broker port", "The port of the broker the door connects to, from its next connection on.",
			"8883", "a port from 1 to 65535", 1, 65535, func(c *DoorConfig) *int { return &c.BrokerPort }),
		yesNoField("broker_tls", "Broker over TLS", "Whether the door connects to the broker over TLS, checking the broker against the broker CA certificate. Turn it off for a development broker only.",
			"true", func(c *DoorConfig) *bool { return &c.BrokerTLS }),
		{Name: "broker_ca_pem", Label: "Broker CA certificate",
			Help: "The certificate the door checks the broker against while it connects over TLS: the broker's CA's, or the broker's own. Empty keeps the one the door has.",
			Hint: caHint, Input: Input{Kind: KindLongText}, fallback: "", set: setBrokerCA},
		wholeField("keepalive_s", "Keep-alive",
			"How often the door lets the broker know it is still there, from its next connection on; it shows offline once one and a half of these pass without a word from it.",
			"30", 10, 300, "seconds", func(c *DoorConfig) *int { return &c.KeepaliveS }),
		wholeField("reconnect_max_s", "Longest reconnect pause",
			"The longest the door waits between attempts to connect again; it waits 1, 2, 4 seconds and so on up to this.",
			"60", 5, 600, "seconds", func(c *DoorConfig) *int { return &c.ReconnectMaxS }),
	}},
	doorGroup{"Wi-Fi", []DoorField{
		textField("wifi_ssid", "Wi-Fi network",
			"The Wi-Fi network the door joins; empty keeps the network it uses now. A door that cannot join a new network goes back to the last one that worked.",
			"a network name of up to 32 bytes, or nothing to keep the door's network", 0, 32, func(c *DoorConfig) *string { return &c.WifiSSID }),
		secret(textField("wifi_password", "Wi-Fi password", "The Wi-Fi network's password. It is stored encrypted and sent to nobody but the door.",
			"a password of up to 63 bytes", 0, 63, func(c *DoorConfig) *string { return &c.WifiPassword })),
		signalField("wifi_roam_rssi", "Roam below",
			"When the signal from its access point is weaker than this, the door moves to an access point of the same network at least 8 dB stronger, if it finds one.",
			"-70", 100, func(c *DoorConfig) *int { return &c.WifiRoamRSSI }),
		yesNoField("wifi_11b_disabled", "No 802.11b rates",
			"Whether the door keeps off the slow 802.11b rates, 1 to 11 Mbps, instead of falling back to them when packets are lost. It helps a door with a strong signal that still loses many packets on a crowded 2.4 GHz network, where the slow rates make things worse, and hurts a door with a weak signal at the edge of coverage, which needs those rates to stay connected. Leave it off unless the door's telemetry shows a good signal together with many TCP retransmits. Changing it makes the door restart its Wi-Fi, dropping off the network for a moment; a door in the mesh restarts.",
			"false", func(c *DoorConfig) *bool { return &c.Wifi11bDisabled }),
	}},
	doorGroup{"Mesh", []DoorField{
		yesNoField("mesh_enabled", "Mesh", "Whether the door may join or form a Wi-Fi mesh with other doors when its own access point's signal is poor.",
			"false", func(c *DoorConfig) *bool { return &c.MeshEnabled }),
		textField("mesh_ssid", "Mesh network", "The mesh network's name, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open.",
			"a network name of up to 32 bytes, or nothing", 0, 32, func(c *DoorConfig) *string { return &c.MeshSSID }),
		secret(textField("mesh_password", "Mesh password",
			"The mesh network's password, the same at every door of the site. It is needed while the mesh is on, which would otherwise leave the mesh's soft-AP open. It is stored encrypted and sent to nobody but the doors.",
			"a password of 8 to 63 bytes, or nothing", 8, 63, func(c *DoorConfig) *string { return &c.MeshPassword })),
		signalField("mesh_min_router_rssi", "Mesh below",
			"When the strongest access point a door with the mesh on can see is weaker than this, the door joins the mesh through a neighboring door instead.",
			"-75", 100, func(c *DoorConfig) *int { return &c.MeshMinRouterRSSI }),
		wholeField("mesh_max_layer", "Deepest mesh hop", "How many hops from the access point a door may sit in the mesh.",
			"4", 1, 10, "hops", func(c *DoorConfig) *int { return &c.MeshMaxLayer }),
	}},
	doorGroup{"Firmware", []DoorField{
		wholeField(ConnectTimeoutField, "Revert new firmware after",
			"How long a door on newly installed firmware tries to connect before it goes back to its previous firmware. Keep it within the update timeout, after which the server counts the update failed.",
			"300", 60, 1800, "seconds", func(c *DoorConfig) *int { return &c.UpdateConnectTimeoutS }),
		yesNoField("accept_unsigned_firmware", "Unsigned firmware",
			"Whether the door installs firmware that has no valid signature. Leave it off except on bench and development boards.",
			"false", func(c *DoorConfig) *bool { return &c.AcceptUnsignedFirmware }),
	}},
)

// doorGroup is a group of door fields, shown together under its name.
type doorGroup struct {
	name   string
	fields []DoorField
}

// grouped is the fields of groups, in order, each given its group's name.
func grouped(groups ...doorGroup) []DoorField {
	var out []DoorField
	for _, g := range groups {
		for _, f := range g.fields {
			f.Group = g.name
			out = append(out, f)
		}
	}
	return out
}

// wholeField is a field, explained by help, that takes a whole number of
// unit from lowest to highest, kept where at says.
func wholeField(name, label, help, fallback string, lowest, highest int, unit string, at func(*DoorConfig) *int) DoorField {
	hint := fmt.Sprintf("a whole number of %s from %d to %d", unit, lowest, highest)
	return numberField(name, label, help, fallback, hint, lowest, highest, at)
}

// signalField is a field, explained by help, that takes a signal strength
// in whole dBm from minus weakest to 0, kept where at says.
func signalField(name, label, help, fallback string, weakest int, at func(*DoorConfig) *int) DoorField {
	return wholeField(name, label, help, fallback, -weakest, 0, "dBm", at)
}

// numberField is a field, explained by help, that takes a whole number
// from lowest to highest, which hint says, kept where at says.
func numberField(name, label, help, fallback, hint string, lowest, highest int, at func(*DoorConfig) *int) DoorField {
	return DoorField{Name: name, Label: label, Help: help, Hint: hint, Input: Input{Kind: KindNumber}, fallback: fallback, set: func(c *DoorConfig, value string) error {
		n, err := wholeNumber(value, lowest, highest, "must be "+hint)
		*at(c) = n
		return err
	}}
}

// yesNoField is a field, explained by help, that takes true or false,
// kept where at says.
func yesNoField(name, label, help, fallback string, at func(*DoorConfig) *bool) DoorField {
	return DoorField{Name: name, Label: label, Help: help, Hint: "true or false", Input: Input{Kind: KindYesNo}, fallback: fallback, set: func(c *DoorConfig, value string) error {
		yes, err := boolean(value)
		*at(c) = yes
		return err
	}}
}

// textField is a field, explained by help, that takes nothing or text of
// shortest to longest bytes, which hint says, kept where at says.
func textField(name, label, help, hint string, shortest, longest int, at func(*DoorConfig) *string) DoorField {
	return DoorField{Name: name, Label: label, Help: help, Hint: hint, fallback: "", set: func(c *DoorConfig, value string) error {
		*at(c) = value
		if value != "" && (len(value) < shortest || len(value) > longest) {
			return errors.New("must be " + hint)
		}
		return nil
	}}
}

// secret is f stored encrypted and shown masked.
func secret(f DoorField) DoorField {
	f.Secret = true
	return f
}

// choiceField is a field, explained by help, that takes one of choices,
// kept where at says.
func choiceField(name, label, help, fallback string, choices []string, at func(*DoorConfig) *string) DoorField {
	hint := "one of " + strings.Join(choices, ", ")
	input := Input{Kind: KindChoice, Choices: choices}
	return DoorField{Name: name, Label: label, Help: help, Hint: hint, Input: input, fallback: fallback, set: func(c *DoorConfig, value string) error {
		*at(c) = value
		if !slices.Contains(choices, value) {
			return errors.New("must be " + hint)
		}
		return nil
	}}
}

// setHost keeps value as the time server when it is a host name.
func setHost(c *DoorConfig, value string) error {
	c.NTPServer = value
	if !check.HostName(value) {
		return errors.New("must be " + check.HostHint)
	}
	return nil
}

// setBrokerHost keeps value as the broker the door connects to when it is
// empty, a host name, or an IP address.
func setBrokerHost(c *DoorConfig, value string) error {
	c.BrokerHost = value
	_, notAddress := netip.ParseAddr(value)
	if value != "" && !check.HostName(value) && notAddress != nil {
		return errors.New("must be " + brokerHint)
	}
	return nil
}

// setBrokerCA keeps value, with its line ends as a browser's CR LF made
// LF, as the certificate the door verifies the broker against, when it is
// empty or PEM certificates and nothing else.
func setBrokerCA(c *DoorConfig, value string) error {
	c.BrokerCAPEM = strings.ReplaceAll(value, "\r\n", "\n")
	if value != "" && !certificates(c.BrokerCAPEM) {
		return errors.New("must be " + caHint)
	}
	return nil
}

// certificates reports whether text is one or more PEM certificates that
// can be read, with nothing but white space beside them.
func certificates(text string) bool {
	block, rest := pem.Decode([]byte(text))
	if block == nil {
		return false
	}
	for block != nil {
		if _, err := x509.ParseCertificate(block.Bytes); block.Type != "CERTIFICATE" || err != nil {
			return false
		}
		block, rest = pem.Decode(rest)
	}
	return strings.TrimSpace(string(rest)) == ""
}

// doorDefinitions is the setting of each door field's default.
func doorDefinitions() []definition {
	var out []definition
	for _, f := range DoorFields {
		check := func(_ Service, value string) error {
			return f.set(&DoorConfig{}, value)
		}
		out = append(out, definition{key: doorPrefix + f.Name, fallback: f.fallback, validate: rule{check: check, input: f.Input},
			secret: f.Secret, label: f.Label, help: f.About(), group: f.Group})
	}
	return out
}

// About is what pages say of f under its label: what it does, then what
// values it takes.
func (f DoorField) About() string {
	return f.Help + " " + sentence(f.Hint)
}

// sentence is text as a sentence: its first letter upper case, ending in a
// full stop.
func sentence(text string) string {
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// doorField is the door field named name, and whether there is one.
func doorField(name string) (DoorField, bool) {
	i := slices.IndexFunc(DoorFields, func(f DoorField) bool { return f.Name == name })
	if i < 0 {
		return DoorField{}, false
	}
	return DoorFields[i], true
}

// Configures reports whether the setting named key is a door
// configuration default, so changing it changes the configuration of
// every door that does not override it.
func Configures(key string) bool {
	return slices.ContainsFunc(DoorFields, func(f DoorField) bool { return doorPrefix+f.Name == key })
}

// Reschedules reports whether the setting named key changes when the
// schedules of every door are, as the site timezone does.
func Reschedules(key string) bool {
	return key == keySiteTimezone
}

// DoorDefaults is the site's door configuration defaults, each value, a
// secret's in the clear, keyed by the name of its field, the update
// timeout a door's firmware revert is measured against, and the Secrets a
// door's secret overrides are decrypted with.
type DoorDefaults struct {
	values        map[string]string
	updateTimeout string
	secrets       Secrets
}

// DoorDefaults returns the site's door configuration defaults.
func (s Service) DoorDefaults(ctx context.Context) (DoorDefaults, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return DoorDefaults{}, err
	}
	values := map[string]string{}
	var errs []error
	for _, f := range DoorFields {
		def, _ := lookup(doorPrefix + f.Name)
		value := s.resolve(def, stored).Value
		if f.Secret {
			value, err = s.secret(def.key, stored)
			errs = append(errs, err)
		}
		values[f.Name] = value
	}
	timeout, _ := lookup(KeyUpdateTimeout)
	return DoorDefaults{values: values, updateTimeout: s.resolve(timeout, stored).Value, secrets: s.Secrets}, errors.Join(errs...)
}

// For is the configuration of a door with overrides, each keyed by the
// name of the field it overrides and a secret's encrypted as SealOverrides
// stores it: the override of each field that has one, the default of the
// others, with the mesh off when it is on but would be left open.
func (d DoorDefaults) For(overrides map[string]string) (DoorConfig, error) {
	var c DoorConfig
	for _, f := range DoorFields {
		value, err := d.value(overrides, f)
		if err == nil {
			err = f.set(&c, value)
		}
		if err != nil {
			return DoorConfig{}, fmt.Errorf("%w for %s: %w", ErrInvalid, doorPrefix+f.Name, err)
		}
	}
	if len(meshProblems(c.MeshEnabled, c.MeshSSID, c.MeshPassword, "")) > 0 {
		c.MeshEnabled = false
	}
	return c, nil
}

// value is the value of f for a door with overrides, in the clear: its
// override when it has one, the default otherwise.
func (d DoorDefaults) value(overrides map[string]string, f DoorField) (string, error) {
	value, overridden := overrides[f.Name]
	if !overridden {
		return d.values[f.Name], nil
	}
	if f.Secret && value != "" {
		return d.secrets.open(doorPrefix+f.Name, value)
	}
	return value, nil
}

// Default is the site's default of f as pages show it: a secret masked.
func (d DoorDefaults) Default(f DoorField) string {
	return f.shown(d.values[f.Name])
}

// Shown is the value of f for a door with overrides as pages show it: its
// override when it has one, the default otherwise, a secret masked.
func (d DoorDefaults) Shown(overrides map[string]string, f DoorField) string {
	if value, overridden := overrides[f.Name]; overridden {
		return f.shown(value)
	}
	return d.Default(f)
}

// RevertWarning is what a page warns of a door with overrides whose
// controller goes back to its old firmware later than the server gives
// up on an update; empty when it does not.
func (d DoorDefaults) RevertWarning(overrides map[string]string) string {
	f, _ := doorField(ConnectTimeoutField)
	return RevertWarning(d.Shown(overrides, f), d.updateTimeout)
}

// RevertWarning is what a page warns of a controller that tries to
// connect on new firmware for connectTimeout seconds before it goes back
// to its old image, when the server counts an update failed after
// updateTimeout minutes, the values of the two settings: empty unless the
// controller outlasts the server, or when the update timeout is not a
// number. A connect timeout that is not one reads as none, which outlasts
// nothing.
func RevertWarning(connectTimeout, updateTimeout string) string {
	seconds, _ := strconv.Atoi(connectTimeout)
	minutes, minutesErr := strconv.Atoi(updateTimeout)
	if minutesErr != nil || seconds <= minutes*60 {
		return ""
	}
	return fmt.Sprintf("Longer than the update timeout of %d minutes (%d seconds): the server counts an update failed before "+
		"a controller that cannot connect on its new firmware goes back to the old one.", minutes, minutes*60)
}

// HostWarning is what a page warns of a door with overrides that would be
// given no broker to connect to; empty when it has one.
func (d DoorDefaults) HostWarning(overrides map[string]string) string {
	host, _ := doorField(BrokerHostField)
	return HostWarning(d.Shown(overrides, host))
}

// HostWarning is what a page warns of controllers given host as the
// broker to connect to: NoBrokerHost while it is empty, empty otherwise.
func HostWarning(host string) string {
	if host != "" {
		return ""
	}
	return NoBrokerHost
}

// MeshWarning is what a page warns of a door with overrides whose mesh is
// on but would be left open; empty when it is off or set.
func (d DoorDefaults) MeshWarning(overrides map[string]string) string {
	return meshWarning(d.meshProblems(overrides))
}

// meshProblems is what leaves the mesh of a door with overrides, sealed as
// SealOverrides makes them, open, each a problem with the field's name.
// A secret is masked, which is as long as a password must be: a stored
// password is never shorter.
func (d DoorDefaults) meshProblems(overrides map[string]string) check.List {
	return meshProblemsOf(func(name string) string {
		f, _ := doorField(name)
		return d.Shown(overrides, f)
	}, "")
}

// siteMeshProblems is what leaves the site's mesh open with changed,
// values keyed by setting key, in place of held, every setting's value by
// key with a secret's masked, each a problem with the key; empty when
// changed sets none of the mesh's fields.
func siteMeshProblems(held, changed map[string]string) check.List {
	touched := slices.ContainsFunc(meshFields, func(name string) bool {
		_, has := changed[doorPrefix+name]
		return has
	})
	if !touched {
		return nil
	}
	return meshProblemsOf(func(name string) string {
		if value, has := changed[doorPrefix+name]; has {
			return value
		}
		return held[doorPrefix+name]
	}, doorPrefix)
}

// BrokerWarning is what a page warns of a door with overrides that would
// connect over TLS with no certificate to verify the broker against; empty
// when it would not.
func (d DoorDefaults) BrokerWarning(overrides map[string]string) string {
	tls, _ := doorField("broker_tls")
	ca, _ := doorField(BrokerCAField)
	return BrokerWarning(d.Shown(overrides, tls), d.Shown(overrides, ca))
}

// BrokerWarning is what a page warns of controllers that connect to the
// broker over TLS when tls, the value of a broker TLS setting, is true,
// with ca as the certificate they verify it against: UnverifiedBroker
// while ca is empty and tls true, empty otherwise.
func BrokerWarning(tls, ca string) string {
	if tls != Yes || ca != "" {
		return ""
	}
	return UnverifiedBroker
}

// shown is value as pages show it for f: masked when f is secret.
func (f DoorField) shown(value string) string {
	if f.Secret {
		return masked(value)
	}
	return value
}

// ShownOverrides is a door's overrides as the API shows them: a secret
// masked.
func ShownOverrides(overrides map[string]string) map[string]string {
	shown := map[string]string{}
	for name, value := range overrides {
		f, _ := doorField(name)
		shown[name] = f.shown(value)
	}
	return shown
}

// SealOverrides is given, a door's new configuration overrides, each keyed
// by the name of the field it overrides, as the store keeps them, a
// secret's encrypted; a secret given as Mask keeps its value among stored,
// the door's overrides now. It reports what is wrong with given: a name no
// field has, a value the field does not take, Mask for a secret the door
// has none of, or a mesh that is on, in the configuration given makes with
// the site's defaults, without a network name or password.
func (s Service) SealOverrides(ctx context.Context, given, stored map[string]string) (map[string]string, error) {
	var found check.List
	for _, name := range slices.Sorted(maps.Keys(given)) {
		found.Add(name, overrideProblem(name, given[name], stored))
	}
	if err := found.Err(); err != nil {
		return nil, err
	}
	sealed := map[string]string{}
	for name, value := range given {
		f, _ := doorField(name)
		kept := f.Secret && value == Mask
		hidden := f.Secret && value != ""
		switch {
		case kept:
			sealed[name] = stored[name]
		case hidden:
			sealed[name] = s.Secrets.seal(doorPrefix+name, value)
		default:
			sealed[name] = value
		}
	}
	defaults, err := s.DoorDefaults(ctx)
	if err != nil {
		return nil, err
	}
	if err := asSentences(defaults.meshProblems(sealed)).Err(); err != nil {
		return nil, err
	}
	return sealed, nil
}

func overrideProblem(name, value string, stored map[string]string) string {
	f, ok := doorField(name)
	if !ok {
		return "No door setting has this name."
	}
	if _, has := stored[name]; f.Secret && value == Mask {
		if !has {
			return "Enter a value; " + Mask + " keeps only a value the door has."
		}
		return ""
	}
	if f.set(&DoorConfig{}, value) != nil {
		return "Enter " + f.Hint + "."
	}
	return ""
}
