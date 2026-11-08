package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// adminEmail is the first administrator's email when no setting changes it.
const adminEmail = "admin@example.com"

// firstAdminMessage is the log line that hands the operator the first
// administrator's password.
const firstAdminMessage = "created the first administrator; sign in with this password and change it"

// start builds the application from f, failing the test if startup fails.
func start(t *testing.T, f *testsupport.Factory) *app.App {
	t.Helper()
	a, err := app.New(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// startBrowser starts the application from f and returns a browser on it
// with no cookies.
func startBrowser(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	return testsupport.NewBrowser(start(t, f).Handler())
}

// adminPassword is the first administrator's password, read from the log
// where the operator would find it.
func adminPassword(t *testing.T, f *testsupport.Factory) string {
	t.Helper()
	for _, e := range f.LogStub.Entries {
		if e.Message == firstAdminMessage {
			return e.Fields["password"]
		}
	}
	t.Fatal("startup logged no administrator password")
	return ""
}

// signIn signs a new browser on h in as the first administrator.
func signIn(t *testing.T, f *testsupport.Factory, h testsupport.Handler) *testsupport.Browser {
	t.Helper()
	b := testsupport.NewBrowser(h)
	got := b.Post("/sign-in", "email", adminEmail, "password", adminPassword(t, f))
	if got.Status != 303 {
		t.Fatalf("sign-in answered %d, want 303:\n%s", got.Status, got.Body)
	}
	return b
}

// adminSession starts the application from f and returns its handler and a
// browser on it signed in as the first administrator.
func adminSession(t *testing.T, f *testsupport.Factory) (testsupport.Handler, *testsupport.Browser) {
	t.Helper()
	h := start(t, f).Handler()
	return h, signIn(t, f, h)
}

// signedInAdmin starts the application from f and returns a browser signed
// in as the first administrator.
func signedInAdmin(t *testing.T, f *testsupport.Factory) *testsupport.Browser {
	t.Helper()
	_, b := adminSession(t, f)
	return b
}

func TestFirstStartCreatesAnAdministratorAndLogsItsPassword(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()

	start(t, f)

	want := []testsupport.LogEntry{
		{Level: "info", Message: "schema migrated"},
		{Level: "info", Message: firstAdminMessage,
			Fields: map[string]string{"email": adminEmail, "password": "AAECAwQFBgcICQoLDA0ODxAR"}},
	}
	if diff := cmp.Diff(want, f.LogStub.Entries); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestFirstAdministratorEmailComesFromTheSettingLowercased(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.EnvStub["DOORCODE_ADMIN_EMAIL"] = " Ops@Example.COM "
	h := start(t, f).Handler()

	got := testsupport.NewBrowser(h).Post("/sign-in", "email", "ops@example.com", "password", adminPassword(t, f))

	if diff := cmp.Diff("ops@example.com", f.LogStub.Entries[1].Fields["email"]); diff != "" {
		t.Errorf("logged email (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(303, got.Status); diff != "" {
		t.Errorf("sign-in status (-want +got):\n%s", diff)
	}
}

func TestStartupWithExistingUsersOnlyMigrates(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	f.StoreStub.SeedUser("someone@example.com", "Someone")

	start(t, f)

	want := []testsupport.LogEntry{{Level: "info", Message: "schema migrated"}}
	if diff := cmp.Diff(want, f.LogStub.Entries); diff != "" {
		t.Errorf("log (-want +got):\n%s", diff)
	}
}

func TestSecondStartKeepsTheFirstAdministratorsPassword(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	start(t, f)
	password := adminPassword(t, f)

	got := startBrowser(t, f).Post("/sign-in", "email", adminEmail, "password", password)

	if diff := cmp.Diff(303, got.Status); diff != "" {
		t.Errorf("sign-in status (-want +got):\n%s", diff)
	}
}

// secretKeyRefusal is why the application does not start without a key
// for its secret settings.
const secretKeyRefusal = "DOORCODE_SECRET_KEY must be 32 random bytes in base64, such as the output of openssl rand -base64 32; " +
	"it encrypts the secret settings the database holds"

func TestStartupFails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		want    string
	}{
		"when no secret key is set": {
			arrange: func(f *testsupport.Factory) { delete(f.EnvStub, "DOORCODE_SECRET_KEY") },
			want:    secretKeyRefusal,
		},
		"when the secret key is not base64": {
			arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_SECRET_KEY"] = "not base64!" },
			want:    secretKeyRefusal,
		},
		"when the secret key is not 32 bytes": {
			arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_SECRET_KEY"] = "MDEyMzQ1Njc4OWFiY2RlZg==" },
			want:    secretKeyRefusal,
		},
		"when the schema cannot be migrated": {
			arrange: func(f *testsupport.Factory) { f.StoreStub.FailMigration() },
			want:    "migrate schema: stub: unavailable",
		},
		"when settings cannot be read": {
			arrange: func(f *testsupport.Factory) { f.StoreStub.FailSettings() },
			want:    "create first administrator: stub: unavailable",
		},
		"when users cannot be stored": {
			arrange: func(f *testsupport.Factory) { f.StoreStub.FailUsers() },
			want:    "create first administrator: stub: unavailable",
		},
		"when the site timezone names no zone": {
			arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Mars/Olympus" },
			want: "create first administrator: invalid value for site_timezone: " +
				"must be a time zone name from the tz database, such as UTC or Europe/Berlin",
		},
		"when the password hashing cost is not a number": {
			arrange: func(f *testsupport.Factory) { f.EnvStub["DOORCODE_PASSWORD_HASH_MEMORY_KIB"] = "lots" },
			want: "create first administrator: invalid value for password_hash_memory_kib: " +
				"must be a whole number of KiB from 8 to 1048576",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := testsupport.NewFactory()
			c.arrange(f)

			_, err := app.New(t.Context(), f)

			if diff := cmp.Diff(c.want, testsupport.ErrorText(err)); diff != "" {
				t.Errorf("error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestHealthzReportsOK(t *testing.T) {
	t.Parallel()
	got := testsupport.Get(start(t, testsupport.NewFactory()).Handler(), "/healthz")

	want := testsupport.Response{Status: 200, ContentType: "text/plain; charset=utf-8", Body: "ok\n"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("response (-want +got):\n%s", diff)
	}
}
