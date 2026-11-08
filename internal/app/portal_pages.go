package app

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/event"
	"github.com/dakriy/doorcode/internal/request"
	"github.com/dakriy/doorcode/internal/rule"
	"github.com/dakriy/doorcode/internal/timeline"
)

// The portal's own pages, beside home at / and the API tokens page.
const (
	codePath     = "/me/code"
	passwordPath = "/me/password"
	accessPath   = "/me/access"
	entriesPath  = "/me/entries"
	requestsPath = "/me/requests"
)

const (
	// entriesPerPage is how many entries the entries page lists, and
	// homeEntries how many home shows.
	entriesPerPage = 25
	homeEntries    = 5
	// requestLayout is how the portal writes when a request was made.
	requestLayout = "2 Jan 2006 15:04"
)

func (a *App) portalPageRoutes() []route {
	return []route{
		pageRoute(http.MethodGet, codePath, signedInUser, a.codePage),
		pageRoute(http.MethodPost, codePath, signedInUser, a.setCodeFromPortal),
		pageRoute(http.MethodGet, passwordPath, signedInUser, a.passwordPage),
		pageRoute(http.MethodPost, passwordPath, signedInUser, a.setPasswordFromPortal),
		pageRoute(http.MethodGet, accessPath, signedInUser, a.accessPage),
		pageRoute(http.MethodGet, accessPath+"/{door}", signedInUser, a.doorAccessPage),
		pageRoute(http.MethodGet, entriesPath, signedInUser, a.entriesPage),
		pageRoute(http.MethodGet, requestsPath, signedInUser, a.requestsPage),
		pageRoute(http.MethodPost, requestsPath, signedInUser, a.createRequestFromForm),
		pageRoute(http.MethodGet, requestsPath+"/{id}", signedInUser, a.requestPage),
		pageRoute(http.MethodPost, requestsPath+"/{id}", signedInUser, a.saveRequestFromForm),
		pageRoute(http.MethodPost, requestsPath+"/{id}/delete", signedInUser, a.deleteRequestFromForm),
	}
}

// inPortal reports whether the page at path is part of the portal, which
// every signed-in person uses, rather than of the administration.
func inPortal(path string) bool {
	return path == "/" || within(path, "/me") || within(path, "/account")
}

// adminAreas is each part of the administration, in the order its
// navigation lists them, with the roles that reach it.
var adminAreas = []struct {
	path  string
	roles []string
}{
	{"/admin/users", []string{auth.RoleManageUsers}},
	{"/admin/groups", []string{auth.RoleManageGroups}},
	{"/admin/schedules", []string{auth.RoleManageSchedules}},
	{"/admin/doors", []string{auth.RoleManageDoors, auth.RoleDoorCommander}},
	{"/admin/firmware", []string{auth.RoleManageFirmware}},
	{"/admin/events", []string{auth.RoleViewLogs}},
}

// adminEntry is the first part of the administration viewer may use, empty
// when there is none.
func adminEntry(viewer auth.User) string {
	for _, area := range adminAreas {
		if slices.ContainsFunc(area.roles, viewer.Has) {
			return area.path
		}
	}
	return ""
}

// portalDoor is a door the portal shows, with what is true of it now.
type portalDoor struct {
	Row doorRow
	// Reached is true when one of the viewer's groups has a rule reaching
	// the door.
	Reached bool
	// Now says what the door does now, and Kind how it is drawn: open,
	// locked, code (the viewer's code works), later (it works at other
	// times), or none (it never does).
	Now  string
	Kind string
}

// inside reports whether at falls in [start, end).
func inside(at, start, end time.Time) bool {
	return !at.Before(start) && at.Before(end)
}

