//go:build slow

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// requestsStart is when the first request of requestWorld is made; the
// second follows an hour later.
var requestsStart = time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC)

// requestWorld is the directory with two requests by Ann (4): an open one
// (1) and, an hour later, a resolved one with a reply (2), and one by the
// administrator (3).
func requestWorld(t *testing.T, impl storeImpl) connector.Store {
	t.Helper()
	store := directory(t, impl)
	resolved := requestsStart.Add(2 * time.Hour)
	for _, r := range []connector.StoredRequest{
		{UserID: 4, Title: "Lab access", Body: "I start in the lab on Monday.", CreatedAt: requestsStart, UpdatedAt: requestsStart},
		{UserID: 4, Title: "Weekend hours", Body: "", Reply: "Granted until June.", ResolvedAt: &resolved,
			CreatedAt: requestsStart.Add(time.Hour), UpdatedAt: requestsStart.Add(time.Hour)},
		{UserID: 1, Title: "Spare badge", Body: "For visitors.", CreatedAt: requestsStart, UpdatedAt: requestsStart},
	} {
		if _, err := store.CreateRequest(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestStoreListsAUsersRequestsNewestFirst(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)

			got, err := store.UserRequests(context.Background(), 4)

			resolved := requestsStart.Add(2 * time.Hour)
			want := []any{[]connector.StoredRequest{
				{ID: 2, UserID: 4, Title: "Weekend hours", Reply: "Granted until June.", ResolvedAt: &resolved,
					CreatedAt: requestsStart.Add(time.Hour), UpdatedAt: requestsStart.Add(time.Hour)},
				{ID: 1, UserID: 4, Title: "Lab access", Body: "I start in the lab on Monday.", CreatedAt: requestsStart, UpdatedAt: requestsStart},
			}, nil}
			if diff := cmp.Diff(want, []any{got, err}); diff != "" {
				t.Errorf("requests, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreUpdatesOnlyARequestsTitleBodyAndUpdateTime(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)
			later := requestsStart.Add(5 * time.Hour)

			found, err := store.UpdateRequest(context.Background(), connector.StoredRequest{
				ID: 2, UserID: 1, Title: "Weekend and evening hours", Body: "Saturdays too.", Reply: "changed", CreatedAt: later, UpdatedAt: later,
			})
			missing, missingErr := store.UpdateRequest(context.Background(), connector.StoredRequest{ID: 9, Title: "None", UpdatedAt: later})
			got, held, readErr := store.Request(context.Background(), 2)

			resolved := requestsStart.Add(2 * time.Hour)
			want := []any{true, nil, false, nil, connector.StoredRequest{
				ID: 2, UserID: 4, Title: "Weekend and evening hours", Body: "Saturdays too.", Reply: "Granted until June.", ResolvedAt: &resolved,
				CreatedAt: requestsStart.Add(time.Hour), UpdatedAt: later,
			}, true, nil}
			if diff := cmp.Diff(want, []any{found, err, missing, missingErr, got, held, readErr}); diff != "" {
				t.Errorf("found, error, missing, error, request, held, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreDeletesARequest(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)

			deleted, err := store.DeleteRequest(context.Background(), 1)
			again, againErr := store.DeleteRequest(context.Background(), 1)
			_, held, readErr := store.Request(context.Background(), 1)

			if diff := cmp.Diff([]any{true, nil, false, nil, false, nil}, []any{deleted, err, again, againErr, held, readErr}); diff != "" {
				t.Errorf("deleted, error, again, error, held, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRefusesARequestOfAUserItDoesNotHold(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := directory(t, impl)

			_, err := store.CreateRequest(context.Background(), connector.StoredRequest{UserID: 9, Title: "Nobody", CreatedAt: requestsStart, UpdatedAt: requestsStart})

			if err == nil {
				t.Error("CreateRequest stored a request of a user that does not exist")
			}
		})
	}
}
