package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// noTargetNote is what a door's page says while the door has no firmware
// to run.
const noTargetNote = "Choose a default version on the firmware page, or one for this door, to update it."

// The SHA-256 of the images imageOf makes of versions 1.5.0 and 1.6.0.
const (
	sha150 = "4d4bd67b11237adcbcdefc64267f6a2cdd6b9a56ddbf6050c2c2113164073ad0"
	sha160 = "1607ac6905854a533304e6d419fd956c83e059aab9ba73a23ad486060cae4245"
)

// imageOf is the firmware image the tests upload as version.
func imageOf(version string) []byte {
	return []byte("firmware " + version)
}

// uploadFirmware uploads the image of version through the API, failing
// the test unless it is created.
func uploadFirmware(t *testing.T, c *testsupport.Browser, version string) {
	t.Helper()
	got := c.Upload("/api/v1/firmware", "image", "doorcode.bin", imageOf(version), "version", version, "notes", "Release "+version+".")
	if got.Status != 201 {
		t.Fatalf("uploading %s answered %d: %s", version, got.Status, got.Body)
	}
}

// fleet is a broker site where the controllers A1, B2, and C3 are
// enrolled as Lobby (1), Dock (2), and Attic (3) and connected, running
// 1.4.3, and version 1.5.0 is uploaded as firmware 1 and is the default.
// Doors download firmware from https://doorcode.example.com, the update
// window opens every day at 02:00 UTC for two hours, and two doors may
// update at once. b is the administrator's API client.
type fleet struct {
	t     *testing.T
	f     *testsupport.Factory
	a     *app.App
	b     *testsupport.Browser
	conns []*testsupport.Connection
	creds []doorCredentials
}

func fleetSite(t *testing.T, f *testsupport.Factory) *fleet {
	t.Helper()
	f.EnvStub["DOORCODE_PUBLIC_URL"] = "https://doorcode.example.com"
	f.EnvStub["DOORCODE_UPDATE_CONCURRENCY"] = "2"
	a, b := brokerSite(t, f)
	fl := &fleet{t: t, f: f, a: a, b: b}
	for i, hwid := range []string{"A1", "B2", "C3"} {
		createDoor(t, b, `{"name":"`+[]string{"Lobby", "Dock", "Attic"}[i]+`"}`)
		conn, creds := enrollWith(t, f, a, b, hwid, itoa(i+1))
		fl.conns, fl.creds = append(fl.conns, conn), append(fl.creds, creds)
	}
	// A token outlasts the session, which ends before the days the tests
	// move the clock on.
	fl.b = tokenClient(a.Handler(), createToken(t, b, `{"name":"ci"}`))
	uploadFirmware(t, fl.b, "1.5.0")
	send(t, fl.b, "PUT", "/api/v1/firmware/default", `{"firmware_id":1}`, 204)
	return fl
}

// at moves the clock to the RFC 3339 time at and runs the server.
func (fl *fleet) at(at string) {
	fl.t.Helper()
	fl.f.ClockStub.Set(at)
	run(fl.t, fl.a)
}

// commands is the command topic of every door sent a command so far, in
// order.
func (fl *fleet) commands() []string {
	return fl.f.BrokerStub.Topics(0, "command")
}

// reconnects has the controller of the door whose id is door publish that
// it is online running version, and the server handle it.
func (fl *fleet) reconnects(door int, version string) {
	fl.t.Helper()
	online := testsupport.CBOR(map[string]any{
		"v": 1, "online": true, "username": fl.creds[door-1].username, "firmware_version": version, "signal": -58,
	})
	fl.publish(door, "online", online)
}

// acks has the controller of the door whose id is door answer the last
// command it received with result and detail.
func (fl *fleet) acks(door int, result, detail string) {
	fl.t.Helper()
	id := lastReceived(fl.t, fl.conns[door-1])["id"]
	fl.publish(door, "ack", testsupport.CBOR(map[string]any{"v": 1, "id": id, "result": result, "detail": detail}))
}

// publish has the controller of the door whose id is door publish payload
// on its topic ending in leaf, and the server handle it.
func (fl *fleet) publish(door int, leaf string, payload []byte) {
	fl.t.Helper()
	if err := fl.conns[door-1].Publish(doorTopic(itoa(door), leaf), payload, leaf == "online"); err != nil {
		fl.t.Fatal(err)
	}
	run(fl.t, fl.a)
}

