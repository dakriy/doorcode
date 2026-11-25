package app

import (
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/settings"
)

// infoView is an explanation shown as a tooltip beside a label: the id
// of the element holding it, the label it explains, and its text.
type infoView struct {
	ID    string
	Label string
	Text  string
}

// latencyExplained is what a door's latency is, wherever a page explains
// it in a tooltip.
const latencyExplained = "Not network latency alone: the time the door took to hand its previous telemetry report to the broker and get the broker's acknowledgement back, measured by the door. It takes in the door's own send queue, the Wi-Fi, the network path to the broker, and the broker itself, not the server's handling of the report. A report without a measurement shows None measured."

// explanations is the tooltip text of each compact figure a page
// explains, by the key its info mark names.
var explanations = map[string]string{
	"door":          "Open or closed as the door sensor reports it. A sensor whose wires are cut or not connected reads open; a door whose sensor terminals are bridged always reads closed.",
	"signal":        "The Wi-Fi signal strength the controller receives from its access point, in dBm. Closer to 0 is stronger: about -50 is excellent, -67 good, below -75 weak, and below -80 the link is likely to drop.",
	"latency":       latencyExplained,
	"latency-chart": latencyExplained,
	"uptime":        "How long the controller has run since it last started. It starts again from 0 after a restart, a firmware update, or a loss of power.",
	"memory":        "The controller's free memory. It should stay about level; one that keeps falling until the controller restarts points to a leak.",
	"reconnects":    "How often the controller connected to the broker again since it last started; 0 means it has stayed connected since. A count that keeps rising points to an unsteady Wi-Fi or network path.",
	"mesh-layer":    "Where the controller sits in the site's Wi-Fi mesh. 0: it connects straight to the building's access point. 1: it reaches the access point through one other controller, its mesh parent; 2 through two, and so on.",
	"mesh-parent":   "The door whose controller this one hops through to reach the access point; None at layer 0.",
	"mesh-children": "How many controllers hop through this one. They lose their way to the access point while it is down.",
	"access-point":  "The radio address (BSSID) and channel of the access point the controller is joined to: the building's, or its mesh parent's.",
	"last-drop":     "Why the controller's Wi-Fi last lost its access point since it started, as its radio reported it.",
	"configuration": "Whether the controller runs the configuration it was last sent, by the digest it reports. Up to date: it does. Mismatched: it keeps reporting another one. Restarting: it started at least twice since without reporting it. Reverted: it gave it up because it could not come online with it.",
}

// info is the explanation of the figure labelled label that explanations
// holds under key.
func info(key, label string) infoView {
	return tip(key, label, explanations[key])
}

// tip is text, the explanation of the figure labelled label, under key.
func tip(key, label, text string) infoView {
	return infoView{ID: "info-" + key, Label: label, Text: text}
}

// doorTimes is how long, for one door, its controller may stay silent
// before it is unresponsive, Unresponsive, its door may stand open before
// it is held open, HeldOpen, and how often it reports, ReportEvery.
type doorTimes struct {
	Unresponsive, HeldOpen, ReportEvery time.Duration
}

// timesOf is the times of a door whose configuration, its own overrides
// over the defaults, is config, at a site whose settings are current.
func timesOf(current settings.Current, config settings.DoorConfig) doorTimes {
	return doorTimes{Unresponsive: current.UnresponsiveAfter, HeldOpen: config.HeldOpen(), ReportEvery: time.Duration(config.TelemetryIntervalS) * time.Second}
}

// stateTips is what the door page's Live state explains of the state,
// of when the controller was last seen, and of its last report, for a
// door with times.
type stateTips struct {
	State, LastSeen, LastReport string
}

// tipsFor is the Live state's explanations of a door with times.
func tipsFor(times doorTimes) stateTips {
	silent := countText(int(times.Unresponsive/time.Minute), "minute")
	return stateTips{
		State: "Online: the controller is connected to the broker. Offline: it is not. Unresponsive: connected, but not heard from for " + silent +
			". Open: the door sensor reads open. Held open: open for longer than " + countText(int(times.HeldOpen/time.Second), "second") +
			" while it should be locked.",
		LastSeen: "When the server last heard anything from the controller: a report, a door event, a code check, or its online status. " +
			"A connected controller silent for " + silent + " is shown unresponsive.",
		LastReport: "When the controller's latest telemetry report arrived. It reports every " + countText(int(times.ReportEvery/time.Second), "second") + ".",
	}
}

// countText is n of unit in words, such as 1 minute or 5 minutes.
func countText(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
