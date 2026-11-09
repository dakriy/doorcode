package memstore

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// requestOut is a copy of r with its user's display name.
func (s *Store) requestOut(r connector.StoredRequest) connector.StoredRequest {
	r.UserName = s.users[r.UserID].DisplayName
	r.ResolvedAt = clone(r.ResolvedAt)
	return r
}

// requestKept reports whether q keeps r, whose user is u.
func requestKept(q connector.RequestQuery, r connector.StoredRequest, u connector.StoredUser) bool {
	if q.Resolved != nil && (r.ResolvedAt != nil) != *q.Resolved {
		return false
	}
	if q.UserID != 0 && r.UserID != q.UserID {
		return false
	}
	return contains(q.Search, r.Title, r.Body, u.DisplayName, u.Email)
}

// requestsWhere returns the requests keep holds for, newest first.
func (s *Store) requestsWhere(keep func(connector.StoredRequest) bool) []connector.StoredRequest {
	requests := []connector.StoredRequest{}
	for _, r := range s.requests {
		if keep(r) {
			requests = append(requests, s.requestOut(r))
		}
	}
	newestFirst(requests, func(r connector.StoredRequest) time.Time { return r.CreatedAt }, func(r connector.StoredRequest) int64 { return r.ID })
	return requests
}

// Requests returns the page of requests q selects, newest first, and how
// many it selects in all.
func (s *Store) Requests(_ context.Context, q connector.RequestQuery) ([]connector.StoredRequest, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.requestsWhere(func(r connector.StoredRequest) bool { return requestKept(q, r, s.users[r.UserID]) })
	return page(kept, q.Offset, q.Limit), len(kept), nil
}

// CreateRequest stores r and returns its id.
func (s *Store) CreateRequest(_ context.Context, r connector.StoredRequest) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[r.UserID]; !ok {
		return 0, errNoReference
	}
	r.ID = s.next("requests")
	r.UserName = ""
	r.ResolvedAt = clone(r.ResolvedAt)
	s.requests[r.ID] = r
	return r.ID, nil
}

// Request returns the request whose id is id.
func (s *Store) Request(_ context.Context, id int64) (connector.StoredRequest, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.requests[id]
	if !ok {
		return connector.StoredRequest{}, false, nil
	}
	return s.requestOut(r), true, nil
}

// UserRequests returns the user's requests, newest first.
func (s *Store) UserRequests(_ context.Context, userID int64) ([]connector.StoredRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requestsWhere(func(r connector.StoredRequest) bool { return r.UserID == userID }), nil
}

// changeRequest applies change to the request whose id is id.
func (s *Store) changeRequest(id int64, change func(r *connector.StoredRequest)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.requests[id]
	if !ok {
		return false, nil
	}
	change(&r)
	s.requests[id] = r
	return true, nil
}

// UpdateRequest stores r's title, body, and update time.
func (s *Store) UpdateRequest(_ context.Context, r connector.StoredRequest) (bool, error) {
	return s.changeRequest(r.ID, func(stored *connector.StoredRequest) {
		stored.Title = r.Title
		stored.Body = r.Body
		stored.UpdatedAt = r.UpdatedAt
	})
}

// SetRequestReply stores reply as the answer to the request.
func (s *Store) SetRequestReply(_ context.Context, id int64, reply string) (bool, error) {
	return s.changeRequest(id, func(r *connector.StoredRequest) { r.Reply = reply })
}

// SetRequestResolved stores at as when the request was resolved.
func (s *Store) SetRequestResolved(_ context.Context, id int64, at *time.Time) (bool, error) {
	return s.changeRequest(id, func(r *connector.StoredRequest) { r.ResolvedAt = clone(at) })
}

// DeleteRequest removes the request whose id is id.
func (s *Store) DeleteRequest(_ context.Context, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.requests[id]
	delete(s.requests, id)
	return ok, nil
}
