package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

const metricsType = "text/plain; version=0.0.4; charset=utf-8"

// scrapeAs scrapes the metrics of h with a new token of b's user and
// returns the body.
func scrapeAs(t *testing.T, h testsupport.Handler, b *testsupport.Browser) string {
	t.Helper()
	return send(t, tokenClient(h, createToken(t, b, `{"name":"prometheus"}`)), "GET", "/metrics", "", 200).Body
}

// withoutHTTP is a scrape with the HTTP request families left out of its
// body, since every request a test makes shows in them.
func withoutHTTP(r testsupport.Response) testsupport.Response {
	r.Body = testsupport.WithoutFamilies(r.Body, "doorcode_http_")
	return r
}

// siteSamples is the samples of a scrape's body but those of the HTTP
// request families.
func siteSamples(body string) []string {
	return testsupport.Samples(testsupport.WithoutFamilies(body, "doorcode_http_"), "doorcode_")
}

// metricFamilies are every family a scrape shows, in order: name, help,
// and type.
var metricFamilies = [][3]string{
	{"doorcode_door_online", "1 while the door's controller is connected to the broker, else 0.", "gauge"},
	{"doorcode_door_unresponsive", "1 while the door's controller is connected but has not been heard from for the unresponsive_minutes setting, else 0.", "gauge"},
	{"doorcode_door_last_seen_timestamp_seconds", "When the server last heard from the door's controller, in Unix seconds.", "gauge"},
	{"doorcode_door_open", "1 while the door is open, as its controller last said, else 0.", "gauge"},
	{"doorcode_door_held_open", "1 while the door has been open longer than its held_open_s while it should be locked, else 0.", "gauge"},
	{"doorcode_door_config_mismatch", "1 while the door's controller runs a configuration other than the one it was last sent, else 0.", "gauge"},
	{"doorcode_door_update_pending", "1 while the door takes part in the firmware rollout and does not run its target yet, else 0.", "gauge"},
	{"doorcode_door_firmware_info", "The firmware version the door's controller runs, in its version label; always 1.", "gauge"},
	{"doorcode_door_signal_dbm", "The Wi-Fi signal strength the door's controller last reported, in dBm.", "gauge"},
	{"doorcode_door_latency_seconds", "The round trip to the server the door's controller last reported, in seconds.", "gauge"},
	{"doorcode_door_free_bytes", "The free memory the door's controller last reported, in bytes.", "gauge"},
	{"doorcode_door_uptime_seconds", "How long the door's controller had run when it last reported, in seconds.", "gauge"},
	{"doorcode_door_mesh_layer", "The mesh layer the door's controller last reported, 0 when it reaches the access point itself.", "gauge"},
	{"doorcode_door_mesh_children", "How many controllers hopped through the door's controller when it last reported.", "gauge"},
	{"doorcode_door_wifi_channel", "The Wi-Fi channel of the access point the door's controller last reported, 0 with no link.", "gauge"},
	{"doorcode_door_opens_total", "How often the door was opened since its controller started.", "counter"},
	{"doorcode_door_reconnects_total", "How often the door's controller connected to the broker again since it started.", "counter"},
	{"doorcode_door_checks_sent_total", "The code checks the door's controller sent since it started.", "counter"},
	{"doorcode_door_checks_lost_total", "The code checks whose reply never reached the door's controller since it started.", "counter"},
	{"doorcode_door_tcp_segments_total", "The TCP segments the door's controller sent for the first time since it started.", "counter"},
	{"doorcode_door_tcp_retransmits_total", "The TCP segments the door's controller sent again since it started.", "counter"},
	{"doorcode_door_tcp_drops_total", "The TCP segments the door's controller dropped since it started.", "counter"},
	{"doorcode_door_wifi_drops_total", "How often the door's controller lost its Wi-Fi access point since it started.", "counter"},
	{"doorcode_door_roams_total", "How often the door's controller moved to another access point by its own choice since it started.", "counter"},
	{"doorcode_events_total", "The events recorded since the server started, by door, type, outcome, reason, and the reason a controller gave for starting.", "counter"},
	{"doorcode_pending_enrollments", "Controllers waiting to be approved as doors.", "gauge"},
	{"doorcode_open_requests", "Access requests waiting for an answer.", "gauge"},
	{"doorcode_firmware_updates", "Doors taking part in the firmware rollout, by their state in it: pending, updating, failed, or done.", "gauge"},
	{"doorcode_broker_connected", "1 while the server is connected to the broker, else 0.", "gauge"},
	{"doorcode_store_up", "1 when the store answered the scrape's reads, else 0.", "gauge"},
}

