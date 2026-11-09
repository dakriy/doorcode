package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/date"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/schedule"
	"github.com/dakriy/doorcode/internal/timeline"
)

func (a *App) calendarPageRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, "/admin/doors/{id}/calendar", opensDoors, a.doorCalendarPage),
		pageRoute(http.MethodGet, "/admin/users/{id}/doors/{door}/calendar", holding(auth.RoleManageUsers), a.accessCalendarPage),
	}
}

// week is the week a calendar page shows and the links to its neighbours.
type week struct {
	// Monday is the day the week starts on, and Today the date it is now,
	// both as YYYY-MM-DD.
	Monday string
	Today  string
	// Heading names the week.
	Heading  string
	Previous string
	Next     string
	This     string
	Zone     string
	// Events is what the calendar draws, as JSON, in local time.
	Events string
	// days is the week's days, Monday to Sunday.
	days dayRange
}

// weekOf is the week the request's week query parameter falls in, by
// default the one it is now in zone, on the calendar page at path.
func (a *App) weekOf(r *http.Request, zone *time.Location, path string) week {
	today := a.today(zone)
	day, ok := queryDate(r, "week", today)
	if !ok {
		day = today
	}
	first := monday(day)
	link := func(day time.Time) string { return path + "?week=" + day.Format(date.Layout) }
	return week{
		Monday: first.Format(date.Layout), Today: today.Format(date.Layout),
		Heading:  "Week of " + first.Format(longDateLayout),
		Previous: link(first.AddDate(0, 0, -7)), Next: link(first.AddDate(0, 0, 7)), This: path,
		Zone: zone.String(), days: dayRange{zone: zone, first: first, last: first.AddDate(0, 0, 6)},
	}
}

// The kinds of event a calendar draws, each in its own color.
const (
	eventOpen   = timeline.Open
	eventLocked = timeline.Locked
	eventAccess = "access"
	eventCode   = "code"
)

// calendarEvent is one event as the calendar script reads it: local times
// without an offset, which it shows as they are, a title, and its kind.
type calendarEvent struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

func eventOf(in schedule.Interval, title, kind string) calendarEvent {
	return calendarEvent{Start: in.Start.Format(localLayout), End: in.End.Format(localLayout), Title: title, Kind: kind}
}

func eventsText(events []calendarEvent) string {
	encoded, _ := json.Marshal(events)
	return string(encoded)
}

// occurrenceRow is one interval as a calendar page lists it.
type occurrenceRow struct {
	Start  string
	End    string
	Length string
}

// rowOfOccurrence lists in, whose times are in the zone to show.
func rowOfOccurrence(in schedule.Interval) occurrenceRow {
	return occurrenceRow{Start: in.Start.Format(occurrenceLayout), End: in.End.Format(occurrenceLayout), Length: lengthText(in.End.Sub(in.Start))}
}

// spanRow is one span of a timeline as a calendar page lists it.
type spanRow struct {
	occurrenceRow
	State string
}

// stateLabels names each state of a timeline on pages.
var stateLabels = map[string]string{timeline.Open: "Open mode", timeline.Locked: "Locked"}

// inZone is in with its times in zone.
func inZone(in schedule.Interval, zone *time.Location) schedule.Interval {
	return schedule.Interval{Start: in.Start.In(zone), End: in.End.In(zone)}
}

// timelineRows lists spans, with their times in zone, and adds them to
// events.
func timelineRows(spans []timeline.Span, zone *time.Location, events []calendarEvent) ([]spanRow, []calendarEvent) {
	rows := []spanRow{}
	for _, s := range spans {
		in := inZone(schedule.Interval{Start: s.Start, End: s.End}, zone)
		rows = append(rows, spanRow{occurrenceRow: rowOfOccurrence(in), State: stateLabels[s.State]})
		events = append(events, eventOf(in, stateLabels[s.State], s.State))
	}
	return rows, events
}

// windowRow is a time an access rule lets its members in, as a door's
// calendar lists it.
type windowRow struct {
	occurrenceRow
	Group    nameRef
	Schedule nameRef
}

// doorCalendarContent is the door calendar page's content.
type doorCalendarContent struct {
	Calendar week
	Door     connector.StoredDoor
	Row      doorRow
	Spans    []spanRow
	Windows  []windowRow
}

