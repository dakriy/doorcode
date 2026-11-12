// Package protocol is the MQTT protocol between the server and the door
// controllers: the topics, and the messages they carry encoded as CBOR. It
// is the only package that encodes or decodes CBOR, so every message passes
// through it. Every message is a CBOR map whose "v" entry is the protocol
// version.
package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fxamacker/cbor/v2"
)

// Version is the protocol version this server speaks.
const Version = 1

// prefix starts every topic.
const prefix = "doorcode/"

// The topic filters the server subscribes to, and the enrollment client
// may use. They are written out in full, since a mutation tester cannot
// see a constant expression run.
const (
	// EnrollFilter matches the announcements of enrolling controllers.
	EnrollFilter = "doorcode/enroll/+"
	// OnlineFilter matches the online status of every door.
	OnlineFilter = "doorcode/doors/+/online"
	// CheckFilter matches the code checks of every door.
	CheckFilter = "doorcode/doors/+/check"
	// TelemetryFilter matches the telemetry of every door.
	TelemetryFilter = "doorcode/doors/+/telemetry"
	// EventFilter matches what every door says happened to it.
	EventFilter = "doorcode/doors/+/event"
	// AckFilter matches what every door answers the commands it was
	// sent.
	AckFilter = "doorcode/doors/+/ack"
	// EnrollOwnFilter is the one topic an enrolling controller may
	// announce itself on: the broker puts the controller's client id,
	// which must be its hardware id, in place of %c.
	EnrollOwnFilter = "doorcode/enroll/%c"
	// CredentialsOwnFilter is the one topic an enrolling controller may
	// receive credentials on, read the same way.
	CredentialsOwnFilter = "doorcode/enroll/%c/credentials"
)

// The leaves of a door's topics.
const (
	// LeafCheck ends the topic a door asks the server to check a code on.
	LeafCheck = "check"
	// LeafReply ends the topic a door receives the server's answers to
	// its code checks on.
	LeafReply = "reply"
	// LeafCommand ends the topic a door receives commands on.
	LeafCommand = "command"
	// LeafConfig ends the topic a door receives its configuration on.
	LeafConfig = "config"
	// LeafTimeline ends the topic a door receives its timeline on.
	LeafTimeline = "timeline"
	// LeafTelemetry ends the topic a door reports its telemetry on.
	LeafTelemetry = "telemetry"
	// LeafEvent ends the topic a door says what happened to it on.
	LeafEvent = "event"
	// LeafAck ends the topic a door answers its commands on.
	LeafAck = "ack"
)

// EnrollTopic is where the controller with hardware id hwid announces
// itself while it enrolls.
func EnrollTopic(hwid string) string {
	return prefix + "enroll/" + hwid
}

// CredentialsTopic is where the controller with hardware id hwid receives
// the credentials it connects as once it is approved.
func CredentialsTopic(hwid string) string {
	return EnrollTopic(hwid) + "/credentials"
}

// DoorTopic is the topic of the door whose id is id that ends in leaf.
func DoorTopic(id int64, leaf string) string {
	return doorPrefix(id) + leaf
}

// DoorFilter matches every topic of the door whose id is id, and nothing
// else.
func DoorFilter(id int64) string {
	return doorPrefix(id) + "#"
}

func doorPrefix(id int64) string {
	return prefix + "doors/" + strconv.FormatInt(id, 10) + "/"
}

// Topic is what a topic says: the hardware id of an enrolling controller,
// or the id of a door and the last level of the door's topic, such as
// LeafCheck.
type Topic struct {
	HardwareID string
	DoorID     int64
	Leaf       string
}

// Parse reads a topic that EnrollFilter or a filter of every door's topics
// matched, and reports whether it names a controller or a door: a
// controller's hardware id is not empty, and a door's id is a whole number
// from 1.
func Parse(topic string) (Topic, bool) {
	levels := strings.Split(topic, "/")
	if levels[1] == "enroll" {
		return Topic{HardwareID: levels[2]}, levels[2] != ""
	}
	id, err := strconv.ParseInt(levels[2], 10, 64)
	return Topic{DoorID: id, Leaf: levels[len(levels)-1]}, err == nil && id > 0
}

// Header is the part every message has: the protocol version.
type Header struct {
	V int `cbor:"v"`
}

func (h *Header) header() *Header { return h }

// Message is any message of the protocol.
type Message interface {
	header() *Header
}

