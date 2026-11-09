package app

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/request"
	"github.com/dakriy/doorcode/internal/rule"
)

type ownCodeView struct {
	HasCode bool   `json:"has_code" doc:"True when you have a door code. The code itself is never shown."`
	Prefix  string `json:"prefix,omitempty" doc:"What every code you choose starts with: your user id without its leading zeros. Absent when you cannot choose one."`
	Refusal string `json:"refusal,omitempty" doc:"Why you cannot choose a code here; absent when you can."`
}

type ownCodeRequest struct {
	Digits string `json:"digits" doc:"The 4 to 8 digits that follow your prefix. Your code is the prefix followed by them."`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password,omitempty" doc:"Your password now; needed when you have one."`
	NewPassword     string `json:"new_password" doc:"The password to sign in with from now on, at least as long as the password_min_length setting says."`
}

type portalDoorView struct {
	Door         nameRef          `json:"door" doc:"The door."`
	Location     string           `json:"location" doc:"Where the door is; may be empty."`
	ThumbnailURL string           `json:"thumbnail_url,omitempty" doc:"Where a thumbnail of the door's primary picture is served; absent without one, and while the setting portal_door_pictures is false."`
	Reached      bool             `json:"reached" doc:"True when an access rule naming one of your groups reaches the door."`
	Works        []occurrenceView `json:"works" doc:"When your code opens the door during the days asked for: while such a rule's schedule is active and the door is not locked."`
	Spans        []spanView       `json:"spans" doc:"When the door is in open mode, and anyone gets in, or locked during those days, in order."`
}

type portalAccessView struct {
	TimeZone string           `json:"time_zone" doc:"The site timezone schedules are read in."`
	From     string           `json:"from" doc:"The first day covered, YYYY-MM-DD."`
	To       string           `json:"to" doc:"The last day covered, YYYY-MM-DD."`
	Doors    []portalDoorView `json:"doors" doc:"Every door the portal shows, by name."`
}

type requestView struct {
	ID         int64      `json:"id" doc:"Names the request."`
	Title      string     `json:"title" doc:"What the request is about."`
	Body       string     `json:"body" doc:"The request in your words; may be empty."`
	Reply      string     `json:"reply,omitempty" doc:"An administrator's answer; absent until there is one."`
	Resolved   bool       `json:"resolved" doc:"True once an administrator has resolved the request, which can then no longer be changed."`
	ResolvedAt *time.Time `json:"resolved_at,omitempty" doc:"When the request was resolved; absent while it is open."`
	CreatedAt  time.Time  `json:"created_at" doc:"When you made the request."`
	UpdatedAt  time.Time  `json:"updated_at" doc:"When you last changed it."`
}

type requestList struct {
	Requests []requestView `json:"requests" doc:"Your requests, newest first."`
}

type requestBody struct {
	Title string `json:"title" doc:"What the request is about, up to 120 characters."`
	Body  string `json:"body,omitempty" doc:"The request in your words, up to 4000 characters."`
}

// portalStatuses maps each refusal the portal reports to the status the
// API answers it with.
var portalStatuses = []refusalStatus{
	{directory.ErrNoUserID, http.StatusConflict},
	{directory.ErrUserIDNotDigits, http.StatusConflict},
	{auth.ErrPasswordsHidden, http.StatusForbidden},
	{request.ErrNoRequest, http.StatusNotFound},
	{request.ErrResolved, http.StatusConflict},
}

// portalError answers an API request with what err, from the portal's
// services, means.
func (a *App) portalError(w http.ResponseWriter, r *http.Request, err error) {
	a.refuse(w, r, err, portalStatuses)
}

var (
	requestIDParam = openapi.Param{Name: "id", In: "path", Description: "The request's id.", Example: int64(1)}
	noRequest      = openapi.Response{Status: http.StatusNotFound, Description: "You have no access request with this id.", Body: errorBody{}}
	resolvedOne    = openapi.Response{Status: http.StatusConflict, Description: "The request is resolved and can no longer be changed.", Body: errorBody{}}
	badRequestBody = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON request, or the title is missing or a field too long.", Body: errorBody{}}
)

