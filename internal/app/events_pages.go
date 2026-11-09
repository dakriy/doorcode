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
	"github.com/dakriy/doorcode/internal/date"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/event"
)

// recentEvents is how many events a door's or a user's page shows.
const recentEvents = 10

func (a *App) eventPageRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, "/admin/events", holding(auth.RoleViewLogs), a.eventsPage),
		pageRoute(http.MethodPost, "/admin/doors/{id}/open", opensDoors, a.openDoorFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/reboot", opensDoors, a.rebootDoorFromForm),
	}
}

// typeLabels names each type of event on pages.
var typeLabels = map[string]string{
	event.Check: "Code check", event.Command: "Door command", event.Open: "Opened from the UI", event.Alarm: "Door alarm",
	event.Update: "Firmware update",
}

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
		At: shownAt(zone, e.At, secondLayout), Door: nameRef{ID: e.DoorID, Name: e.DoorName}, Type: typeLabels[e.Type],
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

// recent is the newest events q selects, with a link to the events page
// filtered as q is, for a door's or a user's page, when viewer may read
// the log, and nil otherwise.
func (a *App) recent(ctx context.Context, viewer auth.User, q connector.EventQuery, zone *time.Location) (*recentLog, error) {
	if !viewer.Has(auth.RoleViewLogs) {
		return nil, nil
	}
	found, err := a.events.Find(ctx, event.Query{EventQuery: q, Page: 1, PerPage: recentEvents})
	link := eventFilters{EventQuery: q}.link(0)
	return &recentLog{Rows: rowsOfEvents(found.Events, zone), Link: link, OfUser: q.UserID != 0}, err
}

func optionsOf(values []string, labels func(string) string, chosen string) []selectOption {
	out := []selectOption{}
	for _, v := range values {
		out = append(out, selectOption{Value: v, Label: labels(v), Selected: v == chosen})
	}
	return out
}

// eventFilters is the events page's filters as entered: dates as a date
// input writes them in the site timezone.
type eventFilters struct {
	connector.EventQuery
	FromDate string
	ToDate   string
	// Who is what was typed in the user filter's text box when nobody has
	// it as their email or user id.
	Who  string
	Page int
}

// eventsContent is the events page's content.
type eventsContent struct {
	Filters  eventFilters
	Doors    []selectOption
	Types    []selectOption
	Outcomes []selectOption
	Reasons  []selectOption
	// User is the user the events are filtered by, though they are
	// deleted or nobody has their id; nil when they are not.
	User *nameRef
	// WithoutUser is the events page with the same filters but the user.
	WithoutUser string
	Rows        []eventRow
	// Empty says why the list is empty, and Filtered whether filters are
	// set, so the page can offer to clear them.
	Empty    string
	Filtered bool
	pager
	// Alert says filters or a page number that could not be read were
	// left out.
	Alert string
}

// readEventFilters reads the events page's filters, their dates in zone,
// and reports whether the filters could be read and whether the page
// number could. A page number that cannot be read is the first page.
func readEventFilters(r *http.Request, zone *time.Location) (eventFilters, bool, bool) {
	q, filterOK := readEventFilter(r)
	f := eventFilters{EventQuery: q, FromDate: r.URL.Query().Get("from"), ToDate: r.URL.Query().Get("to"), Page: 1}
	from, fromErr := formDate(f.FromDate, zone)
	to, toErr := dayAfter(f.ToDate, zone)
	f.From, f.To = from, to
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	if pageOK {
		f.Page = page
	}
	return f, filterOK && fromErr == nil && toErr == nil, pageOK
}

// eventFiltersOf reads the events page's filters, their dates in zone,
// and the email or user id typed in its user filter's text box, which
// names the user it filters by in place of the user id the form holds,
// and says what could not be read or found and was left out.
func (a *App) eventFiltersOf(r *http.Request, zone *time.Location) (eventFilters, string, error) {
	f, filtersOK, pageOK := readEventFilters(r, zone)
	if !filtersOK {
		return eventFilters{Page: 1}, "Some filters could not be read, so every event is shown.", nil
	}
	alert := ""
	if !pageOK {
		alert = "The page number could not be read, so the first page is shown."
	}
	typed := strings.TrimSpace(r.URL.Query().Get("who"))
	if typed == "" {
		return f, alert, nil
	}
	u, found, err := a.directory.FindUser(r.Context(), typed)
	if !found {
		f.UserID, f.Who = 0, typed
		return f, "Nobody has the email or user id you typed, so events of every user are shown.", err
	}
	f.UserID = u.ID
	return f, alert, nil
}

