package testsupport

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// CreateRequest stores r with the next id.
func (s *Store) CreateRequest(_ context.Context, r connector.StoredRequest) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return 0, err
	}
	if s.userByID(r.UserID) == nil {
		return 0, ErrNoSuchReference
	}
	s.nextRequest++
	r.ID = s.nextRequest
	r.ResolvedAt = clonedTime(r.ResolvedAt)
	r.UserName = ""
	s.requests = append(s.requests, r)
	return r.ID, nil
}

// Request returns the request with id.
func (s *Store) Request(_ context.Context, id int64) (connector.StoredRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.usable(requestsAggregate); err != nil {
		return connector.StoredRequest{}, false, err
	}
	held := s.requestByID(id)
	if held == nil {
		return connector.StoredRequest{}, false, nil
	}
	return s.returnedRequest(*held), true, nil
}

// UserRequests returns the requests of the user with userID, newest first.
func (s *Store) UserRequests(_ context.Context, userID int64) ([]connector.StoredRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mine, _, err := s.requestsOf(connector.RequestQuery{UserID: userID, Limit: len(s.requests)})
	return mine, err
}

// Requests returns the page of requests q selects, newest first, and how
// many it selects in all.
func (s *Store) Requests(_ context.Context, q connector.RequestQuery) ([]connector.StoredRequest, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requestsOf(q)
}

// requestsOf is Requests, for a caller holding the lock.
func (s *Store) requestsOf(q connector.RequestQuery) ([]connector.StoredRequest, int, error) {
	if err := s.usable(requestsAggregate); err != nil {
		return nil, 0, err
	}
	selected := []connector.StoredRequest{}
	for _, r := range s.requests {
		if s.selects(q, r) {
			selected = append(selected, s.returnedRequest(r))
		}
	}
	slices.SortFunc(selected, func(a, b connector.StoredRequest) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	end := min(q.Offset+q.Limit, len(selected))
	start := min(q.Offset, end)
	return slices.Clone(selected[start:end]), len(selected), nil
}

// selects reports whether q keeps r.
func (s *Store) selects(q connector.RequestQuery, r connector.StoredRequest) bool {
	u := s.userByID(r.UserID)
	text := strings.ToLower(strings.Join([]string{r.Title, r.Body, u.DisplayName, u.Email}, " "))
	return strings.Contains(text, strings.ToLower(q.Search)) &&
		(q.Resolved == nil || (r.ResolvedAt != nil) == *q.Resolved) &&
		(q.UserID == 0 || r.UserID == q.UserID)
}

// returnedRequest is r as the store returns it, with its user's name.
func (s *Store) returnedRequest(r connector.StoredRequest) connector.StoredRequest {
	r.ResolvedAt = clonedTime(r.ResolvedAt)
	r.UserName = s.userByID(r.UserID).DisplayName
	return r
}

// UpdateRequest stores r's title, body, and update time over the request
// with r.ID.
func (s *Store) UpdateRequest(_ context.Context, r connector.StoredRequest) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return false, err
	}
	held := s.requestByID(r.ID)
	if held == nil {
		return false, nil
	}
	held.Title, held.Body, held.UpdatedAt = r.Title, r.Body, r.UpdatedAt
	return true, nil
}

// SetRequestReply stores reply as the answer to the request with id.
func (s *Store) SetRequestReply(_ context.Context, id int64, reply string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return false, err
	}
	held := s.requestByID(id)
	if held == nil {
		return false, nil
	}
	held.Reply = reply
	return true, nil
}

// SetRequestResolved stores at as when the request with id was resolved.
func (s *Store) SetRequestResolved(_ context.Context, id int64, at *time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return false, err
	}
	held := s.requestByID(id)
	if held == nil {
		return false, nil
	}
	held.ResolvedAt = clonedTime(at)
	return true, nil
}

// DeleteRequest removes the request with id.
func (s *Store) DeleteRequest(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return false, err
	}
	held := s.requestByID(id) != nil
	s.requests = slices.DeleteFunc(slices.Clone(s.requests), func(r connector.StoredRequest) bool { return r.ID == id })
	return held, nil
}

func (s *Store) requestByID(id int64) *connector.StoredRequest {
	for i := range s.requests {
		if s.requests[i].ID == id {
			return &s.requests[i]
		}
	}
	return nil
}

// FailRequests makes the access request methods report ErrUnavailable.
func (s *Store) FailRequests() {
	s.failing[requestsAggregate] = true
}

// FailRequestWrites makes the methods that change access requests report
// ErrUnavailable, while the ones that read them still work.
func (s *Store) FailRequestWrites() {
	s.failing[requestWrites] = true
}

func clonedTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	held := *t
	return &held
}
