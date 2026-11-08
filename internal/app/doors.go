package app

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/openapi"
)

// doorsPerPage is how many doors a page lists unless the API caller asks
// for another number.
const (
	doorsPerPage   = 25
	maxDoorsAPIPer = 200
)

type nameRef struct {
	ID   int64  `json:"id" doc:"Its id."`
	Name string `json:"name" doc:"Its name."`
}

type doorView struct {
	ID               int64           `json:"id" doc:"Names the door in the API."`
	Name             string          `json:"name" doc:"The door's name."`
	Location         string          `json:"location" doc:"Where the door is; may be empty."`
	Notes            string          `json:"notes" doc:"What administrators should know about the door; may be empty."`
	Public           bool            `json:"public" doc:"True when the user portal shows the door."`
	OpenSchedules    []nameRef       `json:"open_schedules" doc:"The schedules during which the door is in open mode and unlocks on any keypress, by name."`
	Zones            []nameRef       `json:"zones" doc:"The zones the door is in, by name."`
	PrimaryPictureID *int64          `json:"primary_picture_id,omitempty" doc:"The picture lists and the user portal show; absent when the door has no pictures."`
	DeletedAt        *time.Time      `json:"deleted_at,omitempty" doc:"When the door was deleted; absent unless it is deleted."`
	CreatedAt        time.Time       `json:"created_at" doc:"When the door was created."`
	Controller       *controllerView `json:"controller,omitempty" doc:"The controller enrolled as the door; absent until one is."`
}

type doorRequest struct {
	Name            string  `json:"name" doc:"The door's name, up to 100 characters; unique, ignoring case."`
	Location        string  `json:"location,omitempty" doc:"Where the door is, up to 200 characters."`
	Notes           string  `json:"notes,omitempty" doc:"What administrators should know about the door, up to 2000 characters."`
	Public          bool    `json:"public,omitempty" doc:"True to show the door in the user portal."`
	OpenScheduleIDs []int64 `json:"open_schedule_ids,omitempty" doc:"The ids of the schedules during which the door is in open mode. Omit it for a door that is never in open mode."`
	ZoneIDs         []int64 `json:"zone_ids,omitempty" doc:"The ids of the zones the door is in."`
}

type doorList struct {
	Doors   []doorView `json:"doors" doc:"This page of doors, by name."`
	Total   int        `json:"total" doc:"How many doors the query selects, on every page together."`
	Page    int        `json:"page" doc:"This page's number, from 1."`
	PerPage int        `json:"per_page" doc:"How many doors a page holds."`
}

var (
	doorIDParam     = openapi.Param{Name: "id", In: "path", Description: "The door's id.", Example: int64(1)}
	noDoor          = openapi.Response{Status: http.StatusNotFound, Description: "No door has this id.", Body: errorBody{}}
	badDoor         = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON door, or a field is wrong; the error says which.", Body: errorBody{}}
	doorQueryParams = []openapi.Param{
		{Name: "q", In: "query", Description: "Keeps doors whose name or location contains this text, ignoring case.", Example: "north"},
		{Name: "show", In: "query", Description: "current (the default) lists doors that are not deleted; deleted lists those that are.", Example: "current"},
		{Name: "zone", In: "query", Description: "Keeps the doors in the zone with this id.", Example: int64(1)},
		{Name: "page", In: "query", Description: "The page to return, from 1 (the default).", Example: 1},
		{Name: "per_page", In: "query", Description: fmt.Sprintf("How many doors a page holds, from 1 to %d; %d by default.", maxDoorsAPIPer, doorsPerPage), Example: doorsPerPage},
	}
)

func (a *App) doorRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	example := doorRequest{
		Name: "North entrance", Location: "Building A, ground floor", Notes: "Strike replaced in 2026.",
		Public: true, OpenScheduleIDs: []int64{1}, ZoneIDs: []int64{1},
	}
	return []route{
		apiRoute(http.MethodGet, "/doors", doors, a.listDoors, apiDoc{
			id: "listDoors", summary: "List doors, a page at a time", tag: "doors", params: doorQueryParams,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The page of doors.", Body: doorList{}},
				{Status: http.StatusBadRequest, Description: "A query parameter is out of range.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/doors", doors, a.createDoor, apiDoc{
			id: "createDoor", summary: "Create a door", tag: "doors", request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new door, waiting for its controller to enroll.", Body: doorView{}}, badDoor},
		}),
		apiRoute(http.MethodGet, "/doors/{id}", doors, a.getDoor, apiDoc{
			id: "getDoor", summary: "Read one door", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door.", Body: doorView{}}, noDoor},
		}),
		apiRoute(http.MethodPut, "/doors/{id}", doors, a.updateDoor, apiDoc{
			id: "updateDoor", summary: "Replace a door", tag: "doors", params: []openapi.Param{doorIDParam}, request: example,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door as it is now.", Body: doorView{}}, badDoor, noDoor},
		}),
		apiRoute(http.MethodDelete, "/doors/{id}", doors, a.deleteDoor, apiDoc{
			id: "deleteDoor", summary: "Delete a door", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The door is deleted; its history stays. Deleting a deleted door changes nothing."}, noDoor,
			},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/restore", doors, a.restoreDoor, apiDoc{
			id: "restoreDoor", summary: "Restore a deleted door", tag: "doors", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The door is no longer deleted."}, noDoor},
		}),
	}
}

