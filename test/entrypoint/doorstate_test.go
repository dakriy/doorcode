package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// reportOf is the telemetry Lobby's controller reports: a signal of -61
// dBm, a round trip of 40 ms, 1 hour 2 minutes up, 118 KiB free,
// firmware 1.4.3, 2 reconnects, the door closed, and no opens, but for
// the fields changed names.
func reportOf(changed map[string]any) []byte {
	return testsupport.CBOR(with(map[string]any{
		"v": 1, "signal": -61, "latency_ms": 40, "uptime_s": 3720, "free_bytes": 120832, "firmware_version": "1.4.3",
		"reconnects": 2, "door_open": false, "opens": 0,
	}, changed))
}

// report has Lobby's controller report its telemetry, as reportOf says,
// and the server handle it.
func (k *keypad) report(changed map[string]any) {
	k.t.Helper()
	k.publish("doorcode/doors/1/telemetry", reportOf(changed))
}

// says has Lobby's controller say what happened to the door, such as
// "opened", and the server handle it.
func (k *keypad) says(what string) {
	k.t.Helper()
	k.publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": what}))
}

// opens has Lobby's controller say the door opened, unlocked or not, and
// the server handle it.
func (k *keypad) opens(unlocked bool) {
	k.t.Helper()
	k.publish("doorcode/doors/1/event", testsupport.CBOR(map[string]any{"v": 1, "event": "opened", "unlocked": unlocked}))
}

// publish has Lobby's controller publish payload on topic, and the server
// handle it.
func (k *keypad) publish(topic string, payload []byte) {
	k.t.Helper()
	if err := k.conn.Publish(topic, payload, false); err != nil {
		k.t.Fatal(err)
	}
	run(k.t, k.a)
}

// at sets the stub clock to the RFC 3339 time at.
func (k *keypad) at(at string) {
	k.f.ClockStub.Set(at)
}

// inOpenMode puts Lobby in open mode all the time, through its zone's
// open-mode schedule.
func (k *keypad) inOpenMode() {
	k.t.Helper()
	send(k.t, k.b, "PUT", "/api/v1/zones/1", `{"name":"North","door_ids":[1],"open_schedule_ids":[2]}`, 200)
}

// state is what the API shows of Lobby's state at the JSON pointer
// pointer, all of it for the empty pointer.
func (k *keypad) state(pointer string) any {
	k.t.Helper()
	return testsupport.JSONAt(send(k.t, k.b, "GET", "/api/v1/doors/1/state", "", 200).Body, pointer)
}

func TestADoorOpenedWithNothingUnlockingItIsLoggedAsForcedOpen(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.says("opened")

	want := testsupport.JSON(`{"id":2,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"alarm","outcome":"alert",` +
		`"reason":"forced open","detail":"Nothing unlocked it in the 6 seconds before it opened."}`)
	if diff := cmp.Diff(want, k.lastEvent()); diff != "" {
		t.Errorf("event (-want +got):\n%s", diff)
	}
}

