//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func TestStoreReportsAnErrorBeforeTheSchemaIsMigrated(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			_, err := impl.fresh(t).Settings(context.Background())

			if err == nil {
				t.Error("Settings succeeded on an unmigrated store")
			}
		})
	}
}

func TestStoreMigrationCanRunAgain(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)

			if err := store.Migrate(context.Background()); err != nil {
				t.Errorf("second Migrate: %v", err)
			}
		})
	}
}

func TestStoreStartsWithNoSettings(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, err := migrated(t, impl).Settings(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(map[string]connector.StoredSetting{}, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReturnsEachPutSettingWithItsTime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			nameAt := time.Date(2026, 3, 1, 9, 30, 15, 123456000, time.UTC)
			zoneAt := time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)

			if err := store.PutSetting(ctx, "site_name", "North Campus", nameAt); err != nil {
				t.Fatal(err)
			}
			if err := store.PutSetting(ctx, "site_timezone", "Asia/Tokyo", zoneAt); err != nil {
				t.Fatal(err)
			}
			got, err := store.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}

			want := map[string]connector.StoredSetting{
				"site_name":     {Value: "North Campus", UpdatedAt: nameAt},
				"site_timezone": {Value: "Asia/Tokyo", UpdatedAt: zoneAt},
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStorePutReplacesTheEarlierValue(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			later := time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

			if err := store.PutSetting(ctx, "site_name", "Old", time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)); err != nil {
				t.Fatal(err)
			}
			if err := store.PutSetting(ctx, "site_name", "New", later); err != nil {
				t.Fatal(err)
			}
			got, err := store.Settings(ctx)
			if err != nil {
				t.Fatal(err)
			}

			want := map[string]connector.StoredSetting{"site_name": {Value: "New", UpdatedAt: later}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("settings (-want +got):\n%s", diff)
			}
		})
	}
}

func storedUser(email string) connector.StoredUser {
	hash := "$argon2id$v=19$m=8,t=2,p=1$c2FsdA$a2V5"
	code := "$argon2id$v=19$m=8,t=2,p=1$c2FsdA$Y29kZQ:" + email
	userID := "id-" + email
	expires := time.Date(2031, 1, 5, 9, 0, 0, 0, time.UTC)
	return connector.StoredUser{
		Email:        email,
		UserID:       &userID,
		FirstName:    "Ada",
		LastName:     "Admin",
		DisplayName:  "Administrator",
		PasswordHash: &hash,
		CodeHash:     &code,
		ExpiresAt:    &expires,
		CreatedAt:    time.Date(2026, 1, 5, 9, 0, 0, 123456000, time.UTC),
	}
}

// adminGroups is what a user in only the seeded administrators group lists
// as their groups.
var adminGroups = []connector.UserGroup{{ID: 1, Slug: "administrators", Name: "Administrators", Roles: []string{"admin"}}}

// asFirst is u as the store returns the first user, a member of groups.
func asFirst(u connector.StoredUser, groups []connector.UserGroup) connector.StoredUser {
	u.ID = 1
	u.Groups = groups
	return u
}

