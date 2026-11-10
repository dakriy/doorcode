package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// Rows of the configuration table of a door's page, counting its head
// and the headings of the groups before them:
// the broker's host and its certificate.
const (
	brokerHostRow = 17
	brokerCARow   = 20
)

// provisioningLink is the links of a page that offers the provisioning
// file at target.
func provisioningLink(target string) map[string]string {
	return map[string]string{"Download provisioning file": target}
}

// provisioningSite is a site whose controllers enroll with enrollPassword
// and verify the broker, mqtt.example.com, against testsupport.Certificate,
// with the Wi-Fi network Example and the door Lobby enrolled: its handler,
// and a browser signed in as its administrator.
func provisioningSite(t *testing.T, f *testsupport.Factory) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	a, b, _, _ := enrolledDoor(t, f)
	for key, value := range map[string]string{
		"door_broker_host": `"mqtt.example.com"`, "door_broker_ca_pem": testsupport.Quoted(testsupport.Certificate),
		"door_wifi_ssid": `"Example"`, "door_wifi_password": `"wifi-secret"`,
	} {
		send(t, b, "PUT", "/api/v1/settings/"+key, `{"value":`+value+`}`, 200)
	}
	return a.Handler(), b
}

// provisioningFile is the provisioning file of a site made by
// provisioningSite, made now, with the broker, the Wi-Fi network, and the
// door configuration's fields changed as changed says.
func provisioningFile(broker, wifi, changed map[string]any) any {
	config := testsupport.JSON(defaultConfigJSON).(map[string]any)
	config["broker_host"], config["broker_ca_pem"] = "mqtt.example.com", testsupport.Certificate
	config["wifi_ssid"], config["wifi_password"] = "Example", "wifi-secret"
	file := map[string]any{
		"format": 1.0, "generated_at": 1767603600.0,
		"broker":      with(map[string]any{"host": "mqtt.example.com", "port": 8883.0, "tls": true, "ca_pem": testsupport.Certificate}, broker),
		"server":      map[string]any{"device_url": ""},
		"enrollment":  map[string]any{"username": "doorcode-enroll", "password": enrollPassword},
		"wifi":        with(map[string]any{"ssid": "Example", "password": "wifi-secret"}, wifi),
		"door_config": with(config, changed),
	}
	return file
}

func TestTheProvisioningFileCarriesTheSitesBrokerEnrollmentWifiAndDoorDefaults(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := provisioningSite(t, f)
	secret := createToken(t, b, `{"name":"bench"}`)

	got := tokenClient(h, secret).Get("/api/v1/firmware/provisioning")

	want := []any{200, jsonType, provisioningFile(nil, nil, nil)}
	if diff := cmp.Diff(want, []any{got.Status, got.ContentType, testsupport.JSON(got.Body)}); diff != "" {
		t.Errorf("status, type, file (-want +got):\n%s", diff)
	}
}