// doorlessScrape is the whole of a scrape of a site with no door,
// enrollment, or request: every family's HELP and TYPE, nothing waiting,
// no firmware update in any state unless the store failed, and the broker
// and store gauges saying broker and store.
func doorlessScrape(broker, store string) testsupport.Response {
	body := ""
	for _, f := range metricFamilies {
		body += "# HELP " + f[0] + " " + f[1] + "\n# TYPE " + f[0] + " " + f[2] + "\n"
		switch f[0] {
		case "doorcode_pending_enrollments", "doorcode_open_requests":
			body += f[0] + " 0\n"
		case "doorcode_firmware_updates":
			if store == "1" {
				body += noUpdates
			}
		case "doorcode_broker_connected":
			body += f[0] + " " + broker + "\n"
		case "doorcode_store_up":
			body += f[0] + " " + store + "\n"
		}
	}
	return testsupport.Response{Status: 200, ContentType: metricsType, Body: body}
}

// noUpdates is the firmware update samples of a site where no door takes
// part in the rollout.
const noUpdates = `doorcode_firmware_updates{state="pending"} 0
doorcode_firmware_updates{state="updating"} 0
doorcode_firmware_updates{state="failed"} 0
doorcode_firmware_updates{state="done"} 0
`

func TestTheMetricsNeedATokenWhoseUserHoldsViewMetrics(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, admin := adminSession(t, f)
	createUser(t, admin, ann)
	annClient := userToken(t, h, admin, "2")
	refusedWithoutToken := testsupport.Get(h, "/metrics")
	refusedWithoutRole := annClient.Get("/metrics")
	createGroup(t, admin, `{"slug":"monitoring","name":"Monitoring","roles":["view-metrics"]}`)
	join(t, admin, "2", "2")

	got := []testsupport.Response{refusedWithoutToken, refusedWithoutRole, withoutHTTP(annClient.Get("/metrics"))}

	want := []testsupport.Response{
		apiError(401, "a valid API token is required"),
		apiError(403, "the view-metrics role is required"),
		doorlessScrape("1", "1"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("without a token, without the role, holding it (-want +got):\n%s", diff)
	}
}

func TestAScrapeShowsADoorsStateItsLatestReportAndItsTotals(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(with(totals(3, 7, 40, 2, 900, 12, 4, 5, 1), map[string]any{"mesh_layer": 2, "mesh_children": 1, "channel": 6}))

	got := siteSamples(scrapeAs(t, k.a.Handler(), k.b))

	want := []string{
		`doorcode_door_online{door="Lobby",door_id="1"} 1`,
		`doorcode_door_unresponsive{door="Lobby",door_id="1"} 0`,
		`doorcode_door_last_seen_timestamp_seconds{door="Lobby",door_id="1"} 1767603600`,
		`doorcode_door_open{door="Lobby",door_id="1"} 0`,
		`doorcode_door_held_open{door="Lobby",door_id="1"} 0`,
		`doorcode_door_config_mismatch{door="Lobby",door_id="1"} 0`,
		`doorcode_door_update_pending{door="Lobby",door_id="1"} 0`,
		`doorcode_door_firmware_info{door="Lobby",door_id="1",version="1.4.3"} 1`,
		`doorcode_door_signal_dbm{door="Lobby",door_id="1"} -61`,
		`doorcode_door_latency_seconds{door="Lobby",door_id="1"} 0.04`,
		`doorcode_door_free_bytes{door="Lobby",door_id="1"} 120832`,
		`doorcode_door_uptime_seconds{door="Lobby",door_id="1"} 3720`,
		`doorcode_door_mesh_layer{door="Lobby",door_id="1"} 2`,
		`doorcode_door_mesh_children{door="Lobby",door_id="1"} 1`,
		`doorcode_door_wifi_channel{door="Lobby",door_id="1"} 6`,
		`doorcode_door_opens_total{door="Lobby",door_id="1"} 7`,
		`doorcode_door_reconnects_total{door="Lobby",door_id="1"} 3`,
		`doorcode_door_checks_sent_total{door="Lobby",door_id="1"} 40`,
		`doorcode_door_checks_lost_total{door="Lobby",door_id="1"} 2`,
		`doorcode_door_tcp_segments_total{door="Lobby",door_id="1"} 900`,
		`doorcode_door_tcp_retransmits_total{door="Lobby",door_id="1"} 12`,
		`doorcode_door_tcp_drops_total{door="Lobby",door_id="1"} 4`,
		`doorcode_door_wifi_drops_total{door="Lobby",door_id="1"} 5`,
		`doorcode_door_roams_total{door="Lobby",door_id="1"} 1`,
		`doorcode_pending_enrollments 0`,
		`doorcode_open_requests 0`,
		`doorcode_firmware_updates{state="pending"} 0`,
		`doorcode_firmware_updates{state="updating"} 0`,
		`doorcode_firmware_updates{state="failed"} 0`,
		`doorcode_firmware_updates{state="done"} 0`,
		`doorcode_broker_connected 1`,
		`doorcode_store_up 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestAnOfflineDoorKeepsItsStateAndTotalsButNotWhatItLastReported(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.report(totals(3, 7, 40, 2, 900, 12, 4, 5, 1))
	k.at("2026-01-05T09:05:00Z")
	k.conn.Drop()
	run(t, k.a)

	got := siteSamples(scrapeAs(t, k.a.Handler(), k.b))

	want := []string{
		`doorcode_door_online{door="Lobby",door_id="1"} 0`,
		`doorcode_door_unresponsive{door="Lobby",door_id="1"} 0`,
		`doorcode_door_last_seen_timestamp_seconds{door="Lobby",door_id="1"} 1767603900`,
		`doorcode_door_open{door="Lobby",door_id="1"} 0`,
		`doorcode_door_held_open{door="Lobby",door_id="1"} 0`,
		`doorcode_door_config_mismatch{door="Lobby",door_id="1"} 0`,
		`doorcode_door_update_pending{door="Lobby",door_id="1"} 0`,
		`doorcode_door_firmware_info{door="Lobby",door_id="1",version="1.4.3"} 1`,
		`doorcode_door_opens_total{door="Lobby",door_id="1"} 7`,
		`doorcode_door_reconnects_total{door="Lobby",door_id="1"} 3`,
		`doorcode_door_checks_sent_total{door="Lobby",door_id="1"} 40`,
		`doorcode_door_checks_lost_total{door="Lobby",door_id="1"} 2`,
		`doorcode_door_tcp_segments_total{door="Lobby",door_id="1"} 900`,
		`doorcode_door_tcp_retransmits_total{door="Lobby",door_id="1"} 12`,
		`doorcode_door_tcp_drops_total{door="Lobby",door_id="1"} 4`,
		`doorcode_door_wifi_drops_total{door="Lobby",door_id="1"} 5`,
		`doorcode_door_roams_total{door="Lobby",door_id="1"} 1`,
		`doorcode_pending_enrollments 0`,
		`doorcode_open_requests 0`,
		`doorcode_firmware_updates{state="pending"} 0`,
		`doorcode_firmware_updates{state="updating"} 0`,
		`doorcode_firmware_updates{state="failed"} 0`,
		`doorcode_firmware_updates{state="done"} 0`,
		`doorcode_broker_connected 1`,
		`doorcode_store_up 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestADoorsGaugesShowItOpenHeldOpenUnresponsiveAndRunningAnotherConfiguration(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.givesUp("2026-01-05T09:01:00Z", k.sentDigest())
	k.at("2026-01-05T09:02:00Z")
	k.opens(false)
	k.sweepAt("2026-01-05T09:08:00Z")

	got := testsupport.Samples(scrapeAs(t, k.a.Handler(), k.b), "doorcode_door_")

	want := []string{
		`doorcode_door_online{door="Lobby",door_id="1"} 1`,
		`doorcode_door_unresponsive{door="Lobby",door_id="1"} 1`,
		`doorcode_door_last_seen_timestamp_seconds{door="Lobby",door_id="1"} 1767603720`,
		`doorcode_door_open{door="Lobby",door_id="1"} 1`,
		`doorcode_door_held_open{door="Lobby",door_id="1"} 1`,
		`doorcode_door_config_mismatch{door="Lobby",door_id="1"} 1`,
		`doorcode_door_update_pending{door="Lobby",door_id="1"} 0`,
		`doorcode_door_firmware_info{door="Lobby",door_id="1",version="1.4.3"} 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestADoorBehindItsTargetFirmwareShowsAnUpdatePending(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.reconnects(2, "1.5.0")

	got := testsupport.Samples(send(t, fl.b, "GET", "/metrics", "", 200).Body, "doorcode_door_update_pending")

	want := []string{
		`doorcode_door_update_pending{door="Lobby",door_id="1"} 1`,
		`doorcode_door_update_pending{door="Dock",door_id="2"} 0`,
		`doorcode_door_update_pending{door="Attic",door_id="3"} 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestOnlyDoorsThatAreNotDeletedAndHaveAControllerHaveSeries(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	for _, name := range []string{"Lobby", "Cellar", "Attic", "Dock"} {
		createDoor(t, b, `{"name":"`+name+`"}`)
	}
	for _, enrolled := range [][2]string{{"A1", "1"}, {"C3", "3"}, {"D4", "4"}} {
		enroll(t, f, a, b, enrolled[0], enrolled[1])
	}
	send(t, b, "DELETE", "/api/v1/doors/4", "", 204)

	got := testsupport.Samples(scrapeAs(t, a.Handler(), b), "doorcode_door_online")

	want := []string{
		`doorcode_door_online{door="Lobby",door_id="1"} 1`,
		`doorcode_door_online{door="Attic",door_id="3"} 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestADoorsNameIsEscapedInItsLabel(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	send(t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Lobby \"North\" \\ Wing\nEast"}`, 200)

	got := testsupport.Samples(scrapeAs(t, k.a.Handler(), k.b), "doorcode_door_online")

	want := []string{`doorcode_door_online{door="Lobby \"North\" \\ Wing\nEast",door_id="1"} 1`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestWhenTheStoreFailsAScrapeSaysSoWithoutDoorsAndLogsIt(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	c := tokenClient(k.a.Handler(), createToken(t, k.b, `{"name":"prometheus"}`))
	k.press("999999")
	k.f.StoreStub.FailDoors()

	got := []any{withoutHTTP(c.Get("/metrics")), lastLog(k.f)}

	want := []any{
		doorlessScrape("1", "0"),
		testsupport.LogEntry{Level: "error", Message: "could not read the doors for metrics", Fields: map[string]string{"error": "stub: unavailable"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scrape, log (-want +got):\n%s", diff)
	}
}

func TestAScrapeSaysWhenTheBrokerIsNotConnected(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	f.BrokerStub.Disconnect()

	got := withoutHTTP(tokenClient(h, secret).Get("/metrics"))

	if diff := cmp.Diff(doorlessScrape("0", "1"), got); diff != "" {
		t.Errorf("scrape (-want +got):\n%s", diff)
	}
}

func TestAScrapeCountsTheEnrollmentsAndRequestsWaiting(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	c := tokenClient(a.Handler(), createToken(t, b, `{"name":"prometheus"}`))
	waitingNow := func() []string {
		body := send(t, c, "GET", "/metrics", "", 200).Body
		return append(testsupport.Samples(body, "doorcode_pending_enrollments"), testsupport.Samples(body, "doorcode_open_requests")...)
	}
	before := waitingNow()
	createDoor(t, b, `{"name":"Lobby"}`)
	announce(t, f, "A1")
	announce(t, f, "B2")
	run(t, a)
	send(t, b, "POST", "/api/v1/me/requests", labAccess, 201)
	waited := waitingNow()
	send(t, b, "POST", "/api/v1/enrollments/A1/approve", `{"door_id":1}`, 200)
	send(t, b, "POST", "/api/v1/requests/1/resolve", "", 200)

	got := [][]string{before, waited, waitingNow()}

	want := [][]string{
		{`doorcode_pending_enrollments 0`, `doorcode_open_requests 0`},
		{`doorcode_pending_enrollments 2`, `doorcode_open_requests 1`},
		{`doorcode_pending_enrollments 1`, `doorcode_open_requests 0`},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("before, while two controllers and a request wait, after approving one and resolving it (-want +got):\n%s", diff)
	}
}

func TestAScrapeCountsTheDoorsInTheRolloutByState(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	updates := func() []string {
		return testsupport.Samples(send(t, fl.b, "GET", "/metrics", "", 200).Body, "doorcode_firmware_updates")
	}
	before := updates()
	fl.at("2026-01-06T02:00:00Z")
	sent := updates()
	fl.reconnects(1, "1.5.0")
	fl.acks(2, "failed", "flash error")

	got := [][]string{before, sent, updates()}

	want := [][]string{
		{
			`doorcode_firmware_updates{state="pending"} 3`, `doorcode_firmware_updates{state="updating"} 0`,
			`doorcode_firmware_updates{state="failed"} 0`, `doorcode_firmware_updates{state="done"} 0`,
		},
		{
			`doorcode_firmware_updates{state="pending"} 1`, `doorcode_firmware_updates{state="updating"} 2`,
			`doorcode_firmware_updates{state="failed"} 0`, `doorcode_firmware_updates{state="done"} 0`,
		},
		{
			`doorcode_firmware_updates{state="pending"} 1`, `doorcode_firmware_updates{state="updating"} 0`,
			`doorcode_firmware_updates{state="failed"} 1`, `doorcode_firmware_updates{state="done"} 1`,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("before the window, once it opens, after one door updates and one fails (-want +got):\n%s", diff)
	}
}

func TestWhenTheRequestsCannotBeReadAScrapeSaysSoWithoutTheirCountAndLogsIt(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	f.StoreStub.FailRequests()

	body := tokenClient(h, secret).Get("/metrics").Body
	got := []any{
		siteSamples(body),
		testsupport.Samples(body, "# TYPE doorcode_open_requests"),
		lastLog(f),
	}

	want := []any{
		[]string{
			`doorcode_pending_enrollments 0`,
			`doorcode_firmware_updates{state="pending"} 0`, `doorcode_firmware_updates{state="updating"} 0`,
			`doorcode_firmware_updates{state="failed"} 0`, `doorcode_firmware_updates{state="done"} 0`,
			`doorcode_broker_connected 1`, `doorcode_store_up 0`,
		},
		[]string{"# TYPE doorcode_open_requests gauge"},
		testsupport.LogEntry{Level: "error", Message: "could not read the access requests for metrics", Fields: map[string]string{"error": "stub: unavailable"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples, the family's type, log (-want +got):\n%s", diff)
	}
}

// eventsScrape is the event counts a scrape of a with a new token of b's
// user shows.
func eventsScrape(t *testing.T, a *app.App, b *testsupport.Browser) []string {
	t.Helper()
	return testsupport.Samples(scrapeAs(t, a.Handler(), b), "doorcode_events_total")
}

func TestAScrapeCountsTheChecksAlarmsAndStartsAtADoor(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	k.press("111111")
	k.press("999999")
	k.press("999999")
	k.starts("panic")
	k.opens(false)

	got := eventsScrape(t, k.a, k.b)

	want := []string{
		`doorcode_events_total{door="Lobby",door_id="1",type="alarm",outcome="alert",reason="forced open",start_reason=""} 1`,
		`doorcode_events_total{door="Lobby",door_id="1",type="check",outcome="allowed",reason="access rule",start_reason=""} 1`,
		`doorcode_events_total{door="Lobby",door_id="1",type="check",outcome="denied",reason="no rule",start_reason=""} 2`,
		`doorcode_events_total{door="Lobby",door_id="1",type="door",outcome="info",reason="opened",start_reason=""} 1`,
		`doorcode_events_total{door="Lobby",door_id="1",type="door",outcome="info",reason="started",start_reason="panic"} 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestAScrapeCountsTheFirmwareUpdatesSentAndHowTheyEnded(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.at("2026-01-06T02:00:00Z")
	fl.reconnects(1, "1.5.0")
	fl.acks(2, "failed", "flash error")

	got := eventsScrape(t, fl.a, fl.b)

	want := []string{
		`doorcode_events_total{door="Lobby",door_id="1",type="update",outcome="info",reason="update sent",start_reason=""} 1`,
		`doorcode_events_total{door="Lobby",door_id="1",type="update",outcome="info",reason="update succeeded",start_reason=""} 1`,
		`doorcode_events_total{door="Dock",door_id="2",type="update",outcome="alert",reason="update failed",start_reason=""} 1`,
		`doorcode_events_total{door="Dock",door_id="2",type="update",outcome="info",reason="update sent",start_reason=""} 1`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestADoorsEventCountsCarryItsCurrentNameAndStayOnceItIsDeleted(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.press("999999")
	send(t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Foyer"}`, 200)
	renamed := eventsScrape(t, k.a, k.b)
	send(t, k.b, "DELETE", "/api/v1/doors/1", "", 204)

	got := [][]string{renamed, eventsScrape(t, k.a, k.b)}

	count := `doorcode_events_total{door="Foyer",door_id="1",type="check",outcome="denied",reason="no rule",start_reason=""} 1`
	if diff := cmp.Diff([][]string{{count}, {count}}, got); diff != "" {
		t.Errorf("renamed, then deleted (-want +got):\n%s", diff)
	}
}

func TestANewlyStartedServerCountsEventsFromZero(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.press("999999")
	k.press("999999")
	restarted := start(t, k.f)
	k.a = restarted
	k.press("999999")

	got := eventsScrape(t, restarted, k.b)

	want := []string{`doorcode_events_total{door="Lobby",door_id="1",type="check",outcome="denied",reason="no rule",start_reason=""} 1`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestEventsRecordedInEveryLaneAtOnceAreAllCounted(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	for i := range 8 {
		for _, door := range []string{"1", "2", "3"} {
			f.BrokerStub.Inject(doorTopic(door, "check"), checkAs("r"+testsupport.Itoa(i)))
		}
	}
	run(t, a)

	got := eventsScrape(t, a, b)

	want := []string{
		`doorcode_events_total{door="Lobby",door_id="1",type="check",outcome="denied",reason="no rule",start_reason=""} 8`,
		`doorcode_events_total{door="Dock",door_id="2",type="check",outcome="denied",reason="no rule",start_reason=""} 8`,
		`doorcode_events_total{door="Attic",door_id="3",type="check",outcome="denied",reason="no rule",start_reason=""} 8`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}
