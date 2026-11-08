package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// start builds the application from f, failing the test if startup fails.
func start(t *testing.T, f *testsupport.Factory) *app.App {
	t.Helper()
	a, err := app.New(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStartupMigratesTheSchemaAndSaysSo(t *testing.T) {
	f := testsupport.NewFactory()

	start(t, f)

	want := []testsupport.LogEntry{{Level: "info", Message: "schema migrated"}}
	if diff := cmp.Diff(want, f.LogStub.Entries); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestStartupFailsWhenTheSchemaCannotBeMigrated(t *testing.T) {
	f := testsupport.NewFactory()
	f.StoreStub.FailMigration()

	_, err := app.New(t.Context(), f)

	if diff := cmp.Diff("migrate schema: stub: unavailable", err.Error()); diff != "" {
		t.Errorf("error (-want +got):\n%s", diff)
	}
}

func TestHealthzReportsOK(t *testing.T) {
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/healthz")

	want := testsupport.Response{Status: 200, Body: "ok\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
