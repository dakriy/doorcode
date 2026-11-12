package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// enroll has the controller hwid wait, approves it as the door whose id
// is door, connects it with the credentials it receives, and returns its
// connection.
func enroll(t *testing.T, f *testsupport.Factory, a *app.App, b *testsupport.Browser, hwid, door string) *testsupport.Connection {
	t.Helper()
	conn, _ := enrollWith(t, f, a, b, hwid, door)
	return conn
}

// enrollWith is enroll, returning the credentials the controller
// connected with as well.
func enrollWith(t *testing.T, f *testsupport.Factory, a *app.App, b *testsupport.Browser, hwid, door string) (*testsupport.Connection, doorCredentials) {
	t.Helper()
	enrolling := announce(t, f, hwid)
	run(t, a)
	send(t, b, "POST", "/api/v1/enrollments/"+hwid+"/approve", `{"door_id":`+door+`}`, 200)
	creds := credentialsOf(t, enrolling)
	enrolling.Close()
	conn := connectAs(t, f, creds)
	run(t, a)
	return conn, creds
}

// pushSite is a broker site with what ruleSite has, where the controllers
// A1, B2, and C3 are enrolled as Lobby (1), Dock (2), and Attic (3) and
// connected.
func pushSite(t *testing.T, f *testsupport.Factory) (*app.App, *testsupport.Browser) {
	t.Helper()
	a, b := brokerSite(t, f)
	furnishRuleSite(t, furnishDoorSite(t, b))
	for i, hwid := range []string{"A1", "B2", "C3"} {
		enroll(t, f, a, b, hwid, testsupport.Itoa(i+1))
	}
	return a, b
}

// doorTopic is the topic of the door whose id is id that ends in leaf.
func doorTopic(id, leaf string) string {
	return "doorcode/doors/" + id + "/" + leaf
}

// topicsOf is the topic of the doors whose ids are ids that ends in leaf,
// in order.
func topicsOf(leaf string, ids ...string) []string {
	out := []string{}
	for _, id := range ids {
		out = append(out, doorTopic(id, leaf))
	}
	return out
}

// configWith is the configuration message of a door that follows the
// site's defaults, with the site's firmware signing key, but for the
// fields changed names.
func configWith(changed map[string]any) map[string]any {
	return with(map[string]any{
		"v": int64(1), "strike_pulse_ms": int64(3000), "keypad_idle_clear_ms": int64(10000), "min_code_length": int64(4),
		"max_code_length": int64(32), "reply_timeout_ms": int64(3000), "telemetry_interval_s": int64(60),
		"held_open_s": int64(60), "timeline_refresh_hours": int64(6), "credential_rotation_days": int64(30), "rate_limit_per_minute": int64(30),
		"rate_limit_burst": int64(10), "ntp_server": "pool.ntp.org", "broker_host": "", "broker_port": int64(8883), "broker_tls": true,
		"broker_ca_pem": "", "wifi_ssid": "", "wifi_password": "", "wifi_roam_rssi": int64(-70), "wifi_11b_disabled": false,
		"keepalive_s": int64(30), "reconnect_max_s": int64(60), "mesh_enabled": false, "mesh_ssid": "", "mesh_password": "",
		"mesh_min_router_rssi": int64(-75), "mesh_max_layer": int64(4), "update_connect_timeout_s": int64(300), "accept_unsigned_firmware": false, "log_level": "info",
		"firmware_signing_key": signingPublicKey,
	}, changed)
}

// span is a span of a timeline message.
func span(start, end int64, state string) map[string]any {
	return map[string]any{"start": start, "end": end, "state": state}
}

// timelineOf is a timeline message from from up to to with spans.
func timelineOf(from, to int64, spans ...any) map[string]any {
	if spans == nil {
		spans = []any{}
	}
	return map[string]any{"v": int64(1), "from": from, "to": to, "spans": spans}
}

// Unix seconds of the times the timeline tests use: Monday 5 January 2026
// at 09:00 (the stub clock's time), 10:00, 12:00, 15:00, and 18:00 UTC,
// and Tuesday at 08:00, 10:00, and 16:00.
const (
	mon0900 = 1767603600
	mon1000 = 1767607200
	mon1200 = 1767614400
	mon1500 = 1767625200
	mon1800 = 1767636000
	tue0800 = 1767686400
	tue1000 = 1767693600
	tue1600 = 1767715200
)

