//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/jackc/tern/v2/migrate"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/postgres"
	"github.com/dakriy/doorcode/internal/driver/postgres/migrations"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// person is a user with only the fields every user has.
func person(email, displayName string) connector.StoredUser {
	return connector.StoredUser{Email: email, DisplayName: displayName, CreatedAt: time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)}
}

func group(slug, name string, roles ...string) connector.StoredGroup {
	return connector.StoredGroup{Slug: slug, Name: name, Description: "About " + name, Roles: append([]string{}, roles...),
		PreassignedUserIDs: []string{"100", "200"}}
}

// mustCreateUser stores u in groups and returns its id.
func mustCreateUser(t *testing.T, store connector.Store, u connector.StoredUser, groups ...int64) int64 {
	t.Helper()
	id, err := store.CreateUser(context.Background(), u, groups)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustCreateGroup(t *testing.T, store connector.Store, g connector.StoredGroup) int64 {
	t.Helper()
	id, err := store.CreateGroup(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustUser(t *testing.T, store connector.Store, id int64) connector.StoredUser {
	t.Helper()
	u, found, err := store.User(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("user %d: found %v, error %v", id, found, err)
	}
	return u
}

func TestStoreSeedsTheAdministratorsGroup(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, err := migrated(t, impl).Groups(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]connector.StoredGroup{testsupport.AdministratorsGroup}, got); diff != "" {
				t.Errorf("groups (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreCreatesTheFirstUserWithoutGroupsWhenTheSlugNamesNone(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)

			created, err := store.CreateFirstUser(ctx, person("a@example.com", "A"), "nobody")
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]any{true, asFirst(person("a@example.com", "A"), nil)}, []any{created, mustUser(t, store, 1)}); diff != "" {
				t.Errorf("created, user (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMigrationMovesTheAdminRoleIntoTheAdministratorsGroup(t *testing.T) {
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
	if err := m.MigrateTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ email, roles string }{{"admin@example.com", "{admin}"}, {"plain@example.com", "{}"}} {
		if _, err := conn.Exec(ctx, `insert into users (email, display_name, roles, created_at) values ($1, 'X', $2, now())`,
			row.email, row.roles); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, _, errAdmin := store.UserByEmail(ctx, "admin@example.com")
	plain, _, errPlain := store.UserByEmail(ctx, "plain@example.com")

	if diff := cmp.Diff([]any{adminGroups, []connector.UserGroup(nil), "<nil>", "<nil>"},
		[]any{admin.Groups, plain.Groups, testsupport.ErrorText(errAdmin), testsupport.ErrorText(errPlain)}); diff != "" {
		t.Errorf("groups and errors (-want +got):\n%s", diff)
	}
}

func TestStoreCreatesAUserInItsGroupsListedByName(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withAdmin(t, impl)
			zebra := mustCreateGroup(t, store, group("zebra", "zebra", "view-logs"))
			apple := mustCreateGroup(t, store, group("apple", "Apple"))
			u := storedUser("ann@example.com")

			id := mustCreateUser(t, store, u, zebra, apple)

			want := u
			want.ID = 2
			want.Groups = []connector.UserGroup{
				{ID: apple, Slug: "apple", Name: "Apple", Roles: []string{}},
				{ID: zebra, Slug: "zebra", Name: "zebra", Roles: []string{"view-logs"}},
			}
			if diff := cmp.Diff([]any{int64(2), want}, []any{id, mustUser(t, store, id)}); diff != "" {
				t.Errorf("id, user (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsAUserByEachUniqueField(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			u := storedUser("ann@example.com")
			mustCreateUser(t, store, u)

			byEmail, foundEmail, errEmail := store.UserByEmail(ctx, "ann@example.com")
			byUserID, foundUserID, errUserID := store.UserByUserID(ctx, *u.UserID)
			byCode, foundCode, errCode := store.UserByCodeHash(ctx, *u.CodeHash)

			if diff := cmp.Diff([]any{int64(2), int64(2), int64(2), true, true, true, "<nil>", "<nil>", "<nil>"},
				[]any{byEmail.ID, byUserID.ID, byCode.ID, foundEmail, foundUserID, foundCode,
					testsupport.ErrorText(errEmail), testsupport.ErrorText(errUserID), testsupport.ErrorText(errCode)}); diff != "" {
				t.Errorf("ids, found, errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsNoUserForAnUnknownValue(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)

			_, foundID, errID := store.User(ctx, 9)
			_, foundUserID, errUserID := store.UserByUserID(ctx, "nobody")
			_, foundCode, errCode := store.UserByCodeHash(ctx, "nothing")

			if diff := cmp.Diff([]any{false, false, false, "<nil>", "<nil>", "<nil>"}, []any{foundID, foundUserID, foundCode,
				testsupport.ErrorText(errID), testsupport.ErrorText(errUserID), testsupport.ErrorText(errCode)}); diff != "" {
				t.Errorf("found, errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesASecondUserWithTheSameUniqueValue(t *testing.T) {
	other := "other"
	cases := map[string]func(u connector.StoredUser) connector.StoredUser{
		"email": func(u connector.StoredUser) connector.StoredUser { u.UserID, u.CodeHash = &other, nil; return u },
		"user id": func(u connector.StoredUser) connector.StoredUser {
			u.Email, u.CodeHash = "x@example.com", nil
			return u
		},
		"code hash": func(u connector.StoredUser) connector.StoredUser {
			u.Email, u.UserID = "x@example.com", &other
			return u
		},
	}
	for _, impl := range storeImpls {
		for name, change := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				store := withAdmin(t, impl)

				_, err := store.CreateUser(context.Background(), change(storedUser("admin@example.com")), nil)

				if err == nil {
					t.Error("CreateUser stored a duplicate " + name)
				}
			})
		}
	}
}

func TestStoreRefusesAUserInAGroupThatDoesNotExist(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)

			_, err := store.CreateUser(ctx, person("ann@example.com", "Ann"), []int64{7})
			_, found, _ := store.UserByEmail(ctx, "ann@example.com")

			if err == nil || found {
				t.Errorf("CreateUser for group 7: error %v, stored %v", err, found)
			}
		})
	}
}

// directory returns a store holding the administrator (id 1) and, by
// display name, Bo (2, deleted), cara (3), Ann (4), and Dan (5).
func directory(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := withAdmin(t, impl)
	bo := person("bo@example.com", "Bo")
	deleted := time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC)
	bo.DeletedAt = &deleted
	userID := "000417"
	cara := person("cara@example.com", "cara")
	cara.UserID = &userID
	ann := person("ann@example.com", "Ann")
	ann.FirstName, ann.LastName = "Ann", "Smith"
	for _, u := range []connector.StoredUser{bo, cara, ann, person("dan@example.com", "Dan")} {
		mustCreateUser(t, store, u)
	}
	return store
}

func ids(users []connector.StoredUser) []int64 {
	out := []int64{}
	for _, u := range users {
		out = append(out, u.ID)
	}
	return out
}

func TestStoreListsAPageOfUsers(t *testing.T) {
	cases := map[string]struct {
		query connector.UserQuery
		ids   []int64
		total int
	}{
		"everyone not deleted, by display name": {query: connector.UserQuery{Limit: 10}, ids: []int64{1, 4, 3, 5}, total: 4},
		"one page":                              {query: connector.UserQuery{Offset: 1, Limit: 2}, ids: []int64{4, 3}, total: 4},
		"past the end":                          {query: connector.UserQuery{Offset: 9, Limit: 2}, ids: []int64{}, total: 4},
		"deleted":                               {query: connector.UserQuery{Deleted: true, Limit: 10}, ids: []int64{2}, total: 1},
		"by email, ignoring case":               {query: connector.UserQuery{Search: "DAN@", Limit: 10}, ids: []int64{5}, total: 1},
		"by user id":                            {query: connector.UserQuery{Search: "0417", Limit: 10}, ids: []int64{3}, total: 1},
		"across first and last name":            {query: connector.UserQuery{Search: "ann smith", Limit: 10}, ids: []int64{4}, total: 1},
		"by display name":                       {query: connector.UserQuery{Search: "administrator", Limit: 10}, ids: []int64{1}, total: 1},
		"matching nobody":                       {query: connector.UserQuery{Search: "zzz", Limit: 10}, ids: []int64{}, total: 0},
	}
	for _, impl := range storeImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				got, total, err := directory(t, impl).Users(context.Background(), c.query)

				if diff := cmp.Diff([]any{c.ids, c.total, "<nil>"}, []any{ids(got), total, testsupport.ErrorText(err)}); diff != "" {
					t.Errorf("ids, total, error (-want +got):\n%s", diff)
				}
			})
		}
	}
}

