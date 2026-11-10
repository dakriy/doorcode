package app

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/picture"
	"github.com/dakriy/doorcode/internal/settings"
)

type settingView struct {
	Key       string     `json:"key" doc:"The setting's key."`
	Value     string     `json:"value" doc:"The value in effect."`
	Source    string     `json:"source" doc:"Where the value comes from: environment, database, or default."`
	ReadOnly  bool       `json:"read_only" doc:"True when the environment provides the value, which then cannot be changed here."`
	EnvVar    string     `json:"env_var" doc:"The environment variable that overrides the setting."`
	Secret    bool       `json:"secret,omitempty" doc:"True for a secret, which is stored encrypted and whose value shows only as ******** when it has one. Storing an empty value leaves a secret as it is; clearing it empties it."`
	UpdatedAt *time.Time `json:"updated_at,omitempty" doc:"When an administrator last stored a value; absent when none is stored."`
}

type settingList struct {
	Settings []settingView `json:"settings" doc:"Every setting, in display order."`
}

type settingUpdate struct {
	Value *string `json:"value,omitempty" doc:"The new value. Give either it or clear."`
	Clear bool    `json:"clear,omitempty" doc:"True stores the empty value, which for a secret removes it, where an empty value leaves a secret as it is. Give either it or value."`
}

// signInImagePath is where the sign-in page's picture is served.
const signInImagePath = "/api/v1/sign-in-image"

// signInImageCache lets browsers keep the sign-in page's picture, whose
// address changes with every new picture.
const signInImageCache = "public, max-age=31536000, immutable"

// signInImageUpload is the body that sets the sign-in page's picture.
type signInImageUpload struct {
	Picture openapi.File `json:"picture" doc:"The image: JPEG, PNG, GIF, or WebP."`
}

// settingKey is the path parameter naming a setting.
var settingKey = openapi.Param{Name: "key", In: "path", Description: "The setting's key.", Example: "site_name"}