// link is the events page with the filters f holds, on page, or on the
// first when page is zero.
func (f eventFilters) link(page int) string {
	values := url.Values{}
	if page > 0 {
		values.Set("page", strconv.Itoa(page))
	}
	for name, value := range map[string]string{
		"door": idText(f.DoorID), "user": idText(f.UserID), "type": f.Type, "outcome": f.Outcome, "reason": f.Reason,
		"from": f.FromDate, "to": f.ToDate,
	} {
		if value != "" {
			values.Set(name, value)
		}
	}
	return "/admin/events?" + values.Encode()
}

// idText is id as a query parameter writes it, empty for zero.
func idText(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// dayAfter is the start, in zone, of the day after the date value names,
// so that events before it are those up to the end of that date; nil when
// value is empty.
func dayAfter(value string, zone *time.Location) (*time.Time, error) {
	day, err := formDate(value, zone)
	if day == nil {
		return nil, err
	}
	next := date.StartOf(date.Of(*day, zone).AddDate(0, 0, 1), zone)
	return &next, err
}

func (a *App) eventsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	f, alert, err := a.eventFiltersOf(r, zone)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := eventsContent{Alert: alert}
	found, err := a.events.Find(r.Context(), event.Query{EventQuery: f.EventQuery, Page: f.Page, PerPage: eventsPerPage})
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	doors, err := a.doors.Doors(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content.User, err = a.filterUser(r.Context(), f.UserID)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	withoutUser := f
	withoutUser.UserID = 0
	content.Filters, content.WithoutUser, content.Rows = f, withoutUser.link(0), rowsOfEvents(found.Events, zone)
	content.Filtered = f.EventQuery != connector.EventQuery{}
	content.Doors = doorOptions(doors, f.DoorID)
	content.Types = optionsOf(event.Types, func(t string) string { return typeLabels[t] }, f.Type)
	content.Outcomes = optionsOf(event.Outcomes, sentence, f.Outcome)
	content.Reasons = optionsOf(event.Reasons, sentence, f.Reason)
	content.Empty = emptyEvents(content.Filtered, found.Total)
	content.pager = pagerOf(f.Page, eventsPerPage, len(found.Events), found.Total, f.link)
	a.render(w, r, http.StatusOK, eventsTemplate, "Events", &viewer, content)
}

// doorOptions is every door as the door filter offers it, with the door
// whose id is chosen selected.
func doorOptions(doors []connector.StoredDoor, chosen int64) []selectOption {
	var out []selectOption
	for _, d := range doors {
		out = append(out, idOption(d.ID, d.Name, d.ID == chosen))
	}
	return out
}

// filterUser is the user whose id is id, by id and display name, for the
// filter a page shows; nil when id is zero.
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
	return "No events yet. Codes entered at keypads, keypad commands, doors opened from here, and door alarms are logged as they happen."
}

func (a *App) openDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.commandFromForm(w, r, viewer, a.openDoor, "the door cannot be opened", "door-opened")
}

func (a *App) rebootDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.commandFromForm(w, r, viewer, a.rebootDoor, "it cannot be rebooted", "door-rebooted")
}

// commandFromForm sends the door of the page the viewer submitted a form
// on a command, by send, and goes back to the page saying flash; while its
// controller is not connected, the page says that refused can only be
// done once it is.
func (a *App) commandFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User,
	send func(context.Context, int64, auth.User) (connector.StoredEvent, error), refused, flash string,
) {
	d, ok := a.pageDoor(w, r, viewer)
	if !ok {
		return
	}
	_, err := send(r.Context(), d.ID, viewer)
	if errors.Is(err, controller.ErrOffline) {
		a.showDoor(w, r, http.StatusConflict, viewer, d, d.fresh(), "The controller is not connected, so "+refused+" from here. Try again once it is online.")
		return
	}
	a.changed(w, r, err, doorPath(d.ID), flash)
}
