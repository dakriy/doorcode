package app

import (
	"net/http"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/openapi"
)

type zoneView struct {
	ID            int64     `json:"id" doc:"Names the zone in the API."`
	Name          string    `json:"name" doc:"The zone's name."`
	Description   string    `json:"description" doc:"What the zone is; may be empty."`
	Doors         []nameRef `json:"doors" doc:"The doors in the zone, by name, deleted ones included."`
	OpenSchedules []nameRef `json:"open_schedules" doc:"The schedules during which the zone's doors are in open mode and unlock on any keypress, by name."`
}

type zoneRequest struct {
	Name            string  `json:"name" doc:"The zone's name, up to 100 characters; unique, ignoring case."`
	Description     string  `json:"description,omitempty" doc:"What the zone is, up to 500 characters."`
	DoorIDs         []int64 `json:"door_ids,omitempty" doc:"The ids of the doors in the zone."`
	OpenScheduleIDs []int64 `json:"open_schedule_ids,omitempty" doc:"The ids of the schedules during which the zone's doors are in open mode. Omit it for a zone that puts no door in open mode."`
}

type zoneList struct {
	Zones []zoneView `json:"zones" doc:"Every zone, by name."`
}

var (
	zoneIDParam = openapi.Param{Name: "id", In: "path", Description: "The zone's id.", Example: int64(1)}
	noZone      = openapi.Response{Status: http.StatusNotFound, Description: "No zone has this id.", Body: errorBody{}}
	badZone     = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON zone, or a field is wrong; the error says which.", Body: errorBody{}}
)

func (a *App) zoneRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	example := zoneRequest{Name: "Building A", Description: "Every outside door of building A.", DoorIDs: []int64{1}, OpenScheduleIDs: []int64{1}}
	return []route{
		apiRoute(http.MethodGet, "/zones", doors, a.listZones, apiDoc{
			id: "listZones", summary: "List every zone", tag: "zones",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every zone.", Body: zoneList{}}},
		}),
		apiRoute(http.MethodPost, "/zones", doors, a.createZone, apiDoc{
			id: "createZone", summary: "Create a zone", tag: "zones", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new zone.", Body: zoneView{}}, badZone},
		}),
		apiRoute(http.MethodGet, "/zones/{id}", doors, a.getZone, apiDoc{
			id: "getZone", summary: "Read one zone", tag: "zones", params: []openapi.Param{zoneIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The zone.", Body: zoneView{}}, noZone},
		}),
		apiRoute(http.MethodPut, "/zones/{id}", doors, a.updateZone, apiDoc{
			id: "updateZone", summary: "Replace a zone", tag: "zones", params: []openapi.Param{zoneIDParam}, request: example,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The zone as it is now.", Body: zoneView{}}, badZone, noZone},
		}),
		apiRoute(http.MethodDelete, "/zones/{id}", doors, a.deleteZone, apiDoc{
			id: "deleteZone", summary: "Delete a zone", tag: "zones", params: []openapi.Param{zoneIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The zone is gone; its doors stay, and its access rules and open mode no longer reach them."}, noZone},
		}),
	}
}

func (n names) viewOfZone(z connector.StoredZone) zoneView {
	return zoneView{
		ID: z.ID, Name: z.Name, Description: z.Description,
		Doors: refs(n.doors, z.DoorIDs, doorRef), OpenSchedules: refs(n.schedules, z.OpenScheduleIDs, scheduleRef),
	}
}

// writeZone answers with z, which err may say could not be read or stored,
// as status.
func (a *App) writeZone(w http.ResponseWriter, r *http.Request, status int, z connector.StoredZone, err error) {
	if err != nil {
		a.doorError(w, err)
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, status, n.viewOfZone(z))
}

func (a *App) listZones(w http.ResponseWriter, r *http.Request, _ auth.User) {
	n, err := a.names(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := []zoneView{}
	for _, z := range n.zones {
		views = append(views, n.viewOfZone(z))
	}
	writeJSON(w, http.StatusOK, zoneList{Zones: views})
}

// readZone reads a JSON zone request.
func readZone(w http.ResponseWriter, r *http.Request) (door.ZoneFields, bool) {
	var body zoneRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "name"`)
		return door.ZoneFields{}, false
	}
	return door.ZoneFields(body), true
}

func (a *App) createZone(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readZone(w, r); ok {
		z, err := a.newZone(r.Context(), f)
		a.writeZone(w, r, http.StatusCreated, z, err)
	}
}

func (a *App) getZone(w http.ResponseWriter, r *http.Request, _ auth.User) {
	z, err := a.doors.Zone(r.Context(), pathID(r, "id"))
	a.writeZone(w, r, http.StatusOK, z, err)
}

func (a *App) updateZone(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readZone(w, r); ok {
		z, err := a.changeZone(r.Context(), pathID(r, "id"), f)
		a.writeZone(w, r, http.StatusOK, z, err)
	}
}

func (a *App) deleteZone(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.removeZone(r.Context(), pathID(r, "id")))
}