func TestStoreListsUsersWithTheirGroups(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, _, err := withAdmin(t, impl).Users(context.Background(), connector.UserQuery{Limit: 1})
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]connector.StoredUser{asFirst(storedUser("admin@example.com"), adminGroups)}, got); diff != "" {
				t.Errorf("users (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesOnlyAUsersProfile(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			userID := "200"
			expires := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
			changed := connector.StoredUser{ID: 1, Email: "new@example.com", UserID: &userID, FirstName: "N", LastName: "W",
				DisplayName: "New", ExpiresAt: &expires, CreatedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}

			found, err := store.UpdateUser(ctx, changed)
			if err != nil {
				t.Fatal(err)
			}
			missing, errMissing := store.UpdateUser(ctx, connector.StoredUser{ID: 9, Email: "nine@example.com"})

			want := asFirst(storedUser("admin@example.com"), adminGroups)
			want.Email, want.UserID, want.FirstName, want.LastName = "new@example.com", &userID, "N", "W"
			want.DisplayName, want.ExpiresAt = "New", &expires
			if diff := cmp.Diff([]any{true, false, "<nil>", want}, []any{found, missing, testsupport.ErrorText(errMissing), mustUser(t, store, 1)}); diff != "" {
				t.Errorf("found, missing, error, user (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnUpdateToAnotherUsersEmail(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := directory(t, impl)
			u := mustUser(t, store, 3)
			u.Email = "dan@example.com"

			_, err := store.UpdateUser(context.Background(), u)

			if err == nil {
				t.Error("UpdateUser stored a duplicate email")
			}
		})
	}
}

func TestStoreDeletesRestoresAndCodesAUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := directory(t, impl)
			at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
			hash := "hash-of-123456"
			password := "hash-of-a-password"

			results := []any{}
			for _, change := range []func() (bool, error){
				func() (bool, error) { return store.SetUserDeleted(ctx, 3, &at) },
				func() (bool, error) { return store.SetUserDeleted(ctx, 2, nil) },
				func() (bool, error) { return store.SetCodeHash(ctx, 4, &hash) },
				func() (bool, error) { return store.SetCodeHash(ctx, 3, nil) },
				func() (bool, error) { return store.SetUserDeleted(ctx, 9, &at) },
				func() (bool, error) { return store.SetCodeHash(ctx, 9, &hash) },
				func() (bool, error) { return store.SetPasswordHash(ctx, 4, password) },
				func() (bool, error) { return store.SetPasswordHash(ctx, 9, password) },
			} {
				found, err := change()
				results = append(results, found, testsupport.ErrorText(err))
			}

			want := []any{true, "<nil>", true, "<nil>", true, "<nil>", true, "<nil>", false, "<nil>", false, "<nil>", true, "<nil>", false, "<nil>"}
			if diff := cmp.Diff(want, results); diff != "" {
				t.Errorf("found and errors (-want +got):\n%s", diff)
			}
			got := []any{mustUser(t, store, 3).DeletedAt, mustUser(t, store, 2).DeletedAt, mustUser(t, store, 4).CodeHash, mustUser(t, store, 4).PasswordHash}
			if diff := cmp.Diff([]any{&at, (*time.Time)(nil), &hash, &password}, got); diff != "" {
				t.Errorf("deleted at, deleted at, code hash, password hash (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAnotherUsersCodeHash(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withAdmin(t, impl)
			id := mustCreateUser(t, store, person("ann@example.com", "Ann"))

			_, err := store.SetCodeHash(context.Background(), id, storedUser("admin@example.com").CodeHash)

			if err == nil {
				t.Error("SetCodeHash stored a duplicate hash")
			}
		})
	}
}

