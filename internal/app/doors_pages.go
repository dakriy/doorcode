package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/picture"
)

func (a *App) doorPageRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	return []route{
		pageRoute(http.MethodGet, "/admin/doors", doors, a.doorsPage),
		pageRoute(http.MethodGet, "/admin/doors/new", doors, a.newDoorPage),
		pageRoute(http.MethodPost, "/admin/doors", doors, a.createDoorFromForm),
		pageRoute(http.MethodGet, "/admin/doors/{id}", doors, a.doorPage),
		pageRoute(http.MethodPost, "/admin/doors/{id}", doors, a.doorSaver(readDoorDetails, "door-saved")),
		pageRoute(http.MethodPost, "/admin/doors/{id}/open-mode", doors, a.doorSaver(readOpenMode, "open-mode-saved")),
		pageRoute(http.MethodPost, "/admin/doors/{id}/zones", doors, a.doorSaver(readDoorZones, "door-zones-saved")),
		pageRoute(http.MethodPost, "/admin/doors/{id}/delete", doors, a.deleteDoorFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/restore", doors, a.restoreDoorFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/pictures", doors, a.uploadPictureFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/pictures/{picture}/primary", doors, a.primaryPictureFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/pictures/{picture}/delete", doors, a.deletePictureFromForm),
	}
}

func doorPath(id int64) string {
	return "/admin/doors/" + strconv.FormatInt(id, 10)
}

// doorRow is one door as the list shows it.
type doorRow struct {
	ID       int64
	Name     string
	Location string
	Public   bool
	// Thumbnail is where the primary picture's thumbnail is served, empty
	// without one.
	Thumbnail string
	Zones     []nameRef
	OpenMode  []nameRef
	// Status is whether a controller is enrolled as the door and connected.
	Status string
}

func (c catalog) rowOfDoor(d connector.StoredDoor) doorRow {
	row := doorRow{
		ID: d.ID, Name: d.Name, Location: d.Location, Public: d.Public,
		Zones: c.zonesOf(d), OpenMode: c.openSchedules(d), Status: doorStatus(d.Controller),
	}
	if d.PrimaryPictureID != nil {
		row.Thumbnail = picturePath(d.ID, *d.PrimaryPictureID) + "/thumbnail"
	}
	return row
}

// doorStatus says whether a controller c is enrolled as a door and
// connected.
func doorStatus(c *connector.StoredController) string {
	if c == nil {
		return "Not enrolled"
	}
	if c.Online {
		return "Online"
	}
	return "Offline"
}

// doorsContent is the door list's content.
type doorsContent struct {
	Search  string
	Deleted bool
	Zone    int64
	Zones   []connector.StoredZone
	Rows    []doorRow
	// Empty says why the list is empty.
	Empty    string
	Total    int
	From, To int
	Previous string
	Next     string
	// Pending is how many controllers wait to be approved.
	Pending int
}

func (a *App) doorsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	q := door.Query{Search: r.URL.Query().Get("q"), Deleted: r.URL.Query().Get("show") == showDeleted, Page: 1, PerPage: doorsPerPage}
	if page, ok := queryNumber(r, "page", 1, 1, maxPage); ok {
		q.Page = page
	}
	if zone, ok := queryID(r, "zone"); ok {
		q.ZoneID = zone
	}
	found, err := a.doors.Find(r.Context(), q)
	if err != nil {
		a.pageError(w, err)
		return
	}
	c, err := a.catalog(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := doorsContent{
		Search: q.Search, Deleted: q.Deleted, Zone: q.ZoneID, Zones: c.zones, Rows: []doorRow{}, Total: found.Total,
		From: (q.Page-1)*doorsPerPage + 1, To: (q.Page-1)*doorsPerPage + len(found.Doors), Empty: emptyDoors(q, found.Total),
		Pending: len(a.controllers.Pending.List()),
	}
	for _, d := range found.Doors {
		content.Rows = append(content.Rows, c.rowOfDoor(d))
	}
	if q.Page > 1 {
		content.Previous = doorsURL(q, q.Page-1)
	}
	if content.To < found.Total {
		content.Next = doorsURL(q, q.Page+1)
	}
	a.render(w, r, http.StatusOK, doorsTemplate, "Doors", &viewer, content)
}

// emptyDoors says why a page of doors has none on it, when total doors
// match q.
func emptyDoors(q door.Query, total int) string {
	if total > 0 {
		return "This page is past the last one."
	}
	if q.Search != "" || q.ZoneID != 0 {
		return "No doors match this search."
	}
	if q.Deleted {
		return "No doors are deleted."
	}
	return "No doors yet. Create one for each controller you install; it enrolls once it is connected."
}

