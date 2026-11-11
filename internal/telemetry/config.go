package telemetry

import (
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// What a door's controller runs, measured against the configuration the
// door was last sent. A State that is none of these is a door the server
// cannot judge yet: it has no controller, it was sent nothing since the
// server began keeping track, its controller has not reported since, or
// it reported no digest (running its defaults, or firmware that does not
// say).
const (
	// ConfigCurrent is a controller whose latest report names the
	// configuration last sent.
	ConfigCurrent = "current"
	// ConfigOlder is a controller that went on naming another
	// configuration in two reports in a row, both after the one last sent
	// went out.
	ConfigOlder = "older"
	// ConfigReverted is a controller that said, after the configuration
	// last sent went out, that it gave that one up because it could not
	// come online with it, and runs the one before.
	ConfigReverted = "reverted"
)

// The badges door lists show of a door whose controller does not run
// the configuration last sent.
const (
	BadgeOlderConfig    = "Older configuration"
	BadgeRevertedConfig = "Configuration given up"
)

// ConfigCheck is how what the controller of a door runs stands against
// the configuration it was last sent: its State and, for ConfigOlder and
// ConfigReverted, Since when.
type ConfigCheck struct {
	State string
	Since *time.Time
}

// Mismatched reports whether the controller runs a configuration other
// than the one last sent.
func (c ConfigCheck) Mismatched() bool {
	return c.State == ConfigOlder || c.State == ConfigReverted
}

// Badge is what door lists show of c, empty unless it is Mismatched.
func (c ConfigCheck) Badge() string {
	switch c.State {
	case ConfigOlder:
		return BadgeOlderConfig
	case ConfigReverted:
		return BadgeRevertedConfig
	}
	return ""
}

// ConfigOf is how what the controller of d, which has one, runs stands
// against the configuration d was last sent.
//
// A single report naming another configuration is not enough: one sent
// just as the configuration went out, or the first after the door came
// back online, may come before the door took it, and a door trying a
// configuration that changes how it connects restarts and reports
// nothing until it is online with it. Only a second report in a row
// that names another configuration makes it older, since the door had
// a whole telemetry interval to take it. A door that says it gave the
// configuration up is flagged at once.
func ConfigOf(d connector.StoredDoor) ConfigCheck {
	c, sent := d.Controller, d.SentConfig
	if sent == nil {
		return ConfigCheck{}
	}
	if t := c.Telemetry; t != nil && t.ConfigDigest == sent.Digest {
		return ConfigCheck{State: ConfigCurrent}
	}
	if at := c.ConfigRevertedAt; at != nil && c.ConfigRevertedDigest == sent.Digest && !at.Before(sent.At) {
		return ConfigCheck{State: ConfigReverted, Since: at}
	}
	if since := c.ConfigMismatchSince; since != nil && !since.Before(sent.At) && c.Telemetry.At.After(*since) {
		return ConfigCheck{State: ConfigOlder, Since: since}
	}
	return ConfigCheck{}
}

// mismatchSince is when the controller of d, held, began naming a
// configuration other than the one d was last sent, given that its
// report at now names digest: nil when the report names that one or
// none, or d was sent none, otherwise when the reports since it was sent
// began naming another, now when this is the first.
func mismatchSince(d connector.StoredDoor, held connector.StoredController, digest string, now time.Time) *time.Time {
	sent := d.SentConfig
	if sent == nil || digest == "" || digest == sent.Digest {
		return nil
	}
	if since := held.ConfigMismatchSince; since != nil && !since.Before(sent.At) {
		return since
	}
	return &now
}