// Enrollment is what a controller publishes, retained, on its enroll topic
// once it connects with the enrollment credentials, with Online true, and
// what its last will says there, with Online false. Its hardware id is the
// topic's last level.
type Enrollment struct {
	Header
	Online          bool   `cbor:"online"`
	FirmwareVersion string `cbor:"firmware_version,omitempty"`
	Signal          int    `cbor:"signal,omitempty"`
}

// Credentials is what the server publishes on an approved controller's
// credentials topic: the door it is, and the username and token it
// connects to the broker as.
type Credentials struct {
	Header
	DoorID   int64  `cbor:"door_id"`
	Username string `cbor:"username"`
	Token    string `cbor:"token"`
}

// Online is what a door's controller publishes, retained, on its online
// topic once it connects as itself, with Online true and the username it
// connected as, and what its last will says there, with Online false and
// the same username.
type Online struct {
	Header
	Online          bool   `cbor:"online"`
	Username        string `cbor:"username"`
	FirmwareVersion string `cbor:"firmware_version,omitempty"`
	Signal          int    `cbor:"signal,omitempty"`
}

// The kinds of command.
const (
	// CommandCredentials hands a door new credentials.
	CommandCredentials = "credentials"
	// CommandOpen releases a door's strike.
	CommandOpen = "open"
	// CommandUpdate installs other firmware.
	CommandUpdate = "update"
	// CommandReboot restarts a door's controller.
	CommandReboot = "reboot"
)

// Command is what the server tells a door to do on its command topic. ID
// names the command, so the door can acknowledge it. A credentials command
// carries the username and token to connect as from now on; the door
// connects with them, publishes its online status, and then closes the
// connection it had. An open command carries how long, in milliseconds,
// to release the strike for. An update command carries the firmware
// version to install, the SHA-256 of its image in hex, and the HTTPS URL
// to download the image from; the door installs it, restarts, and
// connects again reporting the version, or acknowledges that it failed.
// A reboot command carries nothing more; the door acknowledges it,
// restarts, and connects again.
type Command struct {
	Header
	ID       string `cbor:"id"`
	Command  string `cbor:"command"`
	Username string `cbor:"username,omitempty"`
	Token    string `cbor:"token,omitempty"`
	StrikeMS int    `cbor:"strike_ms,omitempty"`
	Version  string `cbor:"version,omitempty"`
	SHA256   string `cbor:"sha256,omitempty"`
	URL      string `cbor:"url,omitempty"`
}

// AckFailed is the result of a command a door could not carry out.
const AckFailed = "failed"

// Ack is what a door answers a command with on its ack topic: the id the
// command gave, its result, AckFailed when the door could not carry it
// out, and why in words.
type Ack struct {
	Header
	ID     string `cbor:"id"`
	Result string `cbor:"result"`
	Detail string `cbor:"detail,omitempty"`
}

// Check is what a door publishes on its check topic when someone enters a
// code: the id it gives the request, so it can match the answer to it, and
// the digits entered, the code followed, when a command is given, by "*"
// and the command.
type Check struct {
	Header
	RequestID string `cbor:"request_id"`
	Digits    string `cbor:"digits"`
}

// Reply is what the server answers a check with on the door's reply topic:
// the request id the check gave, whether to let the person in, and, when
// it does, how long in milliseconds to release the strike for.
type Reply struct {
	Header
	RequestID string `cbor:"request_id"`
	Allow     bool   `cbor:"allow"`
	StrikeMS  int    `cbor:"strike_ms,omitempty"`
}