func TestStoreCreatesTheFirstUserWithAnIDInTheNamedGroup(t *testing.T) {
	bare := connector.StoredUser{Email: "bare@example.com", DisplayName: "Bare",
		CreatedAt: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)}
	deletedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	deleted := storedUser("gone@example.com")
	deleted.DeletedAt = &deletedAt
	users := map[string]connector.StoredUser{
		"every field":       storedUser("admin@example.com"),
		"no optional field": bare,
		"deleted":           deleted,
	}
	for _, impl := range storeImpls {
		for name, u := range users {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				ctx := context.Background()
				store := migrated(t, impl)

				created, err := store.CreateFirstUser(ctx, u, "administrators")
				if err != nil {
					t.Fatal(err)
				}
				got, found, err := store.UserByEmail(ctx, u.Email)
				if err != nil {
					t.Fatal(err)
				}

				want := asFirst(u, adminGroups)
				if diff := cmp.Diff([]any{true, true, want}, []any{created, found, got}); diff != "" {
					t.Errorf("created, found, user (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestStoreCreatesNoSecondFirstUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			if _, err := store.CreateFirstUser(ctx, storedUser("first@example.com"), "administrators"); err != nil {
				t.Fatal(err)
			}

			created, err := store.CreateFirstUser(ctx, storedUser("second@example.com"), "administrators")
			if err != nil {
				t.Fatal(err)
			}
			_, found, err := store.UserByEmail(ctx, "second@example.com")
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]bool{false, false}, []bool{created, found}); diff != "" {
				t.Errorf("created, found (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsNoUserForAnUnknownEmail(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)
			if _, err := store.CreateFirstUser(ctx, storedUser("admin@example.com"), "administrators"); err != nil {
				t.Fatal(err)
			}

			got, found, err := store.UserByEmail(ctx, "Admin@example.com")

			if diff := cmp.Diff([]any{connector.StoredUser{}, false, "<nil>"}, []any{got, found, testsupport.ErrorText(err)}); diff != "" {
				t.Errorf("user, found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func session(token string) connector.StoredSession {
	return connector.StoredSession{
		TokenHash: []byte(token),
		UserID:    1,
		CreatedAt: time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 1, 5, 21, 0, 0, 0, time.UTC),
	}
}

// withAdmin returns a migrated store holding one user, whose id is 1, in
// the administrators group.
func withAdmin(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := migrated(t, impl)
	if _, err := store.CreateFirstUser(context.Background(), storedUser("admin@example.com"), "administrators"); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestStoreReturnsASessionWithItsUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			if err := store.CreateSession(ctx, session("one")); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateSession(ctx, session("two")); err != nil {
				t.Fatal(err)
			}

			gotSession, gotUser, found, err := store.SessionUser(ctx, []byte("two"))
			if err != nil {
				t.Fatal(err)
			}

			wantUser := asFirst(storedUser("admin@example.com"), adminGroups)
			if diff := cmp.Diff([]any{session("two"), wantUser, true}, []any{gotSession, gotUser, found}); diff != "" {
				t.Errorf("session, user, found (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreKeepsWhetherASessionBeganAtTheIdentityProvider(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			viaIdentity := session("provider")
			viaIdentity.ViaIdentity = true
			for _, s := range []connector.StoredSession{session("password"), viaIdentity} {
				if err := store.CreateSession(ctx, s); err != nil {
					t.Fatal(err)
				}
			}

			password, _, _, errPassword := store.SessionUser(ctx, []byte("password"))
			provider, _, _, errProvider := store.SessionUser(ctx, []byte("provider"))

			if diff := cmp.Diff([]any{session("password"), viaIdentity, "<nil>", "<nil>"},
				[]any{password, provider, testsupport.ErrorText(errPassword), testsupport.ErrorText(errProvider)}); diff != "" {
				t.Errorf("sessions, errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsNoSessionForAnUnknownToken(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			if err := store.CreateSession(ctx, session("one")); err != nil {
				t.Fatal(err)
			}

			_, _, found, err := store.SessionUser(ctx, []byte("other"))

			if diff := cmp.Diff([]any{false, "<nil>"}, []any{found, testsupport.ErrorText(err)}); diff != "" {
				t.Errorf("found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesOnlyTheNamedSession(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			for _, token := range []string{"one", "two"} {
				if err := store.CreateSession(ctx, session(token)); err != nil {
					t.Fatal(err)
				}
			}

			if err := store.DeleteSession(ctx, []byte("one")); err != nil {
				t.Fatal(err)
			}
			_, _, foundOne, errOne := store.SessionUser(ctx, []byte("one"))
			_, _, foundTwo, errTwo := store.SessionUser(ctx, []byte("two"))

			if diff := cmp.Diff([]any{false, true, "<nil>", "<nil>"},
				[]any{foundOne, foundTwo, testsupport.ErrorText(errOne), testsupport.ErrorText(errTwo)}); diff != "" {
				t.Errorf("found one, found two, errors (-want +got):\n%s", diff)
			}
		})
	}
}

// sessionsLasting stores a session under each token of lasting, lasting
// that many hours from when session's sessions are created.
func sessionsLasting(t *testing.T, store connector.Store, lasting map[string]int) {
	t.Helper()
	for token, hours := range lasting {
		s := session(token)
		s.ExpiresAt = s.CreatedAt.Add(time.Duration(hours) * time.Hour)
		if err := store.CreateSession(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
}

// sessionsFound is each of tokens store holds a session under.
func sessionsFound(store connector.Store, tokens ...string) []string {
	var found []string
	for _, token := range tokens {
		if _, _, ok, _ := store.SessionUser(context.Background(), []byte(token)); ok {
			found = append(found, token)
		}
	}
	return found
}

func TestStoreDeletesTheSessionsThatHaveExpired(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withAdmin(t, impl)
			sessionsLasting(t, store, map[string]int{"expired": 11, "expiring": 12, "live": 13})

			removed, err := store.DeleteExpiredSessions(context.Background(), time.Date(2026, 1, 5, 21, 0, 0, 0, time.UTC))
			kept := sessionsFound(store, "expired", "expiring", "live")

			if diff := cmp.Diff([]any{2, "<nil>", []string{"live"}}, []any{removed, testsupport.ErrorText(err), kept}); diff != "" {
				t.Errorf("removed, error, kept (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesASessionForAnUnknownUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := session("one")
			s.UserID = 2

			err := withAdmin(t, impl).CreateSession(context.Background(), s)

			if err == nil {
				t.Error("CreateSession succeeded for a user that does not exist")
			}
		})
	}
}

func TestStoreRefusesASecondSessionWithTheSameToken(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			if err := store.CreateSession(ctx, session("one")); err != nil {
				t.Fatal(err)
			}

			err := store.CreateSession(ctx, session("one"))

			if err == nil {
				t.Error("CreateSession stored a duplicate token")
			}
		})
	}
}