// portalDoorOf is d as the portal shows it at now.
func portalDoorOf(d rule.DoorAccess, now time.Time) portalDoor {
	shown := portalDoor{
		Row:     doorRow{ID: d.Door.ID, Name: d.Door.Name, Location: d.Door.Location, Thumbnail: thumbnailOf(d.Door)},
		Reached: len(d.Rules) > 0,
	}
	for _, s := range d.Spans {
		if inside(now, s.Start, s.End) && s.State == timeline.Open {
			shown.Now, shown.Kind = "Open mode now: any keypress unlocks it", eventOpen
			return shown
		}
		if inside(now, s.Start, s.End) {
			shown.Now, shown.Kind = "Locked now: no code opens it", eventLocked
			return shown
		}
	}
	shown.Now, shown.Kind = "Your code never opens this door", "none"
	if shown.Reached {
		shown.Now, shown.Kind = "Your code works here at set times", "later"
	}
	for _, in := range d.Works {
		if inside(now, in.Start, in.End) {
			shown.Now, shown.Kind = "Your code works now", eventCode
		}
	}
	return shown
}

// portalDoors is every door the portal shows, as it is today in zone, for
// viewer.
func (a *App) portalDoors(r *http.Request, viewer auth.User, zone *time.Location) ([]portalDoor, error) {
	groups, err := a.groupsOf(r, viewer)
	if err != nil {
		return nil, err
	}
	today := dayRange{zone: zone, first: a.today(zone), last: a.today(zone)}
	doors, err := a.rules.Public(r.Context(), groups, zone, today.from(), today.to())
	shown := []portalDoor{}
	for _, d := range doors {
		shown = append(shown, portalDoorOf(d, a.clock.Now()))
	}
	return shown, err
}

// homeContent is home's content.
type homeContent struct {
	Code  ownCodeView
	Doors []portalDoor
	// Open is how many of the viewer's requests wait for an answer.
	Open    int
	Entries []eventRow
}

func (a *App) home(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	u, err := a.directory.User(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := homeContent{Code: ownCode(u)}
	content.Doors, err = a.portalDoors(r, viewer, zone)
	if err != nil {
		a.pageError(w, err)
		return
	}
	mine, err := a.requests.Mine(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	for _, one := range mine {
		if one.ResolvedAt == nil {
			content.Open++
		}
	}
	found, err := a.events.Find(r.Context(), event.Query{EventQuery: connector.EventQuery{UserID: viewer.ID}, Page: 1, PerPage: homeEntries})
	if err != nil {
		a.pageError(w, err)
		return
	}
	content.Entries = rowsOfEvents(found.Events, zone)
	a.render(w, r, http.StatusOK, homeTemplate, "Home", &viewer, content)
}

// codeContent is the door code page's content.
type codeContent struct {
	Code  ownCodeView
	Error string
}

func (a *App) codePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.renderCode(w, r, http.StatusOK, viewer, "")
}

func (a *App) renderCode(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, problem string) {
	u, err := a.directory.User(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	a.render(w, r, status, codeTemplate, "Door code", &viewer, codeContent{Code: ownCode(u), Error: problem})
}

func (a *App) setCodeFromPortal(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	err := a.directory.SetOwnCode(r.Context(), viewer.ID, r.PostFormValue("digits"))
	if errs, rejected := fieldErrors(err); rejected {
		a.renderCode(w, r, http.StatusUnprocessableEntity, viewer, errs["digits"])
		return
	}
	if errors.Is(err, directory.ErrNoUserID) || errors.Is(err, directory.ErrUserIDNotDigits) {
		a.renderCode(w, r, http.StatusConflict, viewer, "")
		return
	}
	a.changed(w, r, err, codePath, "own-code-set")
}

// passwordContent is the password page's content.
type passwordContent struct {
	// Hidden is true when nobody signs in with a password here.
	Hidden bool
	// Current is true when the viewer has a password, which they must
	// give to choose another.
	Current   bool
	MinLength int
	Errors    map[string]string
}

func (a *App) passwordPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.renderPassword(w, r, http.StatusOK, viewer, nil)
}

func (a *App) renderPassword(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, errs map[string]string) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	u, err := a.directory.User(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := passwordContent{
		Hidden: current.OIDC.HidesPasswordForms(), Current: u.PasswordHash != nil, MinLength: current.PasswordMinLength, Errors: errs,
	}
	a.render(w, r, status, passwordTemplate, "Password", &viewer, content)
}

func (a *App) setPasswordFromPortal(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	chosen := r.PostFormValue("new_password")
	if chosen != r.PostFormValue("confirm_password") {
		a.renderPassword(w, r, http.StatusUnprocessableEntity, viewer, map[string]string{"confirm_password": "The passwords do not match. Type the new one twice."})
		return
	}
	err := a.auth.ChoosePassword(r.Context(), viewer.ID, r.PostFormValue("current_password"), chosen)
	if errs, rejected := fieldErrors(err); rejected {
		a.renderPassword(w, r, http.StatusUnprocessableEntity, viewer, errs)
		return
	}
	if errors.Is(err, auth.ErrPasswordsHidden) {
		a.renderPassword(w, r, http.StatusForbidden, viewer, nil)
		return
	}
	a.changed(w, r, err, passwordPath, "own-password-set")
}

func (a *App) accessPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	doors, err := a.portalDoors(r, viewer, zone)
	if err != nil {
		a.pageError(w, err)
		return
	}
	a.render(w, r, http.StatusOK, accessTemplate, "Access", &viewer, doors)
}