func TestADoorThatComesOnlineIsSentItsConfigurationAndNext25HoursRetained(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	furnishDoorSite(t, b)
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","open_schedule_ids":[2]}`, 200)
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)
	createOverride(t, b, "1", lockedToday)

	enroll(t, f, a, b, "A1", "1")

	config, _ := f.BrokerStub.Retained(doorTopic("1", "config"))
	want := []any{configWith(nil), true, timelineOf(mon0900, tue1000,
		span(mon0900, mon1000, "open"), span(mon1000, mon1200, "locked"), span(mon1200, mon1800, "open"), span(tue0800, tue1000, "open"))}
	got := []any{f.BrokerStub.RetainedDecoded(doorTopic("1", "config")), config.Retained, f.BrokerStub.RetainedDecoded(doorTopic("1", "timeline"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("config, retained, timeline (-want +got):\n%s", diff)
	}
}

func TestChangingAnOverrideSendsANewTimelineToItsDoorAndNoOther(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	createOverride(t, b, "2", lockedToday)
	sent := len(f.BrokerStub.Published)

	send(t, b, "PUT", "/api/v1/doors/2/overrides/1", `{"kind":"locked","start":"2026-01-05T15:00:00Z","end":"2026-01-06T08:00:00Z"}`, 200)

	want := []any{topicsOf("timeline", "2"), timelineOf(mon0900, tue1000, span(mon1500, tue0800, "locked"))}
	got := []any{f.BrokerStub.Topics(sent, "timeline"), f.BrokerStub.RetainedDecoded(doorTopic("2", "timeline"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("timelines sent, Dock's timeline (-want +got):\n%s", diff)
	}
}

func TestAChangeSendsNewTimelinesToTheDoorsWhoseTimelinesDependOnIt(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		method, target, body string
		status               int
		want                 []string
	}{
		"editing a schedule open mode and a rule use": {"PUT", "/api/v1/schedules/3", `{"name":"Nights","windows":[{"days":["mon"],"start":"21:00","end":"06:00"}]}`, 200, []string{"4"}},
		"creating an override":                        {"POST", "/api/v1/doors/3/overrides", lockedToday, 201, []string{"3"}},
		"editing an override":                         {"PUT", "/api/v1/doors/2/overrides/1", lockedToday, 200, []string{"2"}},
		"cancelling an override":                      {"POST", "/api/v1/doors/1/overrides/2/cancel", "", 200, []string{"1"}},
		"deleting an override":                        {"DELETE", "/api/v1/doors/2/overrides/1", "", 204, []string{"2"}},
		"changing a door's zones":                     {"PUT", "/api/v1/doors/1", `{"name":"Lobby","zone_ids":[1,3]}`, 200, []string{"1"}},
		"giving a zone open mode":                     {"PUT", "/api/v1/zones/2", `{"name":"Annex","door_ids":[2,4],"open_schedule_ids":[2]}`, 200, []string{"2", "4"}},
		"taking a door out of an open-mode zone":      {"PUT", "/api/v1/zones/3", `{"name":"Nightly","door_ids":[5],"open_schedule_ids":[3]}`, 200, []string{"4"}},
		"creating a zone with open mode":              {"POST", "/api/v1/zones", `{"name":"Porch","door_ids":[1,3],"open_schedule_ids":[1]}`, 201, []string{"1", "3"}},
		"deleting an open-mode zone":                  {"DELETE", "/api/v1/zones/3", "", 204, []string{"4"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			a, b := pushSite(t, f)
			createZone(t, b, `{"name":"Nightly","open_schedule_ids":[3]}`)
			createDoor(t, b, `{"name":"Gate","zone_ids":[2,3]}`)
			createDoor(t, b, `{"name":"Shed","zone_ids":[3]}`)
			enroll(t, f, a, b, "D4", "4")
			enroll(t, f, a, b, "E5", "5")
			send(t, b, "DELETE", "/api/v1/doors/5", "", 204)
			createRule(t, b, `{"group_id":2,"schedule_id":3,"zone_ids":[2]}`)
			createOverride(t, b, "2", lockedToday)
			createOverride(t, b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T11:00:00Z"}`)
			sent := len(f.BrokerStub.Published)

			send(t, b, c.method, c.target, c.body, c.status)

			if diff := cmp.Diff(topicsOf("timeline", c.want...), f.BrokerStub.Topics(sent, "timeline")); diff != "" {
				t.Errorf("timelines sent (-want +got):\n%s", diff)
			}
		})
	}
}

func TestChangingADoorsConfigurationSendsItItsConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	sent := len(f.BrokerStub.Published)

	send(t, b, "PUT", "/api/v1/doors/2/config", `{"overrides":{"strike_pulse_ms":"500","log_level":"debug"}}`, 200)

	want := []any{topicsOf("config", "2"), configWith(map[string]any{"strike_pulse_ms": int64(500), "log_level": "debug"}), configWith(nil)}
	got := []any{f.BrokerStub.Topics(sent, "config"), f.BrokerStub.RetainedDecoded(doorTopic("2", "config")),
		f.BrokerStub.RetainedDecoded(doorTopic("1", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configurations sent, Dock's, Lobby's (-want +got):\n%s", diff)
	}
}

func TestADoorThatReconnectsIsSentItsConfigurationAndTimelineWithoutReadingEveryDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	furnishDoorSite(t, b)
	createDoor(t, b, `{"name":"Lobby","zone_ids":[1]}`)
	createDoor(t, b, `{"name":"Dock","zone_ids":[1]}`)
	conn, creds := enrollWith(t, f, a, b, "B2", "2")
	sent, reads := len(f.BrokerStub.Published), f.StoreStub.DoorsCalls()

	if err := conn.Publish(doorTopic("2", "online"), onlineAs(creds.username, true), true); err != nil {
		t.Fatal(err)
	}
	run(t, a)

	want := []any{topicsOf("config", "2"), topicsOf("timeline", "2"), reads}
	got := []any{f.BrokerStub.Topics(sent, "config"), f.BrokerStub.Topics(sent, "timeline"), f.StoreStub.DoorsCalls()}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configurations sent, timelines sent, reads of every door (-want +got):\n%s", diff)
	}
}

func TestChangingADoorsConfigurationSendsItWithoutReadingEveryDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	sent, reads := len(f.BrokerStub.Published), f.StoreStub.DoorsCalls()

	send(t, b, "PUT", "/api/v1/doors/3/config", `{"overrides":{"log_level":"debug"}}`, 200)

	want := []any{topicsOf("config", "3"), reads}
	got := []any{f.BrokerStub.Topics(sent, "config"), f.StoreStub.DoorsCalls()}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configurations sent, reads of every door (-want +got):\n%s", diff)
	}
}

