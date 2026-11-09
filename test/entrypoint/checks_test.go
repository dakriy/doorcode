package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// keypad is a broker site where the controller of Lobby (1, in the zone
// North (1); Annex (2) holds no door) is connected and listens for its
// replies, with the schedules Weekdays (2, active on the stub clock's
// Monday morning) and Nights (3, not), the groups Staff
// (2) and Commanders (3, carrying door-commander), and the users Ann (2,
// code 111111) and Eve (4, code 333333, expired) in Staff and Cid (3,
// code 222222) in Commanders.
type keypad struct {
	t    *testing.T
	f    *testsupport.Factory
	a    *app.App
	b    *testsupport.Browser
	conn *testsupport.Connection
	// checks is how many codes were entered, which numbers their requests.
	checks int
}

func keypadSite(t *testing.T, f *testsupport.Factory) *keypad {
	t.Helper()
	a, b, conn, _ := enrolledDoor(t, f)
	if err := conn.Subscribe("doorcode/doors/1/reply"); err != nil {
		t.Fatal(err)
	}
	furnishDoorSite(t, b)
	send(t, b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1]}`, 200)
	createGroup(t, b, `{"slug":"staff","name":"Staff"}`)
	createGroup(t, b, `{"slug":"commanders","name":"Commanders","roles":["door-commander"]}`)
	createUser(t, b, ann)
	createUser(t, b, `{"email":"cid@example.com","first_name":"Cid","last_name":"Jones"}`)
	createUser(t, b, `{"email":"eve@example.com","first_name":"Eve","last_name":"Brown"}`)
	for user, code := range map[string]string{"2": "111111", "3": "222222", "4": "333333"} {
		send(t, b, "PUT", "/api/v1/users/"+user+"/code", `{"code":"`+code+`"}`, 204)
	}
	join(t, b, "2", "2")
	join(t, b, "3", "3")
	join(t, b, "2", "4")
	f.StoreStub.ExpireUser("eve@example.com", "2026-01-05T08:00:00Z")
	return &keypad{t: t, f: f, a: a, b: b, conn: conn}
}

// press has digits entered at Lobby's keypad, as request r1, r2, ... in
// turn, and the server handle them.
func (k *keypad) press(digits string) {
	k.t.Helper()
	k.checks++
	check := testsupport.CBOR(map[string]any{"v": 1, "request_id": "r" + itoa(k.checks), "digits": digits})
	if err := k.conn.Publish("doorcode/doors/1/check", check, false); err != nil {
		k.t.Fatal(err)
	}
	run(k.t, k.a)
}

// enter is press, returning the last message Lobby's controller received.
func (k *keypad) enter(digits string) map[string]any {
	k.t.Helper()
	k.press(digits)
	return lastReceived(k.t, k.conn)
}

// lastEvent is the newest event the API lists.
func (k *keypad) lastEvent() any {
	k.t.Helper()
	return testsupport.JSONAt(send(k.t, k.b, "GET", "/api/v1/events", "", 200).Body, "/events/0")
}

// allowed and denied are the replies to request r1.
var (
	allowed = map[string]any{"v": int64(1), "request_id": "r1", "allow": true, "strike_ms": int64(3000)}
	denied  = map[string]any{"v": int64(1), "request_id": "r1", "allow": false}
)

// checked is the event of the code check r1 at Lobby, the stub clock's
// time, with outcome and reason, and extra, more of its fields as JSON
// starting with a comma.
func checked(id, outcome, reason, extra string) any {
	return testsupport.JSON(`{"id":` + id + `,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"check",` +
		`"outcome":"` + outcome + `","reason":"` + reason + `","request_id":"r1"` + extra + `}`)
}

// The users of the keypad site as events name them.
const (
	byAnn = `,"user":{"id":2,"name":"Ann Smith"}`
	byCid = `,"user":{"id":3,"name":"Cid Jones"}`
	byEve = `,"user":{"id":4,"name":"Eve Brown"}`
)

// The overrides the decision tests use, in force on the stub clock's
// morning.
const (
	lockedNow = `{"kind":"locked","start":"2026-01-05T08:00:00Z","end":"2026-01-05T11:00:00Z"}`
	openNow   = `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T11:00:00Z"}`
)

