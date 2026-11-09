package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
)

type spanView struct {
	Start time.Time `json:"start" doc:"When the span starts, with the site timezone's offset."`
	End   time.Time `json:"end" doc:"When it ends."`
	State string    `json:"state" doc:"open: the door is in open mode, from a schedule or an open override, and any keypress unlocks it. locked: a locked override keeps it locked."`
}

type timelineView struct {
	TimeZone string     `json:"time_zone" doc:"The site timezone schedules are read in."`
	From     string     `json:"from" doc:"The first day covered, YYYY-MM-DD."`
	To       string     `json:"to" doc:"The last day covered, YYYY-MM-DD."`
	Spans    []spanView `json:"spans" doc:"When the door is open or locked during those days, in order; between spans it is in its normal state, where a code opens it. Locked overrides win over open overrides, which win over open-mode schedules."`
}

type doorAccessView struct {
	Door    nameRef          `json:"door" doc:"The door."`
	RuleIDs []int64          `json:"rule_ids" doc:"The access rules naming one of the user's groups that reach the door."`
	Works   []occurrenceView `json:"works" doc:"When the keypad lets the user's code open the door during the days asked for: never once their account has ended, always for a door commander, and otherwise while the door is open or a rule's schedule is active and the door is not locked."`
}

type userAccessView struct {
	TimeZone string           `json:"time_zone" doc:"The site timezone schedules are read in."`
	From     string           `json:"from" doc:"The first day covered, YYYY-MM-DD."`
	To       string           `json:"to" doc:"The last day covered, YYYY-MM-DD."`
	Doors    []doorAccessView `json:"doors" doc:"Each door that is not deleted which an access rule naming one of the user's groups reaches, or every one for a door commander, by name."`
}

func dayParams(first openapi.Param) []openapi.Param {
	return append([]openapi.Param{first}, dayRangeParams()...)
}

// dayRangeParams are the query parameters naming the days an operation
// covers.
func dayRangeParams() []openapi.Param {
	return []openapi.Param{
		{Name: "from", In: "query", Description: "The first day to cover, YYYY-MM-DD in the site timezone; by default the Monday of the current week.", Example: "2026-11-09"},
		{Name: "to", In: "query", Description: fmt.Sprintf("The last day to cover, YYYY-MM-DD, at most %d days after from; by default the Sunday after from.", maxOccurrenceSpan), Example: "2026-11-15"},
	}
}

var badDays = openapi.Response{Status: http.StatusBadRequest, Description: "from or to is not a date, or the range is backwards or too long.", Body: errorBody{}}

func (a *App) timelineRoutes() []route {
	return []route{
		apiRoute(http.MethodGet, "/doors/{id}/timeline", holding(auth.RoleManageDoors), a.getTimeline, apiDoc{
			id: "getDoorTimeline", summary: "Read when a door is open or locked", tag: "doors", params: dayParams(doorIDParam),
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door's timeline during the days asked for.", Body: timelineView{}}, badDays, noDoor},
		}),
		apiRoute(http.MethodGet, "/users/{id}/access", holding(auth.RoleManageUsers), a.getUserAccess, apiDoc{
			id: "getUserAccess", summary: "Read where and when a user's code works", tag: "users", params: dayParams(userIDParam),
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The doors the user's groups let them enter, and when.", Body: userAccessView{}}, badDays,
				{Status: http.StatusNotFound, Description: "No user has this id.", Body: errorBody{}},
			},
		}),
	}
}

func (a *App) getTimeline(w http.ResponseWriter, r *http.Request, _ auth.User) {
	days, ok := a.apiDays(w, r)
	if !ok {
		return
	}
	_, spans, err := a.doors.Timeline(r.Context(), pathID(r, "id"), days.zone, days.from(), days.to())
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	views := []spanView{}
	for _, s := range spans {
		views = append(views, spanView{Start: s.Start.In(days.zone), End: s.End.In(days.zone), State: s.State})
	}
	writeJSON(w, http.StatusOK, timelineView{TimeZone: days.zone.String(), From: days.firstText(), To: days.lastText(), Spans: views})
}

func (a *App) getUserAccess(w http.ResponseWriter, r *http.Request, _ auth.User) {
	days, ok := a.apiDays(w, r)
	if !ok {
		return
	}
	u, err := a.directory.User(r.Context(), pathID(r, "id"))
	if err != nil {
		a.directoryError(w, r, err)
		return
	}
	doors, err := a.decisions.UserAccess(r.Context(), u, days.zone, days.from(), days.to())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []doorAccessView{}
	for _, d := range doors {
		view := doorAccessView{Door: doorRef(d.Door), RuleIDs: []int64{}, Works: []occurrenceView{}}
		for _, granting := range d.Rules {
			view.RuleIDs = append(view.RuleIDs, granting.ID)
		}
		for _, in := range d.Works {
			view.Works = append(view.Works, occurrenceView{Start: in.Start.In(days.zone), End: in.End.In(days.zone)})
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, userAccessView{TimeZone: days.zone.String(), From: days.firstText(), To: days.lastText(), Doors: views})
}
