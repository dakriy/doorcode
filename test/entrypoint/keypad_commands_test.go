package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// outcomeAndReason is the outcome and reason of the newest event.
func (k *keypad) outcomeAndReason() []any {
	k.t.Helper()
	e, _ := k.lastEvent().(map[string]any)
	return []any{e["outcome"], e["reason"]}
}

// lastReply is the last reply conn received, decoded.
func lastReply(t *testing.T, conn *testsupport.Connection) map[string]any {
	t.Helper()
	var reply map[string]any
	for _, m := range conn.Received() {
		if m.Topic == "doorcode/doors/1/reply" {
			reply, _ = testsupport.Decoded(m.Payload).(map[string]any)
		}
	}
	return reply
}

// lastTwoReceived is the payloads of the last two messages conn received,
// decoded, the command id left out.
func lastTwoReceived(t *testing.T, conn *testsupport.Connection) []any {
	t.Helper()
	received := conn.Received()
	if len(received) < 2 {
		t.Fatal("the controller received fewer than two messages")
	}
	var out []any
	for _, m := range received[len(received)-2:] {
		decoded, _ := testsupport.Decoded(m.Payload).(map[string]any)
		delete(decoded, "id")
		out = append(out, decoded)
	}
	return out
}

func TestTheDigitAfterTheFirstStarAloneSaysWhichKeypadCommandAnEntryIs(t *testing.T) {
	t.Parallel()
	carried := []any{"allowed", "command"}
	bad := []any{"denied", "bad command"}
	cases := map[string]struct {
		digits string
		want   []any
	}{
		"0 alone cancels":                         {"222222*0", carried},
		"0 followed by anything":                  {"222222*01", bad},
		"the three-digit cancel":                  {"222222*000", bad},
		"the three-digit open override":           {"222222*00130", bad},
		"the three-digit locked override":         {"222222*0021440", bad},
		"1 without minutes":                       {"222222*1", bad},
		"1 for no minutes":                        {"222222*10", bad},
		"1 for a minute":                          {"222222*11", carried},
		"1 for a day":                             {"222222*11440", carried},
		"1 for a day and a minute":                {"222222*11441", bad},
		"1 for minutes with a leading zero":       {"222222*1030", bad},
		"2 without minutes":                       {"222222*2", bad},
		"2 for a minute":                          {"222222*21", carried},
		"2 for a day and a minute":                {"222222*21441", bad},
		"9 alone reboots":                         {"222222*9", carried},
		"9 followed by anything":                  {"222222*90", bad},
		"an opcode that names nothing":            {"222222*3", bad},
		"the digit below the reboot opcode":       {"222222*8", bad},
		"a star and nothing after it":             {"222222*", bad},
		"a second star after the opcode":          {"222222*1*30", bad},
		"a commander's code with no star":         {"222222", []any{"allowed", "door commander"}},
		"a commander's code with the opcode only": {"2222229", []any{"denied", "no rule"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())

			k.enter(c.digits)

			if diff := cmp.Diff(c.want, k.outcomeAndReason()); diff != "" {
				t.Errorf("outcome, reason (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAKeypadCommandFollowsACodeOfAnyLength(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"222229", "4211234999", "42112341234567", "99999999999999999999"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			send(t, k.b, "PUT", "/api/v1/users/3/code", `{"code":"`+code+`"}`, 204)

			var got []any
			for _, command := range []string{"*21", "*0", "*130", "*9"} {
				k.enter(code + command)
				got = append(got, k.outcomeAndReason())
			}

			carried := []any{"allowed", "command"}
			if diff := cmp.Diff([]any{carried, carried, carried, carried}, got); diff != "" {
				t.Errorf("outcome, reason of each (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAKeypadRebootOpensTheDoorThenRestartsItAndIsLoggedNamingTheCommander(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	k.press("222222*9")

	events := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events", "", 200).Body, "/events")
	want := []any{
		[]any{allowed, map[string]any{"v": int64(1), "command": "reboot"}},
		testsupport.JSON(`[` +
			`{"id":2,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"check","outcome":"allowed","reason":"command",` +
			`"user":{"id":3,"name":"Cid Jones"},"request_id":"r1"},` +
			`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"command","outcome":"allowed","reason":"keypad reboot",` +
			`"user":{"id":3,"name":"Cid Jones"},"detail":"Told to restart and connect again."}]`),
	}
	if diff := cmp.Diff(want, []any{lastTwoReceived(t, k.conn), events}); diff != "" {
		t.Errorf("reply and command, events (-want +got):\n%s", diff)
	}
}

func TestAKeypadRebootIsRefusedForTenMinutesAfterTheLastOne(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())

	var got []any
	for _, at := range []string{"09:00:00", "09:05:00", "09:10:00", "09:10:01", "09:15:00"} {
		k.at("2026-01-05T" + at + "Z")
		k.press("222222*9")
		got = append(got, []any{at, lastReply(t, k.conn)["allow"], k.outcomeAndReason()})
	}

	want := []any{
		[]any{"09:00:00", true, []any{"allowed", "command"}},
		[]any{"09:05:00", false, []any{"denied", "reboot too soon"}},
		[]any{"09:10:00", false, []any{"denied", "reboot too soon"}},
		[]any{"09:10:01", true, []any{"allowed", "command"}},
		[]any{"09:15:00", false, []any{"denied", "reboot too soon"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("at, allowed, outcome and reason (-want +got):\n%s", diff)
	}
}

func TestARebootFromTheAdministrationDoesNotHoldBackAKeypadReboot(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	send(t, k.b, "POST", "/api/v1/doors/1/reboot", "", 200)

	k.press("222222*9")

	if diff := cmp.Diff([]any{true, []any{"allowed", "command"}}, []any{lastReply(t, k.conn)["allow"], k.outcomeAndReason()}); diff != "" {
		t.Errorf("allowed, outcome and reason (-want +got):\n%s", diff)
	}
}

func TestAKeypadRebootIsRefusedWhileTheDoorIsInstallingFirmware(t *testing.T) {
	t.Parallel()
	fl := fleetSite(t, testsupport.NewFactory())
	createGroup(t, fl.b, `{"slug":"commanders","name":"Commanders","roles":["door-commander"]}`)
	createUser(t, fl.b, `{"email":"cid@example.com","first_name":"Cid","last_name":"Jones"}`)
	send(t, fl.b, "PUT", "/api/v1/users/2/code", `{"code":"222222"}`, 204)
	join(t, fl.b, "1", "2")
	send(t, fl.b, "POST", "/api/v1/doors/1/update", "", 200)
	if err := fl.conns[0].Subscribe("doorcode/doors/1/reply"); err != nil {
		t.Fatal(err)
	}

	fl.publish(1, "check", testsupport.CBOR(map[string]any{"v": 1, "request_id": "r1", "digits": "222222*9"}))

	e, _ := testsupport.JSONAt(send(t, fl.b, "GET", "/api/v1/events?type=check", "", 200).Body, "/events/0").(map[string]any)
	want := []any{denied, "denied", "update under way"}
	if diff := cmp.Diff(want, []any{lastReceived(t, fl.conns[0]), e["outcome"], e["reason"]}); diff != "" {
		t.Errorf("reply, outcome, reason (-want +got):\n%s", diff)
	}
}

func TestAKeypadRebootThatCannotBeSentIsLoggedAsUnreachable(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	f.BrokerStub.FailPublishingOn("command")

	reply := k.enter("222222*9")

	command := testsupport.JSONAt(send(t, k.b, "GET", "/api/v1/events?type=command", "", 200).Body, "/events/0")
	want := []any{allowed, testsupport.JSON(`{"id":1,"at":"2026-01-05T09:00:00Z","door":{"id":1,"name":"Lobby"},"type":"command",` +
		`"outcome":"denied","reason":"unreachable","user":{"id":3,"name":"Cid Jones"},"detail":"Could not be told to restart: stub: unavailable"}`)}
	if diff := cmp.Diff(want, []any{reply, command}); diff != "" {
		t.Errorf("reply, command event (-want +got):\n%s", diff)
	}
}

func TestAKeypadRebootWhoseEventCannotBeStoredIsLoggedAndIgnored(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	logged := len(f.LogStub.Entries)
	f.StoreStub.FailEventWrites()

	k.press("222222*9")

	want := []any{
		map[string]any{"v": int64(1), "command": "reboot"},
		[]testsupport.LogEntry{{Level: "error", Message: "ignored a broker message",
			Fields: map[string]string{"topic": "doorcode/doors/1/check", "error": "stub: unavailable"}}},
	}
	if diff := cmp.Diff(want, []any{commandOf(t, k.conn), logsSince(f, logged)}); diff != "" {
		t.Errorf("command, log (-want +got):\n%s", diff)
	}
}

func TestAKeypadRebootWhoseDoorsEventsCannotBeReadIsDeniedAndLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	k := keypadSite(t, f)
	logged := len(f.LogStub.Entries)
	f.StoreStub.FailEventReads()

	reply := k.enter("222222*9")

	want := []any{denied, []testsupport.LogEntry{{Level: "error",
		Message: "could not carry out a keypad command", Fields: map[string]string{"door": "1", "error": "stub: unavailable"}}}}
	if diff := cmp.Diff(want, []any{reply, logsSince(f, logged)}); diff != "" {
		t.Errorf("reply, log (-want +got):\n%s", diff)
	}
}
