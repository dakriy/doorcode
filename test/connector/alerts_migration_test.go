//go:build slow

package connector_test

import (
	"context"
	"testing"

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

func TestMigrationRemovesTheForcedOpenAlertButNotItsMailAlreadyTriedNorOtherKinds(t *testing.T) {
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
		map[string]bool{"held_open": true}, []string{"held_open"}, []connector.StoredCondition{},
		[][]string{{"held_open", "Held", "pending"}, {"forced_open", "Given up", "failed"}, {"forced_open", "Sent", "sent"}},
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
