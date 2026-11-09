package app

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

type doorConfigView struct {
	StrikePulseMS          int    `json:"strike_pulse_ms" doc:"How long the strike is released for an entry, in milliseconds."`
	KeypadIdleClearMS      int    `json:"keypad_idle_clear_ms" doc:"How long a half-typed code waits for the next key before the keypad clears it, in milliseconds."`
	MinCodeLength          int    `json:"min_code_length" doc:"The fewest digits the keypad sends as a code."`
	MaxCodeLength          int    `json:"max_code_length" doc:"The most digits the keypad sends as a code."`
	SubmitKey              string `json:"submit_key" doc:"The key that ends a code: #, A, B, C, or D."`
	ReplyTimeoutMS         int    `json:"reply_timeout_ms" doc:"How long the door waits for the server's answer to a code, in milliseconds."`
	TelemetryIntervalS     int    `json:"telemetry_interval_s" doc:"How often the door reports its telemetry, in seconds."`
	HeldOpenS              int    `json:"held_open_s" doc:"How long the door may stay open before it is logged as held open, in seconds."`
	TimelineRefreshHours   int    `json:"timeline_refresh_hours" doc:"How often the server sends the door its timeline when nothing changed it, in hours."`
	CredentialRotationDays int    `json:"credential_rotation_days" doc:"How many days the door's broker credentials work before the server issues new ones."`
	RateLimitPerMinute     int    `json:"rate_limit_per_minute" doc:"How many codes a minute the server checks for the door."`
	RateLimitBurst         int    `json:"rate_limit_burst" doc:"How many codes the server checks for the door in a burst."`
	NTPServer              string `json:"ntp_server" doc:"The time server the door sets its clock from."`
	BrokerHost             string `json:"broker_host" doc:"The host name or IP address of the broker the door connects to; empty keeps the broker it has."`
	BrokerPort             int    `json:"broker_port" doc:"The port of the broker the door connects to."`
	BrokerTLS              bool   `json:"broker_tls" doc:"Whether the door connects to the broker over TLS; false is for development brokers only."`
	BrokerCAPEM            string `json:"broker_ca_pem" doc:"The PEM certificate of the CA, or of the broker itself, the door verifies the broker against; empty keeps the one it has."`
	WifiSSID               string `json:"wifi_ssid" doc:"The Wi-Fi network the door joins; empty keeps the network it uses. A door falls back to the last network that worked when a new one fails."`
	WifiPassword           string `json:"wifi_password" doc:"The Wi-Fi network's password, a secret: ******** when it has one."`
	WifiRoamRSSI           int    `json:"wifi_roam_rssi" doc:"The signal, in dBm, below which the door looks for an access point of the same network at least 8 dB stronger and moves to it."`
	KeepaliveS             int    `json:"keepalive_s" doc:"The door's MQTT keep-alive interval, in seconds, from its next connection; it shows offline once one and a half of these pass without a packet."`
	ReconnectMaxS          int    `json:"reconnect_max_s" doc:"The longest pause between the door's attempts to connect again, in seconds; it waits 1, 2, 4 seconds and so on up to this."`
	MeshEnabled            bool   `json:"mesh_enabled" doc:"Whether the door may join or form a Wi-Fi mesh with other doors when its own access point's signal is poor."`
	MeshSSID               string `json:"mesh_ssid" doc:"The mesh network's name, the same at every door of the site."`
	MeshPassword           string `json:"mesh_password" doc:"The mesh network's password, a secret: ******** when it has one."`
	MeshMinRouterRSSI      int    `json:"mesh_min_router_rssi" doc:"The signal, in dBm, below which a door with the mesh enabled joins it through a neighboring door when that is the strongest access point it sees."`
	MeshMaxLayer           int    `json:"mesh_max_layer" doc:"The most hops from the access point a door may be in the mesh."`
	UpdateConnectTimeoutS  int    `json:"update_connect_timeout_s" doc:"How long a door on newly installed firmware tries to connect as its door before it goes back to its previous image, in seconds."`
	AcceptUnsignedFirmware bool   `json:"accept_unsigned_firmware" doc:"Whether the door installs firmware without a valid signature; for bench and development boards only."`
	LogLevel               string `json:"log_level" doc:"How much the door logs: error, warn, info, or debug."`
}

type doorConfigBody struct {
	Effective doorConfigView    `json:"effective" doc:"The configuration the door runs with: its overrides, and the site's defaults for the rest. A secret shows as ******** when it has a value."`
	Overrides map[string]string `json:"overrides" doc:"The door's own values, each keyed by the name of the field of effective it overrides; a secret shows as ******** when it has a value."`
}

type doorConfigRequest struct {
	Overrides map[string]string `json:"overrides" doc:"The door's own values, each keyed by the name of a field of the effective configuration; the site's defaults apply to the fields left out. They replace the overrides the door had. A secret given as ******** keeps the value the door has for it."`
}

