package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/controller"
	"github.com/dakriy/doorcode/internal/firmware"
	"github.com/dakriy/doorcode/internal/settings"
)

var (
	firmwareTemplate        = shellPage("firmware.html")
	firmwareVersionTemplate = shellPage("firmware-version.html")
)

// The form values of the update window's repeat select.
const (
	repeatDaily  = "daily"
	repeatWeekly = "weekly"
)

func (a *App) firmwarePageRoutes() []route {
	manage := holding(auth.RoleManageFirmware)
	return []route{
		pageRoute(http.MethodGet, "/admin/firmware", manage, a.firmwarePage),
		pageRoute(http.MethodPost, "/admin/firmware", manage, a.uploadFirmwareFromForm),
		pageRoute(http.MethodPost, "/admin/firmware/default", manage, a.defaultFirmwareFromForm),
		pageRoute(http.MethodPost, "/admin/firmware/settings", manage, a.rolloutSettingsFromForm),
		pageRoute(http.MethodGet, "/admin/firmware/{id}", manage, a.firmwareVersionPage),
		pageRoute(http.MethodPost, "/admin/firmware/{id}/delete", manage, a.deleteFirmwareFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/firmware", manage, a.doorFirmwareFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/update", manage, a.updateDoorFromForm),
	}
}

func firmwarePath(id int64) string {
	return "/admin/firmware/" + strconv.FormatInt(id, 10)
}

// firmwareRow is one version as the firmware page lists it.
type firmwareRow struct {
	ID        int64
	Version   string
	Uploader  string
	Uploaded  string
	Running   int
	Targeting int
	Default   bool
}

// rolloutGroup is the doors of one status in the rollout, as the firmware
// page lists them, and what it says when there are none.
type rolloutGroup struct {
	Label string
	Empty string
	Rows  []rolloutRow
}

// rolloutRow is one door in the rollout.
type rolloutRow struct {
	DoorID  int64
	Door    string
	Running string
	Target  string
	Update  string
}

// windowForm is the update window form as entered, with what is wrong
// with it: whether the window opens on chosen weekdays only, which, and
// every other value by setting key.
type windowForm struct {
	Weekly bool
	Days   []string
	Values map[string]string
	// ReadOnly says, for each setting the environment provides, that it
	// cannot be changed here.
	ReadOnly []string
	Errors   map[string]string
}

// firmwareContent is the firmware page's content.
type firmwareContent struct {
	Versions []firmwareRow
	Defaults []selectOption
	// Upload is the upload form as entered, and Errors what is wrong with
	// it.
	Upload  firmware.Fields
	Errors  map[string]string
	Window  windowForm
	Rollout []rolloutGroup
	MaxMiB  int
	// NoAddress is true while the public_url setting is empty, so no
	// update can be sent.
	NoAddress bool
	// Unprovisionable is what the page warns of while no provisioning
	// file can be made, each why.
	Unprovisionable []string
}

// statusLabels names each status of a door in the rollout on pages.
var statusLabels = map[string]string{
	firmware.Pending: "Pending update", firmware.Updating: "Updating", firmware.Done: "Up to date", firmware.Failed: "Update failed",
	"": "Not in the rollout",
}

// rolloutGroups is the statuses the firmware page lists doors by, in
// order, with what each says when it has none.
var rolloutGroups = []struct {
	status string
	label  string
	empty  string
}{
	{firmware.Pending, "Pending", "No door is waiting for an update."},
	{firmware.Updating, "Updating", "No door is updating."},
	{firmware.Done, "Done", "No door runs its target yet."},
	{firmware.Failed, "Failed", "No update has failed."},
}

// versionText is v's version, or what to say when there is none.
func versionText(v *connector.StoredFirmware, none string) string {
	if v == nil {
		return none
	}
	return v.Version
}

