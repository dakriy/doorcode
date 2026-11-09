package app

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/stats"
	"github.com/dakriy/doorcode/internal/telemetry"
)

// dashboardPath is the administration's landing page.
const dashboardPath = "/admin"

// adminRoles are the roles that reach a part of the administration, and
// so the dashboard.
var adminRoles = []string{
	auth.RoleManageUsers, auth.RoleManageGroups, auth.RoleManageSchedules, auth.RoleManageDoors, auth.RoleDoorCommander,
	auth.RoleManageFirmware, auth.RoleViewLogs,
}

// The parts of the dashboard, each shown to those holding its roles.
var (
	administers  = holding(adminRoles...)
	readsLog     = holding(auth.RoleViewLogs)
	enrollsDoors = holding(auth.RoleManageDoors)
	runsFirmware = holding(auth.RoleManageFirmware)
)

type rangeView struct {
	Range    string    `json:"range" doc:"today, 7d, 30d, 90d, or custom."`
	From     string    `json:"from" doc:"The range's first day, YYYY-MM-DD, in the site timezone."`
	To       string    `json:"to" doc:"The range's last day, YYYY-MM-DD, in the site timezone."`
	Start    time.Time `json:"start" doc:"When the first day starts."`
	End      time.Time `json:"end" doc:"When the day after the last starts; the range ends just before."`
	Timezone string    `json:"timezone" doc:"The site timezone, in which days, weekdays, and hours are read."`
}

type dayView struct {
	Date    string `json:"date" doc:"The day, YYYY-MM-DD."`
	Allowed int    `json:"allowed" doc:"Entries allowed that day."`
	Denied  int    `json:"denied" doc:"Entries denied that day."`
}

type weekdayView struct {
	Weekday string `json:"weekday" doc:"The weekday: mon, tue, wed, thu, fri, sat, or sun."`
	Hours   []int  `json:"hours" doc:"Entries in each hour of the day, from 00:00 to 23:00."`
	Total   int    `json:"total" doc:"Entries on the weekday in all."`
}

type reasonView struct {
	Reason string `json:"reason" doc:"Why entries were denied."`
	Count  int    `json:"count" doc:"How many were denied for it."`
}

type doorEntriesView struct {
	Door    nameRef `json:"door" doc:"The door, by id and name."`
	Deleted bool    `json:"deleted" doc:"True for a deleted door, listed only when it had entries."`
	Allowed int     `json:"allowed" doc:"Entries allowed at the door."`
	Denied  int     `json:"denied" doc:"Entries denied at the door."`
	Total   int     `json:"total" doc:"Entries at the door in all."`
}

type entryStats struct {
	Range    rangeView         `json:"range" doc:"The range summed up."`
	Total    int               `json:"total" doc:"How many codes were entered at the doors, each a code check."`
	Allowed  int               `json:"allowed" doc:"How many entries were allowed."`
	Denied   int               `json:"denied" doc:"How many entries were denied."`
	Users    int               `json:"users" doc:"How many people entered a code that is theirs, each counted once."`
	Days     []dayView         `json:"days" doc:"Every day of the range, oldest first."`
	Weekdays []weekdayView     `json:"weekdays" doc:"Every weekday, Monday first, with its entries by hour of day."`
	Hours    []int             `json:"hours" doc:"Entries in each hour of the day over every weekday, from 00:00 to 23:00."`
	Reasons  []reasonView      `json:"reasons" doc:"Why entries were denied, most often first."`
	Doors    []doorEntriesView `json:"doors" doc:"Every door, most entries first; deleted doors only when they had entries."`
}

type trendView struct {
	Start     time.Time `json:"start" doc:"When the hour or day starts."`
	Hours     int       `json:"hours" doc:"How many of its hours the controller reported in."`
	Samples   int       `json:"samples" doc:"How many reports the controller made in it."`
	Signal    float64   `json:"signal" doc:"The mean signal strength over the reports, in dBm."`
	LatencyMS float64   `json:"latency_ms" doc:"The mean round trip to the server over the reports, in milliseconds."`
	Opens     int       `json:"opens" doc:"How often the door was opened in it."`
}

type doorHealthView struct {
	Door      nameRef     `json:"door" doc:"The door, by id and name."`
	Uptime    *float64    `json:"uptime,omitempty" doc:"The share, from 0 to 1, of the range's ended hours in which the controller reported telemetry; absent until an hour of the range has ended."`
	Hours     int         `json:"hours" doc:"How many of the range's hours the controller reported in."`
	Samples   int         `json:"samples" doc:"How many reports the controller made."`
	Opens     int         `json:"opens" doc:"How often the door was opened, as the controller counted, open mode included."`
	Signal    *float64    `json:"signal,omitempty" doc:"The mean signal strength over the reports, in dBm; absent without reports."`
	LatencyMS *float64    `json:"latency_ms,omitempty" doc:"The mean round trip over the reports, in milliseconds; absent without reports."`
	Trend     []trendView `json:"trend" doc:"Each hour (ranges of up to 7 days) or day in which the controller reported, oldest first."`
}

