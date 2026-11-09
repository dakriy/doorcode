package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// firmwareNav is the navigation of the firmware page.
var firmwareNav = mainNav("Doors: Firmware", "")

// The heads of the firmware page's tables.
var (
	versionsHead = []string{"Version", "Uploaded by", "Uploaded", "Running", "Targeting"}
	rolloutHead  = []string{"Door", "Running", "Target", "Latest update"}
)

// concurrencyNote is what the firmware page says of the concurrency a
// fleet's environment sets.
const concurrencyNote = "update_concurrency is set by DOORCODE_UPDATE_CONCURRENCY and cannot be changed here."

// windowValues is what the update window form holds on a fleet site, but
// for the values changed names.
func windowValues(changed map[string]string) map[string]string {
	values := map[string]string{
		"version": "", "notes": "", "image": "", "update_window_start": "02:00", "update_window_minutes": "120", "update_concurrency": "2",
		"update_timeout_minutes": "30",
	}
	for key, value := range changed {
		values[key] = value
	}
	return values
}

// browser is a browser signed in as the administrator on fl's site, its
// sign-in flash spent.
func (fl *fleet) browser() *testsupport.Browser {
	fl.t.Helper()
	b := signIn(fl.t, fl.f, fl.a.Handler())
	b.Get("/")
	return b
}

func TestFirmwarePageShowsTheVersionsAndTheRollout(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	fl.reconnects(1, "1.5.0")
	fl.acks(2, "failed", "")
	b := fl.browser()

	got := b.Get("/admin/firmware")

	updated := "Updated to 1.5.0 on Tue 6 Jan 2026 02:00:00"
	failed := "Failed to update to 1.5.0 on Tue 6 Jan 2026 02:00:00: The door could not install it."
	want := testsupport.Page{
		Title: "Firmware · doorcode", Heading: "Firmware", Nav: firmwareNav, Notes: []string{concurrencyNote}, Values: windowValues(nil),
		Tables: map[string][][]string{
			"Versions": {versionsHead, {"1.5.0 Default", "Administrator", "Mon 5 Jan 2026 09:00:00", "1", "3"}},
			"Pending":  {rolloutHead, {"Attic", "1.4.3", "1.5.0", "Never sent one"}},
			"Updating": {rolloutHead, {"No door is updating."}},
			"Done":     {rolloutHead, {"Lobby", "1.5.0", "1.5.0", updated}},
			"Failed":   {rolloutHead, {"Dock", "1.4.3", "1.5.0", failed}},
		},
		Comboboxes: []string{"firmware_id"},
	}
	if diff := cmp.Diff([]any{200, want}, []any{got.Status, pageOf(got)}); diff != "" {
		t.Errorf("status, page (-want +got):\n%s", diff)
	}
}

func TestFirmwarePageOfANewSiteSaysWhatIsMissing(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := pageOf(b.Get("/admin/firmware"))

	addressNote := "No update can be sent until the public_url setting gives the address doors reach this server at, since they download firmware from it."
	want := []any{
		[]string{addressNote},
		map[string][][]string{
			"Versions": {versionsHead, {"No firmware yet. Upload an image below, or from CI through the API."}},
			"Pending":  {rolloutHead, {"No door is waiting for an update."}},
			"Updating": {rolloutHead, {"No door is updating."}},
			"Done":     {rolloutHead, {"No door runs its target yet."}},
			"Failed":   {rolloutHead, {"No update has failed."}},
		},
	}
	if diff := cmp.Diff(want, []any{got.Notes, got.Tables}); diff != "" {
		t.Errorf("notes, tables (-want +got):\n%s", diff)
	}
}

