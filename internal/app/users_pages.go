package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/directory"
)

func (a *App) userPageRoutes() []route {
	users := holding(auth.RoleManageUsers)
	admin := holding(auth.RoleAdmin)
	return []route{
		pageRoute(http.MethodGet, "/admin/users", users, a.usersPage),
		pageRoute(http.MethodGet, "/admin/users/new", users, a.newUserPage),
		pageRoute(http.MethodPost, "/admin/users", users, a.createUserFromForm),
		pageRoute(http.MethodGet, "/admin/users/{id}", users, a.userPage),
		pageRoute(http.MethodPost, "/admin/users/{id}", users, a.saveUserFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/code", users, a.setCodeFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/code/clear", users, a.clearCodeFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/delete", users, a.deleteUserFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/restore", users, a.restoreUserFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/tokens", admin, a.createUserTokenFromForm),
		pageRoute(http.MethodPost, "/admin/users/{id}/tokens/{token}/revoke", admin, a.revokeUserTokenFromForm),
	}
}

// usersContent is the user list's content.
type usersContent struct {
	Search  string
	Deleted bool
	Rows    []userRow
	// Empty says why the list is empty.
	Empty    string
	Total    int
	From, To int
	Previous string
	Next     string
}

// userRow is one user as lists show them.
type userRow struct {
	ID          int64
	DisplayName string
	Email       string
	UserID      string
	Groups      []connector.UserGroup
	// Status is Active, Expired, or Deleted.
	Status string
}

func (a *App) rowOf(u connector.StoredUser) userRow {
	row := userRow{ID: u.ID, DisplayName: u.DisplayName, Email: u.Email, Groups: u.Groups, Status: "Active"}
	if u.UserID != nil {
		row.UserID = *u.UserID
	}
	if reached(u.ExpiresAt, a.clock.Now()) {
		row.Status = "Expired"
	}
	if u.DeletedAt != nil {
		row.Status = "Deleted"
	}
	return row
}

func (a *App) rowsOf(users []connector.StoredUser) []userRow {
	rows := []userRow{}
	for _, u := range users {
		rows = append(rows, a.rowOf(u))
	}
	return rows
}

func (a *App) usersPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	search := r.URL.Query().Get("q")
	deleted := r.URL.Query().Get("show") == showDeleted
	page, ok := queryNumber(r, "page", 1, 1, maxPage)
	if !ok {
		page = 1
	}
	found, err := a.directory.Users(r.Context(), directory.Query{Search: search, Deleted: deleted, Page: page, PerPage: usersPerPage})
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := usersContent{
		Search: search, Deleted: deleted, Rows: a.rowsOf(found.Users), Total: found.Total,
		From: (page-1)*usersPerPage + 1, To: (page-1)*usersPerPage + len(found.Users),
		Empty: emptyUsers(search, found.Total),
	}
	if page > 1 {
		content.Previous = usersURL(search, deleted, page-1)
	}
	if content.To < found.Total {
		content.Next = usersURL(search, deleted, page+1)
	}
	a.render(w, r, http.StatusOK, usersTemplate, "Users", &viewer, content)
}

// emptyUsers says why a page of users has nobody on it, when total users
// match. Without a search only the deleted users can be none, since the
// other users include the viewer.
func emptyUsers(search string, total int) string {
	if total > 0 {
		return "This page is past the last one."
	}
	if search != "" {
		return "No users match this search."
	}
	return "No users are deleted."
}

func usersURL(search string, deleted bool, page int) string {
	q := url.Values{}
	if search != "" {
		q.Set("q", search)
	}
	if deleted {
		q.Set("show", showDeleted)
	}
	q.Set("page", strconv.Itoa(page))
	return "/admin/users?" + q.Encode()
}

// userForm is the profile form, as entered.
type userForm struct {
	Email       string
	UserID      string
	FirstName   string
	LastName    string
	DisplayName string
	ExpiresOn   string
	Errors      map[string]string
}

