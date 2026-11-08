package app

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/openapi"
)

const (
	// usersPerPage is how many users a page lists unless the API caller
	// asks for another number.
	usersPerPage   = 25
	maxUsersAPIPer = 200
)

type userList struct {
	Users   []userView `json:"users" doc:"This page of users, by display name."`
	Total   int        `json:"total" doc:"How many users the search selects, on every page together."`
	Page    int        `json:"page" doc:"This page's number, from 1."`
	PerPage int        `json:"per_page" doc:"How many users a page holds."`
}

type userRequest struct {
	Email       string     `json:"email" doc:"The user's email address; stored lowercase and unique."`
	UserID      string     `json:"user_id,omitempty" doc:"The user's external id, up to 64 letters, digits, dots, dashes, or underscores; unique. Omit it for a user without one."`
	FirstName   string     `json:"first_name,omitempty" doc:"The user's first name, up to 100 characters."`
	LastName    string     `json:"last_name,omitempty" doc:"The user's last name, up to 100 characters."`
	DisplayName string     `json:"display_name,omitempty" doc:"The name pages show, up to 100 characters. Omit it to use the first and last names."`
	ExpiresAt   *time.Time `json:"expires_at,omitempty" doc:"When the account stops working. Omit it for an account that never expires."`
}

type codeRequest struct {
	Code string `json:"code" doc:"The door code: 6 to 20 digits that no other user has."`
}

var (
	userIDParam     = openapi.Param{Name: "id", In: "path", Description: "The user's id.", Example: int64(1)}
	userQueryParams = []openapi.Param{
		{Name: "q", In: "query", Description: "Keeps users whose email, user id, or names contain this text, ignoring case.", Example: "smith"},
		{Name: "show", In: "query", Description: "current (the default) lists users who are not deleted; deleted lists those who are.", Example: "current"},
		{Name: "page", In: "query", Description: "The page to return, from 1 (the default).", Example: 1},
		{Name: "per_page", In: "query", Description: fmt.Sprintf("How many users a page holds, from 1 to %d; %d by default.", maxUsersAPIPer, usersPerPage), Example: usersPerPage},
	}
	userTokenParams = []openapi.Param{userIDParam, {Name: "token", In: "path", Description: "The token's id.", Example: int64(1)}}
	noUser          = openapi.Response{Status: http.StatusNotFound, Description: "No user has this id.", Body: errorBody{}}
)

func (a *App) userRoutes() []route {
	users := holding(auth.RoleManageUsers)
	admin := holding(auth.RoleAdmin)
	expires := time.Date(2030, 6, 30, 0, 0, 0, 0, time.UTC)
	example := userRequest{Email: "ann.smith@example.com", UserID: "100234", FirstName: "Ann", LastName: "Smith", ExpiresAt: &expires}
	days := 30
	badBody := func(what string) openapi.Response {
		return openapi.Response{Status: http.StatusBadRequest, Description: what, Body: errorBody{}}
	}
	return []route{
		apiRoute(http.MethodGet, "/users", users, a.listUsers, apiDoc{
			id: "listUsers", summary: "List users, a page at a time", tag: "users", params: userQueryParams,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The page of users.", Body: userList{}},
				badBody("A query parameter is out of range."),
			},
		}),
		apiRoute(http.MethodPost, "/users", users, a.createUser, apiDoc{
			id: "createUser", summary: "Create a user", tag: "users", request: example,
			responses: []openapi.Response{
				{Status: http.StatusCreated, Description: "The new user, already a member of every group whose pre-assigned user ids hold theirs.", Body: userView{}},
				badBody("The body is not a JSON user, or a field is wrong; the error says which."),
			},
		}),
		apiRoute(http.MethodGet, "/users/{id}", users, a.getUser, apiDoc{
			id: "getUser", summary: "Read one user", tag: "users", params: []openapi.Param{userIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The user.", Body: userView{}}, noUser},
		}),
		apiRoute(http.MethodPut, "/users/{id}", users, a.updateUser, apiDoc{
			id: "updateUser", summary: "Replace a user's profile", tag: "users", params: []openapi.Param{userIDParam}, request: example,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The user with the new profile.", Body: userView{}},
				badBody("The body is not a JSON user, or a field is wrong; the error says which."),
				noUser,
			},
		}),
		apiRoute(http.MethodDelete, "/users/{id}", users, a.deleteUser, apiDoc{
			id: "deleteUser", summary: "Delete a user", tag: "users", params: []openapi.Param{userIDParam},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The user is deleted and refused everywhere; their history stays. Deleting a deleted user changes nothing."},
				noUser,
				{Status: http.StatusConflict, Description: "The user is the caller, who cannot delete their own account.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/users/{id}/restore", users, a.restoreUser, apiDoc{
			id: "restoreUser", summary: "Restore a deleted user", tag: "users", params: []openapi.Param{userIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The user is no longer deleted."}, noUser},
		}),
		apiRoute(http.MethodPut, "/users/{id}/code", users, a.setCode, apiDoc{
			id: "setUserCode", summary: "Set a user's door code", tag: "users", params: []openapi.Param{userIDParam},
			request: codeRequest{Code: "73914862"},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The code is set. It is stored only as a hash and never shown again."},
				badBody("The body is not a JSON code, the code is not 6 to 20 digits, or another user has it."),
				noUser,
			},
		}),
		apiRoute(http.MethodDelete, "/users/{id}/code", users, a.clearCode, apiDoc{
			id: "clearUserCode", summary: "Remove a user's door code", tag: "users", params: []openapi.Param{userIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The user has no door code."}, noUser},
		}),
		apiRoute(http.MethodGet, "/users/{id}/tokens", admin, a.listUserTokens, apiDoc{
			id: "listUserTokens", summary: "List a user's personal API tokens", tag: "users", params: []openapi.Param{userIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The user's tokens, without their secrets.", Body: tokenList{}}, noUser},
		}),
		apiRoute(http.MethodPost, "/users/{id}/tokens", admin, a.createUserToken, apiDoc{
			id: "createUserToken", summary: "Create a personal API token for a user", tag: "users", params: []openapi.Param{userIDParam},
			request: tokenRequest{Name: "door sync", ExpiresInDays: &days},
			responses: []openapi.Response{
				{Status: http.StatusCreated, Description: "The token, which acts as the user with their roles, and its secret, shown this once.", Body: createdToken{}},
				badBody("The body is not a JSON token request, or the name or expiry is out of range."),
				noUser,
			},
		}),
		apiRoute(http.MethodDelete, "/users/{id}/tokens/{token}", admin, a.revokeUserToken, apiDoc{
			id: "revokeUserToken", summary: "Revoke a user's personal API token", tag: "users", params: userTokenParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The token is revoked and refused from now on."},
				{Status: http.StatusNotFound, Description: "No user has this id, or the user has no token with this id.", Body: errorBody{}},
			},
		}),
	}
}

// queryNumber reads the query parameter name as a whole number from low to
// high, fallback when it is absent.
func queryNumber(r *http.Request, name string, fallback, low, high int) (int, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= low && n <= high
}

func (a *App) listUsers(w http.ResponseWriter, r *http.Request, _ auth.User) {
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	perPage, perPageOK := queryNumber(r, "per_page", usersPerPage, 1, maxUsersAPIPer)
	show := r.URL.Query().Get("show")
	if !pageOK || !perPageOK || (show != "" && show != showCurrent && show != showDeleted) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("page must be a whole number from 1, per_page one from 1 to %d, and show current or deleted", maxUsersAPIPer))
		return
	}
	q := directory.Query{Search: r.URL.Query().Get("q"), Deleted: show == showDeleted, Page: page, PerPage: perPage}
	found, err := a.directory.Users(r.Context(), q)
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, userList{Users: a.viewsOfUsers(found.Users), Total: found.Total, Page: page, PerPage: perPage})
}