func TestEachStepOfTheDecisionChainDecidesInItsOrder(t *testing.T) {
	t.Parallel()
	staffRule := func(k *keypad) { createRule(k.t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`) }
	cases := map[string]struct {
		arrange func(k *keypad)
		digits  string
		reply   map[string]any
		event   any
	}{
		"a rate-limited check is denied without being evaluated": {func(k *keypad) {
			staffRule(k)
			send(k.t, k.b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"rate_limit_burst":"1"}}`, 200)
			k.enter("999999")
			k.checks = 0
		}, "111111", denied, checked("2", "denied", "rate limited", "")},
		"an expired user is denied though a rule lets their group in": {staffRule, "333333", denied, checked("1", "denied", "expired", byEve)},
		"a deleted door commander is denied": {func(k *keypad) {
			k.f.StoreStub.DeleteUser("cid@example.com", "2026-01-05T08:00:00Z")
		}, "222222", denied, checked("1", "denied", "expired", byCid)},
		"a deleted door commander's keypad command is denied": {func(k *keypad) {
			k.f.StoreStub.DeleteUser("cid@example.com", "2026-01-05T08:00:00Z")
		}, "222222*00130", denied, checked("1", "denied", "expired", byCid)},
		"a keypad command from someone not a door commander is denied": {staffRule, "111111*00130", denied, checked("1", "denied", "not a commander", byAnn)},
		"a keypad command from an unknown code is denied":              {nil, "999999*00130", denied, checked("1", "denied", "not a commander", "")},
		"a door commander's command that means nothing is denied":      {nil, "222222*00399", denied, checked("1", "denied", "bad command", byCid)},
		"a door commander's command without the opcode is denied":      {nil, "222222*1130", denied, checked("1", "denied", "bad command", byCid)},
		"a door commander's opcode alone is denied":                    {nil, "222222*00", denied, checked("1", "denied", "bad command", byCid)},
		"a door commander's command for no minutes is denied":          {nil, "222222*0010", denied, checked("1", "denied", "bad command", byCid)},
		"a door commander's command for over a day is denied":          {nil, "222222*0021441", denied, checked("1", "denied", "bad command", byCid)},
		"a door commander's command for a minute is carried out":       {nil, "222222*0011", allowed, checked("2", "allowed", "command", byCid)},
		"a door commander gets in at a door no rule reaches":           {nil, "222222", allowed, checked("1", "allowed", "door commander", byCid)},
		"a door commander under a locked override gets in": {func(k *keypad) {
			createOverride(k.t, k.b, "1", lockedNow)
		}, "222222", allowed, checked("1", "allowed", "door commander", byCid)},
		"a locked override denies a member a rule lets in": {func(k *keypad) {
			staffRule(k)
			createOverride(k.t, k.b, "1", lockedNow)
		}, "111111", denied, checked("1", "denied", "locked", byAnn)},
		"a locked override wins over an open override": {func(k *keypad) {
			createOverride(k.t, k.b, "1", openNow)
			createOverride(k.t, k.b, "1", lockedNow)
		}, "999999", denied, checked("1", "denied", "locked", "")},
		"a locked override wins over open mode": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[2]}`, 200)
			createOverride(k.t, k.b, "1", lockedNow)
		}, "999999", denied, checked("1", "denied", "locked", "")},
		"an open override lets an unknown code in": {func(k *keypad) {
			createOverride(k.t, k.b, "1", openNow)
		}, "999999", allowed, checked("1", "allowed", "open override", "")},
		"an override that has not started decides nothing": {func(k *keypad) {
			createOverride(k.t, k.b, "1", `{"kind":"locked","start":"2026-01-05T10:00:00Z","end":"2026-01-05T11:00:00Z"}`)
			staffRule(k)
		}, "111111", allowed, checked("1", "allowed", "access rule", byAnn+`,"rule_id":1`)},
		"an unknown code during open mode of the door's zone gets in": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[3,2]}`, 200)
		}, "999999", allowed, checked("1", "allowed", "open mode", "")},
		"an unknown code during open mode of either of the door's zones gets in": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[3]}`, 200)
			send(k.t, k.b, "PUT", "/api/v1/zones/2", `{"name":"Annex","door_ids":[1],"open_schedule_ids":[2]}`, 200)
		}, "999999", allowed, checked("1", "allowed", "open mode", "")},
		"open mode outside its schedule lets no one in": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[3]}`, 200)
		}, "999999", denied, checked("1", "denied", "no rule", "")},
		"open mode of a zone the door is not in lets no one in": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/zones/2", `{"name":"Annex","open_schedule_ids":[2]}`, 200)
		}, "999999", denied, checked("1", "denied", "no rule", "")},
		"a rule reaching one of the door's zones lets a member of its group in": {func(k *keypad) {
			createDoor(k.t, k.b, `{"name":"Dock","zone_ids":[2]}`)
			createRule(k.t, k.b, `{"group_id":3,"schedule_id":2,"zone_ids":[1]}`)
			createRule(k.t, k.b, `{"group_id":2,"schedule_id":3,"zone_ids":[1]}`)
			createRule(k.t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[2]}`)
			createRule(k.t, k.b, `{"group_id":2,"schedule_id":1,"zone_ids":[1]}`)
		}, "111111", allowed, checked("1", "allowed", "access rule", byAnn+`,"rule_id":4`)},
		"a rule reaching the door through a zone lets a member in": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","zone_ids":[2]}`, 200)
			createRule(k.t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[2]}`)
		}, "111111", allowed, checked("1", "allowed", "access rule", byAnn+`,"rule_id":1`)},
		"a known user outside any rule is denied with no rule": {func(k *keypad) {
			createRule(k.t, k.b, `{"group_id":2,"schedule_id":3,"zone_ids":[1]}`)
			createRule(k.t, k.b, `{"group_id":3,"schedule_id":2,"zone_ids":[1]}`)
		}, "111111", denied, checked("1", "denied", "no rule", byAnn)},
		"an unknown code is denied with no rule": {staffRule, "999999", denied, checked("1", "denied", "no rule", "")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			if c.arrange != nil {
				c.arrange(k)
			}

			reply := k.enter(c.digits)

			if diff := cmp.Diff([]any{c.reply, c.event}, []any{reply, k.lastEvent()}); diff != "" {
				t.Errorf("reply, event (-want +got):\n%s", diff)
			}
		})
	}
}

// reasons is the reason of every event the API lists, newest first.
func (k *keypad) reasons() []any {
	k.t.Helper()
	events, _ := testsupport.JSONAt(send(k.t, k.b, "GET", "/api/v1/events?per_page=500", "", 200).Body, "/events").([]any)
	out := []any{}
	for _, e := range events {
		out = append(out, e.(map[string]any)["reason"])
	}
	return out
}

func TestADoorsRateLimitRefillsAtItsChecksAMinuteUpToItsBurst(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	send(t, k.b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"rate_limit_burst":"1","rate_limit_per_minute":"2"}}`, 200)

	for _, at := range []string{"09:00:00", "09:00:00", "09:00:15", "09:00:30", "10:00:00", "10:00:00"} {
		f.ClockStub.Set("2026-01-05T" + at + "Z")
		k.enter("999999")
	}

	want := []any{"rate limited", "no rule", "no rule", "rate limited", "rate limited", "no rule"}
	if diff := cmp.Diff(want, k.reasons()); diff != "" {
		t.Errorf("reasons, newest first (-want +got):\n%s", diff)
	}
}