func formOfUser(u connector.StoredUser, zone *time.Location) userForm {
	f := userForm{
		Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		DisplayName: u.DisplayName, ExpiresOn: inputDate(u.ExpiresAt, zone),
	}
	if u.UserID != nil {
		f.UserID = *u.UserID
	}
	return f
}

// readUserForm reads the profile form, with the expiry day in zone. It
// reports false, with the form saying why, when the expiry is not a date.
func readUserForm(r *http.Request, zone *time.Location) (directory.Profile, userForm, bool) {
	f := userForm{
		Email: r.PostFormValue("email"), UserID: r.PostFormValue("user_id"),
		FirstName: r.PostFormValue("first_name"), LastName: r.PostFormValue("last_name"),
		DisplayName: r.PostFormValue("display_name"), ExpiresOn: r.PostFormValue("expires_on"),
	}
	expires, err := formDate(f.ExpiresOn, zone)
	if err != nil {
		f.Errors = map[string]string{"expires_on": "Enter a date, or leave it empty for an account that never expires."}
		return directory.Profile{}, f, false
	}
	p := directory.Profile{
		Email: f.Email, UserID: f.UserID, FirstName: f.FirstName, LastName: f.LastName,
		DisplayName: f.DisplayName, ExpiresAt: expires,
	}
	return p, f, true
}

// rejected is f carrying the problems err holds, and whether it holds any.
func (f userForm) rejected(err error) (userForm, bool) {
	errs, rejected := fieldErrors(err)
	f.Errors = errs
	return f, rejected
}

func (a *App) newUserPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.render(w, r, http.StatusOK, newUserTemplate, "New user", &viewer, userForm{})
}

func (a *App) createUserFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	p, form, ok := readUserForm(r, zone)
	if !ok {
		a.render(w, r, http.StatusUnprocessableEntity, newUserTemplate, "New user", &viewer, form)
		return
	}
	u, err := a.directory.CreateUser(r.Context(), p)
	if form, rejected := form.rejected(err); rejected {
		a.render(w, r, http.StatusUnprocessableEntity, newUserTemplate, "New user", &viewer, form)
		return
	}
	a.changed(w, r, err, userPath(u.ID), "user-created")
}

// userContent is the user detail page's content.
type userContent struct {
	User userView
	Row  userRow
	// Self is true when the page is about the viewer.
	Self      bool
	Form      userForm
	CodeError string
	// Alert is a refusal to show above everything else.
	Alert string
	// Access is the doors the user's groups let them enter.
	Access []accessRow
	// ShowTokens is true when the viewer may manage the user's tokens.
	ShowTokens    bool
	Tokens        []auth.Token
	TokenForm     tokenForm
	Choices       []expiryChoice
	MaxNameLength int
	Created       *createdSecret
	// zone is the site timezone the forms' dates are in.
	zone *time.Location
}

// freshUser is the content of u's page with every form as it starts, its
// dates in zone.
func freshUser(u connector.StoredUser, zone *time.Location) userContent {
	return userContent{Form: formOfUser(u, zone), TokenForm: tokenForm{Expiry: defaultTokenExpiry}, zone: zone}
}

// pageUser returns the content of the page about the user the path names,
// with every form as it starts, after showing that there is none or that
// the store failed.
func (a *App) pageUser(w http.ResponseWriter, r *http.Request, viewer auth.User) (connector.StoredUser, userContent, bool) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return connector.StoredUser{}, userContent{}, false
	}
	u, err := a.directory.User(r.Context(), pathID(r, "id"))
	if errors.Is(err, directory.ErrNoUser) {
		a.notFound(w, r, viewer, "There is no user here. They may never have existed; deleted users are listed under Deleted.")
		return u, userContent{}, false
	}
	if err != nil {
		a.pageError(w, err)
		return u, userContent{}, false
	}
	return u, freshUser(u, zone), true
}

func (a *App) userPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if u, content, ok := a.pageUser(w, r, viewer); ok {
		a.renderUser(w, r, http.StatusOK, viewer, u, content)
	}
}