// updateText is what pages say of the latest update u, in zone.
func updateText(u *connector.StoredUpdate, zone *time.Location) string {
	if u == nil {
		return "Never sent one"
	}
	switch u.State {
	case firmware.Updating:
		return "Sent " + u.Version + " on " + shownAt(zone, u.SentAt, secondLayout)
	case firmware.Succeeded:
		return "Updated to " + u.Version + " on " + shownAt(zone, *u.FinishedAt, secondLayout)
	}
	return "Failed to update to " + u.Version + " on " + shownAt(zone, *u.FinishedAt, secondLayout) + ": " + u.Detail
}

func rowOfDoorFirmware(df firmware.DoorFirmware, zone *time.Location) rolloutRow {
	return rolloutRow{
		DoorID: df.Door.ID, Door: df.Door.Name, Running: firmwareText(df.Door.Controller.FirmwareVersion),
		Target: versionText(df.Target, "None"), Update: updateText(df.Update, zone),
	}
}

// firmwareOptions is a select of every firmware of plan, after none,
// with the one whose id is chosen selected, or none when chosen is nil.
func firmwareOptions(all []connector.StoredFirmware, none string, chosen *int64) []selectOption {
	options := []selectOption{{Label: none, Selected: chosen == nil}}
	for _, f := range all {
		selected := chosen != nil && *chosen == f.ID
		options = append(options, idOption(f.ID, f.Version, selected))
	}
	return options
}

// defaultOf is the id of plan's default firmware, nil when there is none.
func defaultOf(plan firmware.Plan) *int64 {
	if f := plan.Default(); f != nil {
		return &f.ID
	}
	return nil
}

// windowOf is the update window form holding the settings all as they
// are.
func windowOf(all []settings.Setting) windowForm {
	form := windowForm{Values: map[string]string{}}
	for _, s := range all {
		if !slices.Contains(settings.UpdateKeys, s.Key) {
			continue
		}
		form.Values[s.Key] = s.Value
		if s.ReadOnly() {
			form.ReadOnly = append(form.ReadOnly, s.Key+" is set by "+s.EnvVar+" and cannot be changed here.")
		}
	}
	days := form.Values[settings.KeyUpdateDays]
	if days != settings.EveryDay {
		form.Weekly, form.Days = true, strings.Split(days, ",")
	}
	return form
}

// unprovisionable is why no provisioning file can be made with the site's
// broker defaults, read from all, every setting.
func unprovisionable(all []settings.Setting) []string {
	values := map[string]string{}
	for _, s := range all {
		values[s.Key] = s.Value
	}
	var problems []string
	for _, warning := range []string{
		settings.HostWarning(values[settings.KeyBrokerHost]),
		settings.BrokerWarning(values[settings.KeyBrokerTLS], values[settings.KeyBrokerCA]),
	} {
		if warning != "" {
			problems = append(problems, warning)
		}
	}
	return problems
}

// showFirmware shows the firmware page with status, its upload form
// holding upload with the problems errs, and its window form holding
// entered, or the settings as they are when entered is nil.
func (a *App) showFirmware(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, upload firmware.Fields, errs map[string]string, entered *windowForm) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	current, err := a.settings.Current(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	window := windowOf(all)
	if entered != nil {
		entered.ReadOnly = window.ReadOnly
		window = *entered
	}
	content := firmwareContent{
		Defaults: firmwareOptions(plan.Firmware, "No default", defaultOf(plan)), Upload: upload, Errors: errs, Window: window,
		MaxMiB: firmware.MaxMiB, NoAddress: current.PublicURL == "", Versions: []firmwareRow{}, Unprovisionable: unprovisionable(all),
	}
	zone := current.Zone
	for _, f := range plan.Firmware {
		running, targeting := counts(plan, f.Version)
		content.Versions = append(content.Versions, firmwareRow{
			ID: f.ID, Version: f.Version, Uploader: f.UploaderName, Uploaded: shownAt(zone, f.UploadedAt, secondLayout),
			Running: running, Targeting: targeting, Default: f.Default,
		})
	}
	for _, g := range rolloutGroups {
		group := rolloutGroup{Label: g.label, Empty: g.empty}
		for _, df := range plan.Doors {
			if df.Status == g.status {
				group.Rows = append(group.Rows, rowOfDoorFirmware(df, zone))
			}
		}
		content.Rollout = append(content.Rollout, group)
	}
	a.render(w, r, status, firmwareTemplate, "Firmware", &viewer, content)
}