func TestFirmwarePageNeedsTheManageFirmwareRole(t *testing.T) {
	t.Parallel()
	_, b := adminHolding(t, testsupport.NewFactory(), `["manage-doors"]`)
	b.Get("/")

	got := b.Get("/admin/firmware")

	if diff := cmp.Diff([]any{403, "You do not have access to this page"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestUploadingFirmwareFromThePageOpensItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	uploaded := b.Upload("/admin/firmware", "image", "doorcode.bin", imageOf("1.5.0"), "version", "1.5.0", "notes", "Fixes the keypad.")
	got := pageOf(b.Get(uploaded.Location))

	want := testsupport.Page{
		Title: "1.5.0 · doorcode", Heading: "1.5.0", Nav: firmwareNav,
		Messages: []string{"The firmware has been uploaded. Doors run it once it is the default or their own."},
		Details:  map[string]string{"Size": "14 bytes", "SHA-256": sha150, "Notes": "Fixes the keypad."},
		Tables:   map[string][][]string{"Doors": {{"Door", "Running", "Target", "Rollout"}, {"No door runs this version or should."}}},
	}
	if diff := cmp.Diff([]any{303, "/admin/firmware/1", want}, []any{uploaded.Status, uploaded.Location, got}); diff != "" {
		t.Errorf("status, location, page (-want +got):\n%s", diff)
	}
}

func TestARefusedUploadFromThePageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		image  []byte
		status int
		errors map[string]string
	}{
		"without a version or an image": {nil, 422, map[string]string{
			"version": "Enter the version, such as 1.4.3.", "image": "Choose the firmware image to upload.",
		}},
		"too large": {testsupport.Padded(nil, 16_777_217), 413, map[string]string{"image": "The image is larger than 16 MiB."}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())
			b.Get("/")

			got := b.Upload("/admin/firmware", "image", "doorcode.bin", c.image, "notes", "Fixes the keypad.")

			page := pageOf(got)
			want := []any{c.status, c.errors, "Fixes the keypad."}
			if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values["notes"]}); diff != "" {
				t.Errorf("status, field errors, notes kept (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChoosingTheDefaultFromThePageSavesIt(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	uploadFirmware(t, b, "1.5.0")
	uploadFirmware(t, b, "1.6.0")
	b.Get("/")

	chosen := b.Post("/admin/firmware/default", "firmware_id", "1")
	page := testsupport.ParsePage(b.Get("/admin/firmware").Body)
	b.Post("/admin/firmware/default", "firmware_id", "")
	cleared := testsupport.ParsePage(b.Get("/admin/firmware").Body)

	want := []any{
		303, "/admin/firmware",
		[]string{"The default firmware has been saved. The rollout sends it to the doors that follow the default in the next update window."},
		[]string{"No default", "1.6.0", "1.5.0 (selected)"},
		[]string{"No default (selected)", "1.6.0", "1.5.0"},
	}
	got := []any{chosen.Status, chosen.Location, page.Messages, page.Options["firmware_id"], cleared.Options["firmware_id"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("redirect, flash, options chosen and cleared (-want +got):\n%s", diff)
	}
}

func TestChoosingFirmwareThatIsNotThereFromThePageSaysSo(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/firmware/default", "firmware_id", "9")

	if diff := cmp.Diff([]any{404, "Not found"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
		t.Errorf("status, heading (-want +got):\n%s", diff)
	}
}

func TestSavingTheUpdateWindowFromThePageStoresIt(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	b := fl.browser()

	saved := b.Post("/admin/firmware/settings", "repeat", "weekly", "days", "mon", "days", "sat", "update_window_start", "03:15",
		"update_window_minutes", "90", "update_concurrency", "9", "update_timeout_minutes", "20")
	page := testsupport.ParsePage(b.Get("/admin/firmware").Body)
	b.Post("/admin/firmware/settings", "repeat", "daily", "days", "mon", "update_window_start", "03:15",
		"update_window_minutes", "90", "update_timeout_minutes", "20")
	daily := testsupport.ParsePage(b.Get("/admin/firmware").Body)

	want := []any{
		303, []string{"The update window and rollout settings have been saved."},
		[]string{"days=mon", "days=sat"}, []string{"Every day", "On the weekdays chosen (selected)"},
		windowValues(map[string]string{"update_window_start": "03:15", "update_window_minutes": "90", "update_timeout_minutes": "20"}),
		[]string(nil), []string{"Every day (selected)", "On the weekdays chosen"},
	}
	got := []any{saved.Status, page.Messages, page.Checked, page.Options["repeat"], page.Values, daily.Checked, daily.Options["repeat"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, flash, weekly days, values, daily (-want +got):\n%s", diff)
	}
}

func TestARefusedUpdateWindowFromThePageSaysWhatIsWrong(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := b.Post("/admin/firmware/settings", "repeat", "weekly", "update_window_start", "later", "update_window_minutes", "90",
		"update_concurrency", "3", "update_timeout_minutes", "20")

	page := pageOf(got)
	want := []any{
		422,
		map[string]string{
			"days":                "Must be daily or weekdays separated by commas, such as sat,sun.",
			"update_window_start": "Must be a time of day as HH:MM, such as 02:00.",
		},
		"later",
	}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors, page.Values["update_window_start"]}); diff != "" {
		t.Errorf("status, field errors, value kept (-want +got):\n%s", diff)
	}
}

func TestFirmwarePageListsTheDoorsThatRunTheVersion(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	createDoor(t, fl.b, `{"name":"Shed"}`)
	b := fl.browser()

	got := pageOf(b.Get("/admin/firmware/1"))

	want := []any{
		[]string{"This is the default version; choose another default before deleting it."},
		[][]string{{"Door", "Running", "Target", "Rollout"},
			{"Lobby", "1.4.3", "1.5.0", "Updating"}, {"Dock", "1.4.3", "1.5.0", "Updating"}, {"Attic", "1.4.3", "1.5.0", "Pending update"},
			{"Shed", "Not reported yet", "1.5.0", "Not in the rollout"}},
	}
	if diff := cmp.Diff(want, []any{got.Notes, got.Tables["Doors"]}); diff != "" {
		t.Errorf("notes, doors (-want +got):\n%s", diff)
	}
}

func TestFirmwarePageListsTheDoorsRunningAVersionTheyShouldNot(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.4.3")
	createDoor(t, fl.b, `{"name":"Shed"}`)
	send(t, fl.b, "DELETE", "/api/v1/doors/3", "", 204)
	b := fl.browser()

	got := pageOf(b.Get("/admin/firmware/2")).Tables["Doors"]

	want := [][]string{{"Door", "Running", "Target", "Rollout"},
		{"Lobby", "1.4.3", "1.5.0", "Pending update"}, {"Dock", "1.4.3", "1.5.0", "Pending update"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("doors (-want +got):\n%s", diff)
	}
}

func TestDeletingFirmwareFromItsPage(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	uploadFirmware(t, b, "1.5.0")
	b.Get("/")

	deleted := b.Post("/admin/firmware/1/delete")
	gone := b.Get("/admin/firmware/1")

	want := []any{303, "/admin/firmware", 404, "Not found"}
	if diff := cmp.Diff(want, []any{deleted.Status, deleted.Location, gone.Status, pageOf(gone).Heading}); diff != "" {
		t.Errorf("redirect, page afterwards (-want +got):\n%s", diff)
	}
}

func TestDeletingFirmwareInUseFromItsPageIsRefused(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/doors/2/firmware", `{"firmware_id":2}`, 200)
	b := fl.browser()

	got := b.Post("/admin/firmware/2/delete")
	missing := b.Post("/admin/firmware/9/delete")

	inUse := "These doors run this version instead of the default: Dock; choose other firmware for them first."
	want := []any{409, []string{inUse}, []string{inUse}, 404}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Messages, pageOf(got).Notes, missing.Status}); diff != "" {
		t.Errorf("status, alert, note, unknown (-want +got):\n%s", diff)
	}
}

func TestADoorPageShowsItsFirmware(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/doors/1/firmware", `{"firmware_id":2}`, 200)
	send(t, fl.b, "POST", "/api/v1/doors/1/update", "", 200)
	b := fl.browser()

	page := testsupport.ParsePage(b.Get("/admin/doors/1").Body)

	want := []any{
		"1.4.3", "1.6.0", "Updating", "Sent 1.6.0 on Mon 5 Jan 2026 09:00:00",
		[]string{"Follow the default (1.5.0)", "1.6.0 (selected)", "1.5.0"},
		[]string{"firmware_id", "zone_ids"},
	}
	got := []any{page.Details["Running"], page.Details["Target"], page.Details["Rollout"], page.Details["Latest update"], page.Options["firmware_id"], page.Comboboxes}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("running, target, latest update, choice, searchable selects (-want +got):\n%s", diff)
	}
}

