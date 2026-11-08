// Package protocol is the MQTT protocol between the server and the door
// controllers: the topics, and the messages they carry encoded as CBOR. It
// is the only package that encodes or decodes CBOR, so every message passes
// through it. Every message is a CBOR map whose "v" entry is the protocol
// version.
package protocol

import (
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
	// CredentialsFilter matches the topics enrolling controllers receive
	// their credentials on.
	CredentialsFilter = "doorcode/enroll/+/credentials"
)

// LeafCommand ends the topic a door receives commands on.
const LeafCommand = "command"

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
// or the id of a door.
type Topic struct {
	HardwareID string
	DoorID     int64
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
	return Topic{DoorID: id}, err == nil && id > 0
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

// CommandCredentials is the kind of command that hands a door new
// credentials.
const CommandCredentials = "credentials"

// Command is what the server tells a door to do on its command topic. ID
// names the command, so the door can acknowledge it. A credentials command
// carries the username and token to connect as from now on; the door
// connects with them, publishes its online status, and then closes the
// connection it had.
type Command struct {
	Header
	ID       string `cbor:"id"`
	Command  string `cbor:"command"`
	Username string `cbor:"username,omitempty"`
	Token    string `cbor:"token,omitempty"`
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