// download requests the URL of the last command the controller of the
// door whose id is door received.
func (fl *fleet) download(door int) testsupport.Response {
	fl.t.Helper()
	url, _ := lastReceived(fl.t, fl.conns[door-1])["url"].(string)
	return testsupport.NewBrowser(fl.a.Handler()).Get(testsupport.Target(url))
}

// status is the rollout status of the door whose id is door, as the API
// shows it.
func (fl *fleet) status(door int) any {
	fl.t.Helper()
	return testsupport.JSONAt(send(fl.t, fl.b, "GET", "/api/v1/doors/"+itoa(door)+"/firmware", "", 200).Body, "/status")
}

// updateEvents is the reason and detail of every firmware update event,
// newest first.
func (fl *fleet) updateEvents() []any {
	fl.t.Helper()
	body := send(fl.t, fl.b, "GET", "/api/v1/events?type=update", "", 200).Body
	events, _ := testsupport.JSONAt(body, "/events").([]any)
	var out []any
	for _, e := range events {
		m, _ := e.(map[string]any)
		out = append(out, []any{m["door"].(map[string]any)["id"], m["outcome"], m["reason"], m["detail"]})
	}
	return out
}

func TestTheRolloutUpdatesAtMostAsManyDoorsAtOnceAsTheSettingAllows(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())

	fl.at("2026-01-06T02:00:00Z")
	first := fl.commands()
	fl.at("2026-01-06T02:01:00Z")
	waiting := fl.commands()
	fl.reconnects(1, "1.5.0")
	fl.at("2026-01-06T02:02:00Z")

	want := []any{topicsOf("command", "1", "2"), topicsOf("command", "1", "2"), topicsOf("command", "1", "2", "3")}
	if diff := cmp.Diff(want, []any{first, waiting, fl.commands()}); diff != "" {
		t.Errorf("commands sent at 02:00, at 02:01, after Lobby came back (-want +got):\n%s", diff)
	}
}

func TestTheRolloutSendsNothingUntilTheWindowOpens(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())

	fl.at("2026-01-06T04:30:00Z")
	after := fl.commands()
	fl.at("2026-01-07T01:59:00Z")
	before := fl.commands()
	fl.at("2026-01-07T02:00:00Z")

	want := []any{[]string{}, []string{}, topicsOf("command", "1", "2")}
	if diff := cmp.Diff(want, []any{after, before, fl.commands()}); diff != "" {
		t.Errorf("commands after the window, before the next, once it opened (-want +got):\n%s", diff)
	}
}

func TestTheUpdateWindowIsReadInTheSiteTimezoneAndMayRunPastMidnight(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Europe/Berlin"
	f.EnvStub["DOORCODE_UPDATE_WINDOW_START"] = "23:30"
	f.EnvStub["DOORCODE_UPDATE_WINDOW_MINUTES"] = "60"
	fl := fleetSite(t, f)

	fl.at("2026-01-05T22:20:00Z")
	early := fl.commands()
	fl.at("2026-01-05T23:20:00Z")

	// 23:20 UTC is 00:20 in Berlin, inside the window that opened at 23:30
	// the day before.
	want := []any{[]string{}, topicsOf("command", "1", "2")}
	if diff := cmp.Diff(want, []any{early, fl.commands()}); diff != "" {
		t.Errorf("commands at 23:20 and 00:20 Berlin time (-want +got):\n%s", diff)
	}
}

func TestAWeeklyUpdateWindowOpensOnlyOnItsWeekdays(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_UPDATE_WINDOW_DAYS"] = "mon,sat"
	fl := fleetSite(t, f)

	fl.at("2026-01-06T02:00:00Z")
	tuesday := fl.commands()
	fl.at("2026-01-10T02:00:00Z")

	want := []any{[]string{}, topicsOf("command", "1", "2")}
	if diff := cmp.Diff(want, []any{tuesday, fl.commands()}); diff != "" {
		t.Errorf("commands on Tuesday and on Saturday (-want +got):\n%s", diff)
	}
}

func TestAnUpdateCommandCarriesTheVersionItsHashAndADownloadOfTheImage(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())

	fl.at("2026-01-06T02:00:00Z")

	command := commandOf(t, fl.conns[0])
	delete(command, "url")
	download := fl.download(1)
	want := []any{
		map[string]any{"v": int64(1), "command": "update", "version": "1.5.0", "sha256": sha150},
		testsupport.Response{Status: 200, ContentType: "application/octet-stream", Body: "firmware 1.5.0"},
	}
	if diff := cmp.Diff(want, []any{command, download}); diff != "" {
		t.Errorf("command, download (-want +got):\n%s", diff)
	}
}