// catalog is every schedule and zone, so a door can name its own.
type catalog struct {
	schedules []connector.StoredSchedule
	zones     []connector.StoredZone
}

func (a *App) catalog(ctx context.Context) (catalog, error) {
	schedules, err := a.schedules.Schedules(ctx)
	if err != nil {
		return catalog{}, err
	}
	zones, err := a.doors.Zones(ctx)
	return catalog{schedules: schedules, zones: zones}, err
}

func (c catalog) openSchedules(d connector.StoredDoor) []nameRef {
	return refs(c.schedules, d.OpenScheduleIDs, scheduleRef)
}

func (c catalog) zonesOf(d connector.StoredDoor) []nameRef {
	return refs(c.zones, d.ZoneIDs, zoneRef)
}

// refs names the items whose ids are among held, in the order of items.
func refs[T any](items []T, held []int64, ref func(T) nameRef) []nameRef {
	out := []nameRef{}
	for _, item := range items {
		if r := ref(item); slices.Contains(held, r.ID) {
			out = append(out, r)
		}
	}
	return out
}

func (c catalog) viewOfDoor(d connector.StoredDoor) doorView {
	return doorView{
		ID: d.ID, Name: d.Name, Location: d.Location, Notes: d.Notes, Public: d.Public,
		OpenSchedules: c.openSchedules(d), Zones: c.zonesOf(d), PrimaryPictureID: d.PrimaryPictureID,
		DeletedAt: utc(d.DeletedAt), CreatedAt: d.CreatedAt.UTC(), Controller: viewOfController(d.Controller),
	}
}

// doorStatuses maps each refusal about doors, zones, pictures, overrides,
// and controllers to the status the API answers it with.
var doorStatuses = []refusalStatus{
	{door.ErrNoDoor, http.StatusNotFound},
	{door.ErrNoZone, http.StatusNotFound},
	{door.ErrNoPicture, http.StatusNotFound},
	{door.ErrNoOverride, http.StatusNotFound},
	{door.ErrNotActive, http.StatusConflict},
	{controller.ErrNotPending, http.StatusNotFound},
	{controller.ErrDeleted, http.StatusConflict},
	{controller.ErrOffline, http.StatusConflict},
	{errTooBig, http.StatusRequestEntityTooLarge},
}

// doorError answers an API request with what err, about doors, zones, or
// pictures, means.
func (a *App) doorError(w http.ResponseWriter, err error) {
	a.refuse(w, err, doorStatuses)
}

// writeDoor answers with d, which err may say could not be read or
// stored, as status.
func (a *App) writeDoor(w http.ResponseWriter, r *http.Request, status int, d connector.StoredDoor, err error) {
	if err != nil {
		a.doorError(w, err)
		return
	}
	c, err := a.catalog(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	writeJSON(w, status, c.viewOfDoor(d))
}

// queryID reads the query parameter name as an id: zero, which names
// nothing, when it is absent.
func queryID(r *http.Request, name string) (int64, bool) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func (a *App) listDoors(w http.ResponseWriter, r *http.Request, _ auth.User) {
	page, pageOK := queryNumber(r, "page", 1, 1, maxPage)
	perPage, perPageOK := queryNumber(r, "per_page", doorsPerPage, 1, maxDoorsAPIPer)
	zone, zoneOK := queryID(r, "zone")
	show := r.URL.Query().Get("show")
	if !pageOK || !perPageOK || !zoneOK || (show != "" && show != showCurrent && show != showDeleted) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("page must be a whole number from 1, per_page one from 1 to %d, zone a zone's id, and show current or deleted", maxDoorsAPIPer))
		return
	}
	found, err := a.doors.Find(r.Context(), door.Query{
		Search: r.URL.Query().Get("q"), Deleted: show == showDeleted, ZoneID: zone, Page: page, PerPage: perPage,
	})
	if err != nil {
		a.internalError(w, err)
		return
	}
	c, err := a.catalog(r.Context())
	if err != nil {
		a.internalError(w, err)
		return
	}
	views := []doorView{}
	for _, d := range found.Doors {
		views = append(views, c.viewOfDoor(d))
	}
	writeJSON(w, http.StatusOK, doorList{Doors: views, Total: found.Total, Page: page, PerPage: perPage})
}

// readDoor reads a JSON door request.
func readDoor(w http.ResponseWriter, r *http.Request) (door.Fields, bool) {
	var body doorRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "name"`)
		return door.Fields{}, false
	}
	return door.Fields(body), true
}

func (a *App) createDoor(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readDoor(w, r); ok {
		d, err := a.doors.Create(r.Context(), f)
		a.writeDoor(w, r, http.StatusCreated, d, err)
	}
}

func (a *App) getDoor(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, err := a.doors.Door(r.Context(), pathID(r, "id"))
	a.writeDoor(w, r, http.StatusOK, d, err)
}

func (a *App) updateDoor(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readDoor(w, r); ok {
		d, err := a.changeDoor(r.Context(), pathID(r, "id"), f)
		a.writeDoor(w, r, http.StatusOK, d, err)
	}
}

// doorDone answers with 204 when err is nil and with what err means
// otherwise.
func (a *App) doorDone(w http.ResponseWriter, err error) {
	if err != nil {
		a.doorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) deleteDoor(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.controllers.Delete(r.Context(), pathID(r, "id")))
}

func (a *App) restoreDoor(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.doors.Restore(r.Context(), pathID(r, "id")))
}
