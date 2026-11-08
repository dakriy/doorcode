//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/testsupport"
)

func token(hash string, expires *time.Time) connector.StoredToken {
	return connector.StoredToken{
		UserID:    1,
		Name:      "deploy " + hash,
		TokenHash: []byte(hash),
		CreatedAt: time.Date(2026, 1, 5, 9, 0, 0, 123456000, time.UTC),
		ExpiresAt: expires,
	}
}

// withTokens returns a store holding the administrator (id 1) and the given
// tokens, created in order.
func withTokens(t *testing.T, impl storeImpl, tokens ...connector.StoredToken) connector.Store {
	t.Helper()
	store := withAdmin(t, impl)
	for _, tok := range tokens {
		if _, err := store.CreateToken(context.Background(), tok); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestStoreListsAUsersTokensOldestFirstWithTheirIDs(t *testing.T) {
	expires := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withAdmin(t, impl)

			first, errFirst := store.CreateToken(ctx, token("one", &expires))
			second, errSecond := store.CreateToken(ctx, token("two", nil))
			got, err := store.UserTokens(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]any{int64(1), int64(2), "<nil>", "<nil>"},
				[]any{first, second, testsupport.ErrorText(errFirst), testsupport.ErrorText(errSecond)}); diff != "" {
				t.Errorf("ids and errors (-want +got):\n%s", diff)
			}
			wantOne, wantTwo := token("one", &expires), token("two", nil)
			wantOne.ID, wantTwo.ID = 1, 2
			if diff := cmp.Diff([]connector.StoredToken{wantOne, wantTwo}, got); diff != "" {
				t.Errorf("tokens (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsNoTokensForAUserWithoutAny(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withTokens(t, impl, token("one", nil))

			got, err := store.UserTokens(context.Background(), 2)

			if diff := cmp.Diff([]any{[]connector.StoredToken{}, "<nil>"}, []any{got, testsupport.ErrorText(err)},
				cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("tokens, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreReturnsATokenWithItsUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withTokens(t, impl, token("one", nil), token("two", nil))

			gotToken, gotUser, found, err := store.TokenUser(context.Background(), []byte("two"))
			if err != nil {
				t.Fatal(err)
			}

			wantToken := token("two", nil)
			wantToken.ID = 2
			wantUser := asFirst(storedUser("admin@example.com"), adminGroups)
			if diff := cmp.Diff([]any{wantToken, wantUser, true}, []any{gotToken, gotUser, found}); diff != "" {
				t.Errorf("token, user, found (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreFindsNoTokenForAnUnknownHash(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := withTokens(t, impl, token("one", nil))

			_, _, found, err := store.TokenUser(context.Background(), []byte("other"))

			if diff := cmp.Diff([]any{false, "<nil>"}, []any{found, testsupport.ErrorText(err)}); diff != "" {
				t.Errorf("found, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesATokenOnlyForItsOwner(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			ctx := context.Background()
			store := withTokens(t, impl, token("one", nil), token("two", nil))

			byOther, errOther := store.DeleteToken(ctx, 2, 1)
			byOwner, errOwner := store.DeleteToken(ctx, 1, 1)
			again, errAgain := store.DeleteToken(ctx, 1, 1)
			left, err := store.UserTokens(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff([]any{false, true, false, "<nil>", "<nil>", "<nil>"}, []any{byOther, byOwner, again,
				testsupport.ErrorText(errOther), testsupport.ErrorText(errOwner), testsupport.ErrorText(errAgain)}); diff != "" {
				t.Errorf("deleted, errors (-want +got):\n%s", diff)
			}
			wantTwo := token("two", nil)
			wantTwo.ID = 2
			if diff := cmp.Diff([]connector.StoredToken{wantTwo}, left); diff != "" {
				t.Errorf("tokens left (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesATokenForAnUnknownUser(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			tok := token("one", nil)
			tok.UserID = 2

			_, err := withAdmin(t, impl).CreateToken(context.Background(), tok)

			if err == nil {
				t.Error("CreateToken succeeded for a user that does not exist")
			}
		})
	}
}

func TestStoreRefusesASecondTokenWithTheSameHash(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			_, err := withTokens(t, impl, token("one", nil)).CreateToken(context.Background(), token("one", nil))

			if err == nil {
				t.Error("CreateToken stored a duplicate hash")
			}
		})
	}
}
