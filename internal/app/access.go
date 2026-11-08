package app

import (
	"net/http"
	"net/url"

	"github.com/dakriy/doorcode/internal/auth"
)

// viewerHandler serves a request from a signed-in person.
type viewerHandler func(w http.ResponseWriter, r *http.Request, viewer auth.User)

// viewer returns who sent r, and whether anyone is signed in.
func (a *App) viewer(r *http.Request) (auth.User, bool, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, false, nil
	}
	return a.auth.Authenticate(r.Context(), cookie.Value)
}

// signedIn serves a page to a signed-in person and sends anyone else to the
// sign-in page, which returns them here afterwards.
func (a *App) signedIn(next viewerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, ok, err := a.viewer(r)
		if err != nil {
			a.pageError(w, err)
			return
		}
		if !ok {
			redirect(w, "/sign-in?next="+url.QueryEscape(r.URL.RequestURI()))
			return
		}
		next(w, r, viewer)
	}
}

// admin serves a page to a signed-in administrator.
func (a *App) admin(next viewerHandler) http.HandlerFunc {
	return a.signedIn(func(w http.ResponseWriter, r *http.Request, viewer auth.User) {
		if !viewer.Has(auth.RoleAdmin) {
			a.render(w, r, http.StatusForbidden, forbiddenTemplate, "Not allowed", &viewer, nil)
			return
		}
		next(w, r, viewer)
	})
}

// adminAPI serves an API call from a signed-in administrator.
func (a *App) adminAPI(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewer, ok, err := a.viewer(r)
		if err != nil {
			a.internalError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "sign in required")
			return
		}
		if !viewer.Has(auth.RoleAdmin) {
			writeError(w, http.StatusForbidden, "the admin role is required")
			return
		}
		next(w, r)
	}
}

// redirect sends the browser to location with 303 See Other, so a form post
// is followed by a GET.
func redirect(w http.ResponseWriter, location string) {
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusSeeOther)
}
