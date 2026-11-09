package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/request"
)

// adminRequestsPath is the administration's list of access requests.
const adminRequestsPath = "/admin/requests"

func (a *App) requestPageRoutes() []route {
	users := answersRequests
	return []route{
		pageRoute(http.MethodGet, adminRequestsPath, users, a.adminRequestsPage),
		pageRoute(http.MethodGet, adminRequestsPath+"/{id}", users, a.adminRequestPage),
		pageRoute(http.MethodPost, adminRequestsPath+"/{id}/reply", users, a.replyFromForm),
		pageRoute(http.MethodPost, adminRequestsPath+"/{id}/resolve", users, a.resolveFromForm),
		pageRoute(http.MethodPost, adminRequestsPath+"/{id}/reopen", users, a.reopenFromForm),
	}
}

// requestFilters is the requests page's filters as entered.
type requestFilters struct {
	Search string
	State  string
	UserID int64
	Page   int
}

// link is the requests page with the filters f holds, on page; on no page
// in particular when page is zero.
func (f requestFilters) link(page int) string {
	values := url.Values{"state": {f.State}}
	if f.Search != "" {
		values.Set("q", f.Search)
	}
	if f.UserID != 0 {
		values.Set("user", idText(f.UserID))
	}
	if page > 0 {
		values.Set("page", strconv.Itoa(page))
	}
	return adminRequestsPath + "?" + values.Encode()
}

// adminRequestRow is an access request as the administration lists it.
type adminRequestRow struct {
	requestRow
	User nameRef
}

// adminRowOf is one as the administration lists it, its times in zone.
func adminRowOf(one connector.StoredRequest, zone *time.Location) adminRequestRow {
	return adminRequestRow{requestRow: rowOfRequest(one, zone), User: requesterOf(one)}
}

// adminRequestsContent is the requests page's content.
type adminRequestsContent struct {
	Filters requestFilters
	States  []filterOption
	// User is the user the requests are filtered by, nil when they are
	// not, and WithoutUser the page with these filters but the user's.
	User        *nameRef
	WithoutUser string
	Rows        []adminRequestRow
	// Empty says why the list is empty.
	Empty string
	pager
	// Alert says filters or a page number that could not be read were
	// left out.
	Alert string
}

// requestStateLabels names each state the requests list filters by.
var requestStateLabels = map[string]string{stateOpen: "Waiting for an answer", stateResolved: "Resolved", stateAll: "Every request"}

// readRequestFilters reads the requests page's filters, and reports
// whether they could be read and whether the page number could. Filters
// that cannot be read are the open requests of everyone, and a page number
// that cannot be read is the first page.
func readRequestFilters(r *http.Request) (requestFilters, bool, bool) {
	query := r.URL.Query()
	f := requestFilters{Search: query.Get("q"), State: stateIn(r), Page: 1}
	_, stateOK := resolvedBy(f.State)
	user, userOK := queryID(r, "user")
	f.UserID = user
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	if pageOK {
		f.Page = page
	}
	if !stateOK || !userOK {
		f = requestFilters{State: stateOpen, Page: f.Page}
	}
	return f, stateOK && userOK, pageOK
}

func (a *App) adminRequestsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	f, filtersOK, pageOK := readRequestFilters(r)
	content := adminRequestsContent{}
	if !pageOK {
		content.Alert = "The page number could not be read, so the first page is shown."
	}
	if !filtersOK {
		content.Alert = "Some filters could not be read, so the requests waiting for an answer are shown."
	}
	resolved, _ := resolvedBy(f.State)
	found, err := a.requests.Find(r.Context(), request.Query{Search: f.Search, Resolved: resolved, UserID: f.UserID, Page: f.Page, PerPage: requestsPerPage})
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content.User, err = a.filterUser(r.Context(), f.UserID)
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content.Filters = f
	content.States = optionsOf([]string{stateOpen, stateResolved, stateAll}, func(s string) string { return requestStateLabels[s] }, f.State)
	content.Rows = []adminRequestRow{}
	for _, one := range found.Requests {
		content.Rows = append(content.Rows, adminRowOf(one, zone))
	}
	content.Empty = emptyRequests(f, found.Total)
	content.pager = pagerOf(f.Page, requestsPerPage, len(found.Requests), found.Total, f.link)
	withoutUser := f
	withoutUser.UserID = 0
	content.WithoutUser = withoutUser.link(0)
	a.render(w, r, http.StatusOK, adminRequestsTemplate, "Access requests", &viewer, content)
}

