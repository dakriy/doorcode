package app

import (
	"context"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/openapi"
)

type enrollmentView struct {
	HardwareID      string    `json:"hardware_id" doc:"The controller's hardware id."`
	FirmwareVersion string    `json:"firmware_version" doc:"The firmware version the controller reports; may be empty."`
	Signal          int       `json:"signal" doc:"The controller's signal strength in dBm; 0 when it reports none."`
	WaitingSince    time.Time `json:"waiting_since" doc:"Since when the controller has waited, connected."`
}

type enrollmentList struct {
	Enrollments []enrollmentView `json:"enrollments" doc:"Every controller connected and waiting to be approved, longest waiting first."`
}

type approvalRequest struct {
	DoorID int64        `json:"door_id,omitempty" doc:"The id of the door the controller becomes. Give this or door, not both."`
	Door   *doorRequest `json:"door,omitempty" doc:"A door to create for the controller to become. Give this or door_id, not both."`
}

type controllerView struct {
	HardwareID      string    `json:"hardware_id" doc:"The controller's hardware id."`
	FirmwareVersion string    `json:"firmware_version" doc:"The firmware version the controller last reported; may be empty."`
	Signal          int       `json:"signal" doc:"The signal strength in dBm the controller last reported; 0 when it reported none."`
	Online          bool      `json:"online" doc:"True while the controller is connected to the broker."`
	LastSeenAt      time.Time `json:"last_seen_at" doc:"When the server last heard of the controller connecting, going away, or enrolling."`
}

type credentialView struct {
	ID        int64      `json:"id" doc:"Names the credential."`
	Username  string     `json:"username" doc:"The broker username the controller connects with; the password is never shown."`
	IssuedAt  time.Time  `json:"issued_at" doc:"When the server issued the credential."`
	UsedAt    *time.Time `json:"used_at,omitempty" doc:"When the controller first connected with it; absent until it has."`
	RevokedAt *time.Time `json:"revoked_at,omitempty" doc:"When the broker stopped accepting it; absent while it works."`
	Status    string     `json:"status" doc:"issued (not yet used), in-use, or revoked."`
}

type credentialList struct {
	Credentials []credentialView `json:"credentials" doc:"Every credential the door's controllers were issued, oldest first."`
}

var (
	hardwareIDParam = openapi.Param{Name: "hardware_id", In: "path", Description: "The controller's hardware id.", Example: "a4cf12f00e3b"}
	notPending      = openapi.Response{Status: http.StatusNotFound, Description: "No connected controller with this hardware id is waiting, or no door has the id given.", Body: errorBody{}}
)

