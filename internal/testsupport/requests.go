package testsupport

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// CreateRequest stores r with the next id.
func (s *Store) CreateRequest(_ context.Context, r connector.StoredRequest) (int64, error) {
	if err := s.writable(requestsAggregate, requestWrites); err != nil {
		return 0, err
	}
	if s.userByID(r.UserID) == nil {
		return 0, ErrNoSuchReference
	}
	s.nextRequest++
	r.ID = s.nextRequest
	r.ResolvedAt = clonedTime(r.ResolvedAt)
	s.requests = append(s.requests, r)
	return r.ID, nil
}

// Request returns the request with id.
func (s *Store) Request(_ context.Context, id int64) (connector.StoredRequest, bool, error) {
	if err := s.usable(requestsAggregate); err != nil {
		return connector.StoredRequest{}, false, err
	}
	held := s.requestByID(id)
	if held == nil {
		return connector.StoredRequest{}, false, nil
	}
	return clonedRequest(*held), true, nil
}

// UserRequests returns the requests of the user with userID, newest first.
func (s *Store) UserRequests(_ context.Context, userID int64) ([]connector.StoredRequest, error) {
	if err := s.usable(requestsAggregate); err != nil {
		return nil, err
	}
	mine := []connector.StoredRequest{}
	for _, r := range s.requests {
		if r.UserID == userID {
			mine = append(mine, clonedRequest(r))
		}
	}
	slices.SortFunc(mine, func(a, b connector.StoredRequest) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	return mine, nil
}

// UpdateRequest stores r's title, body, and update time over the request
// with r.ID.
func (s *Store) UpdateRequest(_ context.Context, r connector.StoredRequest) (bool, error) {
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

// DeleteRequest removes the request with id.
func (s *Store) DeleteRequest(_ context.Context, id int64) (bool, error) {
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

func clonedRequest(r connector.StoredRequest) connector.StoredRequest {
	r.ResolvedAt = clonedTime(r.ResolvedAt)
	return r
}

// ResolveRequest gives the request with id an administrator's reply and
// resolves it at an RFC 3339 time, as an administrator would.
func (s *Store) ResolveRequest(id int64, reply, at string) {
	resolved := mustTime(at)
	held := s.requestByID(id)
	held.Reply, held.ResolvedAt = reply, &resolved
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
