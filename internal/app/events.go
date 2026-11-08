package app

import (
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/openapi"
)

// eventsPerPage is how many events a page lists unless the API caller
// asks for another number.
const (
	eventsPerPage   = 50
	maxEventsAPIPer = 500
)

type eventView struct {
	ID        int64     `json:"id" doc:"Names the event."`
	At        time.Time `json:"at" doc:"When it happened."`
	Door      nameRef   `json:"door" doc:"The door, by id and name; deleted doors keep their events."`
	Type      string    `json:"type" doc:"check (a code entered at the keypad), command (a keypad command carried out), open (opened from the admin UI or the API), or alarm (the door forced open or held open)."`
	Outcome   string    `json:"outcome" doc:"allowed or denied, or alert for an alarm."`
	Reason    string    `json:"reason" doc:"Why, such as access rule, open mode, no rule, locked, expired, rate limited, unreachable, forced open, or held open."`
	User      *nameRef  `json:"user,omitempty" doc:"The user whose code it was, or who gave the command or opened the door, by id and display name; absent for a code nobody has."`
	RuleID    *int64    `json:"rule_id,omitempty" doc:"The access rule that let the user in; absent unless one did."`
	RequestID string    `json:"request_id,omitempty" doc:"The id the door gave the code check; absent for other events."`
	Detail    string    `json:"detail,omitempty" doc:"More about it in words, such as what a command did."`
}

type eventList struct {
	Events  []eventView `json:"events" doc:"This page of events, newest first."`
	Total   int         `json:"total" doc:"How many events the query selects, on every page together."`
	Page    int         `json:"page" doc:"This page's number, from 1."`
	PerPage int         `json:"per_page" doc:"How many events a page holds."`
}

// opensDoors may open doors from the UI and manage their overrides.
var opensDoors = holding(auth.RoleManageDoors, auth.RoleDoorCommander)

func (a *App) eventRoutes() []route {
	params := []openapi.Param{
		{Name: "door", In: "query", Description: "Keeps the events of the door with this id.", Example: int64(1)},
		{Name: "user", In: "query", Description: "Keeps the events of the user with this id.", Example: int64(2)},
		{Name: "type", In: "query", Description: "Keeps the events of this type: check, command, open, or alarm.", Example: event.Check},
		{Name: "outcome", In: "query", Description: "Keeps the events with this outcome: allowed, denied, or alert.", Example: event.Denied},
		{Name: "reason", In: "query", Description: "Keeps the events with this reason.", Example: event.NoRule},
		{Name: "from", In: "query", Description: "Keeps the events at or after this RFC 3339 time.", Example: "2026-01-05T00:00:00Z"},
		{Name: "to", In: "query", Description: "Keeps the events before this RFC 3339 time.", Example: "2026-01-06T00:00:00Z"},
		{Name: "page", In: "query", Description: "The page to return, from 1 (the default).", Example: 1},
		{Name: "per_page", In: "query", Description: fmt.Sprintf("How many events a page holds, from 1 to %d; %d by default.", maxEventsAPIPer, eventsPerPage), Example: eventsPerPage},
	}
	return []route{
		apiRoute(http.MethodGet, "/events", holding(auth.RoleViewLogs), a.listEvents, apiDoc{
			id: "listEvents", summary: "List events", tag: "events", params: params,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "A page of the events the query selects, newest first.", Body: eventList{}},
				{Status: http.StatusBadRequest, Description: "A query parameter is not one this operation takes; the error says which.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/open", opensDoors, a.openDoorNow, apiDoc{
			id: "openDoor", summary: "Release a door's strike", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The door was told to release its strike for its strike pulse; the event logging it, naming the caller.", Body: eventView{}},
				noDoor,
				{Status: http.StatusConflict, Description: "The door's controller is not connected.", Body: errorBody{}},
			},
		}),
	}
}

func viewOfEvent(e connector.StoredEvent) eventView {
	view := eventView{
		ID: e.ID, At: e.At.UTC(), Door: nameRef{ID: e.DoorID, Name: e.DoorName}, Type: e.Type, Outcome: e.Outcome, Reason: e.Reason,
		RuleID: e.RuleID, RequestID: e.RequestID, Detail: e.Detail,
	}
	if e.UserID != nil {
		view.User = &nameRef{ID: *e.UserID, Name: e.UserName}
	}
	return view
}

// readEventFilter reads the door, user, type, outcome, and reason query
// parameters, and reports whether each is absent or one there can be.
func readEventFilter(r *http.Request) (connector.EventQuery, bool) {
	query := r.URL.Query()
	doorID, doorOK := queryID(r, "door")
	userID, userOK := queryID(r, "user")
	q := connector.EventQuery{DoorID: doorID, UserID: userID, Type: query.Get("type"), Outcome: query.Get("outcome"), Reason: query.Get("reason")}
	idsOK := doorOK && userOK
	namesOK := oneOf(q.Type, event.Types) && oneOf(q.Outcome, event.Outcomes) && oneOf(q.Reason, event.Reasons)
	return q, idsOK && namesOK
}

// oneOf reports whether value is empty or among values.
func oneOf(value string, values []string) bool {
	return value == "" || slices.Contains(values, value)
}

// queryTime reads the query parameter name as an RFC 3339 time, nil when
// it is absent.
func queryTime(r *http.Request, name string) (*time.Time, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, value)
	return &t, err == nil
}

func (a *App) listEvents(w http.ResponseWriter, r *http.Request, _ auth.User) {
	q, filterOK := readEventFilter(r)
	from, fromOK := queryTime(r, "from")
	to, toOK := queryTime(r, "to")
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	perPage, perPageOK := queryNumber(r, "per_page", eventsPerPage, 1, maxEventsAPIPer)
	if !filterOK || !fromOK || !toOK || !pageOK || !perPageOK {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("door and user must be ids, type, outcome, and reason ones events have, from and to RFC 3339 times, "+
			"page a whole number from 1, and per_page one from 1 to %d", maxEventsAPIPer))
		return
	}
	q.From, q.To = from, to
	found, err := a.events.Find(r.Context(), event.Query{EventQuery: q, Page: page, PerPage: perPage})
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := []eventView{}
	for _, e := range found.Events {
		views = append(views, viewOfEvent(e))
	}
	writeJSON(w, http.StatusOK, eventList{Events: views, Total: found.Total, Page: page, PerPage: perPage})
}

func (a *App) openDoorNow(w http.ResponseWriter, r *http.Request, caller auth.User) {
	e, err := a.openDoor(r.Context(), pathID(r, "id"), caller)
	if err != nil {
		a.doorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfEvent(e))
}
