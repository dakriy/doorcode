package app

import (
	"net/http"
	"strconv"

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
	zones, err := a.doors.Zones(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	doors, err := a.doors.Doors(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	views := []zoneView{}
	for _, z := range zones {
		views = append(views, viewOfZone(z, doors))
	}
	a.render(w, r, http.StatusOK, zonesTemplate, "Zones", &viewer, views)
}

// zoneContent is the zone pages' content: the zone, zero on the page
// that creates one, and its form as entered, with every door to choose
// from.
type zoneContent struct {
	Zone   connector.StoredZone
	Form   door.ZoneFields
	Errors map[string]string
	Doors  []choice
	// Action is where the form is submitted.
	Action string
}

func readZoneForm(r *http.Request) door.ZoneFields {
	_ = r.ParseForm()
	return door.ZoneFields{Name: r.PostFormValue("name"), Description: r.PostFormValue("description"), DoorIDs: formIDs(r, "door_ids")}
}

// showZone shows the page about z, or the page that creates a zone when z
// is zero, with the form holding f. It offers every door that is not
// deleted and every door f holds.
func (a *App) showZone(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, z connector.StoredZone, f door.ZoneFields, errs map[string]string) {
	doors, err := a.doors.Doors(r.Context())
	if err != nil {
		a.pageError(w, err)
		return
	}
	content := zoneContent{Zone: z, Form: f, Errors: errs, Doors: doorChoices(doors, f.DoorIDs), Action: "/admin/zones"}
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
	z, err := a.doors.CreateZone(r.Context(), f)
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
	_, err := a.doors.UpdateZone(r.Context(), z.ID, f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showZone(w, r, http.StatusUnprocessableEntity, viewer, z, f, errs)
		return
	}
	a.changed(w, r, err, zonePath(z.ID), "zone-saved")
}

func (a *App) deleteZoneFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if z, ok := a.pageZone(w, r, viewer); ok {
		a.changed(w, r, a.doors.DeleteZone(r.Context(), z.ID), "/admin/zones", "zone-deleted")
	}
}
