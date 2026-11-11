package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// Why a mesh that is on is refused while it has no network name, and
// while it has no password of 8 characters or more.
const (
	noMeshName     = "with the mesh on, no network name leaves the mesh soft-AP open"
	noMeshPassword = "with the mesh on, a password of fewer than 8 characters, or none, leaves the mesh soft-AP open"
)

// meshRow is the row of a door's configuration table that shows whether
// its mesh is on, counting its head and the headings of the groups before
// it.
const meshRow = 29

// meshLabel is what the mesh row says of the field under its label.
const meshLabel = "Mesh Whether the door may join or form a Wi-Fi mesh with other doors when its own access point's signal is poor. True or false."

// meshWarning is what a page warns of a mesh left open for want of what
// reason says.
func meshWarning(reason string) string {
	return "Controllers are sent mesh_enabled false: " + reason + "."
}

func TestTheSiteMeshIsRefusedWhileItWouldBeOnWithoutANetworkNameOrPassword(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		before []string
		key    string
		body   string
		reason string
	}{
		"turned on with neither":               {nil, "door_mesh_enabled", `{"value":"true"}`, noMeshName},
		"turned on with a network name only":   {[]string{"door_mesh_ssid", `"site-mesh"`}, "door_mesh_enabled", `{"value":"true"}`, noMeshPassword},
		"turned on with a password only":       {[]string{"door_mesh_password", `"meshword"`}, "door_mesh_enabled", `{"value":"true"}`, noMeshName},
		"its password cleared while it is on":  {meshOn, "door_mesh_password", `{"clear":true}`, noMeshPassword},
		"its network name emptied while it is": {meshOn, "door_mesh_ssid", `{"value":""}`, noMeshName},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			putSettings(t, b, c.before)
			kept := testsupport.JSONAt(b.Get("/api/v1/settings/"+c.key).Body, "/value")

			got := b.PutJSON("/api/v1/settings/"+c.key, c.body)

			want := []any{apiError(400, "invalid value for "+c.key+": "+c.reason), kept}
			if diff := cmp.Diff(want, []any{got, testsupport.JSONAt(b.Get("/api/v1/settings/"+c.key).Body, "/value")}); diff != "" {
				t.Errorf("response, value kept (-want +got):\n%s", diff)
			}
		})
	}
}

// putSettings stores settings, each key followed by its JSON value, in
// order through the API.
func putSettings(t *testing.T, b *testsupport.Browser, settings []string) {
	t.Helper()
	for i := 0; i < len(settings); i += 2 {
		send(t, b, "PUT", "/api/v1/settings/"+settings[i], `{"value":`+settings[i+1]+`}`, 200)
	}
}

// meshOn is the settings, each key followed by its JSON value, that turn
// the site's mesh on with a network name and the shortest password.
var meshOn = []string{"door_mesh_ssid", `"site-mesh"`, "door_mesh_password", `"meshword"`, "door_mesh_enabled", `"true"`}

func TestTheSiteMeshTurnsOnWithANetworkNameAndAPasswordOfEightCharacters(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)

	var statuses []int
	for i := 0; i < len(meshOn); i += 2 {
		statuses = append(statuses, b.PutJSON("/api/v1/settings/"+meshOn[i], `{"value":`+meshOn[i+1]+`}`).Status)
	}

	want := []any{[]int{200, 200, 200}, true, "site-mesh", "meshword"}
	got := []any{statuses, sentConfig(f, "1", "mesh_enabled"), sentConfig(f, "1", "mesh_ssid"), sentConfig(f, "1", "mesh_password")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("statuses, mesh, network, password sent (-want +got):\n%s", diff)
	}
}

func TestTheSettingsFormRefusesTheMeshOnWithoutANetworkNameOrPassword(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/settings", "door_mesh_enabled", "true", "door_mesh_ssid", "", "door_mesh_password", "")

	want := []any{422, map[string]string{"door_mesh_ssid": sentence(noMeshName), "door_mesh_password": sentence(noMeshPassword)}, "false"}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).FieldErrors, testsupport.JSONAt(b.Get("/api/v1/settings/door_mesh_enabled").Body, "/value")}); diff != "" {
		t.Errorf("status, field errors, mesh stored (-want +got):\n%s", diff)
	}
}

func TestTheSettingsFormTurnsTheMeshOnWithANetworkNameAndPassword(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/settings", "door_mesh_enabled", "true", "door_mesh_ssid", "site-mesh", "door_mesh_password", "meshword")

	want := []any{303, "true"}
	if diff := cmp.Diff(want, []any{got.Status, testsupport.JSONAt(b.Get("/api/v1/settings/door_mesh_enabled").Body, "/value")}); diff != "" {
		t.Errorf("status, mesh stored (-want +got):\n%s", diff)
	}
}

