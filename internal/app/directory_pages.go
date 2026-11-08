package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/directory"
)

// pageRefusals maps each refusal the directory reports for a change made
// from a page to the status and the message the page shows.
var pageRefusals = []struct {
	err     error
	status  int
	message string
}{
	{directory.ErrOwnAccount, http.StatusConflict, "You cannot delete your own account. Ask another administrator to do it."},
	{directory.ErrOwnAdmin, http.StatusConflict, "You cannot remove your own admin role. Ask another administrator to do it."},
	{directory.ErrNotHeld, http.StatusForbidden, "You can only change groups when you hold every role they carry."},
	{directory.ErrNotMember, http.StatusNotFound, "That user is not a member of this group."},
}

// refusal is the status and message of err when it is a refusal a page
// shows, and whether it is one.
func refusal(err error) (int, string, bool) {
	for _, known := range pageRefusals {
		if errors.Is(err, known.err) {
			return known.status, known.message, true
		}
	}
	return 0, "", false
}

// notFound shows that the thing a page is about does not exist.
func (a *App) notFound(w http.ResponseWriter, r *http.Request, viewer auth.User, message string) {
	a.render(w, r, http.StatusNotFound, notFoundTemplate, "Not found", &viewer, message)
}

// done sends the browser to location with a flash message.
func done(w http.ResponseWriter, r *http.Request, location, flash string) {
	setCookie(w, r, flashCookie, flash, 0)
	redirect(w, location)
}

// dateLayout is how a date input writes a date.
const dateLayout = "2006-01-02"

// formDate reads a date input: the start of that day in UTC, nil when the
// input is empty.
func formDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	day, err := time.Parse(dateLayout, value)
	return &day, err
}

// inputDate writes t for a date input, empty when t is nil.
func inputDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(dateLayout)
}

func userPath(id int64) string {
	return "/admin/users/" + strconv.FormatInt(id, 10)
}

func groupPath(id int64) string {
	return "/admin/groups/" + strconv.FormatInt(id, 10)
}
