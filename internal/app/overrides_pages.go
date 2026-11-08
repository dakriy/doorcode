package app

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/door"
	"github.com/dakriy/doorcode/internal/timeline"
)

func (a *App) overridePageRoutes() []route {
	doors := opensDoors
	return []route{
		pageRoute(http.MethodGet, "/admin/doors/{id}/overrides/new", doors, a.newOverridePage),
		pageRoute(http.MethodPost, "/admin/doors/{id}/overrides", doors, a.createOverrideFromForm),
		pageRoute(http.MethodGet, "/admin/doors/{id}/overrides/{override}", doors, a.overridePage),
		pageRoute(http.MethodPost, "/admin/doors/{id}/overrides/{override}", doors, a.saveOverrideFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/overrides/{override}/cancel", doors, a.cancelOverrideFromForm),
		pageRoute(http.MethodPost, "/admin/doors/{id}/overrides/{override}/delete", doors, a.deleteOverrideFromForm),
	}
}

const (
	// inputTimeLayout is how a datetime-local input writes a time.
	inputTimeLayout = "2006-01-02T15:04"
	// overrideLayout is how pages write when an override starts and ends.
	overrideLayout = "Mon 2 Jan 2006 15:04"
)

// overrideRow is one override as a door's page lists it.
type overrideRow struct {
	ID      int64
	Kind    string
	Active  bool
	Start   string
	End     string
	Reason  string
	Creator string
}

// kindLabels names each kind of override on pages.
var kindLabels = map[string]string{timeline.Open: "Open", timeline.Locked: "Locked"}

func (a *App) rowOfOverride(o connector.StoredOverride, zone *time.Location) overrideRow {
	return overrideRow{
		ID: o.ID, Kind: kindLabels[o.Kind], Active: door.Active(o, a.clock.Now()),
		Start: o.Start.In(zone).Format(overrideLayout), End: o.End.In(zone).Format(overrideLayout),
		Reason: o.Reason, Creator: o.CreatorName,
	}
}

// overrideForm is the override form as entered: times as a datetime-local
// input writes them in the site timezone.
type overrideForm struct {
	Kind   string
	Start  string
	End    string
	Reason string
}

func formOfOverride(o connector.StoredOverride, zone *time.Location) overrideForm {
	return overrideForm{Kind: o.Kind, Start: inputTime(o.Start, zone), End: inputTime(o.End, zone), Reason: o.Reason}
}

func inputTime(t time.Time, zone *time.Location) string {
	return t.In(zone).Format(inputTimeLayout)
}

// fields is f as an override's fields, its times read in zone; a time that
// cannot be read is one not given.
func (f overrideForm) fields(zone *time.Location) door.OverrideFields {
	start, _ := time.ParseInLocation(inputTimeLayout, f.Start, zone)
	end, _ := time.ParseInLocation(inputTimeLayout, f.End, zone)
	return door.OverrideFields{Kind: f.Kind, Start: start, End: end, Reason: f.Reason}
}

func readOverrideForm(r *http.Request) overrideForm {
	return overrideForm{Kind: r.PostFormValue("kind"), Start: r.PostFormValue("start"), End: r.PostFormValue("end"), Reason: r.PostFormValue("reason")}
}

// overrideContent is the override pages' content: the door, the override,
// zero on the page that creates one, and its form as entered.
type overrideContent struct {
	Door     connector.StoredDoor
	Override connector.StoredOverride
	Form     overrideForm
	Errors   map[string]string
	Kinds    []kindOption
	Zone     string
	// Action is where the form is submitted.
	Action string
}

// kindOption is one kind of override to choose from.
type kindOption struct {
	Value    string
	Label    string
	Selected bool
}

// showOverride shows the page about o, an override of d, or the page that
// creates one when o is zero, with the form holding f.
func (a *App) showOverride(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, d storedDoor, o connector.StoredOverride, f overrideForm, errs map[string]string, zone *time.Location) {
	content := overrideContent{Door: d.StoredDoor, Override: o, Form: f, Errors: errs, Zone: zone.String(), Action: doorPath(d.ID) + "/overrides"}
	for _, kind := range []string{timeline.Open, timeline.Locked} {
		content.Kinds = append(content.Kinds, kindOption{Value: kind, Label: kindLabels[kind], Selected: kind == f.Kind})
	}
	title := "New override"
	if o.ID != 0 {
		title, content.Action = "Override", overridePath(d.ID, o.ID)
	}
	a.render(w, r, status, overrideTemplate, title, &viewer, content)
}

func overridePath(doorID, id int64) string {
	return doorPath(doorID) + "/overrides/" + strconv.FormatInt(id, 10)
}

// pageOverride returns the door and the override of it the path names,
// and the site timezone, after showing that there is none or that the
// store failed.
func (a *App) pageOverride(w http.ResponseWriter, r *http.Request, viewer auth.User) (storedDoor, connector.StoredOverride, *time.Location, bool) {
	d, zone, ok := a.pageOverrideDoor(w, r, viewer)
	if !ok {
		return d, connector.StoredOverride{}, zone, false
	}
	o, err := a.doors.Override(r.Context(), d.ID, pathID(r, "override"))
	if a.unloaded(w, r, viewer, err, door.ErrNoOverride, "There is no override here. It may have been deleted.") {
		return d, o, zone, false
	}
	return d, o, zone, true
}

// pageOverrideDoor returns the door the path names and the site timezone,
// after showing that there is none or that the store failed.
func (a *App) pageOverrideDoor(w http.ResponseWriter, r *http.Request, viewer auth.User) (storedDoor, *time.Location, bool) {
	zone, ok := a.siteZone(w, r)
	if !ok {
		return storedDoor{}, nil, false
	}
	d, ok := a.pageDoor(w, r, viewer)
	return d, zone, ok
}

func (a *App) newOverridePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, zone, ok := a.pageOverrideDoor(w, r, viewer); ok {
		form := overrideForm{Kind: timeline.Locked, Start: inputTime(a.clock.Now(), zone)}
		a.showOverride(w, r, http.StatusOK, viewer, d, connector.StoredOverride{}, form, nil, zone)
	}
}