func (a *App) settingsRoutes() []route {
	admin := holding(auth.RoleAdmin)
	example := "North Campus"
	return []route{
		pageRoute(http.MethodGet, "/admin/settings", admin, a.settingsPage),
		pageRoute(http.MethodPost, "/admin/settings", admin, a.saveSettingsFromForm),
		apiRoute(http.MethodGet, "/settings", admin, a.listSettings, apiDoc{
			id: "listSettings", summary: "List every setting", tag: "settings",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every setting.", Body: settingList{}}},
		}),
		apiRoute(http.MethodGet, "/settings/{key}", admin, a.getSetting, apiDoc{
			id: "getSetting", summary: "Read one setting", tag: "settings",
			params: []openapi.Param{settingKey},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The setting.", Body: settingView{}},
				{Status: http.StatusNotFound, Description: "No setting has this key.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPut, "/settings/{key}", admin, a.updateSetting, apiDoc{
			id: "updateSetting", summary: "Store a new value for a setting", tag: "settings",
			params:  []openapi.Param{settingKey},
			request: settingUpdate{Value: &example},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The setting with its new value.", Body: settingView{}},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object with either a string value or clear set to true, or the setting does not accept the value.", Body: errorBody{}},
				{Status: http.StatusNotFound, Description: "No setting has this key.", Body: errorBody{}},
				{Status: http.StatusConflict, Description: "The environment provides the setting, so it is read-only.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodGet, "/sign-in-image", anyone, a.serveSignInImage, apiDoc{
			id: "getSignInImage", summary: "Read the sign-in page's picture", tag: "settings",
			params: []openapi.Param{{Name: "v", In: "query", Description: "Ignored. The sign-in page adds when the picture was stored, so browsers fetch a new picture at a new address.", Example: "1767603600"}},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The picture, as JPEG.", Body: openapi.File{}, Type: picture.ContentType},
				noSignInImage,
				{Status: http.StatusInternalServerError, Description: "The server failed; the body names a request id to give an administrator.", Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodPut, "/sign-in-image", admin, a.putSignInImage, apiDoc{
			id: "setSignInImage", summary: "Set the sign-in page's picture", tag: "settings", form: signInImageUpload{},
			responses: []openapi.Response{
				{Status: http.StatusNoContent, Description: fmt.Sprintf("The sign-in page shows the picture, reduced to at most %d pixels on its longest side.", picture.MaxSide)},
				{Status: http.StatusBadRequest, Description: "The body is not a multipart form with the file in the field picture, or the file is not an image that can be read.", Body: errorBody{}},
				{Status: http.StatusRequestEntityTooLarge, Description: fmt.Sprintf("The file is larger than %d MB.", picture.Megabytes()), Body: errorBody{}},
			},
		}),
		apiRoute(http.MethodDelete, "/sign-in-image", admin, a.deleteSignInImage, apiDoc{
			id: "deleteSignInImage", summary: "Take the picture off the sign-in page", tag: "settings",
			responses: []openapi.Response{{Status: http.StatusNoContent, Description: "The sign-in page shows no picture."}, noSignInImage},
		}),
		pageRoute(http.MethodPost, "/admin/settings/sign-in-image", admin, a.uploadSignInImageFromForm),
		pageRoute(http.MethodPost, "/admin/settings/sign-in-image/delete", admin, a.deleteSignInImageFromForm),
	}
}

// noSignInImage answers a request for the sign-in page's picture when it
// shows none.
var noSignInImage = openapi.Response{Status: http.StatusNotFound, Description: "The sign-in page shows no picture.", Body: errorBody{}}

func viewOf(s settings.Setting) settingView {
	view := settingView{
		Key:      s.Key,
		Value:    s.Value,
		Source:   string(s.Source),
		ReadOnly: s.ReadOnly(),
		EnvVar:   s.EnvVar,
		Secret:   s.Secret,
	}
	if !s.UpdatedAt.IsZero() {
		updated := s.UpdatedAt.UTC()
		view.UpdatedAt = &updated
	}
	return view
}

func (a *App) listSettings(w http.ResponseWriter, r *http.Request, _ auth.User) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	views := make([]settingView, 0, len(all))
	for _, s := range all {
		views = append(views, viewOf(s))
	}
	writeJSON(w, http.StatusOK, settingList{Settings: views})
}

func (a *App) getSetting(w http.ResponseWriter, r *http.Request, _ auth.User) {
	s, err := a.settings.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		a.settingError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(s))
}

func (a *App) updateSetting(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body settingUpdate
	err := decodeJSON(w, r, &body)
	if err != nil || (body.Value != nil) == body.Clear {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with either a string "value" or "clear": true`)
		return
	}
	var s settings.Setting
	if body.Clear {
		s, err = a.settings.Clear(r.Context(), r.PathValue("key"))
	} else {
		s, err = a.settings.Update(r.Context(), r.PathValue("key"), *body.Value)
	}
	if err != nil {
		a.settingError(w, r, err)
		return
	}
	a.settingsChanged(r.Context(), []string{s.Key})
	writeJSON(w, http.StatusOK, viewOf(s))
}

// settingStatuses maps each refusal the settings report to the status the
// API answers it with.
var settingStatuses = []refusalStatus{
	{settings.ErrUnknown, http.StatusNotFound},
	{settings.ErrInvalid, http.StatusBadRequest},
	{settings.ErrReadOnly, http.StatusConflict},
	{settings.ErrNoSignInImage, http.StatusNotFound},
	{errTooBig, http.StatusRequestEntityTooLarge},
}

// settingError answers an API request with what err, from the settings,
// means.
func (a *App) settingError(w http.ResponseWriter, r *http.Request, err error) {
	a.refuse(w, r, err, settingStatuses)
}

// settingsContent is the settings page's content: every setting in its
// section, with the value entered for it when a form was refused, and
// what is wrong with each refused value; where the sign-in page's picture
// is served, empty when it shows none; and an alert to show above
// everything else. It shows the sections of one tab, and the sign-in
// page's picture with them when Pictures says so.
type settingsContent struct {
	Tabs           tabStrip
	Sections       []settingSection
	Pictures       bool
	Errors         map[string]string
	SignInImage    string
	Alert          string
	MaxPictureMB   int
	MaxPictureSide int
}

// settingsPath is the settings page's address.
const settingsPath = "/admin/settings"

// settingsTabPath is the address of the settings page showing tab.
func settingsTabPath(tab settings.Tab) string {
	return tabPath(settingsPath, tab)
}

// settingSection is one part of the settings page and its settings, On
// when they are in use: always, or while the setting that toggles them is
// on.
type settingSection struct {
	settings.Section
	Rows []settingRow
	On   bool
}

// settingRow is one setting as the settings page shows it, with the value
// its input holds and, for a choice, the options it offers.
type settingRow struct {
	settings.Setting
	Entered string
	Options []selectOption
	// Warning is what the page warns of the value entered.
	Warning string
	// Heading is the group the row starts, empty when it starts none.
	Heading string
}

// heading is the group a row of group starts after a row of previous:
// group when the two differ, empty otherwise.
func heading(group, previous string) string {
	if group == previous {
		return ""
	}
	return group
}

// showSettings shows tab of the settings page with status, its inputs
// holding entered, or each setting's value where entered has none.
func (a *App) showSettings(w http.ResponseWriter, r *http.Request, status int, viewer auth.User, tab settings.Tab, entered map[string]string, errs map[string]string, alert string) {
	all, err := a.settings.List(r.Context())
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	pictures, image := tab == settings.TabLook, ""
	if pictures {
		image, err = a.signInImageURL(r)
	}
	if err != nil {
		a.pageError(w, r, err)
		return
	}
	content := settingsContent{
		Tabs: stripOf("Settings tabs", settingsPath, settings.Tabs, tab), Sections: a.sectionsOf(all, tab, entered), Pictures: pictures,
		Errors: errs, SignInImage: image, Alert: alert,
		MaxPictureMB: picture.Megabytes(), MaxPictureSide: picture.MaxSide,
	}
	a.render(w, r, status, settingsTemplate, "Settings", &viewer, content)
}

// sectionsOf is the settings of all, listed section by section, that tab
// shows, as the settings page's sections, each input holding entered or
// the setting's value where entered has none or the setting is a secret,
// which a page never shows again, and each section on as what its toggle
// holds says.
func (a *App) sectionsOf(all []settings.Setting, tab settings.Tab, entered map[string]string) []settingSection {
	held := map[string]string{}
	for _, s := range all {
		value, given := entered[s.Key]
		if !given || s.Secret {
			value = s.Value
		}
		held[s.Key] = value
	}
	var sections []settingSection
	// Only door fields have a group, and they share one section, so the
	// group of the row before is the last one's of any section.
	previous := ""
	for _, s := range all {
		if s.Section.Tab != tab {
			continue
		}
		last := len(sections) - 1
		if last < 0 || sections[last].Name != s.Section.Name {
			on := s.Section.Toggle == "" || held[s.Section.Toggle] == settings.Yes
			sections = append(sections, settingSection{Section: s.Section, On: on})
			last++
		}
		row := a.settingRowOf(s, held, previous)
		previous = s.Group
		sections[last].Rows = append(sections[last].Rows, row)
	}
	return sections
}

// settingRowOf is the settings page's row for s after a row of the group
// previous, its input holding what held holds for it, with the choices it
// offers and what the page warns of the value held.
func (a *App) settingRowOf(s settings.Setting, held map[string]string, previous string) settingRow {
	row := settingRow{Setting: s, Entered: held[s.Key], Heading: heading(s.Group, previous)}
	if choices := a.settings.Choices(s); len(choices) > 0 {
		row.Options = choiceOptions(choices, row.Entered)
	}
	switch s.Key {
	case settings.KeyConnectTimeout:
		row.Warning = settings.RevertWarning(row.Entered, held[settings.KeyUpdateTimeout])
	case settings.KeyBrokerHost:
		row.Warning = settings.HostWarning(row.Entered)
	case settings.KeyBrokerCA:
		row.Warning = settings.BrokerWarning(held[settings.KeyBrokerTLS], row.Entered)
	case settings.KeyMeshEnabled:
		row.Warning = settings.MeshWarning(row.Entered, held[settings.KeyMeshSSID], held[settings.KeyMeshPassword])
	}
	return row
}

// choiceOptions is the options of a select offering choices with value
// chosen, and value first when it is not among them, so that saving the
// form keeps it.
func choiceOptions(choices []string, value string) []selectOption {
	var options []selectOption
	if !slices.Contains(choices, value) {
		options = append(options, selectOption{Value: value, Label: value, Selected: true})
	}
	for _, c := range choices {
		options = append(options, selectOption{Value: c, Label: c, Selected: c == value})
	}
	return options
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	a.showSettings(w, r, http.StatusOK, viewer, tabNamed(settings.Tabs, r.URL.Query().Get("tab")), nil, nil, "")
}

// saveSettingsFromForm stores every setting the form changed, and clears
// every secret it ticks, or none when one is refused, and sends the doors
// what that changes for them. It goes back to the tab that shows what the
// form sent, or shows the tab with the first value refused.
func (a *App) saveSettingsFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	_ = r.ParseForm()
	entered := map[string]string{}
	for key := range r.PostForm {
		entered[key] = r.PostForm.Get(key)
	}
	changed, err := a.settings.UpdateAll(r.Context(), entered, r.PostForm["clear"])
	if errs, rejected := fieldErrors(err); rejected {
		tab := settings.TabHolding(slices.Collect(maps.Keys(errs)))
		a.showSettings(w, r, http.StatusUnprocessableEntity, viewer, tab, entered, errs, "")
		return
	}
	if err == nil {
		a.settingsChanged(r.Context(), changed)
	}
	sent := append(slices.Collect(maps.Keys(entered)), r.PostForm["clear"]...)
	a.changed(w, r, err, settingsTabPath(settings.TabHolding(sent)), "settings-saved")
}

// signInImageURL is where the sign-in page's picture is served, with when
// it was stored so a new picture has a new address; empty when the page
// shows none.
func (a *App) signInImageURL(r *http.Request) (string, error) {
	image, found, err := a.settings.SignInImage(r.Context())
	if !found {
		return "", err
	}
	return signInImagePath + "?v=" + strconv.FormatInt(image.UpdatedAt.Unix(), 10), err
}

func (a *App) serveSignInImage(w http.ResponseWriter, r *http.Request, _ auth.User) {
	image, found, err := a.settings.SignInImage(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	if !found {
		a.settingError(w, r, settings.ErrNoSignInImage)
		return
	}
	w.Header().Set("Content-Type", picture.ContentType)
	w.Header().Set("Cache-Control", signInImageCache)
	_, _ = w.Write(image.Data)
}

// setSignInImage makes the picture r uploads the sign-in page's.
func (a *App) setSignInImage(w http.ResponseWriter, r *http.Request) error {
	data, err := readUpload(w, r)
	if err != nil {
		return err
	}
	return a.settings.SetSignInImage(r.Context(), data)
}

func (a *App) putSignInImage(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.settingDone(w, r, a.setSignInImage(w, r))
}

func (a *App) deleteSignInImage(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.settingDone(w, r, a.settings.RemoveSignInImage(r.Context()))
}

// settingDone answers an API request that changed the settings with no
// content, or with what err means.
func (a *App) settingDone(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		a.settingError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) uploadSignInImageFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	err := a.setSignInImage(w, r)
	if status, refused := uploadRefusal(err); refused {
		a.showSettings(w, r, status, viewer, settings.TabLook, nil, map[string]string{picture.Field: err.Error()}, "")
		return
	}
	a.changed(w, r, err, settingsTabPath(settings.TabLook), "sign-in-image-saved")
}

func (a *App) deleteSignInImageFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	err := a.settings.RemoveSignInImage(r.Context())
	if errors.Is(err, settings.ErrNoSignInImage) {
		a.showSettings(w, r, http.StatusNotFound, viewer, settings.TabLook, nil, nil, "The sign-in page shows no picture any more. It may have been removed already.")
		return
	}
	a.changed(w, r, err, settingsTabPath(settings.TabLook), "sign-in-image-removed")
}
