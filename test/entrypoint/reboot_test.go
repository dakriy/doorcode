package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestRebootingADoorFromTheAPISendsItTheCommandAndLogsTheCaller(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)

	got := send(t, b, "POST", "/api/v1/doors/1/reboot", "", 200)

	rebooted := testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"command","outcome":"info",` +
		`"reason":"reboot sent","user":{"id":1,"name":"Administrator"},"detail":"Told to restart and connect again."}`)
	want := []any{rebooted, map[string]any{"v": int64(1), "command": "reboot"}, rebooted}
	if diff := cmp.Diff(want, []any{testsupport.JSON(got.Body), commandOf(t, conn),
		testsupport.JSONAt(b.Get("/api/v1/events?type=command&reason=reboot+sent").Body, "/events/0")}); diff != "" {
		t.Errorf("answer, command, event (-want +got):\n%s", diff)
	}
}

func TestADoorCommanderRebootsADoorFromItsPageAndIsToldSo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)
	holdingOnly(t, f, b, `["door-commander"]`)
	b.Get("/")

	rebooted := b.Post("/admin/doors/1/reboot")
	page := pageOf(b.Get(rebooted.Location))

	want := []any{303, "/admin/doors/1?tab=overview", "reboot",
		[]string{"The controller has been told to restart. It goes offline for a moment and connects again. The reboot is logged under your name."}}
	if diff := cmp.Diff(want, []any{rebooted.Status, rebooted.Location, commandOf(t, conn)["command"], page.Messages}); diff != "" {
		t.Errorf("status, where it goes, command, messages (-want +got):\n%s", diff)
	}
}

func TestTheDoorPageOffersRebootBesideOpenAndAsksFirst(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	b.Get("/")

	got := testsupport.Confirmations(b.Get("/admin/doors/1").Body)

	want := []string{
		"Open now: Release the strike of Lobby now?",
		"Reboot: Reboot the controller of Lobby now? The door is offline until it connects again.",
	}
	if diff := cmp.Diff(want, []string{got["/admin/doors/1/open"], got["/admin/doors/1/reboot"]}); diff != "" {
		t.Errorf("open, reboot (-want +got):\n%s", diff)
	}
}

func TestRebootingADoorNeedsADoorRoleAndItsControllerConnected(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, conn *testsupport.Connection)
		target  string
		status  int
		body    string
	}{
		"a user holding neither role": {func(t *testing.T, f *testsupport.Factory, b *testsupport.Browser, _ *testsupport.Connection) {
			holdingOnly(t, f, b, `["view-logs"]`)
		}, "/api/v1/doors/1/reboot", 403, `{"error":"the manage-doors or door-commander role is required"}`},
		"a door that is not there": {nil, "/api/v1/doors/9/reboot", 404, `{"error":"no door has this id"}`},
		"a door no controller is enrolled as": {func(t *testing.T, _ *testsupport.Factory, b *testsupport.Browser, _ *testsupport.Connection) {
			createDoor(t, b, `{"name":"Dock"}`)
		}, "/api/v1/doors/2/reboot", 409, `{"error":"the door's controller is not connected"}`},
		"a door whose controller went away": {func(t *testing.T, _ *testsupport.Factory, _ *testsupport.Browser, conn *testsupport.Connection) {
			conn.Drop()
		}, "/api/v1/doors/1/reboot", 409, `{"error":"the door's controller is not connected"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			a, b, conn, _ := enrolledDoor(t, f)
			if c.arrange != nil {
				c.arrange(t, f, b, conn)
				run(t, a)
			}

			got := b.Post(c.target)

			if diff := cmp.Diff([]any{c.status, testsupport.JSON(c.body)}, []any{got.Status, testsupport.JSON(got.Body)}); diff != "" {
				t.Errorf("status, body (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRebootingADoorFromItsPageWhileItsControllerIsAwaySaysSo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	b.Get("/")

	got := b.Post("/admin/doors/1/reboot")

	want := []any{409, []string{"The controller is not connected, so it cannot be rebooted from here. Try again once it is online."}}
	if diff := cmp.Diff(want, []any{got.Status, pageOf(got).Messages}); diff != "" {
		t.Errorf("status, messages (-want +got):\n%s", diff)
	}
}

func TestRebootingADoorFailsWhenTheCommandCannotBeSentOrLogged(t *testing.T) {
	t.Parallel()
	cases := map[string]func(f *testsupport.Factory){
		"the broker": func(f *testsupport.Factory) { f.BrokerStub.FailPublishing() },
		"the events": func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() },
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			fail(f)

			if diff := cmp.Diff(500, b.Post("/api/v1/doors/1/reboot").Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}