func doorsURL(q door.Query, page int) string {
	values := url.Values{}
	if q.Search != "" {
		values.Set("q", q.Search)
	}
	if q.Deleted {
		values.Set("show", showDeleted)
	}
	if q.ZoneID != 0 {
		values.Set("zone", strconv.FormatInt(q.ZoneID, 10))
	}
	values.Set("page", strconv.Itoa(page))
	return "/admin/doors?" + values.Encode()
}

// doorForm is the door form, as entered, with what is wrong with it.
type doorForm struct {
	door.Fields
	Errors map[string]string
}

// readDoorDetails is f with the name, location, notes, and public flag the
// details form posts.
func readDoorDetails(r *http.Request, f door.Fields) door.Fields {
	_ = r.ParseForm()
	f.Name, f.Location, f.Notes = r.PostFormValue("name"), r.PostFormValue("location"), r.PostFormValue("notes")
	f.Public = r.PostForm.Has("public")
	return f
}

// readOpenMode is f with the open-mode schedules the form posts.
func readOpenMode(r *http.Request, f door.Fields) door.Fields {
	f.OpenScheduleIDs = formIDs(r, "open_schedule_ids")
	return f
}

// readDoorZones is f with the zones the form posts.
func readDoorZones(r *http.Request, f door.Fields) door.Fields {
	f.ZoneIDs = formIDs(r, "zone_ids")
	return f
}

// formIDs reads every value of the form field name as an id; a value that
// is not one is zero, which names nothing.
func formIDs(r *http.Request, name string) []int64 {
	_ = r.ParseForm()
	ids := []int64{}
	for _, value := range r.PostForm[name] {
		id, _ := strconv.ParseInt(value, 10, 64)
		ids = append(ids, id)
	}
	return ids
}

func (a *App) newDoorPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.render(w, r, http.StatusOK, newDoorTemplate, "New door", &viewer, doorForm{})
}

func (a *App) createDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f := readDoorDetails(r, door.Fields{})
	d, err := a.doors.Create(r.Context(), f)
	if errs, rejected := fieldErrors(err); rejected {
		a.render(w, r, http.StatusUnprocessableEntity, newDoorTemplate, "New door", &viewer, doorForm{Fields: f, Errors: errs})
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "door-created")
}

// choice is one checkbox of a list of things to choose from.
type choice struct {
	ID      int64
	Name    string
	Detail  string
	Checked bool
}

// pictureTile is one picture of a door's gallery.
type pictureTile struct {
	pictureView
	Number int
}

// doorContent is the door detail page's content.
type doorContent struct {
	Door connector.StoredDoor
	Row  doorRow
	// Form is the door's fields as entered, which may not be stored.
	Form      doorForm
	Schedules []choice
	Zones     []choice
	Pictures  []pictureTile
	Entrants  []entrantRow
	Overrides []overrideRow
	// Controller is the controller enrolled as the door, nil without one,
	// and Credentials the credentials its controllers were issued.
	Controller  *controllerRow
	Credentials []credentialRow
	// Alert is a refusal to show above everything else.
	Alert          string
	MaxPictureMB   int
	MaxPictureSide int
}

// entrantRow is one access rule as a door's page lists who may enter.
type entrantRow struct {
	RuleID   int64
	Group    nameRef
	Schedule nameRef
	Through  string
}

// storedDoor is a door and its pictures as the store holds them.
type storedDoor struct {
	connector.StoredDoor
	pictures []connector.StoredPicture
}

// pageDoor returns the door the path names and its pictures, after showing
// that there is none or that the store failed.
func (a *App) pageDoor(w http.ResponseWriter, r *http.Request, viewer auth.User) (storedDoor, bool) {
	d, pictures, err := a.doors.Pictures(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, door.ErrNoDoor, noDoorHere) {
		return storedDoor{}, false
	}
	return storedDoor{StoredDoor: d, pictures: pictures}, true
}