// DoorConfig is how a door runs: how long it releases the strike, how
// long a half-typed code waits, how long a code may be, how long it
// waits for the server's answer, how often it reports its
// telemetry, how long it may stay open, how often it expects a new
// timeline, how often its credentials rotate, how many codes a minute
// (and in a burst) the server checks, where it gets the time, which
// broker it connects to (empty to keep the one it has) on which port,
// whether over TLS and verifying it against which certificate, which
// Wi-Fi network it joins (empty to keep the one it has), when it roams,
// whether it keeps off the 802.11b rates, its MQTT keep-alive and
// longest reconnect pause, whether and how it
// meshes with other doors, how long new firmware may take to connect
// before it reverts, whether it installs unsigned firmware, and how much
// it logs. Wi-Fi and mesh passwords travel in the clear here, the one
// place they leave the server.
type DoorConfig struct {
	StrikePulseMS          int    `cbor:"strike_pulse_ms"`
	KeypadIdleClearMS      int    `cbor:"keypad_idle_clear_ms"`
	MinCodeLength          int    `cbor:"min_code_length"`
	MaxCodeLength          int    `cbor:"max_code_length"`
	ReplyTimeoutMS         int    `cbor:"reply_timeout_ms"`
	TelemetryIntervalS     int    `cbor:"telemetry_interval_s"`
	HeldOpenS              int    `cbor:"held_open_s"`
	TimelineRefreshHours   int    `cbor:"timeline_refresh_hours"`
	CredentialRotationDays int    `cbor:"credential_rotation_days"`
	RateLimitPerMinute     int    `cbor:"rate_limit_per_minute"`
	RateLimitBurst         int    `cbor:"rate_limit_burst"`
	NTPServer              string `cbor:"ntp_server"`
	BrokerHost             string `cbor:"broker_host"`
	BrokerPort             int    `cbor:"broker_port"`
	BrokerTLS              bool   `cbor:"broker_tls"`
	BrokerCAPEM            string `cbor:"broker_ca_pem"`
	WifiSSID               string `cbor:"wifi_ssid"`
	WifiPassword           string `cbor:"wifi_password"`
	WifiRoamRSSI           int    `cbor:"wifi_roam_rssi"`
	Wifi11bDisabled        bool   `cbor:"wifi_11b_disabled"`
	KeepaliveS             int    `cbor:"keepalive_s"`
	ReconnectMaxS          int    `cbor:"reconnect_max_s"`
	MeshEnabled            bool   `cbor:"mesh_enabled"`
	MeshSSID               string `cbor:"mesh_ssid"`
	MeshPassword           string `cbor:"mesh_password"`
	MeshMinRouterRSSI      int    `cbor:"mesh_min_router_rssi"`
	MeshMaxLayer           int    `cbor:"mesh_max_layer"`
	UpdateConnectTimeoutS  int    `cbor:"update_connect_timeout_s"`
	AcceptUnsignedFirmware bool   `cbor:"accept_unsigned_firmware"`
	LogLevel               string `cbor:"log_level"`
}

// Config is what the server publishes, retained, on a door's config
// topic: the configuration it runs with, the site's defaults with the
// door's own overrides, and the site's firmware signing key, the public
// point x || y in lowercase hex, that the door checks every image against.
type Config struct {
	Header
	DoorConfig
	FirmwareSigningKey string `cbor:"firmware_signing_key"`
}

// Span is a time from Start up to, but not including, End, both in Unix
// seconds, during which a door is in State: "open" (any keypress unlocks
// it) or "locked" (no code does).
type Span struct {
	Start int64  `cbor:"start"`
	End   int64  `cbor:"end"`
	State string `cbor:"state"`
}

// Timeline is what the server publishes, retained, on a door's timeline
// topic: the spans, in order, from From up to To (Unix seconds) during
// which it is open or locked. Between them it is in its normal state,
// where a code the server allows opens it.
type Timeline struct {
	Header
	From  int64  `cbor:"from"`
	To    int64  `cbor:"to"`
	Spans []Span `cbor:"spans"`
}

// Telemetry is what a door publishes on its telemetry topic at the
// interval its configuration gives: its signal strength in dBm, the round
// trip to the server in milliseconds, how long it has run in seconds, its
// free memory in bytes, its firmware version, how often it has connected
// to the broker again, whether the door is open, how often it was opened,
// and where it sits in the site's Wi-Fi mesh: its layer (0 when it
// reaches the building's access point itself, 1 when it hops through one
// other controller, and so on), the hardware id of the controller it hops
// through (empty at layer 0), and how many controllers hop through it.
// Firmware that knows nothing of the mesh leaves those out, which reads
// as 0 and empty.
//
// It also says how its links fared: the code checks it sent and those
// whose reply never came, the TCP segments it sent for the first time,
// sent again, and dropped, the times its Wi-Fi station lost its access
// point and moved to another by its own choice, the system's reason code
// for its latest loss of the access point since it started (ESP-IDF's
// wifi_err_reason_t, 0 when there was none), and the access point it has
// joined now: its BSSID, 12 lowercase hex digits, and its channel, empty
// and 0 with no link. Firmware that does not report these leaves them
// out, which reads as 0 and empty.
//
// Every count (Reconnects, Opens, ChecksSent, ChecksLost, TCPSent,
// TCPRetransmits, TCPDrops, WifiDrops, and Roams) is a total since the
// controller started, so a report lost on the way loses no counts.
//
// ConfigDigest names the configuration the door runs by its Digest, empty
// while it runs with its defaults and from firmware that does not report
// it.
type Telemetry struct {
	Header
	Signal          int    `cbor:"signal"`
	LatencyMS       int    `cbor:"latency_ms"`
	UptimeS         int64  `cbor:"uptime_s"`
	FreeBytes       int64  `cbor:"free_bytes"`
	FirmwareVersion string `cbor:"firmware_version"`
	Reconnects      int    `cbor:"reconnects"`
	DoorOpen        bool   `cbor:"door_open"`
	Opens           int    `cbor:"opens"`
	MeshLayer       int    `cbor:"mesh_layer"`
	MeshParent      string `cbor:"mesh_parent"`
	MeshChildren    int    `cbor:"mesh_children"`
	ChecksSent      int    `cbor:"checks_sent"`
	ChecksLost      int    `cbor:"checks_lost"`
	TCPSent         int64  `cbor:"tcp_sent"`
	TCPRetransmits  int64  `cbor:"tcp_retransmits"`
	TCPDrops        int64  `cbor:"tcp_drops"`
	WifiDrops       int    `cbor:"wifi_drops"`
	WifiDropReason  int    `cbor:"wifi_drop_reason"`
	Roams           int    `cbor:"roams"`
	BSSID           string `cbor:"bssid"`
	Channel         int    `cbor:"channel"`
	ConfigDigest    string `cbor:"config_digest"`
}

