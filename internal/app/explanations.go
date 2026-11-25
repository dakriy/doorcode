package app

// infoView is an explanation shown as a tooltip beside a label: the id
// of the element holding it, the label it explains, and its text.
type infoView struct {
	ID    string
	Label string
	Text  string
}

// latencyExplained is what a door's latency is, wherever a page explains
// it in a tooltip.
const latencyExplained = "Not network latency alone: the time the door took to hand its previous telemetry report to the broker and get the broker's acknowledgement back, measured by the door. It takes in the door's own send queue, the Wi-Fi, the TLS proxy, and the broker, not the server's handling of the report. A report without a measurement shows None measured."

// explanations is the tooltip text of each compact figure a page
// explains, by the key its info mark names.
var explanations = map[string]string{
	"door-state":    "Online: the controller is connected to the broker. Offline: it is not. Unresponsive: connected, but not heard from for unresponsive_minutes. Open: the door sensor reads open. Held open: open for longer than its held-open time while it should be locked.",
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
	"last-seen":     "When the server last heard anything from the controller: a report, a door event, a code check, or its online status. A connected controller silent for unresponsive_minutes is shown unresponsive.",
	"last-report":   "When the controller's latest telemetry report arrived. It reports every telemetry_interval_s.",
	"configuration": "Whether the controller runs the configuration it was last sent, by the digest it reports. Up to date: it does. Mismatched: it keeps reporting another one. Restarting: it started at least twice since without reporting it. Reverted: it gave it up because it could not come online with it.",
}

// info is the explanation of the figure labelled label that explanations
// holds under key.
func info(key, label string) infoView {
	return infoView{ID: "info-" + key, Label: label, Text: explanations[key]}
}