func (a *App) firmwarePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showFirmware(w, r, http.StatusOK, viewer, firmware.Fields{}, nil, nil)
}

func (a *App) uploadFirmwareFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	f, stored, err := a.addFirmware(w, r, viewer)
	if errors.Is(err, errImageTooBig) {
		a.showFirmware(w, r, http.StatusRequestEntityTooLarge, viewer, f, map[string]string{firmware.Field: sentence(errImageTooBig.Error()) + "."}, nil)
		return
	}
	if errs, rejected := fieldErrors(err); rejected {
		a.showFirmware(w, r, http.StatusUnprocessableEntity, viewer, f, errs, nil)
		return
	}
	a.changed(w, r, err, firmwarePath(stored.ID), "firmware-uploaded")
}

// formFirmware reads the form field firmware_id: nil when it is empty,
// and an id otherwise, zero, which names nothing, when it is not one.
func formFirmware(r *http.Request) *int64 {
	value := r.PostFormValue("firmware_id")
	if value == "" {
		return nil
	}
	id, _ := strconv.ParseInt(value, 10, 64)
	return &id
}

func (a *App) defaultFirmwareFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	err := a.firmware.SetDefault(r.Context(), formFirmware(r))
	if errors.Is(err, firmware.ErrNoFirmware) {
		a.notFound(w, r, viewer, noFirmwareHere)
		return
	}
	a.changed(w, r, err, "/admin/firmware", "firmware-default-saved")
}

// noFirmwareHere is what a page says of firmware that is not there.
const noFirmwareHere = "That firmware is not there. It may have been deleted."

func (a *App) rolloutSettingsFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_ = r.ParseForm()
	form := windowForm{Weekly: r.PostFormValue("repeat") == repeatWeekly, Days: r.PostForm["days"], Values: map[string]string{}}
	for _, key := range settings.UpdateKeys {
		form.Values[key] = r.PostFormValue(key)
	}
	form.Values[settings.KeyUpdateDays] = settings.EveryDay
	if form.Weekly {
		form.Values[settings.KeyUpdateDays] = strings.Join(form.Days, ",")
	}
	_, err := a.settings.UpdateAll(r.Context(), form.Values, nil)
	if errs, rejected := fieldErrors(err); rejected {
		form.Errors = errs
		a.showFirmware(w, r, http.StatusUnprocessableEntity, viewer, firmware.Fields{}, nil, &form)
		return
	}
	a.changed(w, r, err, "/admin/firmware", "rollout-settings-saved")
}

// firmwareVersionContent is the page of one firmware's content: the
// firmware, the doors that run it or should, and why it cannot be
// deleted, empty when it can.
type firmwareVersionContent struct {
	Firmware connector.StoredFirmware
	Uploaded string
	Size     string
	Doors    []rolloutRow
	InUse    string
	Alert    string
}

func (a *App) showFirmwareVersion(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, alert string) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return
	}
	f, err := a.firmware.Get(r.Context(), pathID(r, "id"))
	if a.unloaded(w, r, viewer, err, firmware.ErrNoFirmware, noFirmwareHere) {
		return
	}
	plan, err := a.firmware.Plan(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := firmwareVersionContent{
		Firmware: f, Uploaded: shownAt(zone, f.UploadedAt, secondLayout), Size: strconv.Itoa(f.Size) + " bytes", Alert: alert, Doors: []rolloutRow{},
	}
	for _, df := range plan.Doors {
		runs, targets := concerns(df, f.Version)
		if runs || targets {
			row := rolloutRow{DoorID: df.Door.ID, Door: df.Door.Name, Running: notReported, Target: versionText(df.Target, "None"), Update: statusLabels[df.Status]}
			if df.Door.Controller != nil {
				row.Running = firmwareText(df.Door.Controller.FirmwareVersion)
			}
			content.Doors = append(content.Doors, row)
		}
	}
	if err := plan.InUse(f); err != nil {
		content.InUse = sentence(err.Error()) + "."
	}
	a.render(w, r, status, firmwareVersionTemplate, f.Version, &viewer, content)
}

