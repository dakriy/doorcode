package app

import (
	"cmp"
	"fmt"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/request"
)

const (
	// requestsPerPage is how many access requests a page lists unless the
	// API caller asks for another number.
	requestsPerPage   = 25
	maxRequestsAPIPer = 200
)

// The states the requests list filters by.
const (
	stateOpen     = "open"
	stateResolved = "resolved"
	stateAll      = "all"
)

type adminRequestView struct {
	ID         int64      `json:"id" doc:"Names the request."`
	User       nameRef    `json:"user" doc:"Who made the request, by id and display name."`
	Title      string     `json:"title" doc:"What the request is about."`
	Body       string     `json:"body" doc:"The request in the user's words; may be empty."`
	Reply      string     `json:"reply,omitempty" doc:"The answer the user sees; absent until there is one."`
	Resolved   bool       `json:"resolved" doc:"True once the request is resolved, which its user can then no longer change."`
	ResolvedAt *time.Time `json:"resolved_at,omitempty" doc:"When the request was resolved; absent while it is open."`
	CreatedAt  time.Time  `json:"created_at" doc:"When the user made the request."`
	UpdatedAt  time.Time  `json:"updated_at" doc:"When the user last changed it."`
}

type adminRequestList struct {
	Requests []adminRequestView `json:"requests" doc:"This page of requests, newest first."`
	Total    int                `json:"total" doc:"How many requests the filters select, on every page together."`
	Page     int                `json:"page" doc:"This page's number, from 1."`
	PerPage  int                `json:"per_page" doc:"How many requests a page holds."`
}

type replyBody struct {
	Reply string `json:"reply" doc:"The answer the user sees on their request, up to 4000 characters."`
}

var (
	adminRequestParam = openapi.Param{Name: "id", In: "path", Description: "The request's id.", Example: int64(1)}
	noSuchRequest     = openapi.Response{Status: http.StatusNotFound, Description: "No access request has this id.", Body: errorBody{}}
	requestQueryHelp  = fmt.Sprintf("page must be a whole number from 1, per_page one from 1 to %d, state open, resolved, or all, and user a user's id", maxRequestsAPIPer)
)

// answersRequests may answer access requests, and sees how many wait in
// the administration's navigation.
var answersRequests = holding(auth.RoleManageUsers)

// requestStatuses maps each refusal the administration's request
// operations report to the status the API answers it with.
var requestStatuses = []refusalStatus{{request.ErrNoSuchRequest, http.StatusNotFound}}