// doorAccessContent is the content of the page showing a door's week in
// the portal.
type doorAccessContent struct {
	Calendar week
	Row      doorRow
	Reached  bool
	Works    []occurrenceRow
	Spans    []spanRow
}

// noPortalDoor is what the portal says about a door it does not show.
const noPortalDoor = "This door is not shown in the portal. Doors an administrator has not made public, and doors that are gone, are left out."

func (a *App) doorAccessPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	doorID := pathID(r, "door")
	wk := a.weekOf(r, zone, accessPath+"/"+strconv.FormatInt(doorID, 10))
	groups, err := a.groupsOf(r, viewer)
	if err != nil {
		a.pageError(w, err)
		return
	}
	doors, err := a.rules.Public(r.Context(), groups, zone, wk.days.from(), wk.days.to())
	if err != nil {
		a.pageError(w, err)
		return
	}
	i := slices.IndexFunc(doors, func(d rule.DoorAccess) bool { return d.Door.ID == doorID })
	if i < 0 {
		a.notFound(w, r, viewer, noPortalDoor)
		return
	}
	d := doors[i]
	content := doorAccessContent{Calendar: wk, Row: portalDoorOf(d, a.clock.Now()).Row, Reached: len(d.Rules) > 0, Works: []occurrenceRow{}}
	events := []calendarEvent{}
	for _, in := range d.Works {
		in = inZone(in, zone)
		content.Works = append(content.Works, rowOfOccurrence(in))
		events = append(events, eventOf(in, "Your code works", eventCode))
	}
	content.Spans, events = timelineRows(d.Spans, zone, events)
	content.Calendar.Events = eventsText(events)
	a.render(w, r, http.StatusOK, doorAccessTemplate, d.Door.Name, &viewer, content)
}

// entriesContent is the entries page's content.
type entriesContent struct {
	Rows []eventRow
	// Empty says why the list is empty.
	Empty string
	pager
}

func (a *App) entriesPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	page, ok := queryNumber(r, "page", 1, 1, maxPage)
	if !ok {
		page = 1
	}
	found, err := a.events.Find(r.Context(), event.Query{EventQuery: connector.EventQuery{UserID: viewer.ID}, Page: page, PerPage: entriesPerPage})
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := entriesContent{Rows: rowsOfEvents(found.Events, zone), Empty: "You have no entries yet. Each time you enter your code at a door, it is listed here with what the door decided."}
	if found.Total > 0 {
		content.Empty = "This page is past the last one."
	}
	link := func(page int) string { return entriesPath + "?" + url.Values{"page": {strconv.Itoa(page)}}.Encode() }
	content.pager = pagerOf(page, entriesPerPage, len(found.Events), found.Total, link)
	a.render(w, r, http.StatusOK, entriesTemplate, "Entries", &viewer, content)
}

// requestRow is an access request as the portal shows it, its times in the
// site timezone.
type requestRow struct {
	ID       int64
	Title    string
	Body     string
	Reply    string
	Resolved string
	Created  string
	Updated  string
}

func rowOfRequest(one connector.StoredRequest, zone *time.Location) requestRow {
	row := requestRow{
		ID: one.ID, Title: one.Title, Body: one.Body, Reply: one.Reply,
		Created: one.CreatedAt.In(zone).Format(requestLayout), Updated: one.UpdatedAt.In(zone).Format(requestLayout),
	}
	if one.ResolvedAt != nil {
		row.Resolved = one.ResolvedAt.In(zone).Format(requestLayout)
	}
	return row
}