func TestADoorsConfigurationTellsItWhichBrokerToConnectTo(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	send(t, b, "PUT", "/api/v1/settings/door_broker_host", `{"value":"mqtt.example.com"}`, 200)

	send(t, b, "PUT", "/api/v1/doors/2/config", `{"overrides":{"broker_host":"2001:db8::10"}}`, 200)

	want := []any{configWith(map[string]any{"broker_host": "mqtt.example.com"}), configWith(map[string]any{"broker_host": "2001:db8::10"})}
	got := []any{f.BrokerStub.RetainedDecoded(doorTopic("1", "config")), f.BrokerStub.RetainedDecoded(doorTopic("2", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lobby's, Dock's configuration (-want +got):\n%s", diff)
	}
}

func TestADoorsConfigurationTellsItWhetherToKeepOffThe80211bRates(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	send(t, b, "PUT", "/api/v1/settings/door_wifi_11b_disabled", `{"value":"true"}`, 200)

	send(t, b, "PUT", "/api/v1/doors/2/config", `{"overrides":{"wifi_11b_disabled":"false"}}`, 200)

	want := []any{configWith(map[string]any{"wifi_11b_disabled": true}), configWith(nil)}
	got := []any{f.BrokerStub.RetainedDecoded(doorTopic("1", "config")), f.BrokerStub.RetainedDecoded(doorTopic("2", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lobby's, Dock's configuration (-want +got):\n%s", diff)
	}
}

func TestChangingAConfigurationDefaultSendsEveryEnrolledDoorItsConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	createDoor(t, b, `{"name":"Gate"}`)
	send(t, b, "PUT", "/api/v1/doors/3/config", `{"overrides":{"log_level":"error"}}`, 200)
	sent := len(f.BrokerStub.Published)

	send(t, b, "PUT", "/api/v1/settings/door_log_level", `{"value":"debug"}`, 200)

	want := []any{topicsOf("config", "3", "2", "1"), configWith(map[string]any{"log_level": "debug"}), configWith(map[string]any{"log_level": "error"})}
	got := []any{f.BrokerStub.Topics(sent, "config"), f.BrokerStub.RetainedDecoded(doorTopic("1", "config")),
		f.BrokerStub.RetainedDecoded(doorTopic("3", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configurations sent, Lobby's, Attic's (-want +got):\n%s", diff)
	}
}

func TestChangingTheSiteTimezoneSendsEveryEnrolledDoorItsTimeline(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	sent := len(f.BrokerStub.Published)

	send(t, b, "PUT", "/api/v1/settings/site_name", `{"value":"North Campus"}`, 200)
	send(t, b, "PUT", "/api/v1/settings/site_timezone", `{"value":"Europe/Berlin"}`, 200)

	var got []string
	for _, m := range f.BrokerStub.Published[sent:] {
		got = append(got, m.Topic)
	}
	if diff := cmp.Diff(topicsOf("timeline", "3", "2", "1"), got); diff != "" {
		t.Errorf("messages sent (-want +got):\n%s", diff)
	}
}

func TestTimelinesAreSentAgainOnceEachDoorsRefreshIntervalHasPassed(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	send(t, b, "PUT", "/api/v1/doors/2/config", `{"overrides":{"timeline_refresh_hours":"12"}}`, 200)
	sent := len(f.BrokerStub.Published)

	f.ClockStub.Set("2026-01-05T14:59:00Z")
	run(t, a)
	f.ClockStub.Set("2026-01-05T15:00:00Z")
	run(t, a)

	want := []any{topicsOf("timeline", "3", "1"), timelineOf(mon1500, tue1600)}
	got := []any{f.BrokerStub.Topics(sent, "timeline"), f.BrokerStub.RetainedDecoded(doorTopic("1", "timeline"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("timelines sent, Lobby's timeline (-want +got):\n%s", diff)
	}
}

func TestAfterARestartEveryEnrolledDoorIsSentItsTimeline(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _, conn, _ := enrolledDoor(t, f)
	conn.Drop()
	run(t, a)
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	sent := len(f.BrokerStub.Published)

	run(t, start(t, f))

	want := []any{topicsOf("timeline", "1"), timelineOf(mon1000, mon1000+25*3600)}
	got := []any{f.BrokerStub.Topics(sent, "timeline"), f.BrokerStub.RetainedDecoded(doorTopic("1", "timeline"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("timelines sent, Lobby's timeline (-want +got):\n%s", diff)
	}
}

func TestDeletingADoorRevokesItsCredentialsAndDeletesItsBrokerClient(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, creds := enrolledDoor(t, f)

	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)

	want := []any{[]string{"revoked"}, false, []bool{false}, []string{enrollUser}}
	got := []any{statuses(t, b, "1"), conn.Connected(), connects(f, creds), f.BrokerStub.Usernames()}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("credentials, connected, connects again, broker clients (-want +got):\n%s", diff)
	}
}

func TestDeletingADoorTakesItsControllerOffItEvenOnceRestored(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())

	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)
	deleted := pageOf(b.Get("/admin/doors/1?tab=controller")).Notes
	send(t, b, "POST", "/api/v1/doors/1/restore", "", 204)
	restored := pageOf(b.Get("/admin/doors/1?tab=controller")).Notes

	want := []any{[]string{deletedDoorNote, notEnrolledNote}, []string{notEnrolledNote}}
	if diff := cmp.Diff(want, []any{deleted, restored}); diff != "" {
		t.Errorf("controller tab notes, deleted, restored (-want +got):\n%s", diff)
	}
}

func TestADeletedDoorsControllerEnrollsAgainAndCanBeApprovedAsAnotherDoor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	createDoor(t, b, `{"name":"Dock"}`)
	send(t, b, "DELETE", "/api/v1/doors/1", "", 204)

	conn, creds := enrollWith(t, f, a, b, "A1", "2")

	got := []any{creds.door, conn.Connected(), testsupport.JSONAt(b.Get("/api/v1/doors/2").Body, "/controller/online")}
	if diff := cmp.Diff([]any{"2", true, true}, got); diff != "" {
		t.Errorf("door, connected, online (-want +got):\n%s", diff)
	}
}

func TestACheckForADeletedDoorIsIgnoredAndLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	send(t, k.b, "DELETE", "/api/v1/doors/1", "", 204)
	logged := len(f.LogStub.Entries)

	f.BrokerStub.Inject("doorcode/doors/1/check", testsupport.CBOR(map[string]any{"v": 1, "request_id": "r1", "digits": "111111"}))
	run(t, k.a)

	want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message",
		Fields: map[string]string{"topic": "doorcode/doors/1/check", "error": "the door is deleted"}}}
	if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestDeletingADoorFromItsPageRevokesItsCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, conn, _ := enrolledDoor(t, f)

	got := b.Post("/admin/doors/1/delete")

	if diff := cmp.Diff([]any{303, []string{"revoked"}, false}, []any{got.Status, statuses(t, b, "1"), conn.Connected()}); diff != "" {
		t.Errorf("status, credentials, connected (-want +got):\n%s", diff)
	}
}

func TestDeletingADoorFailsAndKeepsItWhenItsCredentialsCannotBeRevoked(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b, _, _ := enrolledDoor(t, f)
	f.BrokerStub.FailDeleting()

	got := b.Delete("/api/v1/doors/1")

	want := []any{500, []any{[]any{"Lobby"}, 1.0}}
	if diff := cmp.Diff(want, []any{got.Status, doorNames(t, b, "")}); diff != "" {
		t.Errorf("status, current doors (-want +got):\n%s", diff)
	}
}

func TestPeriodicWorkRemovesExpiredSessions(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _ := brokerSite(t, f)
	f.ClockStub.Set("2026-01-05T12:00:00Z")
	later := signIn(t, f, a.Handler())
	f.ClockStub.Set("2026-01-05T21:00:00Z")

	run(t, a)

	want := []any{1, "Home · doorcode"}
	got := []any{f.StoreStub.Sessions(), pageOf(later.Get("/")).Title}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sessions kept, later session's home page (-want +got):\n%s", diff)
	}
}

