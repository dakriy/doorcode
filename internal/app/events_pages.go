package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/schedule"
)

// recentEvents is how many events a door's or a user's page shows.
const recentEvents = 10

// eventLayout is how pages write when an event happened.
const eventLayout = "Mon 2 Jan 2006 15:04:05"

func (a *App) eventPageRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, "/admin/events", holding(auth.RoleViewLogs), a.eventsPage),
		pageRoute(http.MethodPost, "/admin/doors/{id}/open", opensDoors, a.openDoorFromForm),
	}
}

// typeLabels names each type of event on pages.
var typeLabels = map[string]string{event.Check: "Code check", event.Command: "Keypad command", event.Open: "Opened from the UI"}

// eventRow is one event as pages list it.
type eventRow struct {
	At      string
	Door    nameRef
	User    *nameRef
	Type    string
	Outcome string
	Reason  string
	// RuleID is the access rule that let the user in, zero when none did.
	RuleID    int64
	RequestID string
	Detail    string
}

// sentence is s with its first letter upper case, as a badge shows it.
func sentence(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

func rowOfEvent(e connector.StoredEvent, zone *time.Location) eventRow {
	row := eventRow{
		At: e.At.In(zone).Format(eventLayout), Door: nameRef{ID: e.DoorID, Name: e.DoorName}, Type: typeLabels[e.Type],
		Outcome: sentence(e.Outcome), Reason: sentence(e.Reason), RequestID: e.RequestID, Detail: e.Detail,
	}
	if e.UserID != nil {
		row.User = &nameRef{ID: *e.UserID, Name: e.UserName}
	}
	if e.RuleID != nil {
		row.RuleID = *e.RuleID
	}
	return row
}

func rowsOfEvents(events []connector.StoredEvent, zone *time.Location) []eventRow {
	rows := []eventRow{}
	for _, e := range events {
		rows = append(rows, rowOfEvent(e, zone))
	}
	return rows
}

// recentLog is the newest events of a door or a user, as their page shows
// them.
type recentLog struct {
	Rows []eventRow
	// Link is the events page listing every one of them.
	Link string
	// OfUser is true for a user's events, which name their doors, and
	// false for a door's, which name their users.
	OfUser bool
}

// recent is the newest events q selects, which the events page lists at
// link, for a door's or a user's page, when viewer may read the log, and
// nil otherwise.
func (a *App) recent(ctx context.Context, viewer auth.User, q connector.EventQuery, link string, zone *time.Location) (*recentLog, error) {
	if !viewer.Has(auth.RoleViewLogs) {
		return nil, nil
	}
	found, err := a.events.Find(ctx, event.Query{EventQuery: q, Page: 1, PerPage: recentEvents})
	return &recentLog{Rows: rowsOfEvents(found.Events, zone), Link: link, OfUser: q.UserID != 0}, err
}

// filterOption is one option of a filter's select.
type filterOption struct {
	Value    string
	Label    string
	Selected bool
}

func optionsOf(values []string, labels func(string) string, chosen string) []filterOption {
	out := []filterOption{}
	for _, v := range values {
		out = append(out, filterOption{Value: v, Label: labels(v), Selected: v == chosen})
	}
	return out
}

// eventFilters is the events page's filters as entered: dates as a date
// input writes them in the site timezone.
type eventFilters struct {
	connector.EventQuery
	FromDate string
	ToDate   string
	Page     int
}

// eventsContent is the events page's content.
type eventsContent struct {
	Filters  eventFilters
	Doors    []filterOption
	Types    []filterOption
	Outcomes []filterOption
	Reasons  []filterOption
	// User is the user the events are filtered by, nil when they are not.
	User *nameRef
	Rows []eventRow
	// Empty says why the list is empty, and Filtered whether filters are
	// set, so the page can offer to clear them.
	Empty    string
	Filtered bool
	Total    int
	From, To int
	Previous string
	Next     string
	// Alert says filters that could not be read were cleared.
	Alert string
}

// readEventFilters reads the events page's filters, their dates in zone,
// and reports whether each could be read.
func readEventFilters(r *http.Request, zone *time.Location) (eventFilters, bool) {
	q, filterOK := readEventFilter(r)
	f := eventFilters{EventQuery: q, FromDate: r.URL.Query().Get("from"), ToDate: r.URL.Query().Get("to"), Page: 1}
	from, fromErr := formDate(f.FromDate, zone)
	to, toErr := formDate(f.ToDate, zone)
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	if to != nil {
		next := schedule.StartOf(schedule.DateOf(*to, zone).AddDate(0, 0, 1), zone)
		to = &next
	}
	f.From, f.To, f.Page = from, to, page
	return f, filterOK && fromErr == nil && toErr == nil && pageOK
}

func (a *App) eventsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	f, readable := readEventFilters(r, zone)
	content := eventsContent{}
	if !readable {
		f, content.Alert = eventFilters{Page: 1}, "Some filters could not be read, so every event is shown."
	}
	found, err := a.events.Find(r.Context(), event.Query{EventQuery: f.EventQuery, Page: f.Page, PerPage: eventsPerPage})
	if err != nil {
		a.pageError(w, err)
		return
	}
	doors, err := a.doors.Doors(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	user, err := a.filterUser(r.Context(), f.UserID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content.Filters, content.User, content.Rows, content.Total = f, user, rowsOfEvents(found.Events, zone), found.Total
	content.Filtered = f.EventQuery != connector.EventQuery{}
	content.Types = optionsOf(event.Types, func(t string) string { return typeLabels[t] }, f.Type)
	content.Outcomes = optionsOf(event.Outcomes, sentence, f.Outcome)
	content.Reasons = optionsOf(event.Reasons, sentence, f.Reason)
	for _, d := range doors {
		content.Doors = append(content.Doors, filterOption{Value: strconv.FormatInt(d.ID, 10), Label: d.Name, Selected: d.ID == f.DoorID})
	}
	content.From, content.To = (f.Page-1)*eventsPerPage+1, (f.Page-1)*eventsPerPage+len(found.Events)
	content.Empty = emptyEvents(content.Filtered, found.Total)
	if f.Page > 1 {
		content.Previous = eventsURL(r, f.Page-1)
	}
	if content.To < found.Total {
		content.Next = eventsURL(r, f.Page+1)
	}
	a.render(w, r, http.StatusOK, eventsTemplate, "Events", &viewer, content)
}

// filterUser is the user whose id is id, by id and display name, for the
// filter the events page shows; nil when id is zero.
func (a *App) filterUser(ctx context.Context, id int64) (*nameRef, error) {
	if id == 0 {
		return nil, nil
	}
	u, err := a.directory.User(ctx, id)
	if errors.Is(err, directory.ErrNoUser) {
		return &nameRef{ID: id, Name: "No such user"}, nil
	}
	return &nameRef{ID: id, Name: u.DisplayName}, err
}

// emptyEvents says why a page of events has none on it, when total events
// match its filters.
func emptyEvents(filtered bool, total int) string {
	if total > 0 {
		return "This page is past the last one."
	}
	if filtered {
		return "No events match these filters."
	}
	return "No events yet. Codes entered at keypads, keypad commands, and doors opened from here are logged as they happen."
}

// eventsURL is the events page r asked for, on page.
func eventsURL(r *http.Request, page int) string {
	values := url.Values{}
	for name, vs := range r.URL.Query() {
		if vs[0] != "" && name != "page" {
			values.Set(name, vs[0])
		}
	}
	values.Set("page", strconv.Itoa(page))
	return "/admin/events?" + values.Encode()
}

func (a *App) openDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	d, ok := a.pageDoor(w, r, viewer)
	if !ok {
		return
	}
	_, err := a.openDoor(r.Context(), d.ID, viewer)
	if errors.Is(err, controller.ErrOffline) {
		a.showDoor(w, r, http.StatusConflict, viewer, d, d.fresh(), "The controller is not connected, so the door cannot be opened from here. Try again once it is online.")
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "door-opened")
}