// emptyRequests says why a page of requests has none on it, when total
// requests match the filters f.
func emptyRequests(f requestFilters, total int) string {
	if total > 0 {
		return "This page is past the last one."
	}
	if f.Search != "" || f.UserID != 0 {
		return "No requests match these filters."
	}
	if f.State == stateOpen {
		return "Nothing is waiting for an answer. Requests people make in the portal appear here."
	}
	return "No requests yet. People ask for access from the portal, and their requests appear here."
}

// adminRequestContent is the content of the administration's page about
// one access request.
type adminRequestContent struct {
	Row  adminRequestRow
	Form replyForm
	// Waiting lists the requests waiting for an answer.
	Waiting  string
	MaxReply int
}

// replyForm is the reply form, as entered.
type replyForm struct {
	Reply  string
	Errors map[string]string
}

// noRequestToAnswer is what the administration says about a request that
// does not exist.
const noRequestToAnswer = "There is no access request here. Its user may have withdrawn it."

func (a *App) adminRequestPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showAdminRequest(w, r, http.StatusOK, viewer, nil)
}

// showAdminRequest shows the request the path names, its reply form as
// entered, or as stored when form is nil.
func (a *App) showAdminRequest(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form *replyForm) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	one, err := a.requests.Get(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, request.ErrNoSuchRequest, noRequestToAnswer) {
		return
	}
	content := adminRequestContent{
		Row:  adminRowOf(one, zone),
		Form: replyForm{Reply: one.Reply}, Waiting: requestFilters{State: stateOpen}.link(0), MaxReply: request.MaxReplyLength,
	}
	if form != nil {
		content.Form = *form
	}
	a.render(w, r, status, adminRequestTemplate, one.Title, &viewer, content)
}

// adminRequestPath is the administration's page about the request whose
// id is id.
func adminRequestPath(id int64) string {
	return adminRequestsPath + "/" + strconv.FormatInt(id, 10)
}

// answeredFromForm finishes a change to a request made from its page: a
// missing request is shown as missing, and success with flash on its page.
func (a *App) answeredFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User, err error, flash string) {
	if errors.Is(err, request.ErrNoSuchRequest) {
		a.notFound(w, r, viewer, noRequestToAnswer)
		return
	}
	a.changed(w, r, err, adminRequestPath(pathID(r, "id")), flash)
}

func (a *App) replyFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	form := replyForm{Reply: r.PostFormValue("reply")}
	id := pathID(r, "id")
	_, err := a.requests.Reply(r.Context(), id, form.Reply)
	if errs, rejected := fieldErrors(err); rejected {
		form.Errors = errs
		a.showAdminRequest(w, r, http.StatusUnprocessableEntity, viewer, &form)
		return
	}
	flash := "reply-sent"
	if err == nil && r.PostFormValue("resolve") != "" {
		_, err = a.requests.Resolve(r.Context(), id)
		flash = "reply-sent-resolved"
	}
	a.answeredFromForm(w, r, viewer, err, flash)
}

func (a *App) resolveFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_, err := a.requests.Resolve(r.Context(), pathID(r, "id"))
	a.answeredFromForm(w, r, viewer, err, "request-resolved")
}

func (a *App) reopenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_, err := a.requests.Reopen(r.Context(), pathID(r, "id"))
	a.answeredFromForm(w, r, viewer, err, "request-reopened")
}