func (a *App) firmwareVersionPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showFirmwareVersion(w, r, http.StatusOK, viewer, "")
}

func (a *App) deleteFirmwareFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	err := a.firmware.Delete(r.Context(), pathID(r, "id"))
	var inUse firmware.InUseError
	if errors.As(err, &inUse) {
		a.showFirmwareVersion(w, r, http.StatusConflict, viewer, sentence(inUse.Error())+".")
		return
	}
	if errors.Is(err, firmware.ErrNoFirmware) {
		a.notFound(w, r, viewer, noFirmwareHere)
		return
	}
	a.changed(w, r, err, "/admin/firmware", "firmware-deleted")
}

// doorFirmwareCard is what a door's page shows of its firmware: what its
// controller runs, its target, its status in the rollout, its latest
// update, the choice of firmware for it, and why it cannot be sent its
// target now, empty when it can.
type doorFirmwareCard struct {
	Running    string
	Target     string
	Status     string
	LastUpdate string
	Options    []selectOption
	Note       string
}

// firmwareCard is the firmware card of d's page for viewer, nil when the
// viewer may not manage firmware or no controller is enrolled as d.
func (a *App) firmwareCard(ctx context.Context, viewer auth.User, d connector.StoredDoor, zone *time.Location) (*doorFirmwareCard, error) {
	if d.Controller == nil || !viewer.Has(auth.RoleManageFirmware) {
		return nil, nil
	}
	plan, err := a.firmware.Plan(ctx)
	if err != nil {
		return nil, err
	}
	df := plan.Doors[slices.IndexFunc(plan.Doors, func(df firmware.DoorFirmware) bool { return df.Door.ID == d.ID })]
	none := "Follow the default (" + versionText(plan.Default(), "none") + ")"
	card := &doorFirmwareCard{
		Running: firmwareText(d.Controller.FirmwareVersion), Target: versionText(df.Target, "None"), Status: statusLabels[df.Status],
		LastUpdate: updateText(df.Update, zone), Options: firmwareOptions(plan.Firmware, none, d.FirmwareID),
	}
	if df.Target == nil {
		card.Note = "Choose a default version on the firmware page, or one for this door, to update it."
	} else if !d.Controller.Online {
		card.Note = "The door can be updated once its controller is connected."
	}
	return card, nil
}

// firmwareRefusals maps each refusal of a firmware change made from a
// door's page to the status and the message the page shows.
var firmwareRefusals = []doorRefusal{
	{firmware.ErrNoFirmware, http.StatusNotFound, noFirmwareHere},
	{firmware.ErrNoTarget, http.StatusConflict, "The door has no firmware to run. Choose a default version or one for this door first."},
	{firmware.ErrNoAddress, http.StatusConflict, "Set the public_url setting first, so the door can download its firmware."},
	{controller.ErrOffline, http.StatusConflict, "The controller is not connected, so it cannot be updated. Try again once it is online."},
}

func (a *App) doorFirmwareFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		_, err := a.firmware.SetOverride(r.Context(), d.ID, formFirmware(r))
		a.doorRefused(w, r, viewer, d, err, "door-firmware-saved", firmwareRefusals)
	}
}

func (a *App) updateDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		_, err := a.firmware.UpdateNow(r.Context(), d.ID, viewer.ID)
		a.doorRefused(w, r, viewer, d, err, "update-sent", firmwareRefusals)
	}
}
