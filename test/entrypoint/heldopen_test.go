package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// sweepAt has the server do its periodic work at each of the RFC 3339
// times at, in order.
func (k *keypad) sweepAt(at ...string) {
	k.t.Helper()
	for _, a := range at {
		k.at(a)
		run(k.t, k.a)
	}
}

// badges is what the doors list shows of Lobby's state.
func (k *keypad) badges() any {
	k.t.Helper()
	return doorPage(k.t, k.b, "/admin/doors").Tables["Doors"][1][2]
}

func TestADoorIsHeldOpenOnlyForTheTimeItShouldBeLocked(t *testing.T) {
	t.Parallel()
	overriding := func(body string) func(k *keypad) {
		return func(k *keypad) { createOverride(k.t, k.b, "1", body) }
	}
	endsAt910 := overriding(`{"kind":"open","start":"2026-01-05T08:00:00Z","end":"2026-01-05T09:10:00Z"}`)
	brieflyOpen := overriding(`{"kind":"open","start":"2026-01-05T09:00:30Z","end":"2026-01-05T09:00:45Z"}`)
	cases := map[string]struct {
		arrange func(k *keypad)
		at      []string
		want    []any
	}{
		"open through an open override": {overriding(openNow), []string{"2026-01-05T09:01:00Z", "2026-01-05T09:05:00Z"}, []any{}},
		"open through open mode":        {(*keypad).inOpenMode, []string{"2026-01-05T09:01:00Z", "2026-01-05T09:05:00Z"}, []any{}},
		"open for less than its held-open time after an open override ends": {
			endsAt910, []string{"2026-01-05T09:05:00Z", "2026-01-05T09:10:59Z"}, []any{},
		},
		"open for its held-open time after an open override ends, looked at twice": {
			endsAt910, []string{"2026-01-05T09:05:00Z", "2026-01-05T09:11:00Z", "2026-01-05T09:12:00Z"}, []any{"held open"},
		},
		"open for less than its held-open time after an open override that began after it opened": {
			brieflyOpen, []string{"2026-01-05T09:01:44Z"}, []any{},
		},
		"open for its held-open time after an open override that began after it opened": {
			brieflyOpen, []string{"2026-01-05T09:01:45Z"}, []any{"held open"},
		},
		"open through a locked override": {overriding(lockedNow), []string{"2026-01-05T09:01:00Z"}, []any{"held open"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := keypadSite(t, testsupport.NewFactory())
			c.arrange(k)
			k.opens(true)

			k.sweepAt(c.at...)

			if diff := cmp.Diff(c.want, k.reasons()); diff != "" {
				t.Errorf("reasons (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorOpenThroughAnOpenSpanShowsOpen(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	createOverride(t, k.b, "1", openNow)
	k.opens(true)

	k.sweepAt("2026-01-05T09:04:00Z")

	if diff := cmp.Diff("Online Open", k.badges()); diff != "" {
		t.Errorf("status (-want +got):\n%s", diff)
	}
}

func TestADoorHeldOpenWhenAnOpenSpanBeginsShowsOpenInItAndIsHeldOpenAgainAfterIt(t *testing.T) {
	t.Parallel()
	k := keypadSite(t, testsupport.NewFactory())
	k.opens(true)
	k.sweepAt("2026-01-05T09:01:00Z")
	k.at("2026-01-05T09:01:30Z")
	createOverride(t, k.b, "1", `{"kind":"open","start":"2026-01-05T09:01:30Z","end":"2026-01-05T09:03:00Z"}`)

	k.sweepAt("2026-01-05T09:02:00Z")
	inSpan := k.badges()
	k.sweepAt("2026-01-05T09:03:59Z")
	beforeItRunsOut := k.reasons()
	k.sweepAt("2026-01-05T09:04:59Z")

	want := []any{"Online Open", []any{"held open"}, []any{"held open", "held open"}, "Online Held open"}
	if diff := cmp.Diff(want, []any{inSpan, beforeItRunsOut, k.reasons(), k.badges()}); diff != "" {
		t.Errorf("status in the span, reasons before and after the count runs out, status (-want +got):\n%s", diff)
	}
}