const (
	showCurrent = "current"
	showDeleted = "deleted"
	// maxPage keeps the offset of a page far from overflowing.
	maxPage = 1000000
)

// readUser reads a JSON user request as a profile.
func readUser(w http.ResponseWriter, r *http.Request) (directory.Profile, bool) {
	var body userRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "email"`)
		return directory.Profile{}, false
	}
	return directory.Profile{
		Email: body.Email, UserID: body.UserID, FirstName: body.FirstName, LastName: body.LastName,
		DisplayName: body.DisplayName, ExpiresAt: body.ExpiresAt,
	}, true
}

func (a *App) createUser(w http.ResponseWriter, r *http.Request, _ auth.User) {
	p, ok := readUser(w, r)
	if !ok {
		return
	}
	u, err := a.directory.CreateUser(r.Context(), p)
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a.viewOfUser(u))
}

func (a *App) getUser(w http.ResponseWriter, r *http.Request, _ auth.User) {
	u, err := a.directory.User(r.Context(), pathID(r, "id"))
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.viewOfUser(u))
}

func (a *App) updateUser(w http.ResponseWriter, r *http.Request, _ auth.User) {
	p, ok := readUser(w, r)
	if !ok {
		return
	}
	u, err := a.directory.UpdateUser(r.Context(), pathID(r, "id"), p)
	if err != nil {
		a.directoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.viewOfUser(u))
}

// noContent answers with 204 when err is nil and with what err means
// otherwise.
func (a *App) noContent(w http.ResponseWriter, err error) {
	if err != nil {
		a.directoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) deleteUser(w http.ResponseWriter, r *http.Request, caller auth.User) {
	a.noContent(w, a.directory.DeleteUser(r.Context(), caller, pathID(r, "id")))
}

func (a *App) restoreUser(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.noContent(w, a.directory.RestoreUser(r.Context(), pathID(r, "id")))
}

func (a *App) setCode(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body codeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "code"`)
		return
	}
	a.noContent(w, a.directory.SetCode(r.Context(), pathID(r, "id"), body.Code))
}

func (a *App) clearCode(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.noContent(w, a.directory.ClearCode(r.Context(), pathID(r, "id")))
}

// tokenOwner is the id of the user the path names, after answering with a
// refusal when there is none.
func (a *App) tokenOwner(w http.ResponseWriter, r *http.Request) (int64, bool) {
	u, err := a.directory.User(r.Context(), pathID(r, "id"))
	if err != nil {
		a.directoryError(w, err)
		return 0, false
	}
	return u.ID, true
}

func (a *App) listUserTokens(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if owner, ok := a.tokenOwner(w, r); ok {
		a.writeTokens(w, r, owner)
	}
}

func (a *App) createUserToken(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if owner, ok := a.tokenOwner(w, r); ok {
		a.issueToken(w, r, owner)
	}
}

func (a *App) revokeUserToken(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if owner, ok := a.tokenOwner(w, r); ok {
		a.withdrawToken(w, r, owner, "token", "the user has no token with this id")
	}
}
