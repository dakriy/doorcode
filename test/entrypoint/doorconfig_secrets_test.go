package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// Rows of the configuration table of a door's page, counting its head:
// the Wi-Fi password and how long new firmware may take to connect.
const (
	wifiPasswordRow   = 19
	connectTimeoutRow = 28
)

// sentConfig is the value of field in the configuration the door whose id
// is id was last sent.
func sentConfig(f *testsupport.Factory, id, field string) any {
	return f.BrokerStub.RetainedDecoded(doorTopic(id, "config")).(map[string]any)[field]
}

// storedOverride is the override of field as the store holds it for the
// door whose id is id.
func storedOverride(t *testing.T, f *testsupport.Factory, id int64, field string) string {
	t.Helper()
	d, _, _ := f.StoreStub.Door(t.Context(), id)
	return d.Config[field]
}

func TestASecretDoorOverrideIsMaskedWhenReadAndSentInTheClear(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)

	put := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"wifi_password":"hunter2 now"}}`)
	read := b.Get("/api/v1/doors/1/config").Body

	opened, err := testsupport.OpenSecret("door_wifi_password", storedOverride(t, f, 1, "wifi_password"))
	want := []any{200, "********", map[string]any{"wifi_password": "********"}, "********", "hunter2 now", "hunter2 now", nil}
	got := []any{put.Status, testsupport.JSONAt(put.Body, "/effective/wifi_password"), testsupport.JSONAt(read, "/overrides"),
		testsupport.JSONAt(read, "/effective/wifi_password"), sentConfig(f, "1", "wifi_password"), opened, err}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, effective, overrides read, effective read, sent, stored (-want +got):\n%s", diff)
	}
}

func TestAMaskedSecretOverrideKeepsTheDoorsOwnValue(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"mesh_password":"meshword"}}`, 200)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"mesh_password":"********","log_level":"debug"}}`)

	want := []any{200, map[string]any{"mesh_password": "********", "log_level": "debug"}, "meshword", "debug"}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.JSONAt(got.Body, "/overrides"),
		sentConfig(f, "1", "mesh_password"), sentConfig(f, "1", "log_level")}); diff != "" {
		t.Errorf("status, overrides, mesh password sent, log level sent (-want +got):\n%s", diff)
	}
}

func TestAMaskedSecretOverrideADoorHasNoneOfIsRefused(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"wifi_password":"********"}}`)

	want := []any{400, "Enter a value; ******** keeps only a value the door has."}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
		t.Errorf("status, error (-want +got):\n%s", diff)
	}
}

func TestAnEmptySecretOverrideSendsNoPasswordWhateverTheDefault(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/settings/door_wifi_password", `{"value":"sitewide1"}`, 200)

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"wifi_password":""}}`)

	want := []any{map[string]any{"wifi_password": ""}, "", "", ""}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(got.Body, "/overrides"), testsupport.JSONAt(got.Body, "/effective/wifi_password"),
		sentConfig(f, "1", "wifi_password"), storedOverride(t, f, 1, "wifi_password")}); diff != "" {
		t.Errorf("overrides, effective, sent, stored (-want +got):\n%s", diff)
	}
}

func TestASecretDoorDefaultIsMaskedWhenReadAndSentInTheClear(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)

	put := b.PutJSON("/api/v1/settings/door_mesh_password", `{"value":"sitewide1"}`)

	opened, err := testsupport.OpenSecret("door_mesh_password", f.StoreStub.StoredSetting("door_mesh_password"))
	want := []any{200, "********", "********", "sitewide1", "sitewide1", nil}
	got := []any{put.Status, testsupport.JSONAt(put.Body, "/value"), testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/effective/mesh_password"),
		sentConfig(f, "1", "mesh_password"), opened, err}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, setting, door's effective, sent, stored (-want +got):\n%s", diff)
	}
}

func TestADoorsPageMasksItsSecretAndOffersToClearIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"wifi_password":"hunter2 now"}}`, 200)

	page := doorPage(t, b, "/admin/doors/1")

	want := []any{[]string{"Wi-Fi password a password of up to 63 bytes", "", "Clear", "********"}, ""}
	if diff := cmp.Diff(want, []any{page.Tables["Configuration"][wifiPasswordRow], page.Values["wifi_password"]}); diff != "" {
		t.Errorf("row, input (-want +got):\n%s", diff)
	}
}

func TestADoorsPageKeepsASecretLeftBlankReplacesOneTypedAndClearsOneTicked(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		form      []string
		sent      string
		overrides map[string]any
	}{
		"left blank": {[]string{"wifi_password", ""}, "hunter2 now", map[string]any{"wifi_password": "********"}},
		"typed":      {[]string{"wifi_password", " n3w pass "}, " n3w pass ", map[string]any{"wifi_password": "********"}},
		"cleared":    {[]string{"wifi_password", "", "clear", "wifi_password"}, "", map[string]any{}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"wifi_password":"hunter2 now"}}`, 200)
			b.Get("/")

			saved := b.Post("/admin/doors/1/config", c.form...)

			want := []any{303, c.sent, c.overrides}
			got := []any{saved.Status, sentConfig(f, "1", "wifi_password"), testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/overrides")}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("status, sent, overrides (-want +got):\n%s", diff)
			}
		})
	}
}

func TestABrokerCertificateIsSentWithLineFeedsAndShownInATextArea(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)

	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"broker_ca_pem":`+testsupport.Quoted(testsupport.CRLF(testsupport.Certificate))+`}}`, 200)

	page := doorPage(t, b, "/admin/doors/1")
	want := []any{testsupport.Certificate, testsupport.Certificate}
	if diff := cmp.Diff(want, []any{sentConfig(f, "1", "broker_ca_pem"), page.Values["broker_ca_pem"]}); diff != "" {
		t.Errorf("sent, text area (-want +got):\n%s", diff)
	}
}

