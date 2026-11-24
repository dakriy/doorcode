package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// rotationDue is when the credentials enrolledDoor gives are as old as
// the default rotation interval.
const rotationDue = "2026-02-04T09:00:00Z"

// manualRotationNote is what a door's Controller tab says while the
// server does not rotate the door's credentials on its own.
const manualRotationNote = "Automatic credential rotation is off for this door: its credentials change only when someone rotates them."

func TestAutomaticRotationFollowsTheSiteDefaultAndTheDoorsOverride(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		site, door string
		want       []string
	}{
		"on for the site":                   {"", "", []string{"in-use", "issued"}},
		"off for the site":                  {"false", "", []string{"in-use"}},
		"off for the door":                  {"", "false", []string{"in-use"}},
		"on for the door, off for the site": {"false", "true", []string{"in-use", "issued"}},
		"off for the door, on for the site": {"true", "false", []string{"in-use"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			a, b, _, _ := enrolledDoor(t, f)
			if c.site != "" {
				send(t, b, "PUT", "/api/v1/settings/door_auto_credential_rotation", `{"value":"`+c.site+`"}`, 200)
			}
			if c.door != "" {
				send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"auto_credential_rotation":"`+c.door+`"}}`, 200)
			}
			f.ClockStub.Set(rotationDue)

			run(t, a)

			if diff := cmp.Diff(c.want, statuses(t, signIn(t, f, a.Handler()), "1")); diff != "" {
				t.Errorf("statuses (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAutomaticRotationOffForOneDoorStillRotatesTheOthers(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	enrolling := announce(t, f, "B2")
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/B2/approve", `{"door":{"name":"Dock"}}`, 200)
	creds := credentialsOf(t, enrolling)
	enrolling.Close()
	connectAs(t, f, creds)
	run(t, a)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"auto_credential_rotation":"false"}}`, 200)
	f.ClockStub.Set(rotationDue)

	run(t, a)

	b = signIn(t, f, a.Handler())
	want := [][]string{{"in-use"}, {"in-use", "issued"}}
	if diff := cmp.Diff(want, [][]string{statuses(t, b, "1"), statuses(t, b, "2")}); diff != "" {
		t.Errorf("statuses of door 1, door 2 (-want +got):\n%s", diff)
	}
}

func TestAutomaticRotationIsATrueOrFalseSetting(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, frontDoor)

	got := []any{
		b.PutJSON("/api/v1/settings/door_auto_credential_rotation", `{"value":"sometimes"}`).Status,
		b.PutJSON("/api/v1/doors/1/config", `{"overrides":{"auto_credential_rotation":"sometimes"}}`).Status,
		testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/effective/auto_credential_rotation"),
	}
	if diff := cmp.Diff([]any{400, 400, true}, got); diff != "" {
		t.Errorf("default, override, in effect (-want +got):\n%s", diff)
	}
}

func TestRotatingByHandWorksWhileAutomaticRotationIsOff(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/settings/door_auto_credential_rotation", `{"value":"false"}`, 200)

	send(t, b, "POST", "/api/v1/doors/1/credentials/rotate", "", 204)

	want := []any{"credentials", []string{"in-use", "issued"}}
	if diff := cmp.Diff(want, []any{lastReceived(t, conn)["command"], statuses(t, b, "1")}); diff != "" {
		t.Errorf("command sent, statuses (-want +got):\n%s", diff)
	}
}

func TestARotationUnderWayFinishesAfterAutomaticRotationIsTurnedOff(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _, conn, old := enrolledDoor(t, f)
	f.ClockStub.Set(rotationDue)
	run(t, a)
	rotated := credentialsOf(t, conn)
	rotated.door = "1"
	b := signIn(t, f, a.Handler())
	send(t, b, "PUT", "/api/v1/settings/door_auto_credential_rotation", `{"value":"false"}`, 200)
	before := connects(f, old, rotated)

	conn.Close()
	connectAs(t, f, rotated)
	run(t, a)
	f.ClockStub.Set("2026-03-06T09:00:00Z")
	run(t, a)

	b = signIn(t, f, a.Handler())
	got := []any{before, connects(f, old, rotated), statuses(t, b, "1")}
	want := []any{[]bool{true, true}, []bool{false, true}, []string{"revoked", "in-use"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("connects before, after, statuses (-want +got):\n%s", diff)
	}
}

func TestADoorsControllerTabSaysWhenItsCredentialsRotateOnlyByHand(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		overrides string
		want      []string
	}{
		"automatic": {`{"overrides":{}}`, []string{noTargetNote}},
		"by hand":   {`{"overrides":{"auto_credential_rotation":"false"}}`, []string{manualRotationNote, noTargetNote}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/doors/1/config", c.overrides, 200)

			page := doorPage(t, b, "/admin/doors/1?tab=controller")

			if diff := cmp.Diff(c.want, page.Notes); diff != "" {
				t.Errorf("notes (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAutomaticRotationChangesNothingTheControllerIsSent(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	sent := func() []any {
		config, _ := f.BrokerStub.Retained(doorTopic("1", "config"))
		return []any{config.Payload, testsupport.JSONAt(b.Get("/api/v1/doors/1/state").Body, "/config_sent_digest")}
	}
	on := sent()

	send(t, b, "PUT", "/api/v1/settings/door_auto_credential_rotation", `{"value":"false"}`, 200)
	off := sent()
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"auto_credential_rotation":"true"}}`, 200)
	overriddenOn := sent()
	send(t, b, "PUT", "/api/v1/settings/door_auto_credential_rotation", `{"value":"true"}`, 200)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"auto_credential_rotation":"false"}}`, 200)
	overriddenOff := sent()

	if diff := cmp.Diff([][]any{on, on, on}, [][]any{off, overriddenOn, overriddenOff}); diff != "" {
		t.Errorf("payload and digest sent: site off, door on, door off (-want +got):\n%s", diff)
	}
}

func TestADoorsPageAsksWhetherCredentialsRotateOnTheirOwnAsTheDefaultOnOrOff(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
	b.Get("/")

	saved := b.Post("/admin/doors/1/config", "auto_credential_rotation", "false")
	page := testsupport.ParsePage(b.Get("/admin/doors/1?tab=configuration").Body)

	want := []any{303, map[string]any{"auto_credential_rotation": "false"}, []string{"Default (On)", "On", "Off (selected)"}, "select"}
	got := []any{saved.Status, testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/overrides"),
		page.Options["auto_credential_rotation"], page.Inputs["auto_credential_rotation"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, overrides, options, input (-want +got):\n%s", diff)
	}
}