type doorStats struct {
	Range rangeView        `json:"range" doc:"The range summed up."`
	Unit  string           `json:"unit" doc:"hour or day: how long each step of the trends is."`
	Hours int              `json:"hours" doc:"How many whole hours of the range have ended, which uptime is a share of."`
	Doors []doorHealthView `json:"doors" doc:"Every door that is not deleted and has a controller, in id order."`
}

type versionView struct {
	Version string `json:"version" doc:"A firmware version, as the controllers report it."`
	Doors   int    `json:"doors" doc:"How many doors' controllers last reported it."`
}

type firmwareStats struct {
	Versions []versionView `json:"versions" doc:"Each version the doors that are not deleted run, most doors first."`
}

type liveCounts struct {
	OpenRequests       *int `json:"open_requests,omitempty" doc:"Access requests waiting for an answer; only for manage-users."`
	DoorsOpen          *int `json:"doors_open,omitempty" doc:"Doors open now, as their controllers said; only for manage-doors or door-commander."`
	UnresponsiveDoors  *int `json:"unresponsive_doors,omitempty" doc:"Doors whose controller is connected but has not been heard from for the unresponsive_minutes setting; only for manage-doors or door-commander."`
	PendingEnrollments *int `json:"pending_enrollments,omitempty" doc:"Controllers waiting to be approved as doors; only for manage-doors."`
	PendingUpdates     *int `json:"pending_updates,omitempty" doc:"Doors whose controller does not run its target firmware yet; only for manage-firmware."`
}

// rangeParams are the query parameters that choose a dashboard range.
var rangeParams = []openapi.Param{
	{Name: "range", In: "query", Description: "today, 7d (the default), 30d, or 90d: that many days, today included, in the site timezone.", Example: "30d"},
	{Name: "from", In: "query", Description: "With to, in place of range: the first day of a range of up to 366 days, YYYY-MM-DD, in the site timezone.", Example: "2026-03-01"},
	{Name: "to", In: "query", Description: "With from: the range's last day, YYYY-MM-DD.", Example: "2026-03-31"},
}

// badStatsRange answers a range that cannot be read.
var badStatsRange = openapi.Response{
	Status: http.StatusBadRequest, Description: "range names no range, from or to is not a date, to is before from, or the range spans more than 366 days.", Body: errorBody{},
}

func (a *App) dashboardRoutes() []route {
	return []route{
		apiRoute(http.MethodGet, "/stats/now", administers, a.getLiveCounts, apiDoc{
			id: "getLiveCounts", summary: "Count what waits for attention now", tag: "stats",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The dashboard's live counts, each only for the roles that see it.", Body: liveCounts{}}},
		}),
		apiRoute(http.MethodGet, "/stats/entries", readsLog, a.getEntryStats, apiDoc{
			id: "getEntryStats", summary: "Sum up the entries at the doors over a range", tag: "stats", params: rangeParams,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The code checks of the range summed up.", Body: entryStats{}}, badStatsRange},
		}),
		apiRoute(http.MethodGet, "/stats/doors", opensDoors, a.getDoorStats, apiDoc{
			id: "getDoorStats", summary: "Sum up the doors' uptime and telemetry over a range", tag: "stats", params: rangeParams,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Each door's uptime and telemetry trends over the range, from the hourly rollups.", Body: doorStats{}}, badStatsRange},
		}),
		apiRoute(http.MethodGet, "/stats/firmware", runsFirmware, a.getFirmwareStats, apiDoc{
			id: "getFirmwareStats", summary: "Count the doors running each firmware version", tag: "stats",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The firmware versions the doors run.", Body: firmwareStats{}}},
		}),
	}
}

// chooseRange reads the range r asks for when it is now in zone.
func chooseRange(r *http.Request, now time.Time, zone *time.Location) (stats.Range, error) {
	q := r.URL.Query()
	return stats.Choose(stats.Choice{Preset: q.Get("range"), From: q.Get("from"), To: q.Get("to")}, now, zone)
}

// apiRange reads the range an API request asks for, in the site
// timezone, answering it when the range cannot be read.
func (a *App) apiRange(w http.ResponseWriter, r *http.Request) (stats.Range, *time.Location, bool) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.internalError(w, err)
		return stats.Range{}, nil, false
	}
	chosen, err := chooseRange(r, a.clock.Now(), current.Zone)
	if err != nil {
		a.refuse(w, err, nil)
		return stats.Range{}, nil, false
	}
	return chosen, current.Zone, true
}

func viewOfRange(r stats.Range, zone *time.Location) rangeView {
	return rangeView{
		Range: r.Preset, From: r.First.Format(schedule.DateLayout), To: r.Last.Format(schedule.DateLayout),
		Start: r.From.UTC(), End: r.To.UTC(), Timezone: zone.String(),
	}
}

func (a *App) getEntryStats(w http.ResponseWriter, r *http.Request, _ auth.User) {
	chosen, zone, ok := a.apiRange(w, r)
	if !ok {
		return
	}
	e, err := a.stats.Entries(r.Context(), chosen, zone)
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfEntries(chosen, zone, e))
}