func TestADoorsMeshIsCheckedAgainstTheConfigurationItRunsWith(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		site      []string
		overrides string
		status    int
		refusal   any
	}{
		"on with neither":                           {nil, `{"mesh_enabled":"true"}`, 400, sentence(noMeshName)},
		"on with its own network name only":         {nil, `{"mesh_enabled":"true","mesh_ssid":"door-mesh"}`, 400, sentence(noMeshPassword)},
		"on with its own network name and password": {nil, `{"mesh_enabled":"true","mesh_ssid":"door-mesh","mesh_password":"meshword"}`, 200, nil},
		"on with the site's network name and password": {
			[]string{"door_mesh_ssid", `"site-mesh"`, "door_mesh_password", `"meshword"`}, `{"mesh_enabled":"true"}`, 200, nil,
		},
		"its password emptied while the site's mesh is on": {meshOn, `{"mesh_password":""}`, 400, sentence(noMeshPassword)},
		"its mesh off while the site's has no password":    {nil, `{"mesh_enabled":"false"}`, 200, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			createDoor(t, b, frontDoor)
			putSettings(t, b, c.site)

			got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":`+c.overrides+`}`)

			if diff := cmp.Diff([]any{c.status, c.refusal}, []any{got.Status, testsupport.JSONAt(got.Body, "/error")}); diff != "" {
				t.Errorf("status, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageRefusesItsMeshOnWithoutANetworkNameOrPassword(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	b.Get("/")

	got := b.Post("/admin/doors/1/config", "mesh_enabled", "true")

	want := []any{422, map[string]string{"mesh_ssid": sentence(noMeshName), "mesh_password": sentence(noMeshPassword)}, false}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).FieldErrors, testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/effective/mesh_enabled")}); diff != "" {
		t.Errorf("status, field errors, mesh in effect (-want +got):\n%s", diff)
	}
}

func TestADoorWhoseMeshIsLeftOpenIsSentItOff(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := provisioningSite(t, f)
	send(t, b, "PUT", "/api/v1/settings/door_mesh_ssid", `{"value":"site-mesh"}`, 200)
	send(t, b, "PUT", "/api/v1/settings/door_mesh_password", `{"value":"meshword"}`, 200)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"mesh_enabled":"true"}}`, 200)
	on := sentConfig(f, "1", "mesh_enabled")

	send(t, b, "PUT", "/api/v1/settings/door_mesh_password", `{"clear":true}`, 200)

	want := []any{true, false, false, false, []string{meshLabel, "false", "", "true Overridden " + meshWarning(noMeshPassword)}}
	got := []any{on, sentConfig(f, "1", "mesh_enabled"), testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/effective/mesh_enabled"),
		testsupport.JSONAt(b.Get("/api/v1/doors/1/provisioning").Body, "/door_config/mesh_enabled"),
		doorPage(t, b, "/admin/doors/1?tab=configuration").Tables["Configuration"][meshRow]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mesh sent before, after, in effect, provisioned, row (-want +got):\n%s", diff)
	}
}

func TestASiteWhoseStoredMeshIsLeftOpenIsWarnedOfAndSendsItOff(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedSetting("door_mesh_enabled", "true", testsupport.DefaultNow)
	_, b := provisioningSite(t, f)

	provisioning := b.Get("/api/v1/firmware/provisioning")

	want := []any{200, false, false, []string{settingsNote, "Set by DOORCODE_ENROLLMENT_PASSWORD in the environment; change it there.", meshWarning(noMeshName)}, 200}
	got := []any{provisioning.Status, testsupport.JSONAt(provisioning.Body, "/door_config/mesh_enabled"), sentConfig(f, "1", "mesh_enabled"),
		pageOf(b.Get("/admin/settings?tab=doors")).Notes, b.PutJSON("/api/v1/settings/door_log_level", `{"value":"debug"}`).Status}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("provisioning status, mesh provisioned, sent, settings notes, another setting saved (-want +got):\n%s", diff)
	}
}

func TestADoorsMeshCannotBeCheckedWhileTheSettingsCannotBeRead(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	createDoor(t, b, frontDoor)
	f.StoreStub.FailSettings()

	got := b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"log_level":"debug"}}`)

	if diff := cmp.Diff(500, got.Status); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}