// requestForm is the request form, as entered.
type requestForm struct {
	Title  string
	Body   string
	Errors map[string]string
}

// requestsContent is the access requests page's content.
type requestsContent struct {
	Rows []requestRow
	Form requestForm
	// MaxTitle and MaxBody are the most characters the fields take.
	MaxTitle int
	MaxBody  int
}

func (a *App) requestsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.renderRequests(w, r, http.StatusOK, viewer, requestForm{})
}

func (a *App) renderRequests(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form requestForm) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	mine, err := a.requests.Mine(r.Context(), viewer.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := requestsContent{Rows: []requestRow{}, Form: form, MaxTitle: request.MaxTitleLength, MaxBody: request.MaxBodyLength}
	for _, one := range mine {
		content.Rows = append(content.Rows, rowOfRequest(one, zone))
	}
	a.render(w, r, status, requestsTemplate, "Access requests", &viewer, content)
}

// readRequestForm reads the request form.
func readRequestForm(r *http.Request) (request.Fields, requestForm) {
	form := requestForm{Title: r.PostFormValue("title"), Body: r.PostFormValue("body")}
	return request.Fields{Title: form.Title, Body: form.Body}, form
}

func (a *App) createRequestFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f, form := readRequestForm(r)
	_, err := a.requests.Create(r.Context(), viewer.ID, f)
	if errs, rejected := fieldErrors(err); rejected {
		form.Errors = errs
		a.renderRequests(w, r, http.StatusUnprocessableEntity, viewer, form)
		return
	}
	a.changed(w, r, err, requestsPath, "request-sent")
}

// requestContent is the content of the page about one access request.
type requestContent struct {
	Row  requestRow
	Form requestForm
	// Alert is a refusal to show above everything else.
	Alert    string
	MaxTitle int
	MaxBody  int
}

// noRequestHere is what the portal says about a request that is not the
// viewer's.
const noRequestHere = "You have no access request here. It may have been withdrawn."

func (a *App) requestPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showRequest(w, r, http.StatusOK, viewer, nil, "")
}

// showRequest shows the viewer's request the path names, its form as
// entered, or as stored when form is nil, with alert.
func (a *App) showRequest(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form *requestForm, alert string) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	one, err := a.requests.One(r.Context(), viewer.ID, pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, request.ErrNoRequest, noRequestHere) {
		return
	}
	content := requestContent{
		Row: rowOfRequest(one, zone), Form: requestForm{Title: one.Title, Body: one.Body}, Alert: alert,
		MaxTitle: request.MaxTitleLength, MaxBody: request.MaxBodyLength,
	}
	if form != nil {
		content.Form = *form
	}
	a.render(w, r, status, requestTemplate, one.Title, &viewer, content)
}

// requestChanged finishes a change to the viewer's request made from its
// page: a resolved request is shown saying it cannot change, one that is
// not the viewer's as missing, and success with flash at location.
func (a *App) requestChanged(w http.ResponseWriter, r *http.Request, viewer auth.User, err error, location, flash string) {
	if errors.Is(err, request.ErrResolved) {
		a.showRequest(w, r, http.StatusConflict, viewer, nil, "This request is resolved, so it can no longer be changed or withdrawn. Make a new request instead.")
		return
	}
	if errors.Is(err, request.ErrNoRequest) {
		a.notFound(w, r, viewer, noRequestHere)
		return
	}
	a.changed(w, r, err, location, flash)
}

func (a *App) saveRequestFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f, form := readRequestForm(r)
	id := pathID(r, "id")
	_, err := a.requests.Update(r.Context(), viewer.ID, id, f)
	if errs, rejected := fieldErrors(err); rejected {
		form.Errors = errs
		a.showRequest(w, r, http.StatusUnprocessableEntity, viewer, &form, "")
		return
	}
	a.requestChanged(w, r, viewer, err, requestsPath+"/"+strconv.FormatInt(id, 10), "request-saved")
}

func (a *App) deleteRequestFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.requestChanged(w, r, viewer, a.requests.Delete(r.Context(), viewer.ID, pathID(r, "id")), requestsPath, "request-deleted")
}
