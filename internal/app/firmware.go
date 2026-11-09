package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

// firmwareType is how a firmware image is served.
const firmwareType = "application/octet-stream"

type firmwareView struct {
	ID         int64     `json:"id" doc:"Names the firmware in the API."`
	Version    string    `json:"version" doc:"The version, unique."`
	Notes      string    `json:"notes" doc:"What the uploader said of it; may be empty."`
	SHA256     string    `json:"sha256" doc:"The SHA-256 of the image, in hex, which doors check after downloading it."`
	Size       int       `json:"size" doc:"The image's size in bytes."`
	UploadedBy nameRef   `json:"uploaded_by" doc:"Who uploaded it, by id and display name."`
	UploadedAt time.Time `json:"uploaded_at" doc:"When it was uploaded."`
	Default    bool      `json:"default" doc:"True for the site's default version, which every door runs unless it names another."`
	Running    int       `json:"running" doc:"How many doors' controllers last reported running this version."`
	Targeting  int       `json:"targeting" doc:"How many doors should run this version: those that follow the default when it is the default, and those that name it."`
}

type firmwareList struct {
	Firmware []firmwareView `json:"firmware" doc:"Every firmware, newest upload first."`
}

type firmwareUpload struct {
	Version string       `json:"version" doc:"The version, up to 64 letters, digits, dots, dashes, underscores, and plus signs, starting with a letter or digit; unique."`
	Notes   string       `json:"notes,omitempty" doc:"What to know about this version, up to 2000 characters."`
	Image   openapi.File `json:"image" doc:"The firmware image. Any file is accepted."`
}

type firmwareChoice struct {
	FirmwareID *int64 `json:"firmware_id" doc:"The id of the firmware, or null for none."`
}

type versionRef struct {
	ID      int64  `json:"id" doc:"The firmware's id."`
	Version string `json:"version" doc:"Its version."`
}

type updateView struct {
	Version    string     `json:"version" doc:"The version the door was told to install."`
	State      string     `json:"state" doc:"updating until it succeeded (the door connected again with the version) or failed (the door said so, or did not come back with it in time)."`
	SentAt     time.Time  `json:"sent_at" doc:"When the update was sent."`
	FinishedAt *time.Time `json:"finished_at,omitempty" doc:"When it succeeded or failed; absent while it is under way."`
	Detail     string     `json:"detail,omitempty" doc:"Why it failed."`
}

type doorFirmwareView struct {
	Door       nameRef     `json:"door" doc:"The door, by id and name."`
	Version    string      `json:"version" doc:"The firmware version the door's controller last reported; empty until one enrolls."`
	Target     *versionRef `json:"target,omitempty" doc:"The firmware the door should run: its own, or else the default; absent when there is neither."`
	Override   *versionRef `json:"override,omitempty" doc:"The firmware the door runs instead of the default; absent when it follows the default."`
	Status     string      `json:"status,omitempty" doc:"pending (it runs other firmware than its target), updating, done (it runs its target), or failed (its update to its target failed; the rollout tries again in the next window); absent for a door deleted, without a controller, or without a target."`
	LastUpdate *updateView `json:"last_update,omitempty" doc:"The latest update the door was sent; absent when it was never sent one."`
}

type rolloutView struct {
	Pending  []doorFirmwareView `json:"pending" doc:"The doors waiting for an update, by id."`
	Updating []doorFirmwareView `json:"updating" doc:"The doors updating, by id."`
	Done     []doorFirmwareView `json:"done" doc:"The doors running their target, by id."`
	Failed   []doorFirmwareView `json:"failed" doc:"The doors whose update to their target failed, by id."`
}

var (
	firmwareIDParam = openapi.Param{Name: "id", In: "path", Description: "The firmware's id.", Example: int64(1)}
	noFirmware      = openapi.Response{Status: http.StatusNotFound, Description: "No firmware has this id.", Body: errorBody{}}
	badChoice       = openapi.Response{Status: http.StatusBadRequest, Description: "The body is not a JSON object with firmware_id.", Body: errorBody{}}
	chosenFirmware  = int64(1)
)

