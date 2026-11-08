package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
)

const (
	sessionCookie = "doorcode_session"
	flashCookie   = "doorcode_flash"
)

// flashes are the one-time messages a redirect can carry. The cookie holds
// only the key, so nobody can make the site display text of their choosing.
var flashes = map[string]string{
	"signed-in":  "You are signed in.",
	"signed-out": "You have been signed out.",
}

// signInForm is the sign-in page's content.
type signInForm struct {
	Email         string
	Next          string
	Error         string
	EmailError    string
	PasswordError string
}

func (a *App) signInPage(w http.ResponseWriter, r *http.Request) {
	form := signInForm{Next: localPath(r.URL.Query().Get("next"))}
	a.renderSignIn(w, r, http.StatusOK, form)
}

func (a *App) signIn(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	form := signInForm{
		Email:         email,
		Next:          localPath(r.PostFormValue("next")),
		EmailError:    required(email, "Enter your email address."),
		PasswordError: required(password, "Enter your password."),
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

// required returns message when value is empty, and nothing otherwise.
func required(value, message string) string {
	if value == "" {
		return message
	}
	return ""
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

func (a *App) signOut(w http.ResponseWriter, r *http.Request) {
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
