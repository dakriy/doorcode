package app

import (
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/override"
)

type overrideView struct {
	ID        int64     `json:"id" doc:"Names the override in the API."`
	DoorID    int64     `json:"door_id" doc:"The door it overrides."`
	Kind      string    `json:"kind" doc:"open forces the door open, so any keypress unlocks it; locked keeps it locked against every code but a door commander's."`
	Start     time.Time `json:"start" doc:"When it starts."`
	End       time.Time `json:"end" doc:"When it ends."`
	Reason    string    `json:"reason" doc:"Why it was made; may be empty."`
	CreatedBy nameRef   `json:"created_by" doc:"The user who made it, by id and display name."`
	CreatedAt time.Time `json:"created_at" doc:"When it was made."`
	Active    bool      `json:"active" doc:"True while it is in force: it has started and not ended."`
}

type overrideRequest struct {
	Kind   string    `json:"kind" doc:"open or locked."`
	Start  time.Time `json:"start" doc:"When it starts. A start in the past starts it now."`
	End    time.Time `json:"end" doc:"When it ends: after the start, and in the future."`
	Reason string    `json:"reason,omitempty" doc:"Why it is made, up to 500 characters."`
}

type overrideList struct {
	Overrides []overrideView `json:"overrides" doc:"The overrides in force and those to come, by start."`
}

var (
	overrideParams = []openapi.Param{doorIDParam, {Name: "override", In: "path", Description: "The override's id.", Example: int64(1)}}
	noOverride     = openapi.Response{Status: http.StatusNotFound, Description: "No door has this id, or it has no override with this id.", Body: errorBody{}}
	badOverride    = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON override, or a field is wrong; the error says which.", Body: errorBody{}}
)

func (a *App) overrideRoutes() []route {
	doors := opensDoors
	example := overrideRequest{
		Kind: "locked", Start: time.Date(2026, 12, 24, 18, 0, 0, 0, time.UTC), End: time.Date(2026, 12, 27, 6, 0, 0, 0, time.UTC),
		Reason: "Closed over the holiday.",
	}
	one := openapi.Response{Status: http.StatusOK, Description: "The override.", Body: overrideView{}}
	return []route{
		apiRoute(http.MethodGet, "/doors/{id}/overrides", doors, a.listOverrides, apiDoc{
			id: "listDoorOverrides", summary: "List a door's overrides in force and to come", tag: "overrides", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door's overrides that have not ended.", Body: overrideList{}}, noDoor},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/overrides", doors, a.createOverride, apiDoc{
			id: "createDoorOverride", summary: "Force a door open or locked for a while", tag: "overrides", params: []openapi.Param{doorIDParam}, request: example,
			responses: []openapi.Response{{Status: http.StatusCreated, Description: "The new override, made by the caller.", Body: overrideView{}}, badOverride, noDoor},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/overrides/{override}", doors, a.getOverride, apiDoc{
			id: "getDoorOverride", summary: "Read one override", tag: "overrides", params: overrideParams,
			responses: []openapi.Response{one, noOverride},
		}),
		apiRoute(http.MethodPut, "/doors/{id}/overrides/{override}", doors, a.updateOverride, apiDoc{
			id: "updateDoorOverride", summary: "Replace an override", tag: "overrides", params: overrideParams, request: example,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The override as it is now.", Body: overrideView{}}, badOverride, noOverride},
		}),
		apiRoute(http.MethodDelete, "/doors/{id}/overrides/{override}", doors, a.deleteOverride, apiDoc{
			id: "deleteDoorOverride", summary: "Delete an override", tag: "overrides", params: overrideParams,
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The override is gone, as if it had never been made."}, noOverride},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/overrides/{override}/cancel", doors, a.cancelOverride, apiDoc{
			id: "cancelDoorOverride", summary: "End an override now", tag: "overrides", params: overrideParams,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The override, ending now.", Body: overrideView{}}, noOverride,
				{Status: http.StatusConflict, Description: "The override has not started or has ended.", Body: errorBody{}},
			},
		}),
	}
}

func (a *App) viewOfOverride(o connector.StoredOverride) overrideView {
	return overrideView{
		ID: o.ID, DoorID: o.DoorID, Kind: o.Kind, Start: o.Start.UTC(), End: o.End.UTC(), Reason: o.Reason,
		CreatedBy: nameRef{ID: o.CreatedBy, Name: o.CreatorName}, CreatedAt: o.CreatedAt.UTC(),
		Active: override.Active(o, a.clock.Now()),
	}
}

// writeOverride answers with o, which err may say could not be read or
// stored, as status.
func (a *App) writeOverride(w http.ResponseWriter, r *http.Request, status int, o connector.StoredOverride, err error) {
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	writeJSON(w, status, a.viewOfOverride(o))
}

func (a *App) listOverrides(w http.ResponseWriter, r *http.Request, _ auth.User) {
	_, overrides, err := a.overrides.Of(r.Context(), pathID(r, "id"))
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	views := []overrideView{}
	for _, o := range overrides {
		views = append(views, a.viewOfOverride(o))
	}
	writeJSON(w, http.StatusOK, overrideList{Overrides: views})
}

// readOverride reads a JSON override request.
func readOverride(w http.ResponseWriter, r *http.Request) (override.Fields, bool) {
	var body overrideRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "kind" and RFC 3339 times "start" and "end"`)
		return override.Fields{}, false
	}
	return override.Fields(body), true
}

func (a *App) createOverride(w http.ResponseWriter, r *http.Request, caller auth.User) {
	if f, ok := readOverride(w, r); ok {
		o, err := a.newOverride(r.Context(), pathID(r, "id"), caller.ID, f)
		a.writeOverride(w, r, http.StatusCreated, o, err)
	}
}

func (a *App) getOverride(w http.ResponseWriter, r *http.Request, _ auth.User) {
	o, err := a.overrides.Get(r.Context(), pathID(r, "id"), pathID(r, "override"))
	a.writeOverride(w, r, http.StatusOK, o, err)
}

func (a *App) updateOverride(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if f, ok := readOverride(w, r); ok {
		o, err := a.changeOverride(r.Context(), pathID(r, "id"), pathID(r, "override"), f)
		a.writeOverride(w, r, http.StatusOK, o, err)
	}
}

func (a *App) cancelOverride(w http.ResponseWriter, r *http.Request, _ auth.User) {
	o, err := a.endOverride(r.Context(), pathID(r, "id"), pathID(r, "override"))
	a.writeOverride(w, r, http.StatusOK, o, err)
}

func (a *App) deleteOverride(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, r, a.removeOverride(r.Context(), pathID(r, "id"), pathID(r, "override")))
}
