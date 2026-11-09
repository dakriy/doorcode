package app

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
)

// viewerHandler serves a request from a signed-in person. A route anyone may
// use gets the zero User.
type viewerHandler func(w http.ResponseWriter, r *http.Request, viewer auth.User)

// access says who may use a route.
type access struct {
	// public routes are open to anyone, signed in or not.
	public bool
	// roles are the roles a signed-in user must hold one of; none means
	// any signed-in user.
	roles []string
}

var (
	anyone       = access{public: true}
	signedInUser = access{}
)

// holding is the access of a route for signed-in users holding one of
// roles.
func holding(roles ...string) access {
	return access{roles: roles}
}

// eitherOf is the access of a route for signed-in users whom one of needs,
// each holding roles, admits.
func eitherOf(needs ...access) access {
	var roles []string
	for _, need := range needs {
		roles = append(roles, need.roles...)
	}
	return holding(roles...)
}

// roleNames names need's roles, as in "the manage-doors or door-commander
// role".
func (need access) roleNames() string {
	return strings.Join(need.roles, " or ")
}

// viewer returns who sent r from its session cookie, and whether anyone is
// signed in.
func (a *App) viewer(r *http.Request) (auth.User, bool, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, false, nil
	}
	return a.auth.Authenticate(r.Context(), cookie.Value)
}

// caller returns who sent an API request: the owner of the personal API
// token in its Authorization header, sent as a bearer token or as the HTTP
// Basic password, or else the user the session cookie belongs to, so the
// browser's own calls work.
func (a *App) caller(r *http.Request) (auth.User, bool, error) {
	if token, ok := apiToken(r); ok {
		return a.auth.AuthenticateToken(r.Context(), token)
	}
	return a.viewer(r)
}

func apiToken(r *http.Request) (string, bool) {
	if _, password, ok := r.BasicAuth(); ok {
		return password, true
	}
	scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	return token, strings.EqualFold(scheme, "Bearer")
}

// unguarded serves a public route to anyone, as the zero User.
func unguarded(next viewerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r, auth.User{})
	}
}

// page guards a page that needs a signed-in user: anyone else is sent to
// the sign-in page, which returns them here afterwards, and a user without
// the role sees a refusal.
func (a *App) page(need access, next viewerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, ok, err := a.viewer(r)
		if err != nil {
			a.pageError(w, r, err)
			return
		}
		if !ok {
			redirect(w, "/sign-in?next="+url.QueryEscape(r.URL.RequestURI()))
			return
		}
		if !holds(viewer, need) {
			a.render(w, r, http.StatusForbidden, forbiddenTemplate, "Not allowed", &viewer, nil)
			return
		}
		next(w, r, viewer)
	}
}

// api guards an API operation that needs a signed-in caller, answering in
// JSON when the caller is not authenticated or does not hold the role.
func (a *App) api(need access, next viewerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, ok, err := a.caller(r)
		if err != nil {
			a.internalError(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "a valid API token is required")
			return
		}
		if !holds(caller, need) {
			writeError(w, http.StatusForbidden, "the "+need.roleNames()+" role is required")
			return
		}
		next(w, r, caller)
	}
}

func holds(u auth.User, need access) bool {
	return len(need.roles) == 0 || slices.ContainsFunc(need.roles, u.Has)
}

// redirect sends the browser to location with 303 See Other, so a form post
// is followed by a GET.
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}