// showDoor shows the page about d with its forms holding form, answering
// with status.
func (a *App) showDoor(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, d storedDoor, form doorForm, alert string) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	entrants, err := a.rules.Entrants(r.Context(), d.StoredDoor)
	if err != nil {
		a.pageError(w, err)
		return
	}
	_, overrides, err := a.doors.Overrides(r.Context(), d.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	creds, err := a.controllers.Credentials(r.Context(), d.ID)
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := doorContent{
		Door: d.StoredDoor, Row: n.rowOfDoor(d.StoredDoor), Form: form, Alert: alert,
		Entrants: []entrantRow{}, Overrides: []overrideRow{},
		Schedules: scheduleChoices(n.schedules, form.OpenScheduleIDs), Zones: zoneChoices(n.zones, form.ZoneIDs),
		Controller: rowOfController(d.Controller, zone), Credentials: credentialRows(creds, zone),
		MaxPictureMB: picture.Megabytes(), MaxPictureSide: picture.MaxSide,
	}
	for i, p := range d.pictures {
		content.Pictures = append(content.Pictures, pictureTile{pictureView: viewOfPicture(d.StoredDoor, p), Number: i + 1})
	}
	for _, reach := range entrants {
		view := n.viewOfEntrant(reach)
		content.Entrants = append(content.Entrants, entrantRow{
			RuleID: view.RuleID, Group: view.Group, Schedule: view.Schedule, Through: throughText(view.Direct, view.Zones),
		})
	}
	for _, o := range overrides {
		content.Overrides = append(content.Overrides, a.rowOfOverride(o, zone))
	}
	a.render(w, r, status, doorTemplate, d.Name, &viewer, content)
}

// fresh is the form of d as it is stored.
func (d storedDoor) fresh() doorForm {
	return doorForm{Fields: door.FieldsOf(d.StoredDoor)}
}

func (a *App) doorPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.showDoor(w, r, http.StatusOK, viewer, d, d.fresh(), "")
	}
}

// doorSaver saves the part of the door the path names that read takes
// from the form posted, and goes back to its page with flash.
func (a *App) doorSaver(read func(r *http.Request, f door.Fields) door.Fields, flash string) viewerHandler {
	return func(w http.ResponseWriter, r *http.Request, viewer auth.User) {
		d, ok := a.pageDoor(w, r, viewer)
		if !ok {
			return
		}
		f := read(r, door.FieldsOf(d.StoredDoor))
		_, err := a.doors.Update(r.Context(), d.ID, f)
		if errs, rejected := fieldErrors(err); rejected {
			a.showDoor(w, r, http.StatusUnprocessableEntity, viewer, d, doorForm{Fields: f, Errors: errs}, "")
			return
		}
		a.changed(w, r, err, doorPath(d.ID), flash)
	}
}

func (a *App) deleteDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.changed(w, r, a.doors.Delete(r.Context(), d.ID), doorPath(d.ID), "door-deleted")
	}
}

func (a *App) restoreDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.changed(w, r, a.doors.Restore(r.Context(), d.ID), doorPath(d.ID), "door-restored")
	}
}

func (a *App) uploadPictureFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	d, ok := a.pageDoor(w, r, viewer)
	if !ok {
		return
	}
	_, _, err := a.addPicture(w, r)
	if status, refused := uploadRefusal(err); refused {
		form := d.fresh()
		form.Errors = map[string]string{picture.Field: err.Error()}
		a.showDoor(w, r, status, viewer, d, form, "")
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "picture-added")
}

// uploadRefusal is the status a page answers a refused upload with, and
// whether err refuses it.
func uploadRefusal(err error) (int, bool) {
	if errors.Is(err, errTooBig) {
		return http.StatusRequestEntityTooLarge, true
	}
	_, rejected := fieldErrors(err)
	return http.StatusUnprocessableEntity, rejected
}

// doorPageRefusals maps each refusal of a change made from a door's page
// to the status and the message the page shows.
var doorPageRefusals = []struct {
	err     error
	status  int
	message string
}{
	{door.ErrNoPicture, http.StatusNotFound, "That picture is not there any more. It may have been deleted already."},
	{door.ErrNoOverride, http.StatusNotFound, "That override is not there any more. It may have been deleted already."},
	{door.ErrNotActive, http.StatusConflict, "Only an override in force can be cancelled. Delete one that has not started."},
	{controller.ErrOffline, http.StatusConflict, "The controller is not connected, so it cannot be sent new credentials. Try again once it is online."},
}

// doorChanged finishes a change to d made from its page: a refusal is
// shown on the page, a failure as an error, and success by going back to
// the page with flash.
func (a *App) doorChanged(w http.ResponseWriter, r *http.Request, viewer auth.User, d storedDoor, err error, flash string) {
	for _, known := range doorPageRefusals {
		if errors.Is(err, known.err) {
			a.showDoor(w, r, known.status, viewer, d, d.fresh(), known.message)
			return
		}
	}
	a.changed(w, r, err, doorPath(d.ID), flash)
}

func (a *App) primaryPictureFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.doorChanged(w, r, viewer, d, a.doors.SetPrimaryPicture(r.Context(), d.ID, pathID(r, "picture")), "picture-primary")
	}
}

func (a *App) deletePictureFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.doorChanged(w, r, viewer, d, a.doors.DeletePicture(r.Context(), d.ID, pathID(r, "picture")), "picture-deleted")
	}
}
