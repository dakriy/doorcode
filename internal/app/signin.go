package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
)

const (
	sessionCookie = "doorcode_session"
	flashCookie   = "doorcode_flash"
)

// flashes are the one-time messages a redirect can carry. The cookie holds
// only the key, so nobody can make the site display text of their choosing.
var flashes = map[string]string{
	"signed-in":           "You are signed in.",
	"signed-out":          "You have been signed out.",
	"token-revoked":       "The token has been revoked. Anything still using it is refused from now on.",
	"token-gone":          "That token had already been revoked.",
	"user-created":        "The user has been created.",
	"user-saved":          "The user's changes have been saved.",
	"user-deleted":        "The user has been deleted. They are refused everywhere until restored.",
	"user-restored":       "The user has been restored.",
	"code-set":            "The door code has been set. It is stored only as a hash and cannot be shown.",
	"code-cleared":        "The door code has been removed.",
	"group-created":       "The group has been created.",
	"group-saved":         "The group's changes have been saved.",
	"group-deleted":       "The group has been deleted.",
	"member-added":        "The member has been added.",
	"member-removed":      "The member has been removed.",
	"schedule-created":    "The schedule has been created.",
	"schedule-saved":      "The schedule's changes have been saved.",
	"schedule-deleted":    "The schedule has been deleted.",
	"door-created":        "The door has been created. Its controller can enroll once it is connected.",
	"door-saved":          "The door's changes have been saved.",
	"door-config-saved":   "The door's configuration has been saved and sent to its controller.",
	"settings-saved":      "The settings have been saved. Doors receive changed configuration defaults right away.",
	"controller-approved": "The controller has been approved. Its credentials are on their way, and it connects as this door once it has them.",
	"credentials-rotated": "New credentials are on their way to the controller. The old ones work until it connects with the new ones or the grace period passes.",
	"credentials-revoked": "The controller's credentials have been revoked and it has been disconnected. It connects again only by enrolling and being approved.",
	"open-mode-saved":     "The open-mode schedules have been saved.",
	"door-zones-saved":    "The door's zones have been saved.",
	"door-deleted":        "The door has been deleted. Its history stays, and you can restore it.",
	"door-restored":       "The door has been restored.",
	"picture-added":       "The picture has been added.",
	"picture-primary":     "The primary picture has been changed.",
	"picture-deleted":     "The picture has been deleted.",
	"zone-created":        "The zone has been created.",
	"zone-saved":          "The zone's changes have been saved.",
	"zone-deleted":        "The zone has been deleted. Its doors stay.",
	"rule-created":        "The access rule has been created.",
	"rule-saved":          "The access rule's changes have been saved.",
	"rule-deleted":        "The access rule has been deleted. Its members no longer get in by it.",
	"override-created":    "The override has been created.",
	"override-saved":      "The override's changes have been saved.",
	"override-cancelled":  "The override has been cancelled. It ends now.",
	"door-opened":         "The door has been told to release its strike. The opening is logged under your name.",
	"override-deleted":    "The override has been deleted.",
}

// signInForm is the sign-in page's content.
type signInForm struct {
	Email         string
	Next          string
	Error         string
	EmailError    string
	PasswordError string
}

func (a *App) signInPage(w http.ResponseWriter, r *http.Request, _ auth.User) {
	form := signInForm{Next: localPath(r.URL.Query().Get("next"))}
	a.renderSignIn(w, r, http.StatusOK, form)
}

func (a *App) signIn(w http.ResponseWriter, r *http.Request, _ auth.User) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	form := signInForm{
		Email:         email,
		Next:          localPath(r.PostFormValue("next")),
		EmailError:    check.Required(email, "Enter your email address."),
		PasswordError: check.Required(password, "Enter your password."),
	}
	if form.EmailError != "" || form.PasswordError != "" {
		a.renderSignIn(w, r, http.StatusUnprocessableEntity, form)
		return
	}

	token, err := a.auth.SignIn(r.Context(), email, password)
	form.Error = signInRefusal(err)
	if form.Error != "" {
		a.renderSignIn(w, r, http.StatusUnprocessableEntity, form)
		return
	}
	if err != nil {
		a.pageError(w, err)
		return
	}
	setCookie(w, r, sessionCookie, token, int(auth.SessionLifetime().Seconds()))
	setCookie(w, r, flashCookie, "signed-in", 0)
	redirect(w, form.Next)
}

func (a *App) renderSignIn(w http.ResponseWriter, r *http.Request, status int, form signInForm) {
	a.render(w, r, status, signInTemplate, "Sign in", nil, form)
}

// signInRefusal is what the sign-in page says about err, or nothing when err
// is not a refusal.
func signInRefusal(err error) string {
	if errors.Is(err, auth.ErrBadCredentials) {
		return "That email and password do not match an account."
	}
	if errors.Is(err, auth.ErrExpired) {
		return "This account has expired. Ask an administrator to renew it."
	}
	return ""
}

func (a *App) signOut(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		if err := a.auth.SignOut(r.Context(), cookie.Value); err != nil {
			a.pageError(w, err)
			return
		}
	}
	setCookie(w, r, sessionCookie, "", -1)
	setCookie(w, r, flashCookie, "signed-out", 0)
	redirect(w, "/sign-in")
}

// takeFlash returns the flash message r carries, if any, and clears it.
func takeFlash(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	setCookie(w, r, flashCookie, "", -1)
	return flashes[cookie.Value]
}

// setCookie sets a cookie scripts cannot read and other sites do not get.
// maxAge is in seconds: zero lasts until the browser closes, negative
// deletes. The cookie is marked Secure when the browser reached the site
// over HTTPS, directly or through a proxy that says so.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
	})
}

// localPath returns next when it is a path on this site, so the sign-in
// page cannot be used to send people elsewhere, and the home page otherwise.
func localPath(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/"
}