func TestADoorPageSaysWhenAnOfflineDoorCanBeUpdated(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.conns[0].Drop()
	run(t, fl.a)
	b := fl.browser()

	got := pageOf(b.Get("/admin/doors/1")).Notes

	want := []string{"The door can be updated once its controller is connected.", "New credentials can be sent only while the controller is connected.", picturesNote, openNote,
		neverOpenNote, "There are no zones yet. Create one on the zones page."}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("notes (-want +got):\n%s", diff)
	}
}

func TestADoorPageShowsFirmwareOnlyToThoseWhoManageIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	holdingOnly(t, f, b, `["manage-doors"]`)

	got := doorPage(t, b, "/admin/doors/1").Details

	want := map[string]string{
		"Hardware id": "A1", "Door": "Not reported yet", "Firmware": "1.4.3", "Signal": "-58 dBm", "Latency": "Not reported yet",
		"Uptime": "Not reported yet", "Free memory": "Not reported yet", "Reconnects": "Not reported yet",
		"Last seen": "Mon 5 Jan 2026 09:00:00", "Last report": "Not reported yet",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("details (-want +got):\n%s", diff)
	}
}

func TestChoosingADoorsFirmwareFromItsPageSavesIt(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.6.0")
	b := fl.browser()

	saved := b.Post("/admin/doors/1/firmware", "firmware_id", "2")
	page := pageOf(b.Get(saved.Location))

	want := []any{303, []string{"The door's firmware has been saved. The rollout sends it in the next update window."}, "1.6.0"}
	if diff := cmp.Diff(want, []any{saved.Status, page.Messages, page.Details["Target"]}); diff != "" {
		t.Errorf("status, flash, target (-want +got):\n%s", diff)
	}
}

