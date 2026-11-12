package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
	"github.com/dakriy/doorcode/internal/settings"
)

// provisioningFormat is the version of the provisioning file's layout,
// which changes only when a field changes meaning or goes away.
const provisioningFormat = 1

type provisioningBroker struct {
	Host  string `json:"host" doc:"The host name or IP address of the broker."`
	Port  int    `json:"port" doc:"The broker's port."`
	TLS   bool   `json:"tls" doc:"Whether to connect to the broker over TLS."`
	CAPEM string `json:"ca_pem" doc:"The PEM certificate to verify the broker against; empty when tls is false."`
}

type provisioningEnrollment struct {
	Username string `json:"username" doc:"The broker username every controller enrolls with."`
	Password string `json:"password" doc:"Its password, in the clear."`
}

type provisioningWifi struct {
	SSID     string `json:"ssid" doc:"The Wi-Fi network to join."`
	Password string `json:"password" doc:"Its password, in the clear."`
}

type provisioningServer struct {
	DeviceURL string `json:"device_url" doc:"The address to download firmware from, the device_url setting; empty while it is not set."`
}

// provisioningConfig is the configuration a controller starts with, as
// the configuration message carries it: the door's fields and the site's
// firmware signing key.
type provisioningConfig struct {
	doorConfigView
	FirmwareSigningKey string `json:"firmware_signing_key" doc:"The public key every firmware image must be signed with: the P-256 point x || y, each 32 bytes big-endian, in 128 lowercase hex characters."`
}

type provisioningFile struct {
	Format      int                    `json:"format" doc:"The version of this file's layout, 1."`
	GeneratedAt int64                  `json:"generated_at" doc:"When the file was made, in Unix seconds."`
	Broker      provisioningBroker     `json:"broker" doc:"The broker to connect to."`
	Server      provisioningServer     `json:"server" doc:"The server to download firmware from."`
	Enrollment  provisioningEnrollment `json:"enrollment" doc:"The credentials to enroll with."`
	Wifi        provisioningWifi       `json:"wifi" doc:"The Wi-Fi network to join."`
	DoorConfig  provisioningConfig     `json:"door_config" doc:"The configuration to run with until the server sends one, with the names and types of the configuration message, the firmware signing key among them; secrets in the clear."`
}

func (a *App) provisioningRoutes() []route {
	manage := holding(auth.RoleManageFirmware)
	refused := openapi.Response{
		Status: http.StatusConflict, Body: errorBody{},
		Description: "No broker host is given, or the broker is reached over TLS with no CA certificate to verify it against; the error says which: " +
			settings.NoBrokerHost + " " + settings.UnverifiedBroker,
	}
	return []route{
		apiRoute(http.MethodGet, "/firmware/provisioning", manage, a.siteProvisioning, apiDoc{
			id: "getProvisioning", summary: "Download the provisioning file", tag: "firmware",
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "provisioning.json, as an attachment: what a controller is flashed with to enroll, with the site's defaults.", Body: provisioningFile{}},
				refused,
			},
		}),
		apiRoute(http.MethodGet, "/doors/{id}/provisioning", manage, a.doorProvisioning, apiDoc{
			id: "getDoorProvisioning", summary: "Download a door's provisioning file", tag: "firmware", params: []openapi.Param{doorIDParam},
			responses: []openapi.Response{
				{Status: http.StatusOK, Description: "provisioning-<door id>.json, as an attachment: the provisioning file with the door's configuration, its overrides applied.", Body: provisioningFile{}},
				noDoor, refused,
			},
		}),
	}
}

func (a *App) siteProvisioning(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.writeProvisioning(w, r, connector.StoredDoor{}, nil, "provisioning.json")
}

func (a *App) doorProvisioning(w http.ResponseWriter, r *http.Request, _ auth.User) {
	id := pathID(r, "id")
	d, err := a.doors.Door(r.Context(), id)
	a.writeProvisioning(w, r, d, err, "provisioning-"+strconv.FormatInt(id, 10)+".json")
}

// writeProvisioning answers with the provisioning file named name of d,
// which err may say could not be read, with d's configuration, or refuses
// it when a controller could not verify the broker.
func (a *App) writeProvisioning(w http.ResponseWriter, r *http.Request, d connector.StoredDoor, err error, name string) {
	if err != nil {
		a.doorError(w, r, err)
		return
	}
	config, _, err := a.effectiveConfig(r.Context(), d)
	username, password, enrollErr := a.settings.Enrollment(r.Context())
	current, currentErr := a.settings.Current(r.Context())
	if err := errors.Join(err, enrollErr, currentErr); err != nil {
		a.internalError(w, r, err)
		return
	}
	if problems := config.Unprovisionable(); len(problems) > 0 {
		writeError(w, http.StatusConflict, strings.Join(problems, " "))
		return
	}
	file := provisioningFile{
		Format: provisioningFormat, GeneratedAt: a.clock.Now().Unix(),
		Broker:     provisioningBroker{Host: config.BrokerHost, Port: config.BrokerPort, TLS: config.BrokerTLS},
		Server:     provisioningServer{DeviceURL: current.DeviceURL},
		Enrollment: provisioningEnrollment{Username: username, Password: password},
		Wifi:       provisioningWifi{SSID: config.WifiSSID, Password: config.WifiPassword},
		DoorConfig: provisioningConfig{doorConfigView: doorConfigView(config), FirmwareSigningKey: a.signingKey.PublicText()},
	}
	if config.BrokerTLS {
		file.Broker.CAPEM = config.BrokerCAPEM
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	writeJSON(w, http.StatusOK, file)
}
