package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// linkHour is how a controller's links fared in an hour, drawn at At.
type linkHour struct {
	At time.Time
	connector.StoredLinkCounts
}

// latest is the later of a and b.
func latest(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

// linkCharts are the charts of how a controller's links fared, hours an
// hour at a time: the share of code checks lost and of TCP segments sent
// again, the TCP segments dropped, and the Wi-Fi drops and roams.
func (f chartFrame) linkCharts(hours []linkHour) []chartView {
	var lost, retransmits, drops, wifiDrops, roams []point
	for _, h := range hours {
		lost = appendShare(lost, h.At, int64(h.ChecksLost), int64(h.ChecksSent))
		retransmits = appendShare(retransmits, h.At, h.TCPRetransmits, h.TCPSent)
		drops = append(drops, point{h.At, float64(h.TCPDrops)})
		wifiDrops = append(wifiDrops, point{h.At, float64(h.WifiDrops)})
		roams = append(roams, point{h.At, float64(h.Roams)})
	}
	return []chartView{
		f.shareChart("Code checks lost", "code checks sent", lost),
		f.shareChart("TCP retransmits", "TCP segments sent", retransmits),
		f.chart("TCP drops per hour", "", drops),
		f.keyedChart("Wi-Fi drops and roams per hour", "", []string{"Drops", "Roams"}, wifiDrops, roams),
	}
}

// appendShare is points with what share of whole part is, as a
// percentage, at at; points alone when whole is nothing, since an hour
// that sent nothing lost nothing of it.
func appendShare(points []point, at time.Time, part, whole int64) []point {
	if whole == 0 {
		return points
	}
	return append(points, point{at, float64(part) * 100 / float64(whole)})
}

// shareChart is the chart named name of points, each a part of an
// hour's whole as a percentage; with no points it says that no hour had
// any of the whole, which whole names.
func (f chartFrame) shareChart(name, whole string, points []point) chartView {
	view := f.chart(name, "%", points)
	view.Empty = "No " + whole + " " + f.during + "."
	return view
}

// accessPointText is the Wi-Fi access point of link, its BSSID written
// in pairs of hex digits and its channel, as a door's page shows it.
func accessPointText(link connector.StoredLink) string {
	if link.BSSID == "" {
		return "None reported"
	}
	var b strings.Builder
	for i, r := range link.BSSID {
		if i > 0 && i%2 == 0 {
			b.WriteByte(':')
		}
		b.WriteRune(r)
	}
	return b.String() + ", channel " + strconv.Itoa(link.Channel)
}

// dropReasons names the reason codes the ESP32's Wi-Fi station gives
// for losing its access point (ESP-IDF's wifi_err_reason_t) in words a
// building manager can read.
var dropReasons = map[int]string{
	1:   "No reason given",
	2:   "Its sign-in to the access point expired",
	3:   "The access point signed it off",
	4:   "The access point dropped it for inactivity",
	5:   "The access point had too many devices",
	8:   "It left the access point",
	15:  "The Wi-Fi password check timed out; the password may be wrong",
	200: "It stopped hearing the access point",
	201: "It could not find the access point",
	202: "The access point refused its sign-in",
	203: "The access point refused to connect it",
	204: "The Wi-Fi password check timed out; the password may be wrong",
	205: "It could not connect to the access point",
}

// dropReasonText is the reason a controller's station last lost its
// access point, given by its code, as a door's page shows it.
func dropReasonText(code int) string {
	if code == 0 {
		return "None reported"
	}
	name, known := dropReasons[code]
	if !known {
		return "Code " + strconv.Itoa(code)
	}
	return name + " (code " + strconv.Itoa(code) + ")"
}