func (a *App) doorConfigRoutes() []route {
	doors := holding(auth.RoleManageDoors)
	params := []openapi.Param{doorIDParam}
	ok := openapi.Response{Status: http.StatusOK, Description: "The door's configuration.", Body: doorConfigBody{}}
	return []route{
		pageRoute(http.MethodPost, "/admin/doors/{id}/config", doors, a.configureDoorFromForm),
		apiRoute(http.MethodGet, "/doors/{id}/config", doors, a.getDoorConfig, apiDoc{
			id: "getDoorConfig", summary: "Read a door's configuration", tag: "doors", params: params,
			responses: []openapi.Response{ok, noDoor},
		}),
		apiRoute(http.MethodPut, "/doors/{id}/config", doors, a.putDoorConfig, apiDoc{
			id: "updateDoorConfig", summary: "Replace a door's configuration overrides", tag: "doors", params: params,
			request: doorConfigRequest{Overrides: map[string]string{"strike_pulse_ms": "5000", "log_level": "debug"}},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "The door's configuration, which is on its way to its controller.", Body: doorConfigBody{}},
				{Status: http.StatusBadRequest, Description: "The body is not a JSON object with an object of overrides, or an override names no field or has a value the field does not take; the error says which.", Body: errorBody{}},
				noDoor,
			},
		}),
	}
}

// effectiveConfig is the configuration d runs with, and the site's
// defaults it starts from.
func (a *App) effectiveConfig(ctx context.Context, d connector.StoredDoor) (settings.DoorConfig, settings.DoorDefaults, error) {
	defaults, err := a.settings.DoorDefaults(ctx)
	if err != nil {
		return settings.DoorConfig{}, settings.DoorDefaults{}, err
	}
	config, err := defaults.For(d.Config)
	return config, defaults, err
}

// writeDoorConfig answers with the configuration of d, which err may say
// could not be read or stored.
func (a *App) writeDoorConfig(w http.ResponseWriter, r *http.Request, d connector.StoredDoor, err error) {
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	config, _, err := a.effectiveConfig(r.Context(), d)
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, doorConfigBody{Effective: doorConfigView(config.Masked()), Overrides: settings.ShownOverrides(d.Config)})
}

func (a *App) getDoorConfig(w http.ResponseWriter, r *http.Request, _ auth.User) {
	d, err := a.doors.Door(r.Context(), pathID(r, "id"))
	a.writeDoorConfig(w, r, d, err)
}

func (a *App) putDoorConfig(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var body doorConfigRequest
	if err := decodeJSON(w, r, &body); err != nil || body.Overrides == nil {
		writeError(w, http.StatusBadRequest, `body must be a JSON object with an object "overrides" of strings`)
		return
	}
	d, err := a.configureDoor(r.Context(), pathID(r, "id"), body.Overrides)
	a.writeDoorConfig(w, r, d, err)
}

// configRow is one field of a door's configuration as its page shows it:
// the site's default, the door's own value as entered, empty to follow
// the default, and the value in effect, each secret masked; whether the
// door has its own value of a secret, which its page offers to clear; and
// what the page warns of the value in effect.
type configRow struct {
	settings.DoorField
	Default   string
	Entered   string
	Effective string
	Set       bool
	Warning   string
}

// configRows is each field of d's configuration, with the overrides
// entered, or d's own when entered is nil.
func configRows(d connector.StoredDoor, defaults settings.DoorDefaults, entered map[string]string) []configRow {
	if entered == nil {
		entered = d.Config
	}
	var rows []configRow
	for _, f := range settings.DoorFields {
		_, set := d.Config[f.Name]
		row := configRow{DoorField: f, Default: defaults.Default(f), Entered: entered[f.Name], Effective: defaults.Shown(d.Config, f), Set: set}
		if f.Name == settings.ConnectTimeoutField {
			row.Warning = defaults.RevertWarning(d.Config)
		}
		rows = append(rows, row)
	}
	return rows
}

// readDoorConfig reads the overrides the configuration form posts for a
// door whose overrides are stored: each field given a value, trimmed
// unless it is a secret, and each secret the door has that is left blank
// and not cleared as the mask that keeps it.
func readDoorConfig(r *http.Request, stored map[string]string) map[string]string {
	_ = r.ParseForm()
	overrides := map[string]string{}
	for _, f := range settings.DoorFields {
		value := r.PostFormValue(f.Name)
		_, has := stored[f.Name]
		kept := value == "" && has && !slices.Contains(r.PostForm["clear"], f.Name)
		switch {
		case !f.Secret:
			value = strings.TrimSpace(value)
		case kept:
			value = settings.Mask
		}
		if value != "" {
			overrides[f.Name] = value
		}
	}
	return overrides
}

func (a *App) configureDoorFromForm(w http.ResponseWriter, r *http.Request, viewer auth.User) {
	d, ok := a.pageDoor(w, r, viewer)
	if !ok {
		return
	}
	overrides := readDoorConfig(r, d.Config)
	_, err := a.configureDoor(r.Context(), d.ID, overrides)
	if errs, rejected := fieldErrors(err); rejected {
		form := d.fresh()
		form.Config, form.Errors = overrides, errs
		a.showDoor(w, r, http.StatusUnprocessableEntity, viewer, d, form, "")
		return
	}
	a.changed(w, r, err, doorPath(d.ID), "door-config-saved")
}