func TestADoorOpenedWithinTheStrikeWindowOfAnUnlockIsNotForced(t *testing.T) {
	t.Parallel()
	staffRule := `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`
	cases := map[string]struct {
		unlock func(k *keypad)
		opened string
		want   []any
	}{
		"a code allowed": {func(k *keypad) {
			createRule(k.t, k.b, staffRule)
			k.press("111111")
		}, "2026-01-05T09:00:06Z", []any{"opened", "access rule"}},
		"a code allowed longer ago than the strike pulse and reply timeout": {func(k *keypad) {
			createRule(k.t, k.b, staffRule)
			k.press("111111")
		}, "2026-01-05T09:00:06.001Z", []any{"forced open", "opened", "access rule"}},
		"a door commander's code": {func(k *keypad) { k.press("222222") }, "2026-01-05T09:00:06Z", []any{"opened", "door commander"}},
		"a code denied":           {func(k *keypad) { k.press("111111") }, "2026-01-05T09:00:01Z", []any{"forced open", "opened", "no rule"}},
		"an opening from the UI": {func(k *keypad) {
			send(k.t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
		}, "2026-01-05T09:00:02Z", []any{"opened", "admin UI"}},
		"an open override in force": {func(k *keypad) {
			createOverride(k.t, k.b, "1", openNow)
		}, "2026-01-05T10:00:00Z", []any{"opened"}},
		"an open override that ended within the window": {func(k *keypad) {
			createOverride(k.t, k.b, "1", `{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T09:59:55Z"}`)
		}, "2026-01-05T10:00:00Z", []any{"opened"}},
		"a locked override in force": {func(k *keypad) {
			createOverride(k.t, k.b, "1", lockedNow)
		}, "2026-01-05T10:00:00Z", []any{"forced open", "opened"}},
		"open mode": {func(k *keypad) {
			k.inOpenMode()
		}, "2026-01-05T10:00:00Z", []any{"opened"}},
		"a longer strike pulse": {func(k *keypad) {
			createRule(k.t, k.b, staffRule)
			send(k.t, k.b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"strike_pulse_ms":"5000"}}`, 200)
			k.press("111111")
		}, "2026-01-05T09:00:08Z", []any{"opened", "access rule"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.unlock(k)
			k.at(c.opened)

			k.says("opened")

			got := []any{k.state("/door"), k.reasons()}
			if diff := cmp.Diff([]any{"open", c.want}, got); diff != "" {
				t.Errorf("door, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorThatSaysItOpenedWhileOpenIsNotForcedAgain(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	k.at("2026-01-05T09:00:20Z")

	k.says("opened")

	if diff := cmp.Diff([]any{"forced open", "opened"}, k.reasons()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
}

func TestADoorClosedAndOpenedAgainIsForcedAgain(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.says("opened")
	k.says("closed")
	k.at("2026-01-05T09:00:20Z")

	k.says("opened")

	if diff := cmp.Diff([]any{"forced open", "opened", "closed", "forced open", "opened"}, k.reasons()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
}

func TestADoorItsControllerSaysOpenedLockedIsForcedWhateverUnlockedIt(t *testing.T) {
	t.Parallel()
	staffRule := `{"group_id":2,"schedule_id":2,"zone_ids":[1]}`
	cases := map[string]struct {
		unlock func(k *keypad)
		opened string
		want   []any
	}{
		"nothing": {func(*keypad) {}, "2026-01-05T09:00:00Z", []any{"forced open", "opened"}},
		"a code allowed": {func(k *keypad) {
			createRule(k.t, k.b, staffRule)
			k.press("111111")
		}, "2026-01-05T09:00:01Z", []any{"forced open", "opened", "access rule"}},
		"an opening from the UI": {func(k *keypad) {
			send(k.t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
		}, "2026-01-05T09:00:01Z", []any{"forced open", "opened", "admin UI"}},
		"an open override in force": {func(k *keypad) {
			createOverride(k.t, k.b, "1", openNow)
		}, "2026-01-05T10:00:00Z", []any{"forced open", "opened"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.unlock(k)
			k.at(c.opened)

			k.opens(false)

			got := []any{k.state("/door"), k.reasons(), testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events", "", 200).Body, "/events/0/detail")}
			want := []any{"open", c.want, "Its controller says it was locked when it opened."}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("door, reasons, detail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorItsControllerSaysOpenedUnlockedIsNotForcedWhateverTheStrikeWindowHolds(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		before func(k *keypad)
		want   []any
	}{
		"nothing":       {func(*keypad) {}, []any{"opened"}},
		"a code denied": {func(k *keypad) { k.press("111111") }, []any{"opened", "no rule"}},
		"a locked override in force": {func(k *keypad) {
			createOverride(k.t, k.b, "1", lockedNow)
			k.at("2026-01-05T10:00:00Z")
		}, []any{"opened"}},
		"open mode": {func(k *keypad) {
			k.inOpenMode()
			k.at("2026-01-05T10:00:00Z")
		}, []any{"opened"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.before(k)

			k.opens(true)

			got := []any{k.state("/door"), k.reasons()}
			if diff := cmp.Diff([]any{"open", c.want}, got); diff != "" {
				t.Errorf("door, reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorItsControllerSaysOpenedLockedWhileOpenIsNotForcedAgain(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.opens(false)
	k.at("2026-01-05T09:00:20Z")

	k.opens(false)

	if diff := cmp.Diff([]any{"forced open", "opened"}, k.reasons()); diff != "" {
		t.Errorf("reasons (-want +got):\n%s", diff)
	}
}

func TestADoorOpenPastItsHeldOpenTimeIsLoggedAsHeldOpenOnce(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		at      []string
		want    []any
	}{
		"open for its held-open time": {nil, []string{"2026-01-05T09:01:00Z"}, []any{"held open", "opened", "admin UI"}},
		"open for less":               {nil, []string{"2026-01-05T09:00:59Z"}, []any{"opened", "admin UI"}},
		"open for longer, looked at twice": {
			nil, []string{"2026-01-05T09:01:00Z", "2026-01-05T09:02:00Z"}, []any{"held open", "opened", "admin UI"},
		},
		"closed before its held-open time": {func(k *keypad) { k.says("closed") }, []string{"2026-01-05T09:05:00Z"}, []any{"closed", "opened", "admin UI"}},
		"with its own longer held-open time": {func(k *keypad) {
			send(k.t, k.b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"held_open_s":"120"}}`, 200)
		}, []string{"2026-01-05T09:01:59Z"}, []any{"opened", "admin UI"}},
		"opened again by its telemetry after closing": {func(k *keypad) {
			k.says("closed")
			k.at("2026-01-05T09:00:30Z")
			k.report(map[string]any{"door_open": true})
		}, []string{"2026-01-05T09:01:00Z"}, []any{"opened", "closed", "opened", "admin UI"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			send(t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
			k.says("opened")
			if c.arrange != nil {
				c.arrange(k)
			}

			for _, at := range c.at {
				k.at(at)
				run(t, k.a)
			}

			if diff := cmp.Diff(c.want, k.reasons()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAHeldOpenAlarmSaysForHowLong(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	send(t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
	k.says("opened")
	k.at("2026-01-05T09:01:00Z")

	run(t, k.a)

	want := testsupport.JSON(`{"id":3,"at":"2026-01-05T09:01:00Z","door":{"id":1,"name":"Lobby"},"type":"alarm","outcome":"alert",` +
		`"reason":"held open","detail":"Open for longer than 60 seconds."}`)
	if diff := cmp.Diff(want, k.lastEvent()); diff != "" {
		t.Errorf("event (-want +got):\n%s", diff)
	}
}

func TestADoorNotHeardFromForTheUnresponsivePeriodIsMarkedUnresponsive(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		setting string
		at      string
		want    any
	}{
		"for the period":                {"", "2026-01-05T09:05:00Z", true},
		"for less than the period":      {"", "2026-01-05T09:04:59Z", false},
		"for less than a longer period": {"10", "2026-01-05T09:09:59Z", false},
		"for a longer period":           {"10", "2026-01-05T09:10:00Z", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			if c.setting != "" {
				f.StoreStub.SeedSetting("unresponsive_minutes", c.setting, testsupport.DefaultNow)
			}
			k := keypadSite(t, f)
			// The front door, listed before Lobby, has no controller.
			createDoor(t, k.b, frontDoor)
			k.at(c.at)

			run(t, k.a)

			if diff := cmp.Diff(c.want, k.state("/unresponsive")); diff != "" {
				t.Errorf("unresponsive (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnUnresponsiveDoorIsResponsiveAgainOnItsNextContact(t *testing.T) {
	t.Parallel()
	cases := map[string]func(k *keypad){
		"telemetry":    func(k *keypad) { k.report(nil) },
		"a door event": func(k *keypad) { k.says("closed") },
		"a start":      func(k *keypad) { k.starts("power_on") },
		"its online status": func(k *keypad) {
			k.publish("doorcode/doors/1/online", onlineAs(firstUsername, true))
		},
	}
	for name, contact := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			k.at("2026-01-05T09:05:00Z")
			run(t, k.a)
			marked := k.state("/unresponsive")
			k.at("2026-01-05T09:05:30Z")

			contact(k)

			if diff := cmp.Diff([]any{true, false}, []any{marked, k.state("/unresponsive")}); diff != "" {
				t.Errorf("marked, after contact (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADeletedDoorKeepsTheStateItHadWhenItWasDeleted(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	send(t, k.b, "DELETE", "/api/v1/doors/1", "", 204)
	k.at("2026-01-05T09:05:00Z")

	run(t, k.a)

	if diff := cmp.Diff(false, k.state("/unresponsive")); diff != "" {
		t.Errorf("unresponsive (-want +got):\n%s", diff)
	}
}

func TestWhatADoorSaysOfItselfThatCannotBeHandledIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	telemetry, event := "doorcode/doors/1/telemetry", "doorcode/doors/1/event"
	opened := testsupport.CBOR(map[string]any{"v": 1, "event": "opened"})
	unavailable := "stub: unavailable"
	cases := map[string]struct {
		topic   string
		payload []byte
		fail    func(f *testsupport.Factory)
		err     string
	}{
		"a report of another version":          {telemetry, testsupport.CBOR(map[string]any{"v": 2}), nil, "unsupported protocol version 2"},
		"a report of a door that is not there": {"doorcode/doors/9/telemetry", reportOf(nil), nil, "no controller is enrolled as door 9"},
		"a report of a door without a controller": {
			"doorcode/doors/2/telemetry", reportOf(nil), nil, "no controller is enrolled as door 2",
		},
		"a report when doors cannot be read":    {telemetry, reportOf(nil), func(f *testsupport.Factory) { f.StoreStub.FailDoors() }, unavailable},
		"a report that cannot be stored":        {telemetry, reportOf(nil), func(f *testsupport.Factory) { f.StoreStub.FailTelemetryWrites() }, unavailable},
		"a report whose state cannot be stored": {telemetry, reportOf(nil), func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, unavailable},
		"an event of another version":           {event, testsupport.CBOR(map[string]any{"v": 2}), nil, "unsupported protocol version 2"},
		"an event of a door that is not there":  {"doorcode/doors/9/event", opened, nil, "no controller is enrolled as door 9"},
		"an event the server knows nothing of": {
			event, testsupport.CBOR(map[string]any{"v": 1, "event": "jammed"}), nil, `unknown door event "jammed"`,
		},
		"an opening when settings cannot be read": {event, opened, func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, unavailable},
		"an opening when the door's configuration is invalid": {event, opened, func(f *testsupport.Factory) {
			f.StoreStub.SeedSetting("door_strike_pulse_ms", "1", testsupport.DefaultNow)
		}, "invalid value for door_strike_pulse_ms: must be a whole number of milliseconds from 100 to 60000"},
		"an opening when events cannot be read": {event, opened, func(f *testsupport.Factory) { f.StoreStub.FailEventReads() }, unavailable},
		"an opening when a setting is invalid": {event, opened, func(f *testsupport.Factory) {
			f.StoreStub.SeedSetting("telemetry_retention_days", "0", testsupport.DefaultNow)
		}, "invalid value for telemetry_retention_days: must be a whole number of days from 1 to 365"},
		"an opening when schedules cannot be read": {event, opened, func(f *testsupport.Factory) { f.StoreStub.FailSchedules() }, unavailable},
		"a forced opening that cannot be logged":   {event, opened, func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() }, unavailable},
		"a start that cannot be logged": {
			event, testsupport.CBOR(map[string]any{"v": 1, "event": "started"}), func(f *testsupport.Factory) { f.StoreStub.FailEventWrites() }, unavailable,
		},
		"a start whose state cannot be stored": {
			event, testsupport.CBOR(map[string]any{"v": 1, "event": "started"}), func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, unavailable,
		},
		"a closing whose state cannot be stored": {
			event, testsupport.CBOR(map[string]any{"v": 1, "event": "closed"}), func(f *testsupport.Factory) { f.StoreStub.FailControllerWrites() }, unavailable,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			k := keypadSite(t, f)
			createDoor(t, k.b, frontDoor)
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

func TestThePeriodicWorkLogsWhenItCannotWatchTheDoorsOrKeepTheirTelemetry(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(k *keypad)
		want    string
	}{
		"when telemetry cannot be read": {func(k *keypad) { k.f.StoreStub.FailTelemetry() }, "stub: unavailable\nstub: unavailable"},
		"when telemetry cannot be changed": {func(k *keypad) {
			k.report(nil)
			k.f.StoreStub.FailTelemetryWrites()
		}, "stub: unavailable\nstub: unavailable"},
		"when reports cannot be rolled up": {func(k *keypad) {
			k.report(nil)
			k.f.StoreStub.FailSampleReads()
		}, "stub: unavailable"},
		"when a held-open alarm cannot be logged": {func(k *keypad) {
			send(k.t, k.b, "POST", "/api/v1/doors/1/open", "", 200)
			k.says("opened")
			k.f.StoreStub.FailEventWrites()
		}, "stub: unavailable"},
		"when an open door's overrides cannot be read": {func(k *keypad) {
			k.opens(true)
			k.f.StoreStub.FailOverrides()
		}, "stub: unavailable"},
		"when an unresponsive door cannot be marked": {func(k *keypad) { k.f.StoreStub.FailControllerWrites() }, "stub: unavailable"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)
			logged := len(k.f.LogStub.Entries)
			k.at("2026-01-05T10:05:00Z")

			run(t, k.a)

			want := []testsupport.LogEntry{{Level: "error", Message: "periodic work failed", Fields: map[string]string{"error": c.want}}}
			if diff := cmp.Diff(want, logsSince(k.f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}
