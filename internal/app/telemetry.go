package app

import (
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
)

// The ranges, in hours, the telemetry API covers unless it is asked for
// another.
const (
	samplesHours = 24
	rollupsHours = 168
)

type telemetryView struct {
	At              time.Time `json:"at" doc:"When the server received the report."`
	Signal          int       `json:"signal" doc:"The signal strength, in dBm."`
	LatencyMS       int       `json:"latency_ms" doc:"The round trip from the controller to the server and back, in milliseconds."`
	UptimeS         int64     `json:"uptime_s" doc:"How long the controller has run since it started, in seconds."`
	FreeBytes       int64     `json:"free_bytes" doc:"The controller's free memory, in bytes."`
	FirmwareVersion string    `json:"firmware_version" doc:"The firmware version the controller runs."`
	Reconnects      int       `json:"reconnects" doc:"How often the controller connected to the broker again since it started."`
	DoorOpen        bool      `json:"door_open" doc:"True when the door was open."`
	Opens           int       `json:"opens" doc:"How often the door was opened since the controller's last report."`
}

type doorStateView struct {
	Enrolled        bool           `json:"enrolled" doc:"True once a controller has enrolled as the door; the other fields say nothing until one has."`
	Online          bool           `json:"online" doc:"True while the controller is connected to the broker."`
	Door            string         `json:"door" doc:"open or closed, as the controller last said; unknown until it has."`
	OpenedAt        *time.Time     `json:"opened_at,omitempty" doc:"When the door opened; absent unless it is open."`
	HeldOpen        bool           `json:"held_open" doc:"True when the door has been open longer than its configuration allows since it opened."`
	Unresponsive    bool           `json:"unresponsive" doc:"True when the server has not heard from the controller for the unresponsive_minutes setting."`
	FirmwareVersion string         `json:"firmware_version" doc:"The firmware version the controller last reported; may be empty."`
	LastSeenAt      *time.Time     `json:"last_seen_at,omitempty" doc:"When the server last heard from the controller; absent until a controller enrolls."`
	Telemetry       *telemetryView `json:"telemetry,omitempty" doc:"The controller's latest telemetry report; absent until its first."`
}

type sampleList struct {
	Samples []telemetryView `json:"samples" doc:"Every report the door's controller made in the range, oldest first."`
}

type spreadView struct {
	Mean float64 `json:"mean" doc:"The mean of the values."`
	Min  int64   `json:"min" doc:"The smallest value."`
	Max  int64   `json:"max" doc:"The largest value."`
}

type rollupView struct {
	Hour      time.Time  `json:"hour" doc:"The start of the hour, in UTC."`
	Samples   int        `json:"samples" doc:"How many reports the controller made in the hour."`
	Signal    spreadView `json:"signal" doc:"The signal strength, in dBm."`
	LatencyMS spreadView `json:"latency_ms" doc:"The round trip to the server, in milliseconds."`
	FreeBytes spreadView `json:"free_bytes" doc:"The free memory, in bytes."`
	Opens     int        `json:"opens" doc:"How often the door was opened in the hour."`
}

type rollupList struct {
	Rollups []rollupView `json:"rollups" doc:"The hours in the range in which the door's controller reported, oldest first."`
}

// badRange answers a telemetry range that cannot be read.
var badRange = openapi.Response{Status: http.StatusBadRequest, Description: "from or to is not an RFC 3339 time, or to is not after from.", Body: errorBody{}}

func (a *App) telemetryRoutes() []route {
	params := func(span string) []openapi.Param {
		return []openapi.Param{
			doorIDParam,
			{Name: "from", In: "query", Description: "The start of the range, an RFC 3339 time; " + span + " before to by default.", Example: "2026-01-05T00:00:00Z"},
			{Name: "to", In: "query", Description: "The end of the range, an RFC 3339 time; now by default.", Example: "2026-01-06T00:00:00Z"},
		}
	}
	return []route{
		apiRoute(http.MethodGet, "/doors/{id}/state", opensDoors, a.getDoorState, apiDoc{
			id: "getDoorState", summary: "Get a door's current state", tag: "telemetry", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "What the door's controller last said of itself and the door.", Body: doorStateView{}}, noDoor},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/telemetry", opensDoors, a.listSamples, apiDoc{
			id: "listDoorTelemetry", summary: "List a door's telemetry reports", tag: "telemetry", params: params("24 hours"),
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The reports made from from up to to, kept for the telemetry_retention_days setting.", Body: sampleList{}},
				badRange, noDoor,
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/telemetry/hourly", opensDoors, a.listRollups, apiDoc{
			id: "listDoorTelemetryHourly", summary: "List a door's telemetry an hour at a time", tag: "telemetry", params: params("7 days"),
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The reports summed up for each hour that ended and starts from from up to to, kept for the telemetry_rollup_retention_days setting.", Body: rollupList{}},
				badRange, noDoor,
			},
		}),
	}
}