func TestADoorsConfigurationFailsWhenAStoredSecretCannotBeDecrypted(t *testing.T) {
	t.Parallel()
	cases := map[string]func(t *testing.T, f *testsupport.Factory){
		"an override": func(t *testing.T, f *testsupport.Factory) {
			_, _ = f.StoreStub.SetDoorConfig(t.Context(), 1, map[string]string{"wifi_password": "AAAA.AAAA"})
		},
		"a default": func(_ *testing.T, f *testsupport.Factory) {
			f.StoreStub.SeedSetting("door_wifi_password", "AAAA.AAAA", testsupport.DefaultNow)
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			createDoor(t, b, frontDoor)
			arrange(t, f)

			got := b.Get("/api/v1/doors/1/config")

			if diff := cmp.Diff([]any{500, "internal error"}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}

// revertWarning is what a page warns of a door whose new firmware tries
// to connect for longer than an update timeout of minutes, seconds long.
func revertWarning(minutes, seconds string) string {
	return "Longer than the update timeout of " + minutes + " minutes (" + seconds + " seconds): the server counts an update failed " +
		"before a controller that cannot connect on its new firmware goes back to the old one."
}

func TestTheSettingsPageWarnsWhenNewFirmwareMayRevertAfterTheUpdateTimeout(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		timeout string
		notes   []string
	}{
		"as long as the update timeout":   {"30", []string{settingsNote, hostNote, brokerNote}},
		"longer than the update timeout":  {"29", []string{settingsNote, hostNote, brokerNote, revertWarning("29", "1740")}},
		"shorter than the update timeout": {"31", []string{settingsNote, hostNote, brokerNote}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/settings/door_update_connect_timeout_s", `{"value":"1800"}`, 200)
			send(t, b, "PUT", "/api/v1/settings/update_timeout_minutes", `{"value":"`+c.timeout+`"}`, 200)

			got := pageOf(b.Get("/admin/settings?tab=doors")).Notes

			if diff := cmp.Diff(c.notes, got); diff != "" {
				t.Errorf("notes (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageWarnsWhenItsNewFirmwareMayRevertAfterTheUpdateTimeout(t *testing.T) {
	t.Parallel()
	label := "Revert new firmware after a whole number of seconds from 60 to 1800"
	cases := map[string]struct {
		override string
		want     []string
	}{
		"as long as the update timeout":  {"1200", []string{label, "300", "", "1200"}},
		"longer than the update timeout": {"1201", []string{label, "300", "", "1201 " + revertWarning("20", "1200")}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			send(t, b, "PUT", "/api/v1/settings/update_timeout_minutes", `{"value":"20"}`, 200)
			send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"update_connect_timeout_s":"`+c.override+`"}}`, 200)

			got := doorPage(t, b, "/admin/doors/1").Tables["Configuration"][connectTimeoutRow]

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("row (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOpenAPIDescribesEveryDoorConfigurationField(t *testing.T) {
	t.Parallel()
	doc := openAPIDocument(t)
	fields := map[string]string{
		"broker_port": "integer", "broker_tls": "boolean", "broker_ca_pem": "string", "wifi_ssid": "string", "wifi_password": "string",
		"wifi_roam_rssi": "integer", "keepalive_s": "integer", "reconnect_max_s": "integer", "mesh_enabled": "boolean",
		"mesh_ssid": "string", "mesh_password": "string", "mesh_min_router_rssi": "integer", "mesh_max_layer": "integer",
		"update_connect_timeout_s": "integer", "accept_unsigned_firmware": "boolean",
	}

	got := map[string]string{}
	for name := range fields {
		got[name] = testsupport.JSONString(doc, "/components/schemas/DoorConfigView/properties/"+name+"/type")
	}

	want := []any{fields, "The Wi-Fi network's password, a secret: ******** when it has one."}
	if diff := cmp.Diff(want, []any{got, testsupport.JSONAt(doc, "/components/schemas/DoorConfigView/properties/wifi_password/description")}); diff != "" {
		t.Errorf("types, password description (-want +got):\n%s", diff)
	}
}

func TestARefusedUpdateTimeoutShowsItsErrorBesideTheSettingsNotes(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/settings", "update_timeout_minutes", "soon", "door_update_connect_timeout_s", "1800")

	page := pageOf(got)
	want := []any{422, map[string]string{"update_timeout_minutes": "Must be a whole number of minutes from 1 to 1440."},
		[]string{settingsNote, hostNote, brokerNote}}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Notes}); diff != "" {
		t.Errorf("status, field errors, notes (-want +got):\n%s", diff)
	}
}
