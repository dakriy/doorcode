package app

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
)

func (a *App) zonePageRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	return []route{
		pageRoute(http.MethodGet, "/admin/zones", doors, a.zonesPage),
		pageRoute(http.MethodGet, "/admin/zones/new", doors, a.newZonePage),
		pageRoute(http.MethodPost, "/admin/zones", doors, a.createZoneFromForm),
		pageRoute(http.MethodGet, "/admin/zones/{id}", doors, a.zonePage),
		pageRoute(http.MethodPost, "/admin/zones/{id}", doors, a.saveZoneFromForm),
		pageRoute(http.MethodPost, "/admin/zones/{id}/delete", doors, a.deleteZoneFromForm),
	}
}

func zonePath(id int64) string {
	return "/admin/zones/" + strconv.FormatInt(id, 10)
}

func (a *App) zonesPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	views := []zoneView{}
	for _, z := range n.zones {
		views = append(views, n.viewOfZone(z))
	}
	a.render(w, r, http.StatusOK, zonesTemplate, "Zones", &viewer, views)
}

// zoneContent is the zone pages' content: the zone, zero on the page
// that creates one, and its form as entered, with every door and schedule
// to choose from.
type zoneContent struct {
	Zone      connector.StoredZone
	Form      door.ZoneFields
	Errors    map[string]string
	Doors     []choice
	Schedules []choice
	// Action is where the form is submitted.
	Action string
}

func readZoneForm(r *http.Request) door.ZoneFields {
	_ = r.ParseForm()
	return door.ZoneFields{
		Name: r.PostFormValue("name"), Description: r.PostFormValue("description"),
		DoorIDs: formIDs(r, "door_ids"), OpenScheduleIDs: formIDs(r, "open_schedule_ids"),
	}
}

// showZone shows the page about z, or the page that creates a zone when z
// is zero, with the form holding f. It offers every door that is not
// deleted and every door f holds, and every schedule.
func (a *App) showZone(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, z connector.StoredZone, f door.ZoneFields, errs map[string]string) {
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := zoneContent{
		Zone: z, Form: f, Errors: errs, Doors: doorChoices(n.doors, f.DoorIDs),
		Schedules: scheduleChoices(n.schedules, f.OpenScheduleIDs), Action: "/admin/zones",
	}
	page, title := newZoneTemplate, "New zone"
	if z.ID != 0 {
		page, title, content.Action = zoneTemplate, z.Name, zonePath(z.ID)
	}
	a.render(w, r, status, page, title, &viewer, content)
}

// doorDetail is what a door choice says beside the door's name.
func doorDetail(d connector.StoredDoor) string {
	if d.DeletedAt != nil {
		return "Deleted"
	}
	return d.Location
}

func (a *App) newZonePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showZone(w, r, http.StatusOK, viewer, connector.StoredZone{}, door.ZoneFields{}, nil)
}

func (a *App) createZoneFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f := readZoneForm(r)
	z, err := a.newZone(r.Context(), f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showZone(w, r, http.StatusUnprocessableEntity, viewer, connector.StoredZone{}, f, errs)
		return
	}
	a.changed(w, r, err, zonePath(z.ID), "zone-created")
}

// pageZone returns the zone the path names, after showing that there is
// none or that the store failed.
func (a *App) pageZone(w http.ResponseWriter, r *http.Request, viewer auth.User) (connector.StoredZone, bool) {
	z, err := a.doors.Zone(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, door.ErrNoZone, "There is no zone here. It may have been deleted.") {
		return z, false
	}
	return z, true
}

func (a *App) zonePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if z, ok := a.pageZone(w, r, viewer); ok {
		a.showZone(w, r, http.StatusOK, viewer, z, door.ZoneFieldsOf(z), nil)
	}
}

func (a *App) saveZoneFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	z, ok := a.pageZone(w, r, viewer)
	if !ok {
		return
	}
	f := readZoneForm(r)
	_, err := a.changeZone(r.Context(), z.ID, f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showZone(w, r, http.StatusUnprocessableEntity, viewer, z, f, errs)
		return
	}
	a.changed(w, r, err, zonePath(z.ID), "zone-saved")
}

func (a *App) deleteZoneFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if z, ok := a.pageZone(w, r, viewer); ok {
		a.changed(w, r, a.removeZone(r.Context(), z.ID), "/admin/zones", "zone-deleted")
	}
}

// doorChoices offers every door that is not deleted, and every door held,
// checking those held.
func doorChoices(doors []connector.StoredDoor, held []int64) []choice {
	out := []choice{}
	for _, d := range doors {
		checked := slices.Contains(held, d.ID)
		if d.DeletedAt == nil || checked {
			out = append(out, choice{ID: d.ID, Name: d.Name, Detail: doorDetail(d), Checked: checked})
		}
	}
	return out
}

// scheduleChoices are the schedules to choose a zone's open mode from,
// with those among checked checked.
func scheduleChoices(schedules []connector.StoredSchedule, checked []int64) []choice {
	out := []choice{}
	for _, sch := range schedules {
		out = append(out, choice{ID: sch.ID, Name: sch.Name, Detail: strings.Join(windowSummaries(sch.Windows), " · "), Checked: slices.Contains(checked, sch.ID)})
	}
	return out
}

// zoneChoices are the zones to offer, as for a door or a rule to name,
// with those among checked checked.
func zoneChoices(zones []connector.StoredZone, checked []int64) []choice {
	out := []choice{}
	for _, z := range zones {
		out = append(out, choice{ID: z.ID, Name: z.Name, Detail: z.Description, Checked: slices.Contains(checked, z.ID)})
	}
	return out
}