func TestStoreKeepsTheFirstCodeHashingParameters(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := migrated(t, impl)

			first, errFirst := store.CodeHashing(ctx, "first")
			second, errSecond := store.CodeHashing(ctx, "second")

			if diff := cmp.Diff([]string{"first", "first", "<nil>", "<nil>"},
				[]string{first, second, testsupport.ErrorText(errFirst), testsupport.ErrorText(errSecond)}); diff != "" {
				t.Errorf("parameters and errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreCreatesFindsAndListsGroupsByName(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			night := group("night", "night staff", "view-logs", "door-commander")

			id := mustCreateGroup(t, store, night)
			all, errAll := store.Groups(ctx)
			byID, foundID, errID := store.Group(ctx, id)
			bySlug, foundSlug, errSlug := store.GroupBySlug(ctx, "night")
			_, foundNone, errNone := store.GroupBySlug(ctx, "day")

			admins := testsupport.AdministratorsGroup
			admins.MemberCount = 1
			night.ID = 2
			if diff := cmp.Diff([]any{int64(2), []connector.StoredGroup{admins, night}, night, night, true, true, false},
				[]any{id, all, byID, bySlug, foundID, foundSlug, foundNone}); diff != "" {
				t.Errorf("id, groups, found (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]string{"<nil>", "<nil>", "<nil>", "<nil>"}, []string{testsupport.ErrorText(errAll),
				testsupport.ErrorText(errID), testsupport.ErrorText(errSlug), testsupport.ErrorText(errNone)}); diff != "" {
				t.Errorf("errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesASecondGroupWithTheSameSlug(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := migrated(t, impl)

			_, err := store.CreateGroup(context.Background(), group("administrators", "Other"))

			if err == nil {
				t.Error("CreateGroup stored a duplicate slug")
			}
		})
	}
}

func TestStoreUpdatesAGroup(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)
			changed := group("admins", "Admins", "admin", "view-logs")
			changed.ID = 1
			changed.PreassignedUserIDs = []string{}

			found, err := store.UpdateGroup(ctx, changed)
			missing, errMissing := store.UpdateGroup(ctx, connector.StoredGroup{ID: 9, Slug: "nine", Roles: []string{}, PreassignedUserIDs: []string{}})
			got, _, _ := store.Group(ctx, 1)

			changed.MemberCount = 1
			if diff := cmp.Diff([]any{true, false, "<nil>", "<nil>", changed},
				[]any{found, missing, testsupport.ErrorText(err), testsupport.ErrorText(errMissing), got}); diff != "" {
				t.Errorf("found, errors, group (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesAGroupWithItsMemberships(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)

			deleted, err := store.DeleteGroup(ctx, 1)
			again, errAgain := store.DeleteGroup(ctx, 1)
			groups, _ := store.Groups(ctx)

			if diff := cmp.Diff([]any{true, false, "<nil>", "<nil>", []connector.UserGroup(nil)},
				[]any{deleted, again, testsupport.ErrorText(err), testsupport.ErrorText(errAgain), mustUser(t, store, 1).Groups}); diff != "" {
				t.Errorf("deleted, errors, admin's groups (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff([]connector.StoredGroup{}, groups, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("groups (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreAddsListsAndRemovesMembers(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := directory(t, impl)
			id := mustCreateGroup(t, store, group("night", "Night"))

			var errs []string
			for _, user := range []int64{5, 4, 5, 2} {
				errs = append(errs, testsupport.ErrorText(store.AddMember(ctx, id, user)))
			}
			before, errBefore := store.GroupMembers(ctx, id)
			removed, errRemoved := store.RemoveMember(ctx, id, 5)
			again, errAgain := store.RemoveMember(ctx, id, 5)
			after, errAfter := store.GroupMembers(ctx, id)
			counted, _, _ := store.Group(ctx, id)

			if diff := cmp.Diff([]any{[]int64{4, 2, 5}, true, false, []int64{4, 2}, 2},
				[]any{ids(before), removed, again, ids(after), counted.MemberCount}); diff != "" {
				t.Errorf("members, removed (-want +got):\n%s", diff)
			}
			errs = append(errs, testsupport.ErrorText(errBefore), testsupport.ErrorText(errRemoved),
				testsupport.ErrorText(errAgain), testsupport.ErrorText(errAfter))
			if diff := cmp.Diff([]string{"<nil>", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>", "<nil>"}, errs); diff != "" {
				t.Errorf("errors (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsMembersWithTheirGroups(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			got, err := withAdmin(t, impl).GroupMembers(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]connector.StoredUser{asFirst(storedUser("admin@example.com"), adminGroups)}, got); diff != "" {
				t.Errorf("members (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesAMembershipOfSomethingThatDoesNotExist(t *testing.T) {
	cases := map[string][2]int64{"group": {9, 1}, "user": {1, 9}}
	for _, impl := range storeImpls {
		for name, c := range cases {
			t.Run(impl.name+"/"+name, func(t *testing.T) {
				err := withAdmin(t, impl).AddMember(context.Background(), c[0], c[1])

				if err == nil {
					t.Error("AddMember succeeded for a " + name + " that does not exist")
				}
			})
		}
	}
}
