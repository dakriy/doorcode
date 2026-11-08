//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// eventsStart is when the first event of eventWorld happens; the others
// follow an hour apart.
var eventsStart = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

// eventWorld is a store with the administrator (1), the doors Lobby (1)
// and Dock (2), and four events, an hour apart from eventsStart: the
// administrator let in at Lobby by rule 7 (1), an unknown code denied at
// Dock (2), the administrator opening Dock from the UI (3), and the
// administrator denied at Lobby for an expired account (4). Dock is
// deleted afterwards and its events stay.
func eventWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := overrideWorld(t, impl)
	admin, rule := int64(1), int64(7)
	for i, e := range []connector.StoredEvent{
		{DoorID: 1, Type: "check", Outcome: "allowed", Reason: "access rule", UserID: &admin, RuleID: &rule, RequestID: "r1"},
		{DoorID: 2, Type: "check", Outcome: "denied", Reason: "no rule", RequestID: "r2"},
		{DoorID: 2, Type: "open", Outcome: "allowed", Reason: "opened", UserID: &admin, Detail: "Opened for 3000 ms"},
		{DoorID: 1, Type: "check", Outcome: "denied", Reason: "expired", UserID: &admin, RequestID: "r4"},
	} {
		e.At = eventsStart.Add(time.Duration(i) * time.Hour)
		if _, err := store.AddEvent(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.SetDoorDeleted(context.Background(), 2, &eventsStart); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreListsEventsNewestFirstWithTheirDoorsAndUsers(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := eventWorld(t, impl)

			got, total, err := store.Events(context.Background(), connector.EventQuery{Offset: 1, Limit: 2})

			admin := int64(1)
			want := []any{[]connector.StoredEvent{
				{ID: 3, At: eventsStart.Add(2 * time.Hour), DoorID: 2, DoorName: "Dock", Type: "open", Outcome: "allowed", Reason: "opened",
					UserID: &admin, UserName: "Administrator", Detail: "Opened for 3000 ms"},
				{ID: 2, At: eventsStart.Add(time.Hour), DoorID: 2, DoorName: "Dock", Type: "check", Outcome: "denied", Reason: "no rule", RequestID: "r2"},
			}, 4, nil}
			if diff := cmp.Diff(want, []any{got, total, err}); diff != "" {
				t.Errorf("events, total, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsTheEventsAQuerySelects(t *testing.T) {
	from, to := eventsStart.Add(time.Hour), eventsStart.Add(3*time.Hour)
	cases := map[string]struct {
		q    connector.EventQuery
		want []int64
	}{
		"a door":      {connector.EventQuery{DoorID: 1}, []int64{4, 1}},
		"a user":      {connector.EventQuery{UserID: 1}, []int64{4, 3, 1}},
		"a type":      {connector.EventQuery{Type: "check"}, []int64{4, 2, 1}},
		"an outcome":  {connector.EventQuery{Outcome: "denied"}, []int64{4, 2}},
		"a reason":    {connector.EventQuery{Reason: "no rule"}, []int64{2}},
		"a range":     {connector.EventQuery{From: &from, To: &to}, []int64{3, 2}},
		"from a time": {connector.EventQuery{From: &to}, []int64{4}},
		"together":    {connector.EventQuery{DoorID: 1, Outcome: "allowed"}, []int64{1}},
		"none":        {connector.EventQuery{DoorID: 2, Reason: "expired"}, []int64{}},
	}
	for _, impl := range storeImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := eventWorld(t, impl)
				q := c.q
				q.Limit = 10

				listed, total, err := store.Events(context.Background(), q)

				got := []int64{}
				for _, e := range listed {
					got = append(got, e.ID)
				}
				if diff := cmp.Diff([]any{c.want, len(c.want), nil}, []any{got, total, err}); diff != "" {
					t.Errorf("ids, total, error (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestStoreRefusesAnEventNamingWhatItDoesNotHold(t *testing.T) {
	missing := int64(9)
	cases := map[string]connector.StoredEvent{
		"a door": {DoorID: 9, Type: "check", Outcome: "denied", Reason: "no rule", At: eventsStart},
		"a user": {DoorID: 1, Type: "check", Outcome: "denied", Reason: "no rule", UserID: &missing, At: eventsStart},
	}
	for _, impl := range storeImpls {
		for name, e := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := overrideWorld(t, impl)

				_, err := store.AddEvent(context.Background(), e)

				if err == nil {
					t.Error("AddEvent stored an event naming " + name + " that is not there")
				}
			})
		}
	}
}
