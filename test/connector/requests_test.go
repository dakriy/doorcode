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
				{ID: 2, UserID: 4, UserName: "Ann", Title: "Weekend hours", Reply: "Granted until June.", ResolvedAt: &resolved,
					CreatedAt: requestsStart.Add(time.Hour), UpdatedAt: requestsStart.Add(time.Hour)},
				{ID: 1, UserID: 4, UserName: "Ann", Title: "Lab access", Body: "I start in the lab on Monday.", CreatedAt: requestsStart, UpdatedAt: requestsStart},
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
				ID: 2, UserID: 4, UserName: "Ann", Title: "Weekend and evening hours", Body: "Saturdays too.", Reply: "Granted until June.", ResolvedAt: &resolved,
				CreatedAt: requestsStart.Add(time.Hour), UpdatedAt: later,
			}, true, nil}
			if diff := cmp.Diff(want, []any{found, err, missing, missingErr, got, held, readErr}); diff != "" {
				t.Errorf("found, error, missing, error, request, held, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreListsAPageOfRequests(t *testing.T) {
	open, resolved := false, true
	cases := map[string]struct {
		query connector.RequestQuery
		ids   []int64
		total int
	}{
		"every request, newest first":          {query: connector.RequestQuery{Limit: 10}, ids: []int64{2, 3, 1}, total: 3},
		"one page":                             {query: connector.RequestQuery{Offset: 1, Limit: 1}, ids: []int64{3}, total: 3},
		"past the end":                         {query: connector.RequestQuery{Offset: 9, Limit: 2}, ids: []int64{}, total: 3},
		"open ones":                            {query: connector.RequestQuery{Resolved: &open, Limit: 10}, ids: []int64{3, 1}, total: 2},
		"resolved ones":                        {query: connector.RequestQuery{Resolved: &resolved, Limit: 10}, ids: []int64{2}, total: 1},
		"one user's":                           {query: connector.RequestQuery{UserID: 1, Limit: 10}, ids: []int64{3}, total: 1},
		"by title, ignoring case":              {query: connector.RequestQuery{Search: "LAB", Limit: 10}, ids: []int64{1}, total: 1},
		"by body":                              {query: connector.RequestQuery{Search: "visitors", Limit: 10}, ids: []int64{3}, total: 1},
		"by the user's name":                   {query: connector.RequestQuery{Search: "ann", Limit: 10}, ids: []int64{2, 1}, total: 2},
		"by the user's email":                  {query: connector.RequestQuery{Search: "admin@", Limit: 10}, ids: []int64{3}, total: 1},
		"every filter together, matching none": {query: connector.RequestQuery{Search: "lab", Resolved: &resolved, UserID: 4, Limit: 10}, ids: []int64{}, total: 0},
		"every filter together, matching one":  {query: connector.RequestQuery{Search: "lab", Resolved: &open, UserID: 4, Limit: 10}, ids: []int64{1}, total: 1},
	}
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)
			for name, c := range cases {
				t.Run(name, func(t *testing.T) {
					got, total, err := store.Requests(context.Background(), c.query)

					ids := []int64{}
					for _, r := range got {
						ids = append(ids, r.ID)
					}
					if diff := cmp.Diff([]any{c.ids, c.total, nil}, []any{ids, total, err}); diff != "" {
						t.Errorf("ids, total, error (-want +got):\n%s", diff)
					}
				})
			}
		})
	}
}

func TestStoreListsARequestWithItsUsersName(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)

			got, total, err := store.Requests(context.Background(), connector.RequestQuery{UserID: 1, Limit: 1})

			want := []any{[]connector.StoredRequest{
				{ID: 3, UserID: 1, UserName: "Administrator", Title: "Spare badge", Body: "For visitors.", CreatedAt: requestsStart, UpdatedAt: requestsStart},
			}, 1, nil}
			if diff := cmp.Diff(want, []any{got, total, err}); diff != "" {
				t.Errorf("requests, total, error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStoreRepliesToAndResolvesARequest(t *testing.T) {
	for _, impl := range storeImpls {
		t.Run(impl.name, func(t *testing.T) {
			store := requestWorld(t, impl)
			at := requestsStart.Add(3 * time.Hour)

			replied, replyErr := store.SetRequestReply(context.Background(), 1, "Granted.")
			resolved, resolveErr := store.SetRequestResolved(context.Background(), 1, &at)
			first, _, firstErr := store.Request(context.Background(), 1)
			reopened, reopenErr := store.SetRequestResolved(context.Background(), 1, nil)
			second, _, secondErr := store.Request(context.Background(), 1)
			missingReply, missingReplyErr := store.SetRequestReply(context.Background(), 9, "None.")
			missingResolve, missingResolveErr := store.SetRequestResolved(context.Background(), 9, &at)

			answered := connector.StoredRequest{ID: 1, UserID: 4, UserName: "Ann", Title: "Lab access", Body: "I start in the lab on Monday.",
				Reply: "Granted.", ResolvedAt: &at, CreatedAt: requestsStart, UpdatedAt: requestsStart}
			open := answered
			open.ResolvedAt = nil
			want := []any{true, nil, true, nil, answered, nil, true, nil, open, nil, false, nil, false, nil}
			got := []any{replied, replyErr, resolved, resolveErr, first, firstErr, reopened, reopenErr, second, secondErr,
				missingReply, missingReplyErr, missingResolve, missingResolveErr}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("replied, resolved, read, reopened, read, missing reply, missing resolution (-want +got):\n%s", diff)
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