func viewOfTelemetry(t connector.StoredTelemetry) telemetryView {
	return telemetryView{
		At: t.At.UTC(), Signal: t.Signal, LatencyMS: t.LatencyMS, UptimeS: t.UptimeS, FreeBytes: t.FreeBytes,
		FirmwareVersion: t.FirmwareVersion, Reconnects: t.Reconnects, DoorOpen: t.DoorOpen, Opens: t.Opens,
	}
}

// viewOfDoorState is the state of the door controller c is enrolled as,
// a door without a controller when c is nil.
func viewOfDoorState(c *connector.StoredController) doorStateView {
	if c == nil {
		return doorStateView{Door: doorUnknown}
	}
	seen := c.LastSeenAt.UTC()
	view := doorStateView{
		Enrolled: true, Online: c.Online, Door: doorUnknown, OpenedAt: utc(c.OpenedAt), HeldOpen: c.HeldOpen, Unresponsive: c.Unresponsive,
		FirmwareVersion: c.FirmwareVersion, LastSeenAt: &seen,
	}
	if c.DoorState != "" {
		view.Door = c.DoorState
	}
	if c.Telemetry != nil {
		t := viewOfTelemetry(*c.Telemetry)
		view.Telemetry = &t
	}
	return view
}

// doorUnknown is a door's state until its controller says it.
const doorUnknown = "unknown"

func (a *App) getDoorState(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, err := a.doors.Door(r.Context(), pathID(r, "id"))
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfDoorState(d.Controller))
}

// telemetryRange reads the from and to query parameters, to now and from
// hours before to unless they are given, and reports whether they can be
// read and to is after from.
func (a *App) telemetryRange(r *http.Request, hours int) (time.Time, time.Time, bool) {
	to, toOK := queryTime(r, "to")
	end := a.clock.Now()
	if to != nil {
		end = *to
	}
	from, fromOK := queryTime(r, "from")
	start := end.Add(-time.Duration(hours) * time.Hour)
	if from != nil {
		start = *from
	}
	return start, end, fromOK && toOK && start.Before(end)
}

func (a *App) listSamples(w http.ResponseWriter, r *http.Request, _ auth.User) {
	from, to, ok := a.telemetryRange(r, samplesHours)
	if !ok {
		writeError(w, http.StatusBadRequest, "from and to must be RFC 3339 times, to after from")
		return
	}
	samples, err := a.telemetry.Samples(r.Context(), pathID(r, "id"), from, to)
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	views := []telemetryView{}
	for _, s := range samples {
		views = append(views, viewOfTelemetry(s.StoredTelemetry))
	}
	writeJSON(w, http.StatusOK, sampleList{Samples: views})
}

func viewOfSpread(s connector.StoredSpread) spreadView {
	return spreadView{Mean: s.Mean, Min: s.Min, Max: s.Max}
}

func (a *App) listRollups(w http.ResponseWriter, r *http.Request, _ auth.User) {
	from, to, ok := a.telemetryRange(r, rollupsHours)
	if !ok {
		writeError(w, http.StatusBadRequest, "from and to must be RFC 3339 times, to after from")
		return
	}
	rollups, err := a.telemetry.Rollups(r.Context(), pathID(r, "id"), from, to)
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	views := []rollupView{}
	for _, h := range rollups {
		views = append(views, rollupView{
			Hour: h.Hour.UTC(), Samples: h.Samples, Signal: viewOfSpread(h.Signal), LatencyMS: viewOfSpread(h.Latency),
			FreeBytes: viewOfSpread(h.FreeBytes), Opens: h.Opens,
		})
	}
	writeJSON(w, http.StatusOK, rollupList{Rollups: views})
}