func TestTheProvisioningFileNamesTheDeviceAddressAndNotThePublicAddress(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		settings map[string]string
		want     string
	}{
		"the device address": {map[string]string{"public_url": "https://doorcode.example.com", "device_url": "https://devices.example.com:8443/"}, "https://devices.example.com:8443"},
		"no address":         {map[string]string{"public_url": "https://doorcode.example.com/"}, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b := provisioningSite(t, testsupport.NewFactory())
			for key, value := range c.settings {
				send(t, b, "PUT", "/api/v1/settings/"+key, `{"value":"`+value+`"}`, 200)
			}

			got := testsupport.JSON(b.Get("/api/v1/firmware/provisioning").Body).(map[string]any)["server"]

			if diff := cmp.Diff(map[string]any{"device_url": c.want}, got); diff != "" {
				t.Errorf("server (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsProvisioningFileAppliesItsOverrides(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := provisioningSite(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"broker_port":"1883","broker_tls":"false","wifi_ssid":"Lobby net","strike_pulse_ms":"5000"}}`, 200)
	c := testsupport.NewBrowser(h)
	c.SetHeader("Authorization", testsupport.BasicAuth("bench", createToken(t, b, `{"name":"bench"}`)))

	got := c.Get("/api/v1/doors/1/provisioning")

	want := []any{200, provisioningFile(
		map[string]any{"port": 1883.0, "tls": false, "ca_pem": ""},
		map[string]any{"ssid": "Lobby net"},
		map[string]any{"broker_port": 1883.0, "broker_tls": false, "wifi_ssid": "Lobby net", "strike_pulse_ms": 5000.0},
	)}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.JSON(got.Body)}); diff != "" {
		t.Errorf("status, file (-want +got):\n%s", diff)
	}
}

func TestAProvisioningFileIsDownloadedAsAnAttachment(t *testing.T) {
	t.Parallel()
	_, b := provisioningSite(t, testsupport.NewFactory())

	got := []string{
		b.Header("/api/v1/firmware/provisioning", "Content-Disposition"),
		b.Header("/api/v1/doors/1/provisioning", "Content-Disposition"),
	}

	want := []string{`attachment; filename="provisioning.json"`, `attachment; filename="provisioning-1.json"`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("site, door (-want +got):\n%s", diff)
	}
}

func TestAProvisioningFileNeedsTheManageFirmwareRole(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := provisioningSite(t, f)
	holdingOnly(t, f, b, `["manage-doors"]`)

	got := []testsupport.Response{b.Get("/api/v1/firmware/provisioning"), b.Get("/api/v1/doors/1/provisioning")}

	refused := apiError(403, "the manage-firmware role is required")
	if diff := cmp.Diff([]testsupport.Response{refused, refused}, got); diff != "" {
		t.Errorf("site, door (-want +got):\n%s", diff)
	}
}

func TestAProvisioningFileOfADoorThatIsNotThereIsNotFound(t *testing.T) {
	t.Parallel()
	_, b := provisioningSite(t, testsupport.NewFactory())

	got := b.Get("/api/v1/doors/9/provisioning")

	if diff := cmp.Diff(404, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestNoProvisioningFileIsMadeWithoutABrokerHostOrWhileControllersCouldNotVerifyTheBroker(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		target   string
		settings map[string]string
		door     string
		refusal  string
	}{
		"the site's, over TLS without a certificate": {
			"/api/v1/firmware/provisioning", map[string]string{"door_broker_ca_pem": `""`}, `{}`, brokerNote,
		},
		"the site's, without a broker host": {
			"/api/v1/firmware/provisioning", map[string]string{"door_broker_host": `""`}, `{}`, hostNote,
		},
		"the site's, with neither": {
			"/api/v1/firmware/provisioning", map[string]string{"door_broker_host": `""`, "door_broker_ca_pem": `""`}, `{}`, hostNote + " " + brokerNote,
		},
		"a door's, whose override turns TLS on": {
			"/api/v1/doors/1/provisioning", map[string]string{"door_broker_tls": `"false"`, "door_broker_ca_pem": `""`}, `{"broker_tls":"true"}`, brokerNote,
		},
		"a door's, whose override empties the broker host": {
			"/api/v1/doors/1/provisioning", map[string]string{}, `{"broker_host":""}`, hostNote,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b := provisioningSite(t, testsupport.NewFactory())
			for key, value := range c.settings {
				send(t, b, "PUT", "/api/v1/settings/"+key, `{"value":`+value+`}`, 200)
			}
			send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":`+c.door+`}`, 200)

			got := b.Get(c.target)

			if diff := cmp.Diff(apiError(409, c.refusal), got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheFirmwarePageAndADoorsPageOfferItsProvisioningFile(t *testing.T) {
	t.Parallel()
	_, b := provisioningSite(t, testsupport.NewFactory())
	b.Get("/")

	firmwarePage := pageOf(b.Get("/admin/firmware"))
	door := doorPage(t, b, "/admin/doors/1")

	want := []any{
		provisioningLink("/api/v1/firmware/provisioning"), []string{"No update can be sent until the device_url setting gives the address of the entrypoint doors download firmware from."},
		"/api/v1/doors/1/provisioning",
	}
	if diff := cmp.Diff(want, []any{firmwarePage.LinkTargets, firmwarePage.Notes, door.LinkTargets["Download provisioning file"]}); diff != "" {
		t.Errorf("firmware page's links and notes, door page's links (-want +got):\n%s", diff)
	}
}

func TestTheSettingsPageWarnsOnlyWhileNoProvisioningFileCanBeMade(t *testing.T) {
	t.Parallel()
	host := `"mqtt.example.com"`
	cases := map[string]struct {
		settings map[string]string
		notes    []string
	}{
		"without a host, over TLS without a certificate": {map[string]string{}, []string{settingsNote, hostNote, brokerNote}},
		"with a host, over TLS without a certificate":    {map[string]string{"door_broker_host": host}, []string{settingsNote, brokerNote}},
		"with a host, over TLS with a certificate": {
			map[string]string{"door_broker_host": host, "door_broker_ca_pem": testsupport.Quoted(testsupport.Certificate)}, []string{settingsNote},
		},
		"with a host, without TLS": {map[string]string{"door_broker_host": host, "door_broker_tls": `"false"`}, []string{settingsNote}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			for key, value := range c.settings {
				send(t, b, "PUT", "/api/v1/settings/"+key, `{"value":`+value+`}`, 200)
			}

			got := pageOf(b.Get("/admin/settings?tab=doors")).Notes

			if diff := cmp.Diff(c.notes, got); diff != "" {
				t.Errorf("notes (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageWarnsWhileNoProvisioningFileCanBeMadeForIt(t *testing.T) {
	t.Parallel()
	host := "Broker host " +
		"The broker the door connects to, from its next connection on; empty keeps the broker it uses now. A host name of up to 253 letters, digits, dots, and hyphens, an IP address, or nothing to keep the door's broker."
	ca := "Broker CA certificate " +
		"The certificate the door checks the broker against while it connects over TLS: the broker's CA's, or the broker's own. Empty keeps the one the door has. The PEM certificate of the broker's CA or of the broker itself, or nothing."
	cases := map[string]struct {
		overrides string
		want      [][]string
	}{
		"without a host, over TLS without a certificate": {`{}`, [][]string{{host, "", "", hostNote}, {ca, "", "", brokerNote}}},
		"with a host, without TLS": {`{"broker_host":"mqtt.example.com","broker_tls":"false"}`,
			[][]string{{host, "", "", "mqtt.example.com"}, {ca, "", "", ""}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":`+c.overrides+`}`, 200)

			rows := doorPage(t, b, "/admin/doors/1?tab=configuration").Tables["Configuration"]

			if diff := cmp.Diff(c.want, [][]string{rows[brokerHostRow], rows[brokerCARow]}); diff != "" {
				t.Errorf("rows (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAProvisioningFileFailsWhenAStoredSecretCannotBeDecrypted(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"door_wifi_password", "enrollment_password"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			f.StoreStub.SeedSetting(key, "AAAA.AAAA", testsupport.DefaultNow)

			got := b.Get("/api/v1/firmware/provisioning")

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}