func (a *App) doorCalendarPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	wk := a.weekOf(r, zone, doorPath(pathID(r, "id"))+"/calendar")
	d, spans, err := a.doors.Timeline(r.Context(), pathID(r, "id"), zone, wk.days.from(), wk.days.to())
	if a.unloaded(w, r, viewer, err, door.ErrNoDoor, noDoorHere) {
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	entrants, err := a.rules.Entrants(r.Context(), d)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := doorCalendarContent{Calendar: wk, Door: d, Row: n.rowOfDoor(d), Windows: []windowRow{}}
	var events []calendarEvent
	content.Spans, events = timelineRows(spans, zone, []calendarEvent{})
	var reaching []connector.StoredRule
	for _, reach := range entrants {
		reaching = append(reaching, reach.Rule)
	}
	for _, window := range rule.Windows(reaching, n.schedules, zone, wk.days.from(), wk.days.to()) {
		in := inZone(window.Interval, zone)
		group := n.group(window.Rule.GroupID)
		content.Windows = append(content.Windows, windowRow{occurrenceRow: rowOfOccurrence(in), Group: group, Schedule: n.schedule(window.Rule.ScheduleID)})
		events = append(events, eventOf(in, group.Name, eventAccess))
	}
	content.Calendar.Events = eventsText(events)
	a.render(w, r, http.StatusOK, doorCalendarTemplate, d.Name, &viewer, content)
}

// accessCalendarContent is the content of the page showing when a user's
// code works at a door.
type accessCalendarContent struct {
	Calendar week
	User     userView
	Door     connector.StoredDoor
	Reached  bool
	Works    []occurrenceRow
	Spans    []spanRow
}

func (a *App) accessCalendarPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	u, content, ok := a.pageUser(w, r, viewer)
	if !ok {
		return
	}
	zone := content.zone
	doorID := pathID(r, "door")
	wk := a.weekOf(r, zone, userPath(u.ID)+"/doors/"+strconv.FormatInt(doorID, 10)+"/calendar")
	d, spans, err := a.doors.Timeline(r.Context(), doorID, zone, wk.days.from(), wk.days.to())
	if a.unloaded(w, r, viewer, err, door.ErrNoDoor, noDoorHere) {
		return
	}
	reachable, err := a.rules.UserAccess(r.Context(), u.Groups, zone, wk.days.from(), wk.days.to())
	if err != nil {
		a.pageError(w, err)
		return
	}
	calendar := accessCalendarContent{Calendar: wk, User: a.viewOfUser(u), Door: d, Works: []occurrenceRow{}}
	events := []calendarEvent{}
	i := slices.IndexFunc(reachable, func(da rule.DoorAccess) bool { return da.Door.ID == d.ID })
	calendar.Reached = i >= 0
	if calendar.Reached {
		for _, in := range reachable[i].Works {
			in = inZone(in, zone)
			calendar.Works = append(calendar.Works, rowOfOccurrence(in))
			events = append(events, eventOf(in, "Code works", eventCode))
		}
	}
	calendar.Spans, events = timelineRows(spans, zone, events)
	calendar.Calendar.Events = eventsText(events)
	a.render(w, r, http.StatusOK, accessCalendarTemplate, u.DisplayName+" at "+d.Name, &viewer, calendar)
}

// accessRow is one door a user may enter, as their page lists it.
type accessRow struct {
	Door   nameRef
	Groups string
}

// accessRows lists the doors the groups of u let them enter.
func (a *App) accessRows(r *http.Request, u connector.StoredUser) ([]accessRow, error) {
	reachable, err := a.rules.Reachable(r.Context(), u.Groups)
	if err != nil {
		return nil, err
	}
	rows := []accessRow{}
	for _, da := range reachable {
		var groups []string
		for _, g := range u.Groups {
			if slices.ContainsFunc(da.Rules, func(granting connector.StoredRule) bool { return granting.GroupID == g.ID }) {
				groups = append(groups, g.Name)
			}
		}
		rows = append(rows, accessRow{Door: doorRef(da.Door), Groups: strings.Join(groups, ", ")})
	}
	return rows, nil
}
