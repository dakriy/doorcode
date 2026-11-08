package app

import (
	"errors"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

type settingView struct {
	Key       string     `json:"key" doc:"The setting's key."`
	Value     string     `json:"value" doc:"The value in effect."`
	Source    string     `json:"source" doc:"Where the value comes from: environment, database, or default."`
	ReadOnly  bool       `json:"read_only" doc:"True when the environment provides the value, which then cannot be changed here."`
	EnvVar    string     `json:"env_var" doc:"The environment variable that overrides the setting."`
	UpdatedAt *time.Time `json:"updated_at,omitempty" doc:"When an administrator last stored a value; absent when none is stored."`
}

type settingList struct {
	Settings []settingView `json:"settings" doc:"Every setting, in display order."`
}

type settingUpdate struct {
	Value *string `json:"value" doc:"The new value."`
}

// settingKey is the path parameter naming a setting.
var settingKey = openapi.Param{Name: "key", In: "path", Description: "The setting's key.", Example: "site_name"}

func (a *App) settingsRoutes() []route {
	admin := holding(auth.RoleAdmin)
	example := "North Campus"
	return []route{
		pageRoute(http.MethodGet, "/admin/settings", admin, a.settingsPage),
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
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object with a string value, or the setting does not accept the value.", Body: errorBody{}},
				{Status: http.StatusNotFound, Description: "No setting has this key.", Body: errorBody{}},
				{Status: http.StatusConflict, Description: "The environment provides the setting, so it is read-only.", Body: errorBody{}},
			},
		}),
	}
}

func viewOf(s settings.Setting) settingView {
	view := settingView{
		Key:      s.Key,
		Value:    s.Value,
		Source:   string(s.Source),
		ReadOnly: s.ReadOnly(),
		EnvVar:   s.EnvVar,
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
		a.internalError(w, err)
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
		a.settingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(s))
}

func (a *App) updateSetting(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body settingUpdate
	err := decodeJSON(w, r, &body)
	if err != nil || body.Value == nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with a string "value"`)
		return
	}
	s, err := a.settings.Update(r.Context(), r.PathValue("key"), *body.Value)
	if err != nil {
		a.settingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(s))
}

func (a *App) settingError(w http.ResponseWriter, err error) {
	if errors.Is(err, settings.ErrUnknown) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, settings.ErrInvalid) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, settings.ErrReadOnly) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.internalError(w, err)
}
