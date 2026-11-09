package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// statsSite is the keypad site in Berlin, an hour ahead of UTC in
// January, with a rule letting Staff into Lobby on weekdays and the door
// Dock (2). In Berlin's time, Ann was denied at Lobby at 23:59:59 on
// Sunday 4 January (no rule), then on Monday 5 January an unknown code
// was denied at midnight, Ann let in at 09:30, Cid at 09:40, and Eve
// denied as expired at 09:50. The clock then reads 10:00, and Lobby is
// unresponsive, having said nothing since it connected at 23:00 the day
// before.
func statsSite(t *testing.T, f *testsupport.Factory) *keypad {
	t.Helper()
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Europe/Berlin"
	f.ClockStub.Set("2026-01-04T22:00:00Z")
	k := keypadSite(t, f)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"door_ids":[1]}`)
	createDoor(t, k.b, `{"name":"Dock"}`)
	for _, entry := range []struct{ at, digits string }{
		{"2026-01-04T22:59:59Z", "111111"}, {"2026-01-04T23:00:00Z", "999999"},
		{"2026-01-05T08:30:00Z", "111111"}, {"2026-01-05T08:40:00Z", "222222"}, {"2026-01-05T08:50:00Z", "333333"},
	} {
		f.ClockStub.Set(entry.at)
		k.press(entry.digits)
	}
	f.ClockStub.Set(testsupport.DefaultNow)
	return k
}

// dashboardNav is the first administrator's navigation on the dashboard.
var dashboardNav = []string{
	"Home", "Dashboard (current)", "Users", "Requests", "Groups", "Schedules", "Doors", "Zones", "Firmware", "Events", "Settings", "API tokens", "API docs",
}

// twoDigits is hour written with two digits, as the heatmap's table heads
// its columns.
func twoDigits(hour int) string {
	if hour < 10 {
		return "0" + itoa(hour)
	}
	return itoa(hour)
}

// heatTable is the table of entries by weekday and hour of day, where
// busy names the entries of each weekday's busy hours, such as
// {"Mon": {9: 3}}, and every other hour has none.
func heatTable(busy map[string]map[int]int) [][]string {
	head := []string{"Weekday"}
	for hour := range 24 {
		head = append(head, twoDigits(hour))
	}
	rows := [][]string{append(head, "Total")}
	totals, all := make([]int, 24), 0
	for _, day := range []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"} {
		row, sum := []string{day}, 0
		for hour := range 24 {
			row, sum, totals[hour] = append(row, itoa(busy[day][hour])), sum+busy[day][hour], totals[hour]+busy[day][hour]
		}
		rows, all = append(rows, append(row, itoa(sum))), all+sum
	}
	last := []string{"All days"}
	for _, n := range totals {
		last = append(last, itoa(n))
	}
	return append(rows, append(last, itoa(all)))
}

// The heads of the dashboard's tables.
var (
	perDayHead  = []string{"Day", "Allowed", "Denied", "Entries"}
	reasonsHead = []string{"Reason", "Entries"}
	rankingHead = []string{"Door", "Allowed", "Denied", "Entries"}
	uptimeHead  = []string{"Door", "Uptime", "Reported", "Opens", "Signal", "Latency"}
)

// noDoorsLeft is the least active doors when every door is among the
// most active.
var noDoorsLeft = [][]string{rankingHead, {"Every door is among the most active."}}

func TestTheDashboardSumsUpTodaysEntriesInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())

	page := pageOf(k.b.Get("/admin?range=today"))

	want := []any{
		"Dashboard · doorcode", dashboardNav,
		map[string]string{
			"Open requests": "0", "Doors open": "0", "Unresponsive doors": "1 (needs attention)", "Pending enrollments": "0", "Pending updates": "0",
			"Entries": "4", "Allowed": "2 50% allowed", "Denied": "2", "People": "3",
		},
		map[string][][]string{
			"Entries per day":                    {perDayHead, {"Mon 5 Jan", "2", "2", "4"}},
			"Entries by weekday and hour of day": heatTable(map[string]map[int]int{"Mon": {0: 1, 9: 3}}),
			"Denial reasons":                     {reasonsHead, {"Expired", "1"}, {"No rule", "1"}},
			"Most active doors":                  {rankingHead, {"Lobby", "2", "2", "4"}, {"Dock", "0", "0", "0"}},
			"Least active doors":                 noDoorsLeft,
			"Door uptime":                        {uptimeHead, {"Lobby", "0%", "0 of 10 hours", "0", "Not reported yet", "Not reported yet"}},
			"Firmware versions":                  {{"Version", "Doors"}, {"1.4.3", "1"}},
		},
	}
	if diff := cmp.Diff(want, []any{page.Title, page.Nav, page.Details, page.Tables}); diff != "" {
		t.Errorf("title, navigation, figures, tables (-want +got):\n%s", diff)
	}
}

func TestTheDashboardShowsTheLastSevenDaysUnlessAskedForAnotherRange(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())

	page := pageOf(k.b.Get("/admin"))

	zero := func(day string) []string { return []string{day, "0", "0", "0"} }
	want := []any{
		map[string]string{"Show Today": "/admin?range=today", "Show 30 days": "/admin?range=30d", "Show 90 days": "/admin?range=90d"},
		[][]string{
			perDayHead, zero("Tue 30 Dec"), zero("Wed 31 Dec"), zero("Thu 1 Jan"), zero("Fri 2 Jan"), zero("Sat 3 Jan"),
			{"Sun 4 Jan", "0", "1", "1"}, {"Mon 5 Jan", "2", "2", "4"},
		},
		heatTable(map[string]map[int]int{"Mon": {0: 1, 9: 3}, "Sun": {23: 1}}),
		[][]string{reasonsHead, {"No rule", "2"}, {"Expired", "1"}},
		[]string{"5", "2 40% allowed", "3", "3"},
	}
	got := []any{
		page.LinkTargets, page.Tables["Entries per day"], page.Tables["Entries by weekday and hour of day"], page.Tables["Denial reasons"],
		[]string{page.Details["Entries"], page.Details["Allowed"], page.Details["Denied"], page.Details["People"]},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("range links, per day, by hour, reasons, figures (-want +got):\n%s", diff)
	}
}

func TestACustomRangeEndsWithTheLastDayInTheSiteTimezone(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())

	page := pageOf(k.b.Get("/admin?from=2026-01-04&to=2026-01-04"))

	want := []any{
		[][]string{perDayHead, {"Sun 4 Jan", "0", "1", "1"}},
		map[string]string{"from": "2026-01-04", "to": "2026-01-04"},
		[][]string{rankingHead, {"Lobby", "0", "1", "1"}, {"Dock", "0", "0", "0"}},
	}
	if diff := cmp.Diff(want, []any{page.Tables["Entries per day"], page.Values, page.Tables["Most active doors"]}); diff != "" {
		t.Errorf("per day, the form, doors (-want +got):\n%s", diff)
	}
}

func TestTheEntryStatsAPIAnswersTheDashboardsFigures(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())

	got := send(t, k.b, "GET", "/api/v1/stats/entries?range=today", "", 200).Body

	hours := func(busy map[int]int) string {
		out := ""
		for hour := range 24 {
			if hour > 0 {
				out += ","
			}
			out += itoa(busy[hour])
		}
		return "[" + out + "]"
	}
	quiet := hours(nil)
	weekday := func(day, counts string, total int) string {
		return `{"weekday":"` + day + `","hours":` + counts + `,"total":` + itoa(total) + `}`
	}
	want := testsupport.JSON(`{"range":{"range":"today","from":"2026-01-05","to":"2026-01-05","start":"2026-01-04T23:00:00Z",` +
		`"end":"2026-01-05T23:00:00Z","timezone":"Europe/Berlin"},"total":4,"allowed":2,"denied":2,"users":3,` +
		`"days":[{"date":"2026-01-05","allowed":2,"denied":2}],"weekdays":[` + weekday("mon", hours(map[int]int{0: 1, 9: 3}), 4) + `,` +
		weekday("tue", quiet, 0) + `,` + weekday("wed", quiet, 0) + `,` + weekday("thu", quiet, 0) + `,` + weekday("fri", quiet, 0) + `,` +
		weekday("sat", quiet, 0) + `,` + weekday("sun", quiet, 0) + `],"hours":` + hours(map[int]int{0: 1, 9: 3}) + `,` +
		`"reasons":[{"reason":"expired","count":1},{"reason":"no rule","count":1}],` +
		`"doors":[{"door":{"id":1,"name":"Lobby"},"deleted":false,"allowed":2,"denied":2,"total":4},` +
		`{"door":{"id":2,"name":"Dock"},"deleted":false,"allowed":0,"denied":0,"total":0}]}`)
	if diff := cmp.Diff(want, testsupport.JSONAt(got, "")); diff != "" {
		t.Errorf("stats (-want +got):\n%s", diff)
	}
}

func TestTheStatsAPIRefusesARangeItCannotRead(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	cases := map[string]struct {
		query string
		want  string
	}{
		"an unknown range": {"range=week", "Choose today, 7d, 30d, or 90d."},
		"no last day":      {"from=2026-01-01", "Enter a date, such as 2026-03-01."},
		"no first day":     {"to=2026-01-01", "Enter a date, such as 2026-03-01."},
		"not a date":       {"from=yesterday&to=2026-01-01", "Enter a date as year, month, and day, such as 2026-03-01."},
		"backwards":        {"from=2026-01-02&to=2026-01-01", "Choose a day on or after the first."},
		"too long":         {"from=2025-01-01&to=2026-01-02", "Choose at most 366 days."},
	}
	for name, c := range cases {
		for _, path := range []string{"/api/v1/stats/entries?", "/api/v1/stats/doors?"} {
			t.Run(name+path, func(t *testing.T) {
				t.Parallel()
				got := b.Get(path + c.query)

				want := []any{400, `{"error":"` + c.want + `"}` + "\n"}
				if diff := cmp.Diff(want, []any{got.Status, got.Body}); diff != "" {
					t.Errorf("status, body (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestTheDashboardShowsTheLastSevenDaysWhenItCannotReadTheRange(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())
	k.b.Get("/")

	page := pageOf(k.b.Get("/admin?from=2026-01-04&to=2026-01-01"))

	want := []any{
		[]string{"The range could not be read, so the dashboard shows the last 7 days."},
		map[string]string{"to": "Choose a day on or after the first."},
		map[string]string{"from": "2026-01-04", "to": "2026-01-01"},
		"5",
	}
	if diff := cmp.Diff(want, []any{page.Messages, page.FieldErrors, page.Values, page.Details["Entries"]}); diff != "" {
		t.Errorf("messages, field errors, the form, entries (-want +got):\n%s", diff)
	}
}

func TestDoorUptimeAndTrendsComeFromTheHourlyTelemetry(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())

	page := pageOf(k.b.Get("/admin?range=today"))

	want := []any{
		[][]string{uptimeHead, {"Lobby", "10%", "1 of 10 hours", "5", "-65 dBm", "50 ms"}},
		map[string]testsupport.Chart{
			"Signal by door, Mon 5 Jan 2026":  {Lines: []string{"225.0,65.0"}, Series: []string{"viz-line-1"}, Labels: []string{"-65 dBm", "-65 dBm", "Mon 5 Jan 00:00", "Tue 6 Jan 00:00"}},
			"Latency by door, Mon 5 Jan 2026": {Lines: []string{"225.0,65.0"}, Series: []string{"viz-line-1"}, Labels: []string{"50 ms", "50 ms", "Mon 5 Jan 00:00", "Tue 6 Jan 00:00"}},
		},
		[]string{"width: 10%"},
	}
	charts := map[string]testsupport.Chart{}
	for _, label := range []string{"Signal by door, Mon 5 Jan 2026", "Latency by door, Mon 5 Jan 2026"} {
		charts[label] = page.Charts[label]
	}
	if diff := cmp.Diff(want, []any{page.Tables["Door uptime"], charts, page.Bars["Door uptime"]}); diff != "" {
		t.Errorf("uptime, charts, uptime bars (-want +got):\n%s", diff)
	}
}

func TestLongerRangesSumTheDoorsTelemetryUpADayAtATime(t *testing.T) {
	t.Parallel()
	k := reported(t, testsupport.NewFactory())
	cases := map[string]struct {
		query string
		want  string
	}{
		"seven days, by hour": {"range=7d", `{"range":{"range":"7d","from":"2025-12-30","to":"2026-01-05","start":"2025-12-30T00:00:00Z",` +
			`"end":"2026-01-06T00:00:00Z","timezone":"UTC"},"unit":"hour","hours":154,"doors":[{"door":{"id":1,"name":"Lobby"},` +
			`"uptime":0.006493506493506494,"hours":1,"samples":2,"opens":5,"signal":-65,"latency_ms":50,` +
			`"trend":[{"start":"2026-01-05T09:00:00Z","hours":1,"samples":2,"signal":-65,"latency_ms":50,"opens":5}]}]}`},
		"eight days, by day": {"from=2025-12-29&to=2026-01-05", `{"range":{"range":"custom","from":"2025-12-29","to":"2026-01-05",` +
			`"start":"2025-12-29T00:00:00Z","end":"2026-01-06T00:00:00Z","timezone":"UTC"},"unit":"day","hours":178,` +
			`"doors":[{"door":{"id":1,"name":"Lobby"},"uptime":0.0056179775280898875,"hours":1,"samples":2,"opens":5,"signal":-65,"latency_ms":50,` +
			`"trend":[{"start":"2026-01-05T00:00:00Z","hours":1,"samples":2,"signal":-65,"latency_ms":50,"opens":5}]}]}`},
		"a day that has ended": {"from=2026-01-04&to=2026-01-04", `{"range":{"range":"custom","from":"2026-01-04","to":"2026-01-04",` +
			`"start":"2026-01-04T00:00:00Z","end":"2026-01-05T00:00:00Z","timezone":"UTC"},"unit":"hour","hours":24,` +
			`"doors":[{"door":{"id":1,"name":"Lobby"},"uptime":0,"hours":0,"samples":0,"opens":0,"trend":[]}]}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := send(t, k.b, "GET", "/api/v1/stats/doors?"+c.query, "", 200).Body

			if diff := cmp.Diff(testsupport.JSON(c.want), testsupport.JSONAt(got, "")); diff != "" {
				t.Errorf("stats (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorWithoutReportsHasNoUptimeBeforeAnHourOfTheRangeEnds(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.ClockStub.Set("2026-01-05T00:30:00Z")
	_, b, _, _ := enrolledDoor(t, f)

	got := []any{
		pageOf(b.Get("/admin?range=today")).Tables["Door uptime"],
		testsupport.JSONAt(send(t, b, "GET", "/api/v1/stats/doors?range=today", "", 200).Body, "/doors/0"),
	}

	want := []any{
		[][]string{uptimeHead, {"Lobby", "No hour has ended yet", "0 of 0 hours", "0", "Not reported yet", "Not reported yet"}},
		testsupport.JSON(`{"door":{"id":1,"name":"Lobby"},"hours":0,"samples":0,"opens":0,"trend":[]}`),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("uptime on the page and in the API (-want +got):\n%s", diff)
	}
}

// labelsOf is the set of keys of m.
func labelsOf[V any](m map[string]V) map[string]bool {
	out := map[string]bool{}
	for key := range m {
		out[key] = true
	}
	return out
}

// set is a set of labels.
func set(labels ...string) map[string]bool {
	out := map[string]bool{}
	for _, label := range labels {
		out[label] = true
	}
	return out
}

// noFigures is what the dashboard says to a viewer whose roles reach none
// of its figures.
const noFigures = "The entries, door, and firmware figures need the view-logs, manage-doors or door-commander, and manage-firmware roles. " +
	"Your roles reach the pages in the navigation above."

func TestTheDashboardShowsEachRoleOnlyTheFiguresItReaches(t *testing.T) {
	t.Parallel()
	entries := set("Entries per day", "Entries by weekday and hour of day", "Denial reasons", "Most active doors", "Least active doors")
	cases := map[string]struct {
		roles   string
		tables  map[string]bool
		figures map[string]bool
		notes   []string
		assets  int
		ranges  bool
	}{
		"view-logs":       {`["view-logs"]`, entries, set("Entries", "Allowed", "Denied", "People"), nil, 2, true},
		"manage-doors":    {`["manage-doors"]`, set("Door uptime"), set("Doors open", "Unresponsive doors", "Pending enrollments"), nil, 3, true},
		"door-commander":  {`["door-commander"]`, set("Door uptime"), set("Doors open", "Unresponsive doors"), nil, 3, true},
		"manage-firmware": {`["manage-firmware"]`, set("Firmware versions"), set("Pending updates"), nil, 3, false},
		"manage-users":    {`["manage-users"]`, set(), set("Open requests"), []string{noFigures}, 3, false},
		"manage-groups":   {`["manage-groups"]`, set(), set(), []string{noFigures}, 2, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := statsSite(t, testsupport.NewFactory())
			holdingOnly(t, k.f, k.b, c.roles)

			page := testsupport.ParsePage(k.b.Get("/admin").Body)

			ranges := map[string]bool{}
			if c.ranges {
				ranges = set("Show Today", "Show 30 days", "Show 90 days")
			}
			want := []any{"Dashboard", c.tables, c.figures, c.notes, c.assets, ranges}
			got := []any{page.Heading, labelsOf(page.Tables), labelsOf(page.Details), page.Notes, len(page.Assets), labelsOf(page.LinkTargets)}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("heading, tables, figures, notes, assets, range links (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheStatsAPIAnswersOnlyTheRolesThatSeeEachFigure(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		roles string
		want  []any
	}{
		"view-logs":       {`["view-logs"]`, []any{testsupport.JSON(`{}`), 200, 403, 403}},
		"manage-doors":    {`["manage-doors"]`, []any{testsupport.JSON(`{"doors_open":0,"unresponsive_doors":1,"pending_enrollments":0}`), 403, 200, 403}},
		"manage-firmware": {`["manage-firmware"]`, []any{testsupport.JSON(`{"pending_updates":0}`), 403, 403, 200}},
		"manage-users":    {`["manage-users"]`, []any{testsupport.JSON(`{"open_requests":0}`), 403, 403, 403}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := statsSite(t, testsupport.NewFactory())
			holdingOnly(t, k.f, k.b, c.roles)

			got := []any{
				testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/stats/now", "", 200).Body, ""),
				k.b.Get("/api/v1/stats/entries").Status, k.b.Get("/api/v1/stats/doors").Status, k.b.Get("/api/v1/stats/firmware").Status,
			}

			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Errorf("counts, statuses of entries, doors, firmware (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheDashboardIsOnlyForRoleHolders(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := adminHolding(t, f, `[]`)

	got := []any{b.Get("/admin").Status, b.Get("/api/v1/stats/now").Status}

	if diff := cmp.Diff([]any{403, 403}, got); diff != "" {
		t.Errorf("statuses (-want +got):\n%s", diff)
	}
}

func TestTheDashboardCountsTheFirmwareTheDoorsRunAndTheUpdatesPending(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	fl.reconnects(1, "1.5.0")

	page := pageOf(fl.browser().Get("/admin"))
	got := []any{
		page.Tables["Firmware versions"], page.Bars["Firmware versions"], page.Details["Pending updates"],
		testsupport.JSONAt(send(t, fl.b, "GET", "/api/v1/stats/firmware", "", 200).Body, ""),
		testsupport.JSONAt(send(t, fl.b, "GET", "/api/v1/stats/now", "", 200).Body, "/pending_updates"),
	}

	want := []any{
		[][]string{{"Version", "Doors"}, {"1.4.3", "2"}, {"1.5.0", "1"}}, []string{"width: 100%", "width: 50%"}, "2",
		testsupport.JSON(`{"versions":[{"version":"1.4.3","doors":2},{"version":"1.5.0","doors":1}]}`), 2.0,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("versions and pending updates on the page and in the API (-want +got):\n%s", diff)
	}
}

func TestTheDashboardCountsWhatWaitsForAnAdministrator(t *testing.T) {
	t.Parallel()
	b, _ := waiting(t, testsupport.NewFactory())
	send(t, b, "POST", "/api/v1/me/requests", labAccess, 201)

	got := []any{
		pageOf(b.Get("/admin")).Details,
		testsupport.JSONAt(send(t, b, "GET", "/api/v1/stats/now", "", 200).Body, ""),
	}

	want := []any{
		map[string]string{
			"Open requests": "1", "Doors open": "0", "Unresponsive doors": "0", "Pending enrollments": "1", "Pending updates": "0",
			"Entries": "0", "Allowed": "0", "Denied": "0", "People": "0",
		},
		testsupport.JSON(`{"open_requests":1,"doors_open":0,"unresponsive_doors":0,"pending_enrollments":1,"pending_updates":0}`),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("figures and counts (-want +got):\n%s", diff)
	}
}

func TestTheDashboardsLiveCountsFollowTheDoorsState(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	stream := k.b.Stream("/admin/live")
	defer stream.Close()
	stream.Next()
	before := pageOf(k.b.Get("/admin")).Details

	k.at("2026-01-05T09:05:00Z")
	run(t, k.a)

	after := pageOf(k.b.Get("/admin")).Details
	want := []any{"1", "0", "event: doors\ndata: changed\n\n", "1", "1 (needs attention)"}
	got := []any{before["Doors open"], before["Unresponsive doors"], stream.Next(), after["Doors open"], after["Unresponsive doors"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("open and unresponsive before, the stream's news, open and unresponsive after (-want +got):\n%s", diff)
	}
}

func TestTheDashboardRanksTheMostAndLeastActiveDoors(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	for _, name := range []string{"Dock", "Attic", "Cellar", "Annex", "Garage", "Roof", "Shed", "Vault", "Wing", "Yard", "Zoo", "Abandoned"} {
		createDoor(t, k.b, `{"name":"`+name+`"}`)
	}
	entries := map[string]int{"1": 3, "2": 2, "3": 1}
	for door, n := range entries {
		for i := range n {
			f.BrokerStub.Inject("doorcode/doors/"+door+"/check", testsupport.CBOR(map[string]any{"v": 1, "request_id": "d" + door + itoa(i), "digits": "999999"}))
			run(t, k.a)
		}
	}
	send(t, k.b, "DELETE", "/api/v1/doors/3", "", 204)
	send(t, k.b, "DELETE", "/api/v1/doors/13", "", 204)

	page := pageOf(k.b.Get("/admin?range=today"))

	want := []any{
		[][]string{
			rankingHead, {"Lobby", "0", "3", "3"}, {"Dock", "0", "2", "2"}, {"Attic (deleted)", "0", "1", "1"}, {"Annex", "0", "0", "0"},
			{"Cellar", "0", "0", "0"},
		},
		[][]string{
			rankingHead, {"Zoo", "0", "0", "0"}, {"Yard", "0", "0", "0"}, {"Wing", "0", "0", "0"}, {"Vault", "0", "0", "0"},
			{"Shed", "0", "0", "0"},
		},
		[]string{"width: 100%", "width: 66%", "width: 33%", "width: 0%", "width: 0%"},
		[]string{"10", "0", "Mon 5 Jan", "Mon 5 Jan"},
	}
	got := []any{
		page.Tables["Most active doors"], page.Tables["Least active doors"], page.Bars["Most active doors"],
		page.Charts["Entries per day, Mon 5 Jan 2026"].Labels,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("most and least active, the most active's bars, the chart's labels (-want +got):\n%s", diff)
	}
}

func TestTheDashboardSaysWhenThereIsNothingToSumUp(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	page := pageOf(b.Get("/admin?range=today"))

	want := []any{
		map[string][][]string{
			"Entries per day":                    {perDayHead, {"Mon 5 Jan", "0", "0", "0"}},
			"Entries by weekday and hour of day": heatTable(nil),
			"Denial reasons":                     {reasonsHead, {"No entry was denied in this range."}},
			"Most active doors":                  {rankingHead, {"No doors yet."}},
			"Least active doors":                 {rankingHead, {"No doors yet."}},
			"Door uptime":                        {uptimeHead, {"No controller is enrolled as a door yet. Approve one on the pending enrollments page."}},
			"Firmware versions":                  {{"Version", "Doors"}, {"No controller is enrolled as a door yet."}},
		},
		"0", "No codes were entered at the doors in this range.",
	}
	got := []any{page.Tables, page.Details["Allowed"], page.Charts["Entries per day, Mon 5 Jan 2026"].Labels[0]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tables, allowed, the chart (-want +got):\n%s", diff)
	}
}

func TestTheDashboardFailsWhenTheStoreDoes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		target string
	}{
		"the page's timezone":   {(*testsupport.Store).FailSettings, "/admin"},
		"the page's requests":   {(*testsupport.Store).FailRequests, "/admin"},
		"the page's doors":      {(*testsupport.Store).FailDoors, "/admin"},
		"the page's firmware":   {(*testsupport.Store).FailFirmware, "/admin"},
		"the page's entries":    {(*testsupport.Store).FailEvents, "/admin"},
		"the page's telemetry":  {(*testsupport.Store).FailTelemetry, "/admin"},
		"the counts":            {(*testsupport.Store).FailRequests, "/api/v1/stats/now"},
		"the entries' timezone": {(*testsupport.Store).FailSettings, "/api/v1/stats/entries"},
		"the entries":           {(*testsupport.Store).FailEvents, "/api/v1/stats/entries"},
		"the entries' doors":    {(*testsupport.Store).FailDoors, "/api/v1/stats/entries"},
		"the doors' telemetry":  {(*testsupport.Store).FailTelemetry, "/api/v1/stats/doors"},
		"the doors' doors":      {(*testsupport.Store).FailDoors, "/api/v1/stats/doors"},
		"the firmware's doors":  {(*testsupport.Store).FailDoors, "/api/v1/stats/firmware"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			c.fail(f.StoreStub)

			if diff := cmp.Diff(500, b.Get(c.target).Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheTrendChartsDrawEachOfTheFirstEightDoorsOnOneScale(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := brokerSite(t, f)
	var conns []*testsupport.Connection
	for i := 1; i <= 10; i++ {
		createDoor(t, b, `{"name":"Door `+itoa(i)+`"}`)
		conn, _ := enrollWith(t, f, a, b, "HW"+itoa(i), itoa(i))
		conns = append(conns, conn)
	}
	f.ClockStub.Set("2026-01-05T09:10:00Z")
	for _, report := range []struct {
		at   string
		door int
		says map[string]any
	}{
		{"2026-01-05T09:10:00Z", 1, map[string]any{"signal": -60, "latency_ms": 40, "opens": 1}},
		{"2026-01-05T09:10:00Z", 2, map[string]any{"signal": -70, "latency_ms": 60}},
		{"2026-01-05T10:10:00Z", 1, map[string]any{"signal": -64, "latency_ms": 44, "opens": 2}},
	} {
		f.ClockStub.Set(report.at)
		if err := conns[report.door-1].Publish("doorcode/doors/"+itoa(report.door)+"/telemetry", reportOf(report.says), false); err != nil {
			t.Fatal(err)
		}
		run(t, a)
	}
	f.ClockStub.Set("2026-01-05T11:30:00Z")
	run(t, a)

	page := pageOf(b.Get("/admin?range=today"))

	want := []any{
		[]string{"The charts draw the first 8 doors; the table lists 2 more."},
		testsupport.Chart{
			Lines: []string{"225.0,5.0 250.0,53.0", "225.0,125.0"}, Series: []string{"viz-line-1", "viz-line-2"},
			Labels: []string{"-60 dBm", "-70 dBm", "Mon 5 Jan 00:00", "Tue 6 Jan 00:00"},
		},
		[]string{"225.0,125.0 250.0,101.0", "225.0,5.0"},
		[]string{"Door 1", "18.2%", "2 of 11 hours", "3", "-62 dBm", "42 ms"},
		[]string{"Door 10", "0%", "0 of 11 hours", "0", "Not reported yet", "Not reported yet"},
	}
	got := []any{
		page.Notes, page.Charts["Signal by door, Mon 5 Jan 2026"], page.Charts["Latency by door, Mon 5 Jan 2026"].Lines,
		page.Tables["Door uptime"][1], page.Tables["Door uptime"][10],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("note, signal chart, latency lines, the first and tenth doors (-want +got):\n%s", diff)
	}
}

func TestUptimeCountsOnlyTheWholeHoursThatStartInTheRange(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Asia/Kolkata"
	b := signedInAdmin(t, f)

	got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/stats/doors?range=today", "", 200).Body, "")

	want := testsupport.JSON(`{"range":{"range":"today","from":"2026-01-05","to":"2026-01-05","start":"2026-01-04T18:30:00Z",` +
		`"end":"2026-01-05T18:30:00Z","timezone":"Asia/Kolkata"},"unit":"hour","hours":14,"doors":[]}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stats (-want +got):\n%s", diff)
	}
}

// column is the two marks of a day's column at x, width wide, titled
// title: its allowed entries from allowedY, allowedH high, and its denied
// ones from deniedY, deniedH high.
func column(x, width, allowedY, allowedH, deniedY, deniedH, title string) []testsupport.Mark {
	return []testsupport.Mark{
		{X: x, Y: allowedY, Width: width, Height: allowedH, Class: "viz-fill-allowed", Title: title},
		{X: x, Y: deniedY, Width: width, Height: deniedH, Class: "viz-fill-denied", Title: title},
	}
}

// emptyColumns are the columns of days without entries at each x, titled
// with the days.
func emptyColumns(xs, days []string) []testsupport.Mark {
	var out []testsupport.Mark
	for i, x := range xs {
		out = append(out, column(x, "24", "160", "0", "160", "0", days[i]+": 0 allowed, 0 denied")...)
	}
	return out
}

// shaded is the marks of chart that are shaded, those of hours with
// entries.
func shaded(chart testsupport.Chart) []testsupport.Mark {
	var out []testsupport.Mark
	for _, m := range chart.Marks {
		if m.Class != "viz-mark viz-heat-0" {
			out = append(out, m)
		}
	}
	return out
}

// heatCell is the heatmap's cell at x and y of level, titled title.
func heatCell(x, y string, level, title string) testsupport.Mark {
	return testsupport.Mark{X: x, Y: y, Width: "23", Height: "22", Class: "viz-mark viz-heat-" + level, Title: title}
}

// earlyDays are the first five days of statsSite's last seven, and
// earlyXs where their columns start.
var (
	earlyDays = []string{"Tue 30 Dec", "Wed 31 Dec", "Thu 1 Jan", "Fri 2 Jan", "Sat 3 Jan"}
	earlyXs   = []string{"30.9", "116.6", "202.3", "288", "373.7"}
)

func TestTheDashboardDrawsEachDaysEntriesAndTheBusiestHours(t *testing.T) {
	t.Parallel()
	k := statsSite(t, testsupport.NewFactory())

	page := testsupport.ParsePage(k.b.Get("/admin").Body)

	columns := emptyColumns(earlyXs, earlyDays)
	columns = append(columns, column("459.4", "24", "160", "0", "130", "30", "Sun 4 Jan: 0 allowed, 1 denied")...)
	columns = append(columns, column("545.1", "24", "100", "60", "38", "60", "Mon 5 Jan: 2 allowed, 2 denied")...)
	heat := page.Charts["Entries by weekday and hour of day"]
	want := []any{
		testsupport.Chart{Marks: columns, Labels: []string{"5", "0", "Tue 30 Dec", "Mon 5 Jan"}},
		[]testsupport.Mark{
			heatCell("40", "20", "2", "Mon 00:00–01:00: 1"), heatCell("265", "20", "5", "Mon 09:00–10:00: 3"),
			heatCell("615", "164", "2", "Sun 23:00–00:00: 1"),
		},
		[]string{"00:00", "06:00", "12:00", "18:00", "Mon", "4", "Tue", "0", "Wed", "0", "Thu", "0", "Fri", "0", "Sat", "0", "Sun", "1"},
		[]string{"width: 100%", "width: 50%"},
		[]string{"width: 100%", "width: 0%"},
	}
	got := []any{
		page.Charts["Entries per day, 30 Dec 2025 – 5 Jan 2026"], shaded(heat), heat.Labels, page.Bars["Denial reasons"], page.Bars["Most active doors"],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("columns, shaded cells, the heatmap's labels, reason and door bars (-want +got):\n%s", diff)
	}
}

func TestADayWithOnlyOneOutcomeDrawsItsColumnWithoutAGap(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.ClockStub.Set("2026-01-04T08:00:00Z")
	k := keypadSite(t, f)
	f.ClockStub.Set("2026-01-04T09:00:00Z")
	k.press("222222")
	f.ClockStub.Set(testsupport.DefaultNow)
	k.press("999999")
	b := signIn(t, f, k.a.Handler())

	week := testsupport.ParsePage(b.Get("/admin").Body)
	month := testsupport.ParsePage(b.Get("/admin?range=30d").Body)

	columns := emptyColumns(earlyXs, earlyDays)
	columns = append(columns, column("459.4", "24", "10", "150", "10", "0", "Sun 4 Jan: 1 allowed, 0 denied")...)
	columns = append(columns, column("545.1", "24", "160", "0", "10", "150", "Mon 5 Jan: 0 allowed, 1 denied")...)
	want := []any{
		testsupport.Chart{Marks: columns, Labels: []string{"1", "0", "Tue 30 Dec", "Mon 5 Jan"}},
		"1 50% allowed", heatTable(map[string]map[int]int{"Sun": {9: 1}, "Mon": {9: 1}}),
		column("4", "12", "160", "0", "160", "0", "Sun 7 Dec: 0 allowed, 0 denied"),
	}
	got := []any{
		week.Charts["Entries per day, 30 Dec 2025 – 5 Jan 2026"], week.Details["Allowed"], week.Tables["Entries by weekday and hour of day"],
		month.Charts["Entries per day, 7 Dec 2025 – 5 Jan 2026"].Marks[:2],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the week's columns, allowed, by hour, the month's first column (-want +got):\n%s", diff)
	}
}

func TestTheStatsAPIReadsARangeOfAtMost366Days(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())

	got := send(t, b, "GET", "/api/v1/stats/entries?from=2025-01-01&to=2026-01-01", "", 200).Body

	if diff := cmp.Diff([]any{"2025-01-01", 366}, []any{testsupport.JSONAt(got, "/range/from"), len(testsupport.JSONAt(got, "/days").([]any))}); diff != "" {
		t.Errorf("first day, days (-want +got):\n%s", diff)
	}
}