// The things a door says happened to it.
const (
	// EventOpened is the door opening.
	EventOpened = "opened"
	// EventClosed is the door closing.
	EventClosed = "closed"
	// EventStarted is the controller coming online as the door for the
	// first time since it started.
	EventStarted = "started"
	// EventConfigReverted is the controller, back online, saying it gave
	// up a configuration it tried because it could not come online with
	// it, and went back to the one it ran before.
	EventConfigReverted = "config_reverted"
)

// The reasons a controller gives with EventStarted for why it started.
const (
	// StartPowerOn is the controller powered on.
	StartPowerOn = "power_on"
	// StartSoftware is a restart its firmware asked for, such as for a
	// reboot command, an update, or a revert.
	StartSoftware = "software"
	// StartPanic is its firmware panicking.
	StartPanic = "panic"
	// StartWatchdog is its watchdog resetting it.
	StartWatchdog = "watchdog"
	// StartBrownout is its supply voltage dropping too low.
	StartBrownout = "brownout"
	// StartExternal is its reset pin.
	StartExternal = "external"
	// StartUnknown is the controller not knowing why it started.
	StartUnknown = "unknown"
)

// StartReasons is every reason a controller gives for why it started, in
// the order pages list them.
var StartReasons = []string{StartPowerOn, StartSoftware, StartPanic, StartWatchdog, StartBrownout, StartExternal, StartUnknown}

// DoorEvent is what a door publishes on its event topic when something
// happens to it, such as EventOpened. A door says each time it opens and
// closes, in open mode too; controllers that predate that say nothing in
// open mode. With EventOpened, Unlocked says whether the
// door was unlocked when it opened, and with EventClosed, OpenMS how many
// milliseconds it was open by the controller's clock, sent only when the
// controller saw the opening from its start outside an open span. A
// controller says EventStarted once, the first time it comes online as
// the door after it started, with Reason saying why it started, such as
// StartPowerOn, when it knows. Controllers that predate these leave them
// out. With EventConfigReverted, ConfigDigest is the Digest of the
// configuration the controller gave up.
type DoorEvent struct {
	Header
	Event        string  `cbor:"event"`
	Unlocked     *bool   `cbor:"unlocked,omitempty"`
	OpenMS       *uint64 `cbor:"open_ms,omitempty"`
	Reason       string  `cbor:"reason,omitempty"`
	ConfigDigest string  `cbor:"config_digest,omitempty"`
}

// ErrVersion reports a message of another protocol version.
var ErrVersion = errors.New("unsupported protocol version")

var encoding = func() cbor.EncMode {
	mode, _ := cbor.CoreDetEncOptions().EncMode()
	return mode
}()

// Encode is m as a CBOR map carrying the protocol version.
func Encode(m Message) []byte {
	m.header().V = Version
	out, _ := encoding.Marshal(m)
	return out
}

// Digest names the configuration message whose payload is payload, as
// the door names the configuration it runs: the first 8 bytes of the
// payload's SHA-256, as 16 lowercase hex digits. Encode is deterministic,
// so the same configuration always has the same digest.
func Digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:8])
}

// Decode reads payload into m, refusing anything but a CBOR map of this
// protocol version.
func Decode(payload []byte, m Message) error {
	if err := cbor.Unmarshal(payload, m); err != nil {
		return fmt.Errorf("not a protocol message: %w", err)
	}
	if v := m.header().V; v != Version {
		return fmt.Errorf("%w %d", ErrVersion, v)
	}
	return nil
}