func (a *App) firmwareRoutes() []route {
	manage := holding(auth.RoleManageFirmware)
	choice := firmwareChoice{FirmwareID: &chosenFirmware}
	return []route{
		apiRoute(http.MethodGet, "/firmware", manage, a.listFirmware, apiDoc{
			id: "listFirmware", summary: "List the firmware", tag: "firmware",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every firmware.", Body: firmwareList{}}},
		}),
		apiRoute(http.MethodPost, "/firmware", manage, a.uploadFirmware, apiDoc{
			id: "uploadFirmware", summary: "Upload firmware", tag: "firmware", form: firmwareUpload{},
			responses: []openapi.Response{
				{Status: http.StatusCreated, Description: "The new firmware. Doors run it once it is the default or their own.", Body: firmwareView{}},
				{Status: http.StatusBadRequest, Description: "The body is not a multipart form with the image in the field image, or a field is wrong; the error says which.", Body: errorBody{}},
				{Status: http.StatusRequestEntityTooLarge, Description: fmt.Sprintf("The image is larger than %d MiB.", firmware.MaxMiB), Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/firmware/rollout", manage, a.getRollout, apiDoc{
			id: "getRollout", summary: "Show how far the rollout is", tag: "firmware",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The doors that take part in the rollout, by status.", Body: rolloutView{}}},
		}),
		apiRoute(http.MethodPut, "/firmware/default", manage, a.setDefaultFirmware, apiDoc{
			id: "setDefaultFirmware", summary: "Choose the default firmware", tag: "firmware", request: choice,
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The firmware is the default, or with null there is none. The rollout sends it to the doors that follow the default."},
				badChoice, noFirmware,
			},
		}),
		apiRoute(http.MethodGet, "/firmware/settings", manage, a.listRolloutSettings, apiDoc{
			id: "listRolloutSettings", summary: "List the rollout settings", tag: "firmware",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The settings of the update window and the rollout.", Body: settingList{}}},
		}),
		apiRoute(http.MethodPut, "/firmware/settings", manage, a.updateRolloutSettings, apiDoc{
			id: "updateRolloutSettings", summary: "Change the rollout settings", tag: "firmware",
			request: map[string]string{"update_window_days": "sat,sun", "update_window_start": "02:00", "update_concurrency": "5"},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The settings of the update window and the rollout as they are now. Settings the environment provides are left as they are.", Body: settingList{}},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object of rollout setting keys and values, or a value is refused; the error says which.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/firmware/{id}", manage, a.getFirmware, apiDoc{
			id: "getFirmware", summary: "Read one firmware", tag: "firmware", params: []openapi.Param{firmwareIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "The firmware.", Body: firmwareView{}}, noFirmware},
		}),
		apiRoute(http.MethodDelete, "/firmware/{id}", manage, a.deleteFirmware, apiDoc{
			id: "deleteFirmware", summary: "Delete firmware", tag: "firmware", params: []openapi.Param{firmwareIDParam},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: "The firmware is gone."}, noFirmware,
				{Status: http.StatusConflict, Description: "The firmware is the default, or doors run it instead of the default.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/firmware", manage, a.getDoorFirmware, apiDoc{
			id: "getDoorFirmware", summary: "Show a door's firmware", tag: "firmware", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{{Status: http.StatusOK, Description: "What the door runs, should run, and its latest update.", Body: doorFirmwareView{}}, noDoor},
		}),
		apiRoute(http.MethodPut, "/doors/{id}/firmware", manage, a.setDoorFirmware, apiDoc{
			id: "setDoorFirmware", summary: "Choose the firmware a door runs", tag: "firmware", params: []openapi.Param{doorIDParam}, request: choice,
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The door's firmware as it is now. With null the door follows the default.", Body: doorFirmwareView{}},
				badChoice,
				{Status: http.StatusNotFound, Description: "No door has this id, or no firmware has the id given.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPost, "/doors/{id}/update", manage, a.updateDoorNow, apiDoc{
			id: "updateDoorNow", summary: "Send a door its firmware now", tag: "firmware", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The door was sent its target, whatever the update window and however many doors are updating.", Body: doorFirmwareView{}},
				noDoor,
				{Status: http.StatusConflict, Description: "The door's controller is not connected, the door has no target, or the public_url setting is not set.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/firmware/{version}", anyone, a.downloadFirmware, apiDoc{
			id: "downloadFirmware", summary: "Download the firmware a door was told to install", tag: "firmware",
			params: []openapi.Param{
				doorIDParam,
				{Name: "version", In: "path", Description: "The version the door was told to install.", Example: "1.4.3"},
				{Name: "token", In: "query", Description: "The token the update command's URL carries.", Example: "c2VjcmV0"},
			},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The image. Doors call this from the URL of their update command, which needs no API token.", Body: openapi.File{}, Type: firmwareType},
				{Status: http.StatusForbidden, Description: "The token is not the one the door was sent for this version, its update has ended, or it has expired.", Body: errorBody{}},
				{Status: http.StatusNotFound, Description: "The version has been deleted since.", Body: errorBody{}},
				{Status: http.StatusInternalServerError, Description: "The server failed; the body names a request id to give an administrator.", Body: errorBody{}},
			},
		}),
	}
}

// firmwareImagePath is where the door whose id is doorID downloads
// version.
func firmwareImagePath(doorID int64, version string) string {
	return fmt.Sprintf("%s/doors/%d/firmware/%s", apiPrefix, doorID, url.PathEscape(version))
}

// firmwareStatuses maps each refusal about firmware to the status the API
// answers it with.
var firmwareStatuses = []refusalStatus{
	{firmware.ErrNoFirmware, http.StatusNotFound},
	{firmware.ErrInUse, http.StatusConflict},
	{firmware.ErrNoTarget, http.StatusConflict},
	{firmware.ErrNoAddress, http.StatusConflict},
	{firmware.ErrNotAllowed, http.StatusForbidden},
	{controller.ErrOffline, http.StatusConflict},
	{door.ErrNoDoor, http.StatusNotFound},
	{errImageTooBig, http.StatusRequestEntityTooLarge},
}

// firmwareError answers an API request with what err, about firmware,
// means.
func (a *App) firmwareError(w http.ResponseWriter, r *http.Request, err error) {
	a.refuse(w, r, err, firmwareStatuses)
}

// concerns reports whether df's door, unless it is deleted, runs version
// and whether it should.
func concerns(df firmware.DoorFirmware, version string) (runs, targets bool) {
	if df.Door.DeletedAt != nil {
		return false, false
	}
	c := df.Door.Controller
	return c != nil && c.FirmwareVersion == version, df.Target != nil && df.Target.Version == version
}

// counts is how many doors run version and how many should, as plan
// stands.
func counts(plan firmware.Plan, version string) (running, targeting int) {
	for _, df := range plan.Doors {
		runs, targets := concerns(df, version)
		if runs {
			running++
		}
		if targets {
			targeting++
		}
	}
	return running, targeting
}

func viewOfFirmware(f connector.StoredFirmware, plan firmware.Plan) firmwareView {
	running, targeting := counts(plan, f.Version)
	return firmwareView{
		ID: f.ID, Version: f.Version, Notes: f.Notes, SHA256: f.SHA256, Size: f.Size,
		UploadedBy: nameRef{ID: f.UploadedBy, Name: f.UploaderName}, UploadedAt: f.UploadedAt.UTC(), Default: f.Default,
		Running: running, Targeting: targeting,
	}
}

func refOf(f *connector.StoredFirmware) *versionRef {
	if f == nil {
		return nil
	}
	return &versionRef{ID: f.ID, Version: f.Version}
}

func viewOfDoorFirmware(df firmware.DoorFirmware) doorFirmwareView {
	view := doorFirmwareView{
		Door: nameRef{ID: df.Door.ID, Name: df.Door.Name}, Target: refOf(df.Target), Override: refOf(df.Override), Status: df.Status,
	}
	if c := df.Door.Controller; c != nil {
		view.Version = c.FirmwareVersion
	}
	if u := df.Update; u != nil {
		view.LastUpdate = &updateView{Version: u.Version, State: u.State, SentAt: u.SentAt.UTC(), FinishedAt: utc(u.FinishedAt), Detail: u.Detail}
	}
	return view
}

func (a *App) listFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []firmwareView{}
	for _, f := range plan.Firmware {
		views = append(views, viewOfFirmware(f, plan))
	}
	writeJSON(w, http.StatusOK, firmwareList{Firmware: views})
}

// writeFirmware answers with f, which err may say could not be read or
// stored, as status.
func (a *App) writeFirmware(w http.ResponseWriter, r *http.Request, status int, f connector.StoredFirmware, err error) {
	if err != nil {
		a.firmwareError(w, r, err)
		return
	}
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, status, viewOfFirmware(f, plan))
}

// errImageTooBig reports an upload over firmware.MaxBytes.
var errImageTooBig = fmt.Errorf("the image is larger than %d MiB", firmware.MaxMiB)

// notFirmware is the problem with a body that is not a firmware upload.
var notFirmware = check.Problems{{Field: firmware.Field, Message: "Send the firmware as a multipart form with the image in the field image."}}

// readFirmware reads r's multipart form: the version and notes, and the
// image, of which it reads no more than one byte past the limit. It
// reports errImageTooBig for an image over the limit and notFirmware for
// a body that is not such a form. Nothing is written to disk.
func readFirmware(w http.ResponseWriter, r *http.Request) (firmware.Fields, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, firmware.MaxBytes+uploadOverhead)
	var f firmware.Fields
	parts, err := r.MultipartReader()
	if err != nil {
		return f, nil, notFirmware
	}
	var image []byte
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			return f, image, nil
		}
		if err != nil {
			return f, nil, notFirmware
		}
		data, err := io.ReadAll(io.LimitReader(part, firmware.MaxBytes+1))
		if err != nil {
			return f, nil, notFirmware
		}
		switch part.FormName() {
		case "version":
			f.Version = string(data)
		case "notes":
			f.Notes = string(data)
		case firmware.Field:
			image = data
		}
		if len(data) > firmware.MaxBytes {
			return f, nil, errImageTooBig
		}
	}
}