func TestTheDownloadRouteRefusesATokenThatDoesNotFit(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(fl *fleet)
		target  func(fl *fleet) string
	}{
		"expired": {
			arrange: func(fl *fleet) { fl.f.ClockStub.Set("2026-01-06T02:30:00Z") },
			target:  func(fl *fleet) string { return lastURL(fl, 1) },
		},
		"for another door": {
			arrange: func(*fleet) {},
			target:  func(fl *fleet) string { return "/api/v1/doors/2/firmware/1.5.0?" + query(lastURL(fl, 1)) },
		},
		"for a door never sent an update": {
			arrange: func(*fleet) {},
			target:  func(fl *fleet) string { return "/api/v1/doors/3/firmware/1.5.0?" + query(lastURL(fl, 1)) },
		},
		"for another version": {
			arrange: func(*fleet) {},
			target:  func(fl *fleet) string { return "/api/v1/doors/1/firmware/1.4.3?" + query(lastURL(fl, 1)) },
		},
		"of an update that has ended": {
			arrange: func(fl *fleet) { fl.reconnects(1, "1.5.0") },
			target:  func(fl *fleet) string { return lastURL(fl, 1) },
		},
		"missing": {
			arrange: func(*fleet) {},
			target:  func(*fleet) string { return "/api/v1/doors/1/firmware/1.5.0" },
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			fl.at("2026-01-06T02:00:00Z")
			target := c.target(fl)
			c.arrange(fl)

			got := testsupport.NewBrowser(fl.a.Handler()).Get(target)

			want := testsupport.Response{Status: 403, ContentType: jsonType, Body: `{"error":"this token does not let the door download this version"}` + "\n"}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}

// lastURL is the target of the URL of the last command the controller of
// the door whose id is door received.
func lastURL(fl *fleet, door int) string {
	url, _ := lastReceived(fl.t, fl.conns[door-1])["url"].(string)
	return testsupport.Target(url)
}

// query is what follows the first ? of target.
func query(target string) string {
	for i := range target {
		if target[i] == '?' {
			return target[i+1:]
		}
	}
	return ""
}

func TestADownloadOfAVersionDeletedSinceIsNotFound(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	send(t, fl.b, "POST", "/api/v1/doors/1/update", "", 200)
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/firmware/default", `{"firmware_id":2}`, 204)
	send(t, fl.b, "DELETE", "/api/v1/firmware/1", "", 204)

	got := fl.download(1)

	if diff := cmp.Diff([]any{404, `{"error":"no firmware has this id"}` + "\n"}, []any{got.Status, got.Body}); diff != "" {
		t.Errorf("status, body (-want +got):\n%s", diff)
	}
}

func TestUpdatingADoorNowIgnoresTheWindowAndTheLimit(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())

	var bodies []any
	for _, door := range []string{"1", "2", "3"} {
		bodies = append(bodies, testsupport.JSONAt(send(t, fl.b, "POST", "/api/v1/doors/"+door+"/update", "", 200).Body, ""))
	}

	sent := `"last_update":{"version":"1.5.0","state":"updating","sent_at":"2026-01-05T09:00:00Z"}`
	want := []any{
		topicsOf("command", "1", "2", "3"),
		testsupport.JSON(`{"door":{"id":3,"name":"Attic"},"version":"1.4.3","target":{"id":1,"version":"1.5.0"},"status":"updating",` + sent + `}`),
		[]any{"update sent", "Version 1.5.0.", map[string]any{"id": float64(1), "name": "Administrator"}},
	}
	event := testsupport.JSONAt(send(t, fl.b, "GET", "/api/v1/events?type=update", "", 200).Body, "/events/0")
	m, _ := event.(map[string]any)
	got := []any{fl.commands(), bodies[2], []any{m["reason"], m["detail"], m["user"]}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("commands, last answer, last event (-want +got):\n%s", diff)
	}
}

