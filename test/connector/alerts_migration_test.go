//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/tern/v2/migrate"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/postgres"
	"github.com/dakriy/doorcode/internal/driver/postgres/migrations"
)

// migratedTo opens a store on a new schema whose migrations are applied up
// to version, and a connection to the schema for rows the store cannot
// write at that version.
func migratedTo(t *testing.T, version int32) (connector.Store, *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	schema := freshSchema(t)
	conn := connect(t, schema)
	store, err := postgres.Open(ctx, schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	m, err := migrate.NewMigrator(ctx, conn, "schema_version")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.LoadMigrations(migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateTo(ctx, version); err != nil {
		t.Fatal(err)
	}
	return store, conn
}

// exec runs each statement on conn, failing the test when one fails.
func exec(t *testing.T, conn *pgx.Conn, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := conn.Exec(context.Background(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationsRemoveTheForcedOpenAlertAndAllItsMailButNotOtherKinds(t *testing.T) {
	ctx := context.Background()
	store, conn := migratedTo(t, 32)
	exec(t, conn,
		`insert into alert_kinds (kind, enabled) values ('forced_open', true), ('held_open', true)`,
		`insert into alert_conditions (kind, subject, active, mailed, owed, changed_at, name, detail) values
			('forced_open', 'door 1', true, false, true, '2026-01-05T09:00:00Z', 'Lobby', 'Forced.'),
			('held_open', 'door 1', true, true, false, '2026-01-05T09:00:00Z', 'Lobby', 'Held.')`,
		`insert into mail_outbox (kind, subject, body, recipients, status, attempts, created_at, next_attempt_at, last_error) values
			('forced_open', 'Waiting', '', '{ops@example.com}', 'pending', 0, '2026-01-05T09:00:00Z', '2026-01-05T09:00:00Z', ''),
			('forced_open', 'Sent', '', '{ops@example.com}', 'sent', 1, '2026-01-05T09:00:00Z', '2026-01-05T09:00:00Z', ''),
			('forced_open', 'Given up', '', '{ops@example.com}', 'failed', 6, '2026-01-05T09:00:00Z', '2026-01-05T09:00:00Z', 'refused'),
			('held_open', 'Held', '', '{ops@example.com}', 'pending', 0, '2026-01-05T09:00:00Z', '2026-01-05T09:00:00Z', '')`,
	)

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	kinds, kindsErr := store.AlertKinds(ctx)
	active, activeErr := store.ActiveAlertConditions(ctx)
	owed, owedErr := store.OwedAlertConditions(ctx)
	recent, recentErr := store.RecentMail(ctx, 10)
	var outbox [][]string
	for _, m := range recent {
		outbox = append(outbox, []string{m.Kind, m.Subject, m.Status})
	}

	want := []any{
		map[string]string{"held_open": "mail"}, []string{"held_open"}, []connector.StoredCondition{},
		[][]string{{"held_open", "Held", "pending"}},
		[]error{nil, nil, nil, nil},
	}
	var activeKinds []string
	for _, c := range active {
		activeKinds = append(activeKinds, c.Kind)
	}
	got := []any{kinds, activeKinds, owed, outbox, []error{kindsErr, activeErr, owedErr, recentErr}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("kinds stored, active kinds, owed, outbox, errors (-want +got):\n%s", diff)
	}
}

func TestMigrationSetsEachKindThatMailedToMailAndEveryOtherOnMutingWhatItOwes(t *testing.T) {
	ctx := context.Background()
	store, conn := migratedTo(t, 33)
	exec(t, conn,
		`insert into alert_kinds (kind, enabled) values ('held_open', true), ('offline', false)`,
		`insert into alert_conditions (kind, subject, active, mailed, owed, changed_at, name, detail) values
			('held_open', 'door 1', true, false, true, '2026-01-05T09:00:00Z', 'Lobby', 'Held.'),
			('offline', 'door 1', true, false, true, '2026-01-05T09:00:00Z', 'Lobby', 'Offline.'),
			('unresponsive', 'door 1', true, true, false, '2026-01-05T09:00:00Z', 'Lobby', 'Silent.'),
			('certificate', 'broker changed 1', false, false, true, '2026-01-05T09:00:00Z', 'Broker', 'Changed.')`,
	)

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	kinds, kindsErr := store.AlertKinds(ctx)
	var conditions [][]any
	for _, key := range [][2]string{{"held_open", "door 1"}, {"offline", "door 1"}, {"unresponsive", "door 1"}, {"certificate", "broker changed 1"}} {
		c, _, err := store.AlertCondition(ctx, key[0], key[1])
		conditions = append(conditions, []any{c.Kind, c.Active, c.Mailed, c.Owed, c.Muted, err})
	}

	want := []any{map[string]string{"held_open": "mail", "offline": "on"}, nil, [][]any{
		{"held_open", true, false, true, false, nil},
		{"offline", true, false, false, true, nil},
		{"unresponsive", true, true, false, false, nil},
		{"certificate", false, false, false, false, nil},
	}}
	if diff := cmp.Diff(want, []any{kinds, kindsErr, conditions}); diff != "" {
		t.Errorf("kinds, error, conditions: kind, active, mailed, owed, muted, error (-want +got):\n%s", diff)
	}
}

func TestMigrationRemovesEveryForcedOpenEventAndMailButNothingElse(t *testing.T) {
	ctx := context.Background()
	store, conn := migratedTo(t, 35)
	exec(t, conn, `insert into doors (name, location, notes, public, created_at) values ('Lobby', '', '', true, now())`)
	at := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	for _, e := range []connector.StoredEvent{
		{At: at, DoorID: 1, Type: "alarm", Outcome: "alert", Reason: "forced open", Detail: "Forced."},
		{At: at.Add(time.Minute), DoorID: 1, Type: "door", Outcome: "info", Reason: "opened"},
		{At: at.Add(2 * time.Minute), DoorID: 1, Type: "alarm", Outcome: "alert", Reason: "held open", Detail: "Held."},
	} {
		if _, err := store.AddEvent(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []connector.StoredMail{
		{Kind: "forced_open", Subject: "Sent", To: []string{"ops@example.com"}, Status: "sent", CreatedAt: at, NextAttemptAt: at},
		{Kind: "forced_open", Subject: "Given up", To: []string{"ops@example.com"}, Status: "failed", CreatedAt: at, NextAttemptAt: at},
		{Kind: "held_open", Subject: "Held", To: []string{"ops@example.com"}, Status: "sent", CreatedAt: at, NextAttemptAt: at},
	} {
		if _, err := store.AddMail(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	events, total, eventsErr := store.Events(ctx, connector.EventQuery{Limit: 10})
	recent, mailErr := store.RecentMail(ctx, 10)
	var reasons, subjects []string
	for _, e := range events {
		reasons = append(reasons, e.Reason)
	}
	for _, m := range recent {
		subjects = append(subjects, m.Kind+": "+m.Subject)
	}

	want := []any{[]string{"held open", "opened"}, 2, []string{"held_open: Held"}, []error{nil, nil}}
	if diff := cmp.Diff(want, []any{reasons, total, subjects, []error{eventsErr, mailErr}}); diff != "" {
		t.Errorf("event reasons, events, mail, errors (-want +got):\n%s", diff)
	}
}