func TestAKeypadCommandCreatesAnOverrideAndIsLoggedNamingTheCommander(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		digits, kind, reason, detail string
		end                          string
		state                        string
		ends                         int64
	}{
		"an open override":  {"222222*00130", "open", "keypad open", "Open for 30 minutes.", "2026-01-05T09:30:00Z", "open", mon0900 + 30*60},
		"a locked override": {"222222*0021440", "locked", "keypad locked", "Locked for 1440 minutes.", "2026-01-06T09:00:00Z", "locked", mon0900 + 1440*60},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)

			reply := k.enter(c.digits)

			events := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events", "", 200).Body, "/events")
			want := []any{allowed,
				testsupport.JSON(`[{"id":1,"start":"2026-01-05T09:00:00Z","end":"` + c.end + `","kind":"` + c.kind + `","reason":"Keypad command",` +
					`"created_by":{"id":3,"name":"Cid Jones"},"created_at":"2026-01-05T09:00:00Z","active":true,"door_id":1}]`),
				testsupport.JSON(`[` +
					`{"id":2,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"check","outcome":"allowed","reason":"command",` +
					`"user":{"id":3,"name":"Cid Jones"},"request_id":"r1"},` +
					`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"command","outcome":"allowed","reason":"` + c.reason + `",` +
					`"user":{"id":3,"name":"Cid Jones"},"detail":"` + c.detail + `"}]`),
				timelineOf(mon0900, tue1000, span(mon0900, c.ends, c.state)),
			}
			got := []any{reply, testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/overrides", "", 200).Body, "/overrides"), events,
				f.BrokerStub.RetainedDecoded(doorTopic("1", "timeline"))}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("reply, overrides, events, timeline (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheCancelCommandEndsTheOverridesInForceAndKeepsThoseToCome(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	createOverride(t, k.b, "1", lockedNow)
	createOverride(t, k.b, "1", openNow)
	createOverride(t, k.b, "1", lockedToday)

	reply := k.enter("222222*000")

	overrides, _ := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/overrides", "", 200).Body, "/overrides").([]any)
	want := []any{allowed, 1, testsupport.JSON(`"2026-01-05T10:00:00Z"`), "Overrides in force ended: 2.", "keypad cancel"}
	got := []any{reply, len(overrides), testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/doors/1/overrides", "", 200).Body, "/overrides/0/start"),
		testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events?type=command", "", 200).Body, "/events/0/detail"), k.reasons()[1]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("reply, overrides left, the one left starts, detail, reason (-want +got):\n%s", diff)
	}
}