func TestUpdatingADoorNowIsRefusedWhenItCannotBeSent(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(fl *fleet)
		door    string
		status  int
		error   string
	}{
		"offline": {
			arrange: func(fl *fleet) { fl.conns[0].Drop(); run(fl.t, fl.a) },
			door:    "1", status: 409, error: "the door's controller is not connected",
		},
		"without a target": {
			arrange: func(fl *fleet) { send(fl.t, fl.b, "PUT", "/api/v1/firmware/default", `{"firmware_id":null}`, 204) },
			door:    "1", status: 409, error: "the door has no firmware to run; choose a default version or one for the door",
		},
		"without the public address": {
			arrange: func(fl *fleet) { fl.f.EnvStub["DOORCODE_PUBLIC_URL"] = "" },
			door:    "1", status: 409, error: "set the public_url setting first, so doors can download firmware",
		},
		"of an unknown door": {
			arrange: func(*fleet) {},
			door:    "9", status: 404, error: "no door has this id",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			c.arrange(fl)

			got := send(t, fl.b, "POST", "/api/v1/doors/"+c.door+"/update", "", c.status)

			if diff := cmp.Diff(`{"error":"`+c.error+`"}`+"\n", got.Body); diff != "" {
				t.Errorf("body (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorThatReconnectsWithTheNewVersionHasUpdated(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")

	fl.f.ClockStub.Set("2026-01-06T02:03:00Z")
	fl.reconnects(1, "1.5.0")

	got := send(t, fl.b, "GET", "/api/v1/doors/1/firmware", "", 200)
	want := testsupport.JSON(`{"door":{"id":1,"name":"Lobby"},"version":"1.5.0","target":{"id":1,"version":"1.5.0"},"status":"done",` +
		`"last_update":{"version":"1.5.0","state":"succeeded","sent_at":"2026-01-06T02:00:00Z","finished_at":"2026-01-06T02:03:00Z"}}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "")); diff != "" {
		t.Errorf("door firmware (-want +got):\n%s", diff)
	}
	events := []any{
		[]any{float64(1), "info", "update succeeded", "Version 1.5.0."},
		[]any{float64(2), "info", "update sent", "Version 1.5.0."},
		[]any{float64(1), "info", "update sent", "Version 1.5.0."},
	}
	if diff := cmp.Diff(events, fl.updateEvents()); diff != "" {
		t.Errorf("events (-want +got):\n%s", diff)
	}
}

func TestADoorThatReconnectsStillOnTheOldVersionIsStillUpdating(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")

	fl.reconnects(1, "1.4.3")

	if diff := cmp.Diff("updating", fl.status(1)); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestADoorThatSaysItsUpdateFailedHasFailed(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		detail string
		want   string
	}{
		"saying why":     {"image does not fit", "The door could not install it: image does not fit."},
		"not saying why": {"", "The door could not install it."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			fl.at("2026-01-06T02:00:00Z")

			fl.acks(1, "failed", c.detail)

			got := []any{fl.status(1), fl.updateEvents()[0]}
			want := []any{"failed", []any{float64(1), "alert", "update failed", "Version 1.5.0. " + c.want}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("status, newest event (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnswersThatDoNotFailTheUpdateLeaveItUnderWay(t *testing.T) {
	t.Parallel()
	cases := map[string]func(fl *fleet){
		"another command's": func(fl *fleet) {
			fl.publish(1, "ack", testsupport.CBOR(map[string]any{"v": 1, "id": "0001020304050607", "result": "failed"}))
		},
		"one that succeeded": func(fl *fleet) { fl.acks(1, "ok", "") },
		"one of an update that has ended": func(fl *fleet) {
			fl.reconnects(1, "1.5.0")
			fl.acks(1, "failed", "")
		},
	}
	want := map[string]string{"another command's": "updating", "one that succeeded": "updating", "one of an update that has ended": "done"}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			fl.at("2026-01-06T02:00:00Z")

			answer(fl)

			if diff := cmp.Diff(want[name], fl.status(1)); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnUpdateThatTakesLongerThanTheTimeoutHasFailedAndFreesItsPlace(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")

	fl.at("2026-01-06T02:29:00Z")
	before := fl.commands()
	fl.at("2026-01-06T02:30:00Z")

	want := []any{
		topicsOf("command", "1", "2"),
		topicsOf("command", "1", "2", "3"),
		[]any{float64(2), "alert", "update failed", "Version 1.5.0. It did not connect again with it within 30 minutes."},
	}
	if diff := cmp.Diff(want, []any{before, fl.commands(), fl.updateEvents()[1]}); diff != "" {
		t.Errorf("commands at 02:29 and 02:30, the second failure (-want +got):\n%s", diff)
	}
}

func TestAFailedDoorIsTriedAgainInTheNextWindowAndNotThisOne(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "")
	fl.reconnects(2, "1.5.0")

	fl.at("2026-01-06T02:05:00Z")
	same := fl.commands()
	fl.reconnects(3, "1.5.0")
	fl.at("2026-01-07T02:00:00Z")

	want := []any{topicsOf("command", "1", "2", "3"), topicsOf("command", "1", "2", "3", "1")}
	if diff := cmp.Diff(want, []any{same, fl.commands()}); diff != "" {
		t.Errorf("commands later that window and in the next (-want +got):\n%s", diff)
	}
}

func TestADoorGivenAnotherTargetIsSentItInTheSameWindow(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	fl.reconnects(1, "1.5.0")
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/doors/1/firmware", `{"firmware_id":2}`, 200)

	fl.at("2026-01-06T02:01:00Z")

	command := commandOf(t, fl.conns[0])
	delete(command, "url")
	want := []any{topicsOf("command", "1", "2", "1"), map[string]any{"v": int64(1), "command": "update", "version": "1.6.0", "sha256": sha160}}
	if diff := cmp.Diff(want, []any{fl.commands(), command}); diff != "" {
		t.Errorf("commands, Lobby's last (-want +got):\n%s", diff)
	}
}

func TestADoorWhoseUpdateToAnotherVersionFailedIsPending(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	fl.acks(1, "failed", "")
	uploadFirmware(t, fl.b, "1.6.0")

	send(t, fl.b, "PUT", "/api/v1/doors/1/firmware", `{"firmware_id":2}`, 200)

	if diff := cmp.Diff("pending", fl.status(1)); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestTheRolloutPassesOverDoorsThatAreOfflineOrUpToDate(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.conns[0].Drop()
	fl.reconnects(2, "1.5.0")

	fl.at("2026-01-06T02:00:00Z")

	if diff := cmp.Diff(topicsOf("command", "3"), fl.commands()); diff != "" {
		t.Errorf("commands (-want +got):\n%s", diff)
	}
}

func TestTheRolloutShowsEveryDoorByHowFarItsUpdateIs(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	createDoor(t, fl.b, `{"name":"Shed"}`)
	fl.at("2026-01-06T02:00:00Z")
	fl.reconnects(1, "1.5.0")
	fl.acks(2, "failed", "")

	got := send(t, fl.b, "GET", "/api/v1/firmware/rollout", "", 200)

	target := `"target":{"id":1,"version":"1.5.0"}`
	want := testsupport.JSON(`{
		"pending": [{"door":{"id":3,"name":"Attic"},"version":"1.4.3",` + target + `,"status":"pending"}],
		"updating": [],
		"done": [{"door":{"id":1,"name":"Lobby"},"version":"1.5.0",` + target + `,"status":"done",
			"last_update":{"version":"1.5.0","state":"succeeded","sent_at":"2026-01-06T02:00:00Z","finished_at":"2026-01-06T02:00:00Z"}}],
		"failed": [{"door":{"id":2,"name":"Dock"},"version":"1.4.3",` + target + `,"status":"failed",
			"last_update":{"version":"1.5.0","state":"failed","sent_at":"2026-01-06T02:00:00Z","finished_at":"2026-01-06T02:00:00Z",
				"detail":"The door could not install it."}}]
	}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "")); diff != "" {
		t.Errorf("rollout (-want +got):\n%s", diff)
	}
}

func TestFirmwareIsListedWithItsUploaderAndTheDoorsThatRunIt(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.f.ClockStub.Set("2026-01-05T10:00:00Z")
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/doors/3/firmware", `{"firmware_id":2}`, 200)
	fl.reconnects(1, "1.5.0")
	send(t, fl.b, "DELETE", "/api/v1/doors/2", "", 204)

	got := send(t, fl.b, "GET", "/api/v1/firmware", "", 200)

	by := `"uploaded_by":{"id":1,"name":"Administrator"}`
	want := testsupport.JSON(`{"firmware":[
		{"id":2,"version":"1.6.0","notes":"Release 1.6.0.","sha256":"` + sha160 + `","size":14,` + by + `,
		 "uploaded_at":"2026-01-05T10:00:00Z","default":false,"running":0,"targeting":1},
		{"id":1,"version":"1.5.0","notes":"Release 1.5.0.","sha256":"` + sha150 + `","size":14,` + by + `,
		 "uploaded_at":"2026-01-05T09:00:00Z","default":true,"running":1,"targeting":1}
	]}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "")); diff != "" {
		t.Errorf("firmware (-want +got):\n%s", diff)
	}
}

func TestUploadedFirmwareCanBeReadBack(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	created := b.Upload("/api/v1/firmware", "image", "doorcode.bin", imageOf("1.5.0"), "version", " 1.5.0 ", "notes", " Fixes the keypad. ")

	got := b.Get("/api/v1/firmware/1")

	want := testsupport.JSON(`{"id":1,"version":"1.5.0","notes":"Fixes the keypad.","sha256":"` + sha150 + `","size":14,` +
		`"uploaded_by":{"id":1,"name":"Administrator"},"uploaded_at":"2026-01-05T09:00:00Z","default":false,"running":0,"targeting":0}`)
	if diff := cmp.Diff([]any{201, want, created.Body}, []any{created.Status, testsupport.JSONAt(created.Body, ""), got.Body}); diff != "" {
		t.Errorf("status, created, read back (-want +got):\n%s", diff)
	}
}

func TestAFirmwareUploadThatIsWrongIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		upload func(b *testsupport.Browser) testsupport.Response
		status int
		error  string
	}{
		"without a version": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("1"), "version", " ")
			},
			status: 400, error: "Enter the version, such as 1.4.3.",
		},
		"with a version too long": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("1"), "version", repeat("1", 65))
			},
			status: 400, error: "Use at most 64 characters.",
		},
		"with a version of other characters": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("1"), "version", "1.5/0")
			},
			status: 400, error: "Use letters, digits, dots, dashes, underscores, and plus signs, starting with a letter or digit.",
		},
		"with a version uploaded already": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("1"), "version", "1.5.0")
				return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("2"), "version", "1.5.0")
			},
			status: 400, error: "This version is uploaded already.",
		},
		"with notes too long": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("1"), "version", "1.5.0", "notes", repeat("n", 2001))
			},
			status: 400, error: "Use at most 2000 characters.",
		},
		"without an image": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", nil, "version", "1.5.0")
			},
			status: 400, error: "Choose the firmware image to upload.",
		},
		"not as a multipart form": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.SendForm("/api/v1/firmware", "application/json", `{"version":"1.5.0"}`)
			},
			status: 400, error: "Send the firmware as a multipart form with the image in the field image.",
		},
		"cut short": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.SendForm("/api/v1/firmware", "multipart/form-data; boundary=x", "--x\r\nContent-Disposition: form-data; name=\"version\"\r\n\r\n1.5.0")
			},
			status: 400, error: "Send the firmware as a multipart form with the image in the field image.",
		},
		"with a part that is not one": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.SendForm("/api/v1/firmware", "multipart/form-data; boundary=x",
					"--x\r\nContent-Disposition: form-data; name=\"version\"\r\n\r\n1.5.0\r\n--x\r\nno header\r\n\r\nx\r\n--x--\r\n")
			},
			status: 400, error: "Send the firmware as a multipart form with the image in the field image.",
		},
		"too large": {
			upload: func(b *testsupport.Browser) testsupport.Response {
				return b.Upload("/api/v1/firmware", "image", "a.bin", testsupport.Padded(nil, 16_777_217), "version", "1.5.0")
			},
			status: 413, error: "the image is larger than 16 MiB",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())

			got := c.upload(b)

			if diff := cmp.Diff([]any{c.status, `{"error":"` + c.error + `"}` + "\n"}, []any{got.Status, got.Body}); diff != "" {
				t.Errorf("status, body (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnImageOfTheLargestSizeIsAccepted(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := b.Upload("/api/v1/firmware", "image", "a.bin", testsupport.Padded(nil, 16_777_216), "version", "1.5.0")

	if diff := cmp.Diff([]any{201, float64(16_777_216)}, []any{got.Status, testsupport.JSONAt(got.Body, "/size")}); diff != "" {
		t.Errorf("status, size (-want +got):\n%s", diff)
	}
}

func TestFirmwareInUseCannotBeDeleted(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.6.0")
	send(t, fl.b, "PUT", "/api/v1/doors/1/firmware", `{"firmware_id":2}`, 200)
	send(t, fl.b, "PUT", "/api/v1/doors/3/firmware", `{"firmware_id":2}`, 200)

	got := []any{
		send(t, fl.b, "DELETE", "/api/v1/firmware/1", "", 409).Body,
		send(t, fl.b, "DELETE", "/api/v1/firmware/2", "", 409).Body,
	}

	want := []any{
		`{"error":"this is the default version; choose another default before deleting it"}` + "\n",
		`{"error":"these doors run this version instead of the default: Lobby, Attic; choose other firmware for them first"}` + "\n",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("refusals (-want +got):\n%s", diff)
	}
}

func TestFirmwareNoDoorRunsCanBeDeleted(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	uploadFirmware(t, b, "1.5.0")

	send(t, b, "DELETE", "/api/v1/firmware/1", "", 204)

	if diff := cmp.Diff(`{"firmware":[]}`+"\n", b.Get("/api/v1/firmware").Body); diff != "" {
		t.Errorf("firmware (-want +got):\n%s", diff)
	}
}

func TestFirmwareThatIsNotThereIsNotFound(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby"}`)
	cases := []struct{ method, target, body string }{
		{"GET", "/api/v1/firmware/9", ""},
		{"DELETE", "/api/v1/firmware/9", ""},
		{"PUT", "/api/v1/firmware/default", `{"firmware_id":9}`},
		{"PUT", "/api/v1/doors/1/firmware", `{"firmware_id":9}`},
	}

	var got []string
	for _, c := range cases {
		got = append(got, send(t, b, c.method, c.target, c.body, 404).Body)
	}

	want := `{"error":"no firmware has this id"}` + "\n"
	if diff := cmp.Diff([]string{want, want, want, want}, got); diff != "" {
		t.Errorf("bodies (-want +got):\n%s", diff)
	}
}

func TestAFirmwareChoiceThatIsNotJSONIsRefused(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby"}`)

	got := []string{
		send(t, b, "PUT", "/api/v1/firmware/default", `[1]`, 400).Body,
		send(t, b, "PUT", "/api/v1/doors/1/firmware", `{`, 400).Body,
		send(t, b, "PUT", "/api/v1/doors/9/firmware", `{"firmware_id":null}`, 404).Body,
	}

	refused := `{"error":"body must be a JSON object with \"firmware_id\", a firmware's id or null"}` + "\n"
	if diff := cmp.Diff([]string{refused, refused, `{"error":"no door has this id"}` + "\n"}, got); diff != "" {
		t.Errorf("bodies (-want +got):\n%s", diff)
	}
}

func TestADoorFollowsTheDefaultUntilItIsGivenFirmwareOfItsOwn(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	uploadFirmware(t, fl.b, "1.6.0")

	own := send(t, fl.b, "PUT", "/api/v1/doors/2/firmware", `{"firmware_id":2}`, 200)
	followed := send(t, fl.b, "PUT", "/api/v1/doors/2/firmware", `{"firmware_id":null}`, 200)

	want := []any{
		testsupport.JSON(`{"door":{"id":2,"name":"Dock"},"version":"1.4.3","target":{"id":2,"version":"1.6.0"},` +
			`"override":{"id":2,"version":"1.6.0"},"status":"pending"}`),
		testsupport.JSON(`{"door":{"id":2,"name":"Dock"},"version":"1.4.3","target":{"id":1,"version":"1.5.0"},"status":"pending"}`),
	}
	if diff := cmp.Diff(want, []any{testsupport.JSONAt(own.Body, ""), testsupport.JSONAt(followed.Body, "")}); diff != "" {
		t.Errorf("door firmware with its own and following the default (-want +got):\n%s", diff)
	}
}

func TestADoorWithoutAControllerTakesNoPartInTheRollout(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	createDoor(t, b, `{"name":"Lobby"}`)
	uploadFirmware(t, b, "1.5.0")
	send(t, b, "PUT", "/api/v1/firmware/default", `{"firmware_id":1}`, 204)

	got := send(t, b, "GET", "/api/v1/doors/1/firmware", "", 200)

	want := testsupport.JSON(`{"door":{"id":1,"name":"Lobby"},"version":"","target":{"id":1,"version":"1.5.0"}}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "")); diff != "" {
		t.Errorf("door firmware (-want +got):\n%s", diff)
	}
}

func TestTheRolloutSettingsCanBeReadAndChanged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	f.EnvStub["DOORCODE_UPDATE_TIMEOUT_MINUTES"] = "45"

	got := send(t, b, "PUT", "/api/v1/firmware/settings", `{"update_window_days":"sat,sun","update_window_start":"03:15","update_timeout_minutes":"10"}`, 200)

	stored := func(key, value string) string {
		return `{"key":"` + key + `","value":"` + value + `","source":"database","read_only":false,"env_var":"DOORCODE_` + upper(key) +
			`","updated_at":"2026-01-05T09:00:00Z"}`
	}
	want := testsupport.JSON(`{"settings":[` + stored("update_window_days", "sat,sun") + `,` + stored("update_window_start", "03:15") + `,` +
		defaultSetting("update_window_minutes", "120") + `,` + defaultSetting("update_concurrency", "5") + `,` +
		`{"key":"update_timeout_minutes","value":"45","source":"environment","read_only":true,"env_var":"DOORCODE_UPDATE_TIMEOUT_MINUTES"}]}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "")); diff != "" {
		t.Errorf("settings (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(got.Body, b.Get("/api/v1/firmware/settings").Body); diff != "" {
		t.Errorf("read back (-want +got):\n%s", diff)
	}
}

func TestRolloutSettingsThatAreWrongAreRefused(t *testing.T) {
	t.Parallel()
	notRollout := `body must be a JSON object whose keys are rollout settings (update_window_days, update_window_start, update_window_minutes, ` +
		`update_concurrency, update_timeout_minutes) and whose values are strings`
	cases := map[string]struct{ body, error string }{
		"another setting":   {`{"site_name":"x"}`, notRollout},
		"not an object":     {`["update_concurrency"]`, notRollout},
		"no weekday":        {`{"update_window_days":"someday"}`, "Must be daily or weekdays separated by commas, such as sat,sun."},
		"no time of day":    {`{"update_window_start":"25:00"}`, "Must be a time of day as HH:MM, such as 02:00."},
		"too long a window": {`{"update_window_minutes":"1441"}`, "Must be a whole number of minutes from 1 to 1440."},
		"no doors at once":  {`{"update_concurrency":"0"}`, "Must be a whole number of doors from 1 to 1000."},
		"too many at once":  {`{"update_concurrency":"1001"}`, "Must be a whole number of doors from 1 to 1000."},
		"no timeout":        {`{"update_timeout_minutes":"0"}`, "Must be a whole number of minutes from 1 to 1440."},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())

			got := send(t, b, "PUT", "/api/v1/firmware/settings", c.body, 400)

			if diff := cmp.Diff(`{"error":"`+c.error+`"}`+"\n", got.Body); diff != "" {
				t.Errorf("body (-want +got):\n%s", diff)
			}
		})
	}
}

func TestThePublicAddressMustBeAnHTTPSAddressWithoutAQuery(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	refused := `{"error":"invalid value for public_url: must be empty or an https:// address without a query, such as https://doorcode.example.com"}` + "\n"
	cases := []struct {
		value  string
		status int
	}{
		{"http://doorcode.example.com", 400},
		{"https://", 400},
		{"https://doorcode.example.com/?x=1", 400},
		{"https://doorcode.example.com/#top", 400},
		{"https://doorcode.example.com/%zz", 400},
		{"https://doorcode.example.com/", 200},
		{"", 200},
	}

	var got, want []string
	for _, c := range cases {
		r := send(t, b, "PUT", "/api/v1/settings/public_url", `{"value":"`+c.value+`"}`, c.status)
		if c.status == 400 {
			got, want = append(got, r.Body), append(want, refused)
		}
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("refusals (-want +got):\n%s", diff)
	}
}

func TestUpdatesGoToThePublicAddressWithoutATrailingSlash(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	fl := fleetSite(t, f)
	f.EnvStub["DOORCODE_PUBLIC_URL"] = "https://doorcode.example.com/site/"
	send(t, fl.b, "POST", "/api/v1/doors/1/update", "", 200)

	url, _ := lastReceived(t, fl.conns[0])["url"].(string)

	if diff := cmp.Diff("https://doorcode.example.com/site/api/v1/doors/1/firmware/1.5.0?token=", url[:len(url)-43]); diff != "" {
		t.Errorf("url without its token (-want +got):\n%s", diff)
	}
}

func TestAnOfflineDoorIsNotBadgedUnresponsive(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	run(t, a)

	got := []any{
		testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/state", "", 200).Body, "/unresponsive"),
		pageOf(b.Get("/admin/doors")).Tables["Doors"][1][2],
	}

	if diff := cmp.Diff([]any{false, "Offline"}, got); diff != "" {
		t.Errorf("unresponsive, door list status (-want +got):\n%s", diff)
	}
}

func TestARevokedDoorIsNoLongerUnresponsive(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	run(t, a)
	unresponsive := testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/state", "", 200).Body, "/unresponsive")

	send(t, b, "POST", "/api/v1/doors/1/credentials/revoke", "", 204)

	got := []any{unresponsive, testsupport.JSONAt(send(t, b, "GET", "/api/v1/doors/1/state", "", 200).Body, "/unresponsive")}
	if diff := cmp.Diff([]any{true, false}, got); diff != "" {
		t.Errorf("unresponsive before and after revoking (-want +got):\n%s", diff)
	}
}