// renderUser shows the page about u, starting from what content already
// says about the forms.
func (a *App) renderUser(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, u connector.StoredUser, content userContent) {
	content.User = a.viewOfUser(u)
	content.Row = a.rowOf(u)
	content.Self = u.ID == viewer.ID
	access, err := a.accessRows(r, u)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content.Access = access
	content.ShowTokens = viewer.Has(auth.RoleAdmin)
	if content.ShowTokens {
		tokens, err := a.auth.Tokens(r.Context(), u.ID)
		if err != nil {
			a.pageError(w, err)
			return
		}
		content.Tokens = tokens
	}
	content.Choices, content.MaxNameLength = expiryChoices, maxTokenNameLength
	a.render(w, r, status, userTemplate, u.DisplayName, &viewer, content)
}

func (a *App) saveUserFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	u, content, ok := a.pageUser(w, r, viewer)
	if !ok {
		return
	}
	p, form, ok := readUserForm(r, content.zone)
	if !ok {
		content.Form = form
		a.renderUser(w, r, http.StatusUnprocessableEntity, viewer, u, content)
		return
	}
	_, err := a.directory.UpdateUser(r.Context(), u.ID, p)
	if form, rejected := form.rejected(err); rejected {
		content.Form = form
		a.renderUser(w, r, http.StatusUnprocessableEntity, viewer, u, content)
		return
	}
	a.userChanged(w, r, viewer, u, content, err, "user-saved")
}

// userChanged finishes a change to u made from its page, whose content
// fresh is as it starts: a refusal is shown on the page, a failure as an
// error, and success with flash.
func (a *App) userChanged(w http.ResponseWriter, r *http.Request, viewer auth.User, u connector.StoredUser, fresh userContent, err error, flash string) {
	if status, message, refused := refusal(err); refused {
		content := fresh
		content.Alert = message
		a.renderUser(w, r, status, viewer, u, content)
		return
	}
	a.changed(w, r, err, userPath(u.ID), flash)
}

func (a *App) setCodeFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	u, content, ok := a.pageUser(w, r, viewer)
	if !ok {
		return
	}
	err := a.directory.SetCode(r.Context(), u.ID, r.PostFormValue("code"))
	var problems check.Problems
	if errors.As(err, &problems) {
		content.CodeError = problems.Error()
		a.renderUser(w, r, http.StatusUnprocessableEntity, viewer, u, content)
		return
	}
	a.userChanged(w, r, viewer, u, content, err, "code-set")
}

func (a *App) clearCodeFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if u, content, ok := a.pageUser(w, r, viewer); ok {
		a.userChanged(w, r, viewer, u, content, a.directory.ClearCode(r.Context(), u.ID), "code-cleared")
	}
}

func (a *App) deleteUserFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if u, content, ok := a.pageUser(w, r, viewer); ok {
		a.userChanged(w, r, viewer, u, content, a.directory.DeleteUser(r.Context(), viewer, u.ID), "user-deleted")
	}
}

func (a *App) restoreUserFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if u, content, ok := a.pageUser(w, r, viewer); ok {
		a.userChanged(w, r, viewer, u, content, a.directory.RestoreUser(r.Context(), u.ID), "user-restored")
	}
}

func (a *App) createUserTokenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	u, content, ok := a.pageUser(w, r, viewer)
	if !ok {
		return
	}
	form, days, ok := readTokenForm(r)
	if !ok {
		content.TokenForm = form
		a.renderUser(w, r, http.StatusUnprocessableEntity, viewer, u, content)
		return
	}
	token, secret, err := a.auth.CreateToken(r.Context(), u.ID, form.Name, days)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content.Created = &createdSecret{Name: token.Name, Secret: secret}
	a.renderUser(w, r, http.StatusCreated, viewer, u, content)
}

func (a *App) revokeUserTokenFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if u, _, ok := a.pageUser(w, r, viewer); ok {
		a.revokeFromForm(w, r, u.ID, "token", userPath(u.ID))
	}
}
