//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// alertsStart is when the first message of outboxWorld is queued.
var alertsStart = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

// outboxWorld is a store whose outbox holds a pending message to one
// address (1), a sent one to two (2), and another pending one (3), each
// queued a minute after the one before.
func outboxWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := migrated(t, impl)
	sent := alertsStart.Add(90 * time.Second)
	for i, m := range []connector.StoredMail{
		{Kind: "held_open", Subject: "Held", Text: "Open.\n", To: []string{"ops@example.com"}, Status: "pending"},
		{Kind: "config_mismatch", Subject: "Mismatched", Text: "Mismatched.\n", To: []string{"ops@example.com", "desk@example.com"}, Status: "sent",
			Attempts: 1, LastAttemptAt: &sent, SentAt: &sent},
		{Kind: "held_open", Subject: "Held again", Text: "Again.\n", To: []string{"desk@example.com"}, Status: "pending",
			Attempts: 2, LastAttemptAt: &sent, LastError: "421 Busy"},
	} {
		m.CreatedAt = alertsStart.Add(time.Duration(i) * time.Minute)
		m.NextAttemptAt = m.CreatedAt
		if _, err := store.AddMail(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestStoreKeepsTheStateOfEachKindOfAlert(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()

			before, beforeErr := store.AlertKinds(ctx)
			errs := []error{
				store.SetAlertKind(ctx, "held_open", "mail"),
				store.SetAlertKind(ctx, "config_mismatch", "off"),
				store.SetAlertKind(ctx, "held_open", "on"),
			}
			after, afterErr := store.AlertKinds(ctx)

			want := []any{map[string]string{}, nil, []error{nil, nil, nil}, map[string]string{"held_open": "on", "config_mismatch": "off"}, nil}
			if diff := cmp.Diff(want, []any{before, beforeErr, errs, after, afterErr}); diff != "" {
				t.Errorf("before, error, set errors, after, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsEachAlertConditionByKindAndSubject(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()
			later := alertsStart.Add(time.Hour)

			none, noneHeld, noneErr := store.AlertCondition(ctx, "held_open", "door 1")
			errs := []error{
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "held_open", Subject: "door 1", Active: true, Mailed: true, ChangedAt: alertsStart}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "held_open", Subject: "door 2", Active: true, ChangedAt: alertsStart}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "config_mismatch", Subject: "door 1", Active: true, ChangedAt: alertsStart}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "held_open", Subject: "door 1", Mailed: true, ChangedAt: later}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "offline", Subject: "door 1", Active: true, Owed: true, ChangedAt: alertsStart,
					Name: "Lobby", Detail: "Not connected."}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "unresponsive", Subject: "door 1", ChangedAt: alertsStart}),
				store.PutAlertCondition(ctx, connector.StoredCondition{Kind: "unresponsive", Subject: "door 1", Active: true, Muted: true, ChangedAt: alertsStart}),
			}
			one, oneHeld, oneErr := store.AlertCondition(ctx, "held_open", "door 1")
			other, otherHeld, otherErr := store.AlertCondition(ctx, "config_mismatch", "door 1")
			owed, owedHeld, owedErr := store.AlertCondition(ctx, "offline", "door 1")
			muted, mutedHeld, mutedErr := store.AlertCondition(ctx, "unresponsive", "door 1")

			want := []any{
				connector.StoredCondition{}, false, nil, []error{nil, nil, nil, nil, nil, nil, nil},
				connector.StoredCondition{Kind: "held_open", Subject: "door 1", Mailed: true, ChangedAt: later}, true, nil,
				connector.StoredCondition{Kind: "config_mismatch", Subject: "door 1", Active: true, ChangedAt: alertsStart}, true, nil,
				connector.StoredCondition{Kind: "offline", Subject: "door 1", Active: true, Owed: true, ChangedAt: alertsStart, Name: "Lobby", Detail: "Not connected."}, true, nil,
				connector.StoredCondition{Kind: "unresponsive", Subject: "door 1", Active: true, Muted: true, ChangedAt: alertsStart}, true, nil,
			}
			got := []any{none, noneHeld, noneErr, errs, one, oneHeld, oneErr, other, otherHeld, otherErr, owed, owedHeld, owedErr, muted, mutedHeld, mutedErr}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("none, one, other (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheActiveAlertConditionsByKindAndSubject(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()
			none, noneErr := store.ActiveAlertConditions(ctx)
			for _, c := range []connector.StoredCondition{
				{Kind: "offline", Subject: "door 2", Active: true, Mailed: true, ChangedAt: alertsStart},
				{Kind: "held_open", Subject: "door 1", ChangedAt: alertsStart},
				{Kind: "offline", Subject: "door 10", Active: true, ChangedAt: alertsStart},
				{Kind: "config_mismatch", Subject: "door 3", Active: true, ChangedAt: alertsStart},
			} {
				if err := store.PutAlertCondition(ctx, c); err != nil {
					t.Fatal(err)
				}
			}

			active, err := store.ActiveAlertConditions(ctx)

			want := []any{[]connector.StoredCondition{}, nil, []connector.StoredCondition{
				{Kind: "config_mismatch", Subject: "door 3", Active: true, ChangedAt: alertsStart},
				{Kind: "offline", Subject: "door 10", Active: true, ChangedAt: alertsStart},
				{Kind: "offline", Subject: "door 2", Active: true, Mailed: true, ChangedAt: alertsStart},
			}, nil}
			if diff := cmp.Diff(want, []any{none, noneErr, active, err}); diff != "" {
				t.Errorf("none, error, active, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheAlertConditionsOwedMailByKindAndSubject(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)
			ctx := context.Background()
			none, noneErr := store.OwedAlertConditions(ctx)
			for _, c := range []connector.StoredCondition{
				{Kind: "offline", Subject: "door 2", Active: true, Owed: true, ChangedAt: alertsStart, Name: "Lobby"},
				{Kind: "held_open", Subject: "door 1", Active: true, Mailed: true, ChangedAt: alertsStart},
				{Kind: "offline", Subject: "door 10", Active: true, Owed: true, ChangedAt: alertsStart},
				{Kind: "certificate", Subject: "a:443 chain", Owed: true, ChangedAt: alertsStart, Detail: "Changed."},
			} {
				if err := store.PutAlertCondition(ctx, c); err != nil {
					t.Fatal(err)
				}
			}

			owed, err := store.OwedAlertConditions(ctx)

			want := []any{[]connector.StoredCondition{}, nil, []connector.StoredCondition{
				{Kind: "certificate", Subject: "a:443 chain", Owed: true, ChangedAt: alertsStart, Detail: "Changed."},
				{Kind: "offline", Subject: "door 10", Active: true, Owed: true, ChangedAt: alertsStart},
				{Kind: "offline", Subject: "door 2", Active: true, Owed: true, ChangedAt: alertsStart, Name: "Lobby"},
			}, nil}
			if diff := cmp.Diff(want, []any{none, noneErr, owed, err}); diff != "" {
				t.Errorf("none, error, owed, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheOutboxsMessagesOfAStatusOldestFirst(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := outboxWorld(t, impl)

			pending, err := store.MailWithStatus(context.Background(), "pending")
			failed, failedErr := store.MailWithStatus(context.Background(), "failed")

			attempted := alertsStart.Add(90 * time.Second)
			want := []any{[]connector.StoredMail{
				{ID: 1, Kind: "held_open", Subject: "Held", Text: "Open.\n", To: []string{"ops@example.com"}, Status: "pending",
					CreatedAt: alertsStart, NextAttemptAt: alertsStart},
				{ID: 3, Kind: "held_open", Subject: "Held again", Text: "Again.\n", To: []string{"desk@example.com"}, Status: "pending", Attempts: 2,
					CreatedAt: alertsStart.Add(2 * time.Minute), NextAttemptAt: alertsStart.Add(2 * time.Minute), LastAttemptAt: &attempted, LastError: "421 Busy"},
			}, nil, []connector.StoredMail{}, nil}
			if diff := cmp.Diff(want, []any{pending, err, failed, failedErr}); diff != "" {
				t.Errorf("pending, error, failed, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsTheNewestMessagesOfTheOutboxUpToALimit(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := outboxWorld(t, impl)

			two, err := store.RecentMail(context.Background(), 2)
			all, allErr := store.RecentMail(context.Background(), 10)

			ids := func(mail []connector.StoredMail) []int64 {
				out := []int64{}
				for _, m := range mail {
					out = append(out, m.ID)
				}
				return out
			}
			want := []any{[]int64{3, 2}, nil, []int64{3, 2, 1}, nil}
			if diff := cmp.Diff(want, []any{ids(two), err, ids(all), allErr}); diff != "" {
				t.Errorf("two, error, all, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesOnlyWhatSendingAMessageChanges(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := outboxWorld(t, impl)
			ctx := context.Background()
			at := alertsStart.Add(time.Hour)

			found, err := store.UpdateMail(ctx, connector.StoredMail{
				ID: 1, Kind: "changed", Subject: "Changed", Text: "Changed.\n", To: []string{"x@example.com"}, Status: "failed", Attempts: 6,
				CreatedAt: at, NextAttemptAt: at.Add(time.Minute), LastAttemptAt: &at, SentAt: &at, LastError: "550 No",
			})
			missing, missingErr := store.UpdateMail(ctx, connector.StoredMail{ID: 9, Status: "sent"})
			got, readErr := store.MailWithStatus(ctx, "failed")

			want := []any{true, nil, false, nil, []connector.StoredMail{{
				ID: 1, Kind: "held_open", Subject: "Held", Text: "Open.\n", To: []string{"ops@example.com"}, Status: "failed", Attempts: 6,
				CreatedAt: alertsStart, NextAttemptAt: at.Add(time.Minute), LastAttemptAt: &at, SentAt: &at, LastError: "550 No",
			}}, nil}
			if diff := cmp.Diff(want, []any{found, err, missing, missingErr, got, readErr}); diff != "" {
				t.Errorf("found, error, missing, error, message, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAMessageWithNoRecipientList(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)

			_, err := store.AddMail(context.Background(), connector.StoredMail{Kind: "held_open", Status: "pending", CreatedAt: alertsStart, NextAttemptAt: alertsStart})

			if err == nil {
				t.Error("a message with a nil recipient list was stored")
			}
		})
	}
}