func (a *App) createOverrideFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	d, zone, ok := a.pageOverrideDoor(w, r, viewer)
	if !ok {
		return
	}
	form := readOverrideForm(r)
	_, err := a.newOverride(r.Context(), d.ID, viewer.ID, form.fields(zone))
	if errs, rejected := fieldErrors(err); rejected {
		a.showOverride(w, r, http.StatusUnprocessableEntity, viewer, d, connector.StoredOverride{}, form, errs, zone)
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "override-created")
}

func (a *App) overridePage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, o, zone, ok := a.pageOverride(w, r, viewer); ok {
		a.showOverride(w, r, http.StatusOK, viewer, d, o, formOfOverride(o, zone), nil, zone)
	}
}

func (a *App) saveOverrideFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	d, o, zone, ok := a.pageOverride(w, r, viewer)
	if !ok {
		return
	}
	form := readOverrideForm(r)
	_, err := a.changeOverride(r.Context(), d.ID, o.ID, form.fields(zone))
	if errs, rejected := fieldErrors(err); rejected {
		a.showOverride(w, r, http.StatusUnprocessableEntity, viewer, d, o, form, errs, zone)
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "override-saved")
}

func (a *App) cancelOverrideFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		_, err := a.endOverride(r.Context(), d.ID, pathID(r, "override"))
		a.doorChanged(w, r, viewer, d, err, "override-cancelled")
	}
}

func (a *App) deleteOverrideFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	if d, ok := a.pageDoor(w, r, viewer); ok {
		a.doorChanged(w, r, viewer, d, a.removeOverride(r.Context(), d.ID, pathID(r, "override")), "override-deleted")
	}
}