func (a *App) controllerRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	doorParams := []openapi.Param{doorIDParam}
	return []route{
		pageRoute(http.MethodGet, "/admin/live", watchesDoors, a.liveStream),
		apiRoute(http.MethodGet, "/enrollments", doors, a.listEnrollments, apiDoc{
			id: "listEnrollments", summary: "List the controllers waiting to be approved", tag: "enrollment",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every waiting controller.", Body: enrollmentList{}}},
		}),
		apiRoute(http.MethodPost, "/enrollments/{hardware_id}/approve", doors, a.approveEnrollment, apiDoc{
			id: "approveEnrollment", summary: "Approve a waiting controller as a door", tag: "enrollment",
			params: []openapi.Param{hardwareIDParam}, request: approvalRequest{DoorID: 1},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The door the controller now is. Its credentials are on their way to it, and the credentials of a controller the door had before no longer work.", Body: doorView{}},
				{Status: http.StatusBadRequest, Description: "The body names no door or both kinds, or a field of the new door is wrong; the error says which.", Body: errorBody{}},
				notPending,
				{Status: http.StatusConflict, Description: "The door is deleted.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/credentials", doors, a.listCredentials, apiDoc{
			id: "listDoorCredentials", summary: "List the broker credentials of a door's controller", tag: "enrollment", params: doorParams,
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The door's credentials.", Body: credentialList{}}, noDoor},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/credentials/rotate", doors, a.rotateCredentials, apiDoc{
			id: "rotateDoorCredentials", summary: "Issue a door's controller new broker credentials", tag: "enrollment", params: doorParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The new credentials are on their way to the controller. The old ones work until it connects with the new ones or the grace period passes."},
				noDoor,
				{Status: http.StatusConflict, Description: "The door's controller is not connected, so it cannot be sent new credentials.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/credentials/revoke", doors, a.revokeCredentials, apiDoc{
			id: "revokeDoorCredentials", summary: "Revoke every broker credential of a door's controller", tag: "enrollment", params: doorParams,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The credentials no longer work and the controller is disconnected. It connects again only by enrolling and being approved."},
				noDoor,
			},
		}),
	}
}

func viewOfEnrollment(e controller.Enrollment) enrollmentView {
	return enrollmentView{HardwareID: e.HardwareID, FirmwareVersion: e.FirmwareVersion, Signal: e.Signal, WaitingSince: e.Since.UTC()}
}

func viewOfController(c *connector.StoredController) *controllerView {
	if c == nil {
		return nil
	}
	return &controllerView{HardwareID: c.HardwareID, FirmwareVersion: c.FirmwareVersion, Signal: c.Signal, Online: c.Online, LastSeenAt: c.LastSeenAt.UTC()}
}

// credentialStatus is what a credential's state is called.
func credentialStatus(c connector.StoredCredential) string {
	if c.RevokedAt != nil {
		return "revoked"
	}
	if c.UsedAt == nil {
		return "issued"
	}
	return "in-use"
}

func viewOfCredential(c connector.StoredCredential) credentialView {
	return credentialView{
		ID: c.ID, Username: c.Username, IssuedAt: c.IssuedAt.UTC(), UsedAt: utc(c.UsedAt), RevokedAt: utc(c.RevokedAt), Status: credentialStatus(c),
	}
}

func (a *App) listEnrollments(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	views := []enrollmentView{}
	for _, e := range a.controllers.Pending.List() {
		views = append(views, viewOfEnrollment(e))
	}
	writeJSON(w, http.StatusOK, enrollmentList{Enrollments: views})
}

// approve makes the controller waiting with hardware id hwid the door
// whose id is doorID, or a new door made from fields when fields is not
// nil, and returns the door.
func (a *App) approve(ctx context.Context, hwid string, doorID int64, fields *door.Fields) (connector.StoredDoor, error) {
	e, err := a.controllers.Pending.Get(hwid)
	if err != nil {
		return connector.StoredDoor{}, err
	}
	if fields != nil {
		d, err := a.doors.Create(ctx, *fields)
		if err != nil {
			return connector.StoredDoor{}, err
		}
		doorID = d.ID
	}
	if err := a.controllers.Approve(ctx, e, doorID); err != nil {
		return connector.StoredDoor{}, err
	}
	return a.doors.Door(ctx, doorID)
}

func (a *App) approveEnrollment(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body approvalRequest
	if err := decodeJSON(w, r, &body); err != nil || (body.DoorID == 0) == (body.Door == nil) {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with either a door_id or a door to create`)
		return
	}
	var fields *door.Fields
	if body.Door != nil {
		f := door.Fields(*body.Door)
		fields = &f
	}
	d, err := a.approve(r.Context(), r.PathValue("hardware_id"), body.DoorID, fields)
	a.writeDoor(w, r, http.StatusOK, d, err)
}

func (a *App) listCredentials(w http.ResponseWriter, r *http.Request, _ auth.User) {
	creds, err := a.controllers.Credentials(r.Context(), pathID(r, "id"))
	if err != nil {
		a.doorError(w, err)
		return
	}
	views := []credentialView{}
	for _, c := range creds {
		views = append(views, viewOfCredential(c))
	}
	writeJSON(w, http.StatusOK, credentialList{Credentials: views})
}

func (a *App) rotateCredentials(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.controllers.Rotate(r.Context(), pathID(r, "id")))
}

func (a *App) revokeCredentials(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.doorDone(w, a.controllers.Revoke(r.Context(), pathID(r, "id")))
}