func TestUpdatingADoorFromItsPageSendsTheUpdate(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	b := fl.browser()

	sent := b.Post("/admin/doors/2/update")
	page := pageOf(b.Get(sent.Location))

	want := []any{
		303, []string{"The update has been sent. The door installs it, restarts, and connects again with the new version."},
		topicsOf("command", "2"), "Sent 1.5.0 on Mon 5 Jan 2026 09:00:00",
	}
	if diff := cmp.Diff(want, []any{sent.Status, page.Messages, fl.commands(), page.Details["Latest update"]}); diff != "" {
		t.Errorf("status, flash, commands, latest update (-want +got):\n%s", diff)
	}
}

func TestAFirmwareChangeTheDoorPageCannotMakeIsExplained(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(fl *fleet)
		action  func(b *testsupport.Browser) testsupport.Response
		status  int
		alert   string
	}{
		"updating an offline door": {
			arrange: func(fl *fleet) { fl.conns[0].Drop(); run(fl.t, fl.a) },
			action:  func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/1/update") },
			status:  409, alert: "The controller is not connected, so it cannot be updated. Try again once it is online.",
		},
		"updating a door without a target": {
			arrange: func(fl *fleet) { send(fl.t, fl.b, "PUT", "/api/v1/firmware/default", `{"firmware_id":null}`, 204) },
			action:  func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/1/update") },
			status:  409, alert: "The door has no firmware to run. Choose a default version or one for this door first.",
		},
		"updating without the public address": {
			arrange: func(fl *fleet) { fl.f.EnvStub["DOORCODE_PUBLIC_URL"] = "" },
			action:  func(b *testsupport.Browser) testsupport.Response { return b.Post("/admin/doors/1/update") },
			status:  409, alert: "Set the public_url setting first, so the door can download its firmware.",
		},
		"choosing firmware that is not there": {
			arrange: func(*fleet) {},
			action: func(b *testsupport.Browser) testsupport.Response {
				return b.Post("/admin/doors/1/firmware", "firmware_id", "9")
			},
			status: 404, alert: "That firmware is not there. It may have been deleted.",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			c.arrange(fl)
			b := fl.browser()

			got := c.action(b)

			if diff := cmp.Diff([]any{c.status, []string{c.alert}}, []any{got.Status, pageOf(got).Messages}); diff != "" {
				t.Errorf("status, alert (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEventsPageListsFirmwareUpdates(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	b := fl.browser()

	got := pageOf(b.Get("/admin/events?type=update")).Tables["Events"]

	want := [][]string{eventsHead,
		{"Tue 6 Jan 2026 02:00:00", "Dock", "No one", "Info Update sent", "Firmware update Version 1.5.0."},
		{"Tue 6 Jan 2026 02:00:00", "Lobby", "No one", "Info Update sent", "Firmware update Version 1.5.0."},
		{"Showing 1 to 2 of 2"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}