// addFirmware stores the firmware r uploads on behalf of viewer.
func (a *App) addFirmware(w http.ResponseWriter, r *http.Request, viewer auth.User) (firmware.Fields, connector.StoredFirmware, error) {
	f, image, err := readFirmware(w, r)
	if err != nil {
		return f, connector.StoredFirmware{}, err
	}
	stored, err := a.firmware.Upload(r.Context(), f, image, viewer.ID)
	return f, stored, err
}

func (a *App) uploadFirmware(w http.ResponseWriter, r *http.Request, caller auth.User) {
	_, f, err := a.addFirmware(w, r, caller)
	a.writeFirmware(w, r, http.StatusCreated, f, err)
}

func (a *App) getFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	f, err := a.firmware.Get(r.Context(), pathID(r, "id"))
	a.writeFirmware(w, r, http.StatusOK, f, err)
}

func (a *App) deleteFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	if err := a.firmware.Delete(r.Context(), pathID(r, "id")); err != nil {
		a.firmwareError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readChoice reads a JSON firmware choice.
func readChoice(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	var body firmwareChoice
	if !readJSON(w, r, &body, `a JSON object with "firmware_id", a firmware's id or null`) {
		return nil, false
	}
	return body.FirmwareID, true
}

func (a *App) setDefaultFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	id, ok := readChoice(w, r)
	if !ok {
		return
	}
	if err := a.firmware.SetDefault(r.Context(), id); err != nil {
		a.firmwareError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) getRollout(w http.ResponseWriter, r *http.Request, _ auth.User) {
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	view := rolloutView{Pending: []doorFirmwareView{}, Updating: []doorFirmwareView{}, Done: []doorFirmwareView{}, Failed: []doorFirmwareView{}}
	lists := map[string]*[]doorFirmwareView{
		firmware.Pending: &view.Pending, firmware.Updating: &view.Updating, firmware.Done: &view.Done, firmware.Failed: &view.Failed,
	}
	for _, df := range plan.Doors {
		if list, ok := lists[df.Status]; ok {
			*list = append(*list, viewOfDoorFirmware(df))
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// rolloutSettings is every setting of the update window and the rollout.
func (a *App) rolloutSettings(w http.ResponseWriter, r *http.Request) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := []settingView{}
	for _, s := range all {
		if slices.Contains(settings.UpdateKeys, s.Key) {
			views = append(views, viewOf(s))
		}
	}
	writeJSON(w, http.StatusOK, settingList{Settings: views})
}

func (a *App) listRolloutSettings(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.rolloutSettings(w, r)
}

// rolloutKeysOnly reports whether every key of values is a rollout
// setting's.
func rolloutKeysOnly(values map[string]string) bool {
	for key := range values {
		if !slices.Contains(settings.UpdateKeys, key) {
			return false
		}
	}
	return true
}

func (a *App) updateRolloutSettings(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body map[string]string
	if err := decodeJSON(w, r, &body); err != nil || !rolloutKeysOnly(body) {
		writeError(w, http.StatusBadRequest, "body must be a JSON object whose keys are rollout settings ("+strings.Join(settings.UpdateKeys, ", ")+
			") and whose values are strings")
		return
	}
	if _, err := a.settings.UpdateAll(r.Context(), body, nil); err != nil {
		a.settingError(w, r, err)
		return
	}
	a.rolloutSettings(w, r)
}

func (a *App) getDoorFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	df, err := a.firmware.Door(r.Context(), pathID(r, "id"))
	a.writeDoorFirmware(w, r, df, err)
}

func (a *App) setDoorFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	id, ok := readChoice(w, r)
	if !ok {
		return
	}
	d, err := a.firmware.SetOverride(r.Context(), pathID(r, "id"), id)
	if err != nil {
		a.firmwareError(w, r, err)
		return
	}
	df, err := a.firmware.Door(r.Context(), d.ID)
	a.writeDoorFirmware(w, r, df, err)
}

func (a *App) updateDoorNow(w http.ResponseWriter, r *http.Request, caller auth.User) {
	df, err := a.firmware.UpdateNow(r.Context(), pathID(r, "id"), caller.ID)
	a.writeDoorFirmware(w, r, df, err)
}

// writeDoorFirmware answers with df, which err may say could not be read
// or changed.
func (a *App) writeDoorFirmware(w http.ResponseWriter, r *http.Request, df firmware.DoorFirmware, err error) {
	if err != nil {
		a.firmwareError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOfDoorFirmware(df))
}

func (a *App) downloadFirmware(w http.ResponseWriter, r *http.Request, _ auth.User) {
	image, err := a.firmware.Download(r.Context(), pathID(r, "id"), r.PathValue("version"), r.URL.Query().Get("token"))
	if err != nil {
		a.firmwareError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", firmwareType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(image)
}