func viewOfEntries(chosen stats.Range, zone *time.Location, e stats.Entries) entryStats {
	view := entryStats{
		Range: viewOfRange(chosen, zone), Total: e.Total(), Allowed: e.Allowed, Denied: e.Denied, Users: e.Users, Hours: e.Hours[:],
		Days: []dayView{}, Weekdays: []weekdayView{}, Reasons: []reasonView{}, Doors: []doorEntriesView{},
	}
	for _, d := range e.Days {
		view.Days = append(view.Days, dayView{Date: d.Date.Format(schedule.DateLayout), Allowed: d.Allowed, Denied: d.Denied})
	}
	for _, d := range e.Weekdays {
		view.Weekdays = append(view.Weekdays, weekdayView{Weekday: schedule.DayNames[schedule.DayIndex(d.Day)], Hours: d.Hours[:], Total: d.Total})
	}
	for _, reason := range e.Reasons {
		view.Reasons = append(view.Reasons, reasonView(reason))
	}
	for _, d := range e.Doors {
		view.Doors = append(view.Doors, doorEntriesView{Door: nameRef{ID: d.ID, Name: d.Name}, Deleted: d.Deleted, Allowed: d.Allowed, Denied: d.Denied, Total: d.Total()})
	}
	return view
}

func (a *App) getDoorStats(w http.ResponseWriter, r *http.Request, _ auth.User) {
	chosen, zone, ok := a.apiRange(w, r)
	if !ok {
		return
	}
	h, err := a.stats.Health(r.Context(), chosen, a.clock.Now(), zone)
	if err != nil {
		a.internalError(w, err)
		return
	}
	view := doorStats{Range: viewOfRange(chosen, zone), Unit: h.Unit, Hours: h.Hours, Doors: []doorHealthView{}}
	for _, d := range h.Doors {
		view.Doors = append(view.Doors, viewOfDoorHealth(h, d))
	}
	writeJSON(w, http.StatusOK, view)
}

func viewOfDoorHealth(h stats.Health, d stats.DoorHealth) doorHealthView {
	view := doorHealthView{Door: nameRef{ID: d.ID, Name: d.Name}, Hours: d.Hours, Samples: d.Samples, Opens: d.Opens, Trend: []trendView{}}
	if uptime, ok := h.Uptime(d); ok {
		view.Uptime = &uptime
	}
	view.Signal, view.LatencyMS = d.Signal, d.Latency
	for _, t := range d.Trend {
		view.Trend = append(view.Trend, trendView{Start: t.Start.UTC(), Hours: t.Hours, Samples: t.Samples, Signal: t.Signal, LatencyMS: t.Latency, Opens: t.Opens})
	}
	return view
}

func (a *App) getFirmwareStats(w http.ResponseWriter, r *http.Request, _ auth.User) {
	versions, err := a.stats.Versions(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	view := firmwareStats{Versions: []versionView{}}
	for _, v := range versions {
		view.Versions = append(view.Versions, versionView(v))
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *App) getLiveCounts(w http.ResponseWriter, r *http.Request, caller auth.User) {
	counts, err := a.liveCounts(r.Context(), caller)
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

// liveCounts is what waits for attention now, of what viewer may see.
func (a *App) liveCounts(ctx context.Context, viewer auth.User) (liveCounts, error) {
	var counts liveCounts
	var err error
	if holds(viewer, answersRequests) {
		counts.OpenRequests, err = counted(a.requests.Waiting(ctx))
	}
	if err == nil && holds(viewer, opensDoors) {
		counts.DoorsOpen, counts.UnresponsiveDoors, err = a.doorAlarms(ctx)
	}
	if err == nil && holds(viewer, enrollsDoors) {
		waiting := len(a.controllers.Pending.List())
		counts.PendingEnrollments = &waiting
	}
	if err == nil && holds(viewer, runsFirmware) {
		counts.PendingUpdates, err = a.pendingUpdates(ctx)
	}
	return counts, err
}

// counted is n, which err may say could not be counted.
func counted(n int, err error) (*int, error) {
	return &n, err
}

// doorAlarms counts the doors that are not deleted and are open now, and
// those that are unresponsive, as the door list's badges say.
func (a *App) doorAlarms(ctx context.Context) (*int, *int, error) {
	doors, err := a.doors.Doors(ctx)
	open, unresponsive := 0, 0
	for _, d := range doors {
		if d.DeletedAt != nil || d.Controller == nil {
			continue
		}
		badges := telemetry.Badges(*d.Controller)
		if slices.Contains(badges, telemetry.BadgeOpen) || slices.Contains(badges, telemetry.BadgeHeldOpen) {
			open++
		}
		if slices.Contains(badges, telemetry.BadgeUnresponsive) {
			unresponsive++
		}
	}
	return &open, &unresponsive, err
}

// pendingUpdates counts the doors taking part in the rollout that do not
// run their target yet.
func (a *App) pendingUpdates(ctx context.Context) (*int, error) {
	plan, err := a.firmware.Plan(ctx)
	pending := 0
	for _, d := range plan.Doors {
		if d.Status != "" && d.Status != firmware.Done {
			pending++
		}
	}
	return &pending, err
}
