package app

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
)

// noEnrollmentHere is what a page about a controller that is not waiting
// says.
const noEnrollmentHere = "No controller with this hardware id is waiting. It may have disconnected, or been approved already."

func (a *App) controllerPageRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	return []route{
		pageRoute(http.MethodGet, "/admin/enrollments", doors, a.enrollmentsPage),
		pageRoute(http.MethodGet, "/admin/enrollments/{hardware_id}", doors, a.approvalPage),
		pageRoute(http.MethodPost, "/admin/enrollments/{hardware_id}/new-door", doors, a.approveAsNewDoor),
		pageRoute(http.MethodPost, "/admin/enrollments/{hardware_id}/door", doors, a.approveAsDoor),
		pageRoute(http.MethodPost, "/admin/doors/{id}/credentials/rotate", doors, a.rotateCredentialsFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/credentials/revoke", doors, a.revokeCredentialsFromForm),
	}
}

func enrollmentPath(hwid string) string {
	return "/admin/enrollments/" + url.PathEscape(hwid)
}

// enrollmentRow is a waiting controller as the pages show it.
type enrollmentRow struct {
	HardwareID string
	Path       string
	Firmware   string
	Signal     string
	Since      string
}

// signalText is a signal strength as the pages show it.
func signalText(dBm int) string {
	if dBm == 0 {
		return "Not reported"
	}
	return strconv.Itoa(dBm) + " dBm"
}

// firmwareText is a firmware version as the pages show it.
func firmwareText(version string) string {
	if version == "" {
		return "Not reported"
	}
	return version
}

func rowOfEnrollment(e controller.Enrollment, zone *time.Location) enrollmentRow {
	return enrollmentRow{
		HardwareID: e.HardwareID, Path: enrollmentPath(e.HardwareID), Firmware: firmwareText(e.FirmwareVersion),
		Signal: signalText(e.Signal), Since: shownAt(zone, e.Since, minuteLayout),
	}
}

func (a *App) enrollmentsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	rows := []enrollmentRow{}
	for _, e := range a.controllers.Pending.List() {
		rows = append(rows, rowOfEnrollment(e, zone))
	}
	a.render(w, r, http.StatusOK, enrollmentsTemplate, "Pending enrollments", &viewer, rows)
}

// approvalContent is the approval page's content: the waiting controller,
// the form of a new door for it, and the doors it could become instead.
type approvalContent struct {
	Enrollment enrollmentRow
	Form       doorForm
	Zones      []choice
	Doors      []selectOption
	// DoorError is what is wrong with the door chosen.
	DoorError string
}

// showApproval shows the approval page of the controller waiting with the
// hardware id the path names, with its forms as given, answering with
// status.
func (a *App) showApproval(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, form doorForm, doorError string) {
	e, err := a.controllers.Pending.Get(r.PathValue("hardware_id"))
	if a.unloaded(w, r, viewer, err, controller.ErrNotPending, noEnrollmentHere) {
		return
	}
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	n, err := a.names(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := approvalContent{
		Enrollment: rowOfEnrollment(e, zone), Form: form, DoorError: doorError,
		Zones: zoneChoices(n.zones, form.ZoneIDs),
	}
	for _, d := range n.doors {
		if d.DeletedAt == nil {
			content.Doors = append(content.Doors, idOption(d.ID, labelled(d.Name, replacedText(d.Controller)), false))
		}
	}
	a.render(w, r, status, approvalTemplate, "Approve "+e.HardwareID, &viewer, content)
}

// replacedText says what approving a controller as a door does to the
// controller c the door has.
func replacedText(c *connector.StoredController) string {
	if c == nil {
		return ""
	}
	return "replaces " + c.HardwareID
}

func (a *App) approvalPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showApproval(w, r, http.StatusOK, viewer, doorForm{}, "")
}

// readNewDoor is the new door the approval form posts.
func readNewDoor(r *http.Request) door.Fields {
	f := readDoorDetails(r, door.Fields{})
	return readDoorZones(r, f)
}

func (a *App) approveAsNewDoor(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f := readNewDoor(r)
	d, err := a.approve(r.Context(), r.PathValue("hardware_id"), 0, &f)
	if errs, rejected := fieldErrors(err); rejected {
		a.showApproval(w, r, http.StatusUnprocessableEntity, viewer, doorForm{Fields: f, Errors: errs}, "")
		return
	}
	a.approved(w, r, viewer, d, err)
}

func (a *App) approveAsDoor(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	id, _ := strconv.ParseInt(r.PostFormValue("door_id"), 10, 64)
	d, err := a.approve(r.Context(), r.PathValue("hardware_id"), id, nil)
	if errors.Is(err, door.ErrNoDoor) || errors.Is(err, controller.ErrDeleted) {
		a.showApproval(w, r, http.StatusUnprocessableEntity, viewer, doorForm{}, "Choose one of the doors listed.")
		return
	}
	a.approved(w, r, viewer, d, err)
}

// approved finishes an approval: a controller that stopped waiting is
// shown as gone, a failure as an error, and success by going to the door.
func (a *App) approved(w http.ResponseWriter, r *http.Request, viewer auth.User, d connector.StoredDoor, err error) {
	if errors.Is(err, controller.ErrNotPending) {
		a.notFound(w, r, viewer, noEnrollmentHere)
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "controller-approved")
}

func (a *App) rotateCredentialsFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.doorChanged(w, r, viewer, d, a.controllers.Rotate(r.Context(), d.ID), "credentials-rotated")
	}
}

func (a *App) revokeCredentialsFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.doorChanged(w, r, viewer, d, a.controllers.Revoke(r.Context(), d.ID), "credentials-revoked")
	}
}

// controllerRow is the controller enrolled as a door, as its page shows
// it.
type controllerRow struct {
	HardwareID string
	Online     bool
}

// credentialRow is one credential as a door's page lists it.
type credentialRow struct {
	Username string
	Issued   string
	Status   string
}

// credentialLabels name each credential status on a door's page.
var credentialLabels = map[string]string{
	"issued":  "Issued, not yet used",
	"in-use":  "In use",
	"revoked": "Revoked",
}

func rowOfController(c *connector.StoredController) *controllerRow {
	if c == nil {
		return nil
	}
	return &controllerRow{HardwareID: c.HardwareID, Online: c.Online}
}

// credentialRows are a door's credentials as its page lists them, newest
// first.
func credentialRows(creds []connector.StoredCredential, zone *time.Location) []credentialRow {
	rows := []credentialRow{}
	for _, c := range slices.Backward(creds) {
		rows = append(rows, credentialRow{
			Username: c.Username, Issued: shownAt(zone, c.IssuedAt, minuteLayout), Status: credentialLabels[credentialStatus(c)],
		})
	}
	return rows
}
