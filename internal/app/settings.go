package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dakriy/doorcode/internal/settings"
)

// maxSettingBody caps the size of a setting update request.
const maxSettingBody = 65536 // 64 KiB

type settingView struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Source    string     `json:"source"`
	ReadOnly  bool       `json:"read_only"`
	EnvVar    string     `json:"env_var"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type settingList struct {
	Settings []settingView `json:"settings"`
}

type settingUpdate struct {
	Value *string `json:"value"`
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

func (a *App) listSettings(w http.ResponseWriter, r *http.Request) {
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

func (a *App) getSetting(w http.ResponseWriter, r *http.Request) {
	s, err := a.settings.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		a.settingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewOf(s))
}

func (a *App) updateSetting(w http.ResponseWriter, r *http.Request) {
	var body settingUpdate
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSettingBody)).Decode(&body)
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
	if errors.Is(err, settings.ErrReadOnly) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	a.internalError(w, err)
}
