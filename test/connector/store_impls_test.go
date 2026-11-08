package connector_test

import (
	"context"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/postgres"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// testDatabaseEnv names the variable holding the URL of a disposable local
// Postgres, such as the one docker-compose.yml starts.
const testDatabaseEnv = "DOORCODE_TEST_DATABASE_URL"

// storeImpl builds an empty store whose schema has not been migrated.
type storeImpl struct {
	name  string
	fresh func(t *testing.T) connector.Store
}

var storeImpls = []storeImpl{
	{
		name:  "stub",
		fresh: func(*testing.T) connector.Store { return testsupport.NewStore() },
	},
	{
		name:  "postgres",
		fresh: freshPostgres,
	},
}

// freshPostgres opens a store on a new, empty schema of the test database and
// drops the schema when the test ends.
func freshPostgres(t *testing.T) connector.Store {
	t.Helper()
	base := os.Getenv(testDatabaseEnv)
	if base == "" {
		t.Fatalf("%s must name a disposable Postgres database (see AGENTS.md)", testDatabaseEnv)
	}
	ctx := context.Background()
	schema := "test_" + hex.EncodeToString(cryptoentropy.Source{}.Bytes(8))

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close(ctx) })
	if _, err := admin.Exec(ctx, "create schema "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "drop schema "+schema+" cascade") })

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	store, err := postgres.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

// migrated returns a fresh store with its schema in place.
func migrated(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := impl.fresh(t)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}