func (a *App) requestRoutes() []route {
	users := answersRequests
	answered := func(what string) []openapi.Response {
		return []openapi.Response{{Status: http.StatusOK, Description: what, Body: adminRequestView{}}, noSuchRequest}
	}
	return []route{
		apiRoute(http.MethodGet, "/requests", users, a.listRequests, apiDoc{
			id: "listRequests", summary: "List access requests, a page at a time", tag: "requests",
			params: []openapi.Param{
				{Name: "q", In: "query", Description: "Keeps requests whose title or body, or whose user's name or email, contains this text, ignoring case.", Example: "lab"},
				{Name: "state", In: "query", Description: "open (the default) keeps requests waiting for an answer, resolved those that are resolved, and all both.", Example: stateOpen},
				{Name: "user", In: "query", Description: "Keeps the requests of the user with this id.", Example: int64(2)},
				{Name: "page", In: "query", Description: "The page to return, from 1 (the default).", Example: 1},
				{Name: "per_page", In: "query", Description: fmt.Sprintf("How many requests a page holds, from 1 to %d; %d by default.", maxRequestsAPIPer, requestsPerPage), Example: requestsPerPage},
			},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The page of requests.", Body: adminRequestList{}},
				{Status: http.StatusBadRequest, Description: "A query parameter is out of range.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/requests/{id}", users, a.getRequest, apiDoc{
			id: "getRequest", summary: "Read an access request", tag: "requests", params: []openapi.Param{adminRequestParam},
			responses: answered("The request, with its user and any reply."),
		}),
		apiRoute(http.MethodPut, "/requests/{id}/reply", users, a.replyToRequest, apiDoc{
			id: "replyToRequest", summary: "Reply to an access request", tag: "requests", params: []openapi.Param{adminRequestParam},
			request: replyBody{Reply: "You can enter the lab on weekdays from Monday."},
			responses: append(answered("The request with the reply, which its user sees from now on. Replying neither resolves nor reopens it."),
				openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON reply, or the reply is empty or too long.", Body: errorBody{}}),
		}),
		apiRoute(http.MethodPost, "/requests/{id}/resolve", users, a.resolveRequest, apiDoc{
			id: "resolveRequest", summary: "Resolve an access request", tag: "requests", params: []openapi.Param{adminRequestParam},
			responses: answered("The request, resolved; its user can no longer change or withdraw it. A resolved request keeps when it was resolved."),
		}),
		apiRoute(http.MethodPost, "/requests/{id}/reopen", users, a.reopenRequest, apiDoc{
			id: "reopenRequest", summary: "Reopen an access request", tag: "requests", params: []openapi.Param{adminRequestParam},
			responses: answered("The request, open again; its user may change or withdraw it."),
		}),
	}
}

// requesterOf is who made r.
func requesterOf(r connector.StoredRequest) nameRef {
	return nameRef{ID: r.UserID, Name: r.UserName}
}

func viewOfAdminRequest(r connector.StoredRequest) adminRequestView {
	return adminRequestView{
		ID: r.ID, User: requesterOf(r), Title: r.Title, Body: r.Body, Reply: r.Reply,
		Resolved: r.ResolvedAt != nil, ResolvedAt: utc(r.ResolvedAt), CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// stateIn is the requests list's state r asks for: open unless it names
// another.
func stateIn(r *http.Request) string {
	return cmp.Or(r.URL.Query().Get("state"), stateOpen)
}

// resolvedBy is the Resolved filter of the requests list's state, and
// whether state is one.
func resolvedBy(state string) (*bool, bool) {
	open, resolved := false, true
	switch state {
	case stateOpen:
		return &open, true
	case stateResolved:
		return &resolved, true
	case stateAll:
		return nil, true
	}
	return nil, false
}

func (a *App) listRequests(w http.ResponseWriter, r *http.Request, _ auth.User) {
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	perPage, perPageOK := queryNumber(r, "per_page", requestsPerPage, 1, maxRequestsAPIPer)
	resolved, stateOK := resolvedBy(stateIn(r))
	user, userOK := queryID(r, "user")
	if !pageOK || !perPageOK || !stateOK || !userOK {
		writeError(w, http.StatusBadRequest, requestQueryHelp)
		return
	}
	q := request.Query{Search: r.URL.Query().Get("q"), Resolved: resolved, UserID: user, Page: page, PerPage: perPage}
	found, err := a.requests.Find(r.Context(), q)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []adminRequestView{}
	for _, one := range found.Requests {
		views = append(views, viewOfAdminRequest(one))
	}
	writeJSON(w, http.StatusOK, adminRequestList{Requests: views, Total: found.Total, Page: page, PerPage: perPage})
}

// writeAdminRequest answers with one, which err may say could not be read
// or changed.
func (a *App) writeAdminRequest(w http.ResponseWriter, r *http.Request, one connector.StoredRequest, err error) {
	if err != nil {
		a.refuse(w, r, err, requestStatuses)
		return
	}
	writeJSON(w, http.StatusOK, viewOfAdminRequest(one))
}

func (a *App) getRequest(w http.ResponseWriter, r *http.Request, _ auth.User) {
	one, err := a.requests.Get(r.Context(), pathID(r, "id"))
	a.writeAdminRequest(w, r, one, err)
}

func (a *App) replyToRequest(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body replyBody
	if !readJSON(w, r, &body, `a JSON object with a string "reply"`) {
		return
	}
	one, err := a.requests.Reply(r.Context(), pathID(r, "id"), body.Reply)
	a.writeAdminRequest(w, r, one, err)
}

func (a *App) resolveRequest(w http.ResponseWriter, r *http.Request, _ auth.User) {
	one, err := a.requests.Resolve(r.Context(), pathID(r, "id"))
	a.writeAdminRequest(w, r, one, err)
}

func (a *App) reopenRequest(w http.ResponseWriter, r *http.Request, _ auth.User) {
	one, err := a.requests.Reopen(r.Context(), pathID(r, "id"))
	a.writeAdminRequest(w, r, one, err)
}
