package telemetry

import (
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/protocol"
)

// totalsOf is the counts report m gives, each a total since its
// controller started.
func totalsOf(m protocol.Telemetry) connector.StoredCounts {
	return connector.StoredCounts{
		Reconnects: m.Reconnects, Opens: m.Opens,
		StoredLinkCounts: connector.StoredLinkCounts{
			ChecksSent: m.ChecksSent, ChecksLost: m.ChecksLost, TCPSent: m.TCPSent, TCPRetransmits: m.TCPRetransmits,
			TCPDrops: m.TCPDrops, WifiDrops: m.WifiDrops, Roams: m.Roams,
		},
	}
}

// totalsBefore is the totals a report that says its controller has run
// for uptimeS seconds counts on from: those of the report before it,
// held's latest, unless there is none or the controller has restarted
// since, its uptime lower than then, when it counts from nothing.
func totalsBefore(held connector.StoredController, uptimeS int64) connector.StoredCounts {
	before := held.Telemetry
	if before == nil || uptimeS < before.UptimeS {
		return connector.StoredCounts{}
	}
	return before.Totals
}

// counted is what a controller counted between reporting the totals
// before and the totals now, count by count.
func counted(now, before connector.StoredCounts) connector.StoredCounts {
	return connector.StoredCounts{
		Reconnects: sinceInt(now.Reconnects, before.Reconnects), Opens: sinceInt(now.Opens, before.Opens),
		StoredLinkCounts: connector.StoredLinkCounts{
			ChecksSent: sinceInt(now.ChecksSent, before.ChecksSent), ChecksLost: sinceInt(now.ChecksLost, before.ChecksLost),
			TCPSent: since(now.TCPSent, before.TCPSent), TCPRetransmits: since(now.TCPRetransmits, before.TCPRetransmits),
			TCPDrops: since(now.TCPDrops, before.TCPDrops), WifiDrops: sinceInt(now.WifiDrops, before.WifiDrops),
			Roams: sinceInt(now.Roams, before.Roams),
		},
	}
}

// since is what a count that stood at before and now stands at now
// counted in between. A count lower than before started again, from a
// restart or its counter wrapping, so all of it is new.
func since(now, before int64) int64 {
	if now < before {
		return now
	}
	return now - before
}

// sinceInt is since for a count kept as an int.
func sinceInt(now, before int) int {
	return int(since(int64(now), int64(before)))
}

// Added is the opens and the link counts of a and b together; no sum
// keeps how often a controller connected again.
func Added(a, b connector.StoredCounts) connector.StoredCounts {
	return connector.StoredCounts{
		Opens: a.Opens + b.Opens,
		StoredLinkCounts: connector.StoredLinkCounts{
			ChecksSent: a.ChecksSent + b.ChecksSent, ChecksLost: a.ChecksLost + b.ChecksLost, TCPSent: a.TCPSent + b.TCPSent,
			TCPRetransmits: a.TCPRetransmits + b.TCPRetransmits, TCPDrops: a.TCPDrops + b.TCPDrops,
			WifiDrops: a.WifiDrops + b.WifiDrops, Roams: a.Roams + b.Roams,
		},
	}
}