func (a *App) portalRoutes() []route {
	example := requestBody{Title: "Lab access", Body: "I start in the electronics lab on Monday and need to get in from 8:00."}
	return []route{
		apiRoute(http.MethodGet, "/me/code", signedInUser, a.getOwnCode, apiDoc{
			id: "getMyCode", summary: "Read whether you have a door code and what yours starts with", tag: "me",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Whether you have a code, and what a code you choose starts with or why you cannot choose one.", Body: ownCodeView{}}},
		}),
		apiRoute(http.MethodPut, "/me/code", signedInUser, a.setOwnCode, apiDoc{
			id: "setMyCode", summary: "Choose your door code", tag: "me", request: ownCodeRequest{Digits: "4821"},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "Your code is your prefix followed by the digits. It is stored only as a hash and never shown."},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object with digits, they are not 4 to 8 digits, or another user has the code they make.", Body: errorBody{}},
				{Status: http.StatusConflict, Description: "You have no user id, or one that is not a number, so you cannot choose a code; an administrator can set one.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPut, "/me/password", signedInUser, a.setOwnPassword, apiDoc{
			id: "setMyPassword", summary: "Choose your password", tag: "me",
			request: passwordRequest{CurrentPassword: "old horse battery staple", NewPassword: "correct horse battery staple"},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "You sign in with the new password from now on."},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON password request, the current password is wrong, or the new one is too short or too long.", Body: errorBody{}},
				{Status: http.StatusForbidden, Description: "Everyone signs in with single sign-on here, so passwords are not used.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/me/access", signedInUser, a.getOwnAccess, apiDoc{
			id: "getMyAccess", summary: "Read when the portal's doors are open and when your code works there", tag: "me",
			params:    dayRangeParams(),
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every door the portal shows, with when it is open or locked and when your code works there.", Body: portalAccessView{}}, badDays},
		}),
		apiRoute(http.MethodGet, "/me/entries", signedInUser, a.listOwnEntries, apiDoc{
			id: "listMyEntries", summary: "List your entries, a page at a time", tag: "me",
			params: []openapi.Param{
				{Name: "page", In: "query", Description: "The page to return, from 1 (the default).", Example: 1},
				{Name: "per_page", In: "query", Description: fmt.Sprintf("How many entries a page holds, from 1 to %d; %d by default.", maxEventsAPIPer, eventsPerPage), Example: eventsPerPage},
			},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "A page of your events, newest first: your codes entered at doors, your keypad commands, and doors you opened.", Body: eventList{}},
				{Status: http.StatusBadRequest, Description: "page or per_page is out of range.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/me/requests", signedInUser, a.listOwnRequests, apiDoc{
			id: "listMyRequests", summary: "List your access requests", tag: "me",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Your requests, newest first, with any reply.", Body: requestList{}}},
		}),
		apiRoute(http.MethodPost, "/me/requests", signedInUser, a.createOwnRequest, apiDoc{
			id: "createMyRequest", summary: "Ask an administrator for access", tag: "me", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The request, open until an administrator resolves it.", Body: requestView{}}, badRequestBody},
		}),
		apiRoute(http.MethodGet, "/me/requests/{id}", signedInUser, a.getOwnRequest, apiDoc{
			id: "getMyRequest", summary: "Read one of your access requests", tag: "me", params: []openapi.Param{requestIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The request, with any reply.", Body: requestView{}}, noRequest},
		}),
		apiRoute(http.MethodPut, "/me/requests/{id}", signedInUser, a.updateOwnRequest, apiDoc{
			id: "updateMyRequest", summary: "Change one of your open access requests", tag: "me", params: []openapi.Param{requestIDParam}, request: example,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The request as it is now.", Body: requestView{}}, badRequestBody, noRequest, resolvedOne},
		}),
		apiRoute(http.MethodDelete, "/me/requests/{id}", signedInUser, a.deleteOwnRequest, apiDoc{
			id: "deleteMyRequest", summary: "Withdraw one of your open access requests", tag: "me", params: []openapi.Param{requestIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The request is gone."}, noRequest, resolvedOne},
		}),
	}
}

// ownCode is what the portal says of the door code of u: whether they have
// one, and what a code they choose starts with or why they cannot choose
// one.
func ownCode(u connector.StoredUser) ownCodeView {
	view := ownCodeView{HasCode: u.CodeHash != nil}
	prefix, err := directory.CodePrefix(u)
	view.Prefix = prefix
	if err != nil {
		view.Refusal = sentence(err.Error()) + "."
	}
	return view
}

func (a *App) getOwnCode(w http.ResponseWriter, r *http.Request, caller auth.User) {
	u, err := a.directory.User(r.Context(), caller.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ownCode(u))
}

func (a *App) setOwnCode(w http.ResponseWriter, r *http.Request, caller auth.User) {
	var body ownCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "digits"`)
		return
	}
	a.portalDone(w, r, a.directory.SetOwnCode(r.Context(), caller.ID, body.Digits))
}

// portalDone answers with 204 when err is nil and with what err means
// otherwise.
func (a *App) portalDone(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		a.portalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) setOwnPassword(w http.ResponseWriter, r *http.Request, caller auth.User) {
	var body passwordRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "new_password"`)
		return
	}
	a.portalDone(w, r, a.auth.ChoosePassword(r.Context(), caller.ID, body.CurrentPassword, body.NewPassword))
}

// viewOfPortalDoor is d with its times in zone.
func viewOfPortalDoor(d rule.DoorAccess, zone *time.Location) portalDoorView {
	view := portalDoorView{
		Door: doorRef(d.Door), Location: d.Door.Location, ThumbnailURL: thumbnailOf(d.Door),
		Reached: d.Reached(), Works: []occurrenceView{}, Spans: []spanView{},
	}
	for _, in := range d.Works {
		view.Works = append(view.Works, occurrenceView{Start: in.Start.In(zone), End: in.End.In(zone)})
	}
	for _, s := range d.Spans {
		view.Spans = append(view.Spans, spanView{Start: s.Start.In(zone), End: s.End.In(zone), State: s.State})
	}
	return view
}

func (a *App) getOwnAccess(w http.ResponseWriter, r *http.Request, caller auth.User) {
	current, ok := a.apiSettings(w, r)
	if !ok {
		return
	}
	days, ok := a.daysIn(w, r, current.Zone)
	if !ok {
		return
	}
	doors, err := a.publicDoors(r, caller, current, days.from(), days.to())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []portalDoorView{}
	for _, d := range doors {
		views = append(views, viewOfPortalDoor(d, days.zone))
	}
	writeJSON(w, http.StatusOK, portalAccessView{TimeZone: days.zone.String(), From: days.firstText(), To: days.lastText(), Doors: views})
}

func (a *App) listOwnEntries(w http.ResponseWriter, r *http.Request, caller auth.User) {
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	perPage, perPageOK := queryNumber(r, "per_page", eventsPerPage, 1, maxEventsAPIPer)
	if !pageOK || !perPageOK {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("page must be a whole number from 1, and per_page one from 1 to %d", maxEventsAPIPer))
		return
	}
	found, err := a.events.Find(r.Context(), ownEntries(caller.ID, page, perPage))
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeEvents(w, found, page, perPage)
}

func viewOfRequest(r connector.StoredRequest) requestView {
	return requestView{
		ID: r.ID, Title: r.Title, Body: r.Body, Reply: r.Reply, Resolved: r.ResolvedAt != nil, ResolvedAt: utc(r.ResolvedAt),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func (a *App) listOwnRequests(w http.ResponseWriter, r *http.Request, caller auth.User) {
	mine, err := a.requests.Mine(r.Context(), caller.ID)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []requestView{}
	for _, one := range mine {
		views = append(views, viewOfRequest(one))
	}
	writeJSON(w, http.StatusOK, requestList{Requests: views})
}

// readRequest reads a JSON access request.
func readRequest(w http.ResponseWriter, r *http.Request) (request.Fields, bool) {
	var body requestBody
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "title"`)
		return request.Fields{}, false
	}
	return request.Fields{Title: body.Title, Body: body.Body}, true
}

// writeRequest answers with one, which err may say could not be read or
// stored, as status.
func (a *App) writeRequest(w http.ResponseWriter, r *http.Request, status int, one connector.StoredRequest, err error) {
	if err != nil {
		a.portalError(w, r, err)
		return
	}
	writeJSON(w, status, viewOfRequest(one))
}

func (a *App) createOwnRequest(w http.ResponseWriter, r *http.Request, caller auth.User) {
	if f, ok := readRequest(w, r); ok {
		one, err := a.requests.Create(r.Context(), caller.ID, f)
		a.writeRequest(w, r, http.StatusCreated, one, err)
	}
}

func (a *App) getOwnRequest(w http.ResponseWriter, r *http.Request, caller auth.User) {
	one, err := a.requests.One(r.Context(), caller.ID, pathID(r, "id"))
	a.writeRequest(w, r, http.StatusOK, one, err)
}

func (a *App) updateOwnRequest(w http.ResponseWriter, r *http.Request, caller auth.User) {
	if f, ok := readRequest(w, r); ok {
		one, err := a.requests.Update(r.Context(), caller.ID, pathID(r, "id"), f)
		a.writeRequest(w, r, http.StatusOK, one, err)
	}
}

func (a *App) deleteOwnRequest(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.portalDone(w, r, a.requests.Delete(r.Context(), caller.ID, pathID(r, "id")))
}