func TestADoorsRotationIntervalOverrideRotatesItsCredentials(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"credential_rotation_days":"1"}}`, 200)
	f.ClockStub.Set("2026-01-06T09:00:00Z")

	run(t, a)

	if diff := cmp.Diff([]string{"in-use", "issued"}, statuses(t, signIn(t, f, a.Handler()), "1")); diff != "" {
		t.Errorf("credentials (-want +got):\n%s", diff)
	}
}

func TestAChangeStandsAndIsLoggedWhenDoorsCannotBeSentIt(t *testing.T) {
	t.Parallel()
	failPublishing := func(f *testsupport.Factory, _ *testsupport.Browser) { f.BrokerStub.FailPublishing() }
	cases := map[string]struct {
		fail                 func(f *testsupport.Factory, b *testsupport.Browser)
		method, target, body string
		status               int
	}{
		"an override":             {failPublishing, "POST", "/api/v1/doors/1/overrides", lockedToday, 201},
		"a door":                  {failPublishing, "PUT", "/api/v1/doors/1", `{"name":"Lobby"}`, 200},
		"a door's configuration":  {failPublishing, "PUT", "/api/v1/doors/1/config", `{"overrides":{}}`, 200},
		"a configuration default": {failPublishing, "PUT", "/api/v1/settings/door_log_level", `{"value":"debug"}`, 200},
		"the site timezone":       {failPublishing, "PUT", "/api/v1/settings/site_timezone", `{"value":"Europe/Berlin"}`, 200},
		"zones cannot be read": {func(f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailZones() },
			"PUT", "/api/v1/settings/site_timezone", `{"value":"Europe/Berlin"}`, 200},
		"doors cannot be read": {func(f *testsupport.Factory, b *testsupport.Browser) {
			createSchedule(t, b, `{"name":"Nights","windows":[{"days":["mon"],"start":"22:00","end":"06:00"}]}`)
			f.StoreStub.FailDoors()
		}, "PUT", "/api/v1/schedules/2", `{"name":"Nights","windows":[{"days":["tue"],"start":"22:00","end":"06:00"}]}`, 200},
		"what was sent cannot be kept": {func(f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailSentConfigWrites() },
			"PUT", "/api/v1/doors/1/config", `{"overrides":{"log_level":"debug"}}`, 200},
		"its door cannot be looked up": {func(f *testsupport.Factory, _ *testsupport.Browser) { f.StoreStub.FailDoorLookups() },
			"POST", "/api/v1/zones", `{"name":"Porch","door_ids":[1]}`, 201},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			_, b, _, _ := enrolledDoor(t, f)
			c.fail(f, b)
			logged := len(f.LogStub.Entries)

			got := b.Send(c.method, c.target, c.body)

			want := []any{c.status, []testsupport.LogEntry{{Level: "error", Message: "could not send doors what changed",
				Fields: map[string]string{"error": "stub: unavailable"}}}}
			if diff := cmp.Diff(want, []any{got.Status, logsSince(f, logged)}); diff != "" {
				t.Errorf("status, log (-want +got):\n%s", diff)
			}
		})
	}
}
