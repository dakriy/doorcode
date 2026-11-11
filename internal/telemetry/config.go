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
	// configuration in two reports in a row over one connection, both
	// after the one last sent went out.
	ConfigOlder = "older"
	// ConfigReverted is a controller that said, after the configuration
	// last sent went out, that it gave that one up because it could not
	// come online with it, and runs the one before.
	ConfigReverted = "reverted"
)

// The badges door lists show of a door whose controller does not run
// the configuration last sent.
const (
	BadgeMismatchedConfig = "Mismatched configuration"
	BadgeRevertedConfig   = "Reverted configuration"
)

// ConfigCheck is how what the controller of a door runs stands against
// the configuration it was last sent: its State and, for ConfigOlder and
// ConfigReverted, Since when.
type ConfigCheck struct {
	State string
	Since *time.Time
}

// Mismatched reports whether the controller runs a configuration other
// than the one last sent, which is when door lists show a Badge of it.
func (c ConfigCheck) Mismatched() bool {
	return c.Badge() != ""
}

// Badge is what door lists show of c, empty unless the controller runs a
// configuration other than the one last sent.
func (c ConfigCheck) Badge() string {
	switch c.State {
	case ConfigOlder:
		return BadgeMismatchedConfig
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
// over the same connection that names another configuration makes it
// older, since the door had a whole telemetry interval to take it. A
// door that says it gave the configuration up is flagged at once.
func ConfigOf(d connector.StoredDoor) ConfigCheck {
	c, sent := d.Controller, d.SentConfig
	if sent == nil {
		return ConfigCheck{}
	}
	if t := c.Telemetry; t != nil && t.ConfigDigest == sent.Digest {
		return ConfigCheck{State: ConfigCurrent}
	}
	if at := c.ConfigRevertedAt; sinceSent(*sent, at) && c.ConfigRevertedDigest == sent.Digest {
		return ConfigCheck{State: ConfigReverted, Since: at}
	}
	if since := c.ConfigMismatchSince; sinceSent(*sent, since) && c.Telemetry.At.After(*since) {
		return ConfigCheck{State: ConfigOlder, Since: since}
	}
	return ConfigCheck{}
}

// mismatchSince is when the controller of d, held, began naming a
// configuration other than the one d was last sent, given its report t:
// nil when t names that one or none, or d was sent none, otherwise when
// the reports since it was sent began naming another over the connection
// t came over, t.At when t is the first. A report over a new connection
// starts the count again, since the first after the door came back may
// come before it took the configuration, however long ago the one before
// it was.
func mismatchSince(d connector.StoredDoor, held connector.StoredController, t connector.StoredTelemetry) *time.Time {
	sent := d.SentConfig
	if sent == nil || t.ConfigDigest == "" || t.ConfigDigest == sent.Digest {
		return nil
	}
	if since := held.ConfigMismatchSince; sinceSent(*sent, since) && sameConnection(*held.Telemetry, t) {
		return since
	}
	return &t.At
}

// sameConnection reports whether the controller made report t over the
// connection it made report prev over: it has neither restarted nor
// connected again since.
func sameConnection(prev, t connector.StoredTelemetry) bool {
	return t.Totals.Reconnects == prev.Totals.Reconnects && t.UptimeS >= prev.UptimeS
}

// sinceSent reports whether at, nil for never, is no earlier than when
// sent was first sent, so that what the controller said then can speak
// of it.
func sinceSent(sent connector.StoredSentConfig, at *time.Time) bool {
	return at != nil && !at.Before(sent.At)
}