func TestAKeypadCommandThatCannotBeCarriedOutIsDeniedAndLogged(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		digits string
		fail   func(f *testsupport.Factory)
	}{
		"an override that cannot be stored": {"222222*00130", func(f *testsupport.Factory) { f.StoreStub.FailOverrideWrites() }},
		"overrides that cannot be read":     {"222222*000", func(f *testsupport.Factory) { f.StoreStub.FailOverrides() }},
		"an override that cannot be ended":  {"222222*000", func(f *testsupport.Factory) { f.StoreStub.FailOverrideWrites() }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)
			createOverride(t, k.b, "1", lockedNow)
			logged := len(f.LogStub.Entries)
			c.fail(f)

			reply := k.enter(c.digits)

			want := []any{denied, checked("1", "denied", "command failed", byCid), []testsupport.LogEntry{{Level: "error",
				Message: "could not carry out a keypad command", Fields: map[string]string{"door": "1", "error": "stub: unavailable"}}}}
			if diff := cmp.Diff(want, []any{reply, k.lastEvent(), logsSince(f, logged)}); diff != "" {
				t.Errorf("reply, event, log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestACheckWhoseReplyCannotBeSentIsLoggedAsDeniedUnreachableWithTheRuleThatDecided(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`)
	f.BrokerStub.FailPublishing()

	k.press("111111")

	if diff := cmp.Diff(checked("1", "denied", "unreachable", byAnn+`,"rule_id":1`), k.lastEvent()); diff != "" {
		t.Errorf("event (-want +got):\n%s", diff)
	}
}

func TestACheckThatCannotBeHandledIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	check := testsupport.CBOR(map[string]any{"v": 1, "request_id": "r1", "digits": "999999"})
	cases := map[string]struct {
		topic   string
		payload []byte
		fail    func(f *testsupport.Factory)
		err     string
	}{
		"another protocol version":     {"doorcode/doors/1/check", testsupport.CBOR(map[string]any{"v": 2}), nil, "unsupported protocol version 2"},
		"a door that is not there":     {"doorcode/doors/9/check", check, nil, "no door has this id"},
		"settings that cannot be read": {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, "stub: unavailable"},
		"a site timezone that cannot be read": {"doorcode/doors/1/check", check, func(f *testsupport.Factory) {
			f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Mars/Olympus"
		}, "invalid value for site_timezone: must be a time zone name from the tz database, such as UTC or Europe/Berlin"},
		"code hashing that cannot be stored": {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailSettingWrites() }, "stub: unavailable"},
		"users that cannot be read":          {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailUsers() }, "stub: unavailable"},
		"overrides that cannot be read":      {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailOverrides() }, "stub: unavailable"},
		"schedules that cannot be read":      {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailSchedules() }, "stub: unavailable"},
		"zones that cannot be read":          {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailZones() }, "stub: unavailable"},
		"rules that cannot be read":          {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailRules() }, "stub: unavailable"},
		"an event that cannot be stored":     {"doorcode/doors/1/check", check, func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() }, "stub: unavailable"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)
			logged := len(f.LogStub.Entries)
			if c.fail != nil {
				c.fail(f)
			}

			f.BrokerStub.Inject(c.topic, c.payload)
			run(t, k.a)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{"topic": c.topic, "error": c.err}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheKeypadAndThePortalAgreeAtADoorInTwoZones(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	createSchedule(t, k.b, `{"name":"Evenings","windows":[{"days":["mon"],"start":"18:00","end":"20:00"}]}`)
	send(t, k.b, "PUT", "/api/v1/doors/1", `{"name":"Lobby","public":true,"zone_ids":[1,2]}`, 200)
	send(t, k.b, "PUT", "/api/v1/zones/2", `{"name":"Annex","door_ids":[1],"open_schedule_ids":[4]}`, 200)
	createRule(t, k.b, `{"group_id":2,"schedule_id":2,"zone_ids":[2]}`)
	ann := signedInAs(t, k.b, "2", annEmail)

	var got []any
	for _, at := range []string{"2026-01-05T09:00:00Z", "2026-01-05T19:00:00Z", "2026-01-05T20:30:00Z"} {
		k.at(at)
		portal := pageOf(ann.Get("/me/access")).Tables["Your doors"][1][1]
		k.press("111111")
		got = append(got, []any{portal, k.reasons()[0]})
	}

	want := []any{
		[]any{"Your code works now", "access rule"},
		[]any{"Open now", "open mode"},
		[]any{"Your code works at set times", "no rule"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("what the portal says and the keypad decides at 09:00, 19:00, 20:30 (-want +got):\n%s", diff)
	}
}
