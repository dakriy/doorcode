package app

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/directory"
	"github.com/dakriy/doorcode/internal/schedule"
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

// What a page shows when the door or group it is about does not exist.
const (
	noDoorHere  = "There is no door here. Deleted doors are listed under Deleted on the doors page."
	noGroupHere = "There is no group here. It may have been deleted."
)

// unloaded reports whether err kept a page from loading what it is about,
// after showing why: message when err is absent, which says it does not
// exist, and the failure otherwise.
func (a *App) unloaded(w http.ResponseWriter, r *http.Request, viewer auth.User, err, absent error, message string) bool {
	if errors.Is(err, absent) {
		a.notFound(w, r, viewer, message)
		return true
	}
	if err != nil {
		a.pageError(w, err)
		return true
	}
	return false
}

// done sends the browser to location with a flash message.
func done(w http.ResponseWriter, r *http.Request, location, flash string) {
	setCookie(w, r, flashCookie, flash, 0)
	redirect(w, location)
}

// changed finishes a change made from a page that err does not refuse: a
// failure is shown as an error, and success by going to location with
// flash.
func (a *App) changed(w http.ResponseWriter, r *http.Request, err error, location, flash string) {
	if err != nil {
		a.pageError(w, err)
		return
	}
	done(w, r, location, flash)
}

// fieldErrors maps each field err finds a problem with to the problem, and
// reports whether err holds field problems for a form to show.
func fieldErrors(err error) (map[string]string, bool) {
	var problems check.Problems
	if !errors.As(err, &problems) {
		return nil, false
	}
	return problems.ByField(), true
}

// formDate reads a date input: the start of that day in zone, nil when the
// input is empty.
func formDate(value string, zone *time.Location) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	day, err := time.ParseInLocation(schedule.DateLayout, value, zone)
	return &day, err
}

// inputDate writes the day t falls on in zone for a date input, empty when
// t is nil.
func inputDate(t *time.Time, zone *time.Location) string {
	if t == nil {
		return ""
	}
	return t.In(zone).Format(schedule.DateLayout)
}

// siteZone returns the site timezone, after showing that it could not be
// read.
func (a *App) siteZone(w http.ResponseWriter, r *http.Request) (*time.Location, bool) {
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, err)
		return nil, false
	}
	return current.Zone, true
}

func userPath(id int64) string {
	return "/admin/users/" + strconv.FormatInt(id, 10)
}

func groupPath(id int64) string {
	return "/admin/groups/" + strconv.FormatInt(id, 10)
}
