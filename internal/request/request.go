// Package request keeps access requests: what a user asks an administrator
// in their own words, and the administrator's reply. A user may change or
// withdraw their request until it is resolved.
package request

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

const (
	// MaxTitleLength and MaxBodyLength are the most characters a
	// request's title and body may have.
	MaxTitleLength = 120
	MaxBodyLength  = 4000
	// MaxReplyLength is the most characters a reply may have.
	MaxReplyLength = 4000
)

var (
	// ErrNoRequest reports an id that names none of the user's requests.
	ErrNoRequest = errors.New("you have no access request with this id")
	// ErrResolved reports a change to a request that is resolved.
	ErrResolved = errors.New("this access request is resolved and can no longer be changed")
	// ErrNoSuchRequest reports an id that names no request.
	ErrNoSuchRequest = errors.New("no access request has this id")
)

// Service reads and changes access requests. Changed is told each time
// a request is made, withdrawn, resolved, or reopened, which changes how
// many wait for an answer.
type Service struct {
	Store   connector.RequestsStore
	Clock   connector.Clock
	Changed func()
}

// Fields is what a user writes in a request. Both are trimmed before they
// are checked.
type Fields struct {
	Title string
	Body  string
}

// Mine returns the requests of the user whose id is userID, newest first.
func (s Service) Mine(ctx context.Context, userID int64) ([]connector.StoredRequest, error) {
	return s.Store.UserRequests(ctx, userID)
}

// One returns the request whose id is id when it is one of the user's
// whose id is userID.
func (s Service) One(ctx context.Context, userID, id int64) (connector.StoredRequest, error) {
	r, err := check.Lookup(ctx, id, s.Store.Request, ErrNoRequest)
	if err == nil && r.UserID != userID {
		return connector.StoredRequest{}, ErrNoRequest
	}
	return r, err
}

// Create stores a request from f by the user whose id is userID.
func (s Service) Create(ctx context.Context, userID int64, f Fields) (connector.StoredRequest, error) {
	f, err := checked(f)
	if err != nil {
		return connector.StoredRequest{}, err
	}
	now := s.Clock.Now()
	r := connector.StoredRequest{UserID: userID, Title: f.Title, Body: f.Body, CreatedAt: now, UpdatedAt: now}
	r.ID, err = s.Store.CreateRequest(ctx, r)
	if err != nil {
		return connector.StoredRequest{}, err
	}
	s.Changed()
	return r, nil
}

// Update replaces what the user whose id is userID wrote in their request
// whose id is id with f, while it is not resolved.
func (s Service) Update(ctx context.Context, userID, id int64, f Fields) (connector.StoredRequest, error) {
	r, err := s.open(ctx, userID, id)
	if err != nil {
		return connector.StoredRequest{}, err
	}
	f, err = checked(f)
	if err != nil {
		return connector.StoredRequest{}, err
	}
	r.Title, r.Body, r.UpdatedAt = f.Title, f.Body, s.Clock.Now()
	_, err = s.Store.UpdateRequest(ctx, r)
	return r, err
}

// Delete withdraws the request whose id is id of the user whose id is
// userID, while it is not resolved.
func (s Service) Delete(ctx context.Context, userID, id int64) error {
	if _, err := s.open(ctx, userID, id); err != nil {
		return err
	}
	if _, err := s.Store.DeleteRequest(ctx, id); err != nil {
		return err
	}
	s.Changed()
	return nil
}

// open returns the user's request whose id is id, refusing one that is
// resolved.
func (s Service) open(ctx context.Context, userID, id int64) (connector.StoredRequest, error) {
	r, err := s.One(ctx, userID, id)
	if err == nil && r.ResolvedAt != nil {
		return connector.StoredRequest{}, ErrResolved
	}
	return r, err
}

// Query selects a page of every user's requests; Page counts from 1. A nil
// Resolved keeps open and resolved ones, and a zero UserID everyone's.
type Query struct {
	Search   string
	Resolved *bool
	UserID   int64
	Page     int
	PerPage  int
}

// Page is one page of requests and how many requests the query selects.
type Page struct {
	Requests []connector.StoredRequest
	Total    int
}

// Find returns the page of requests q selects, newest first.
func (s Service) Find(ctx context.Context, q Query) (Page, error) {
	found, total, err := s.Store.Requests(ctx, connector.RequestQuery{
		Search: strings.TrimSpace(q.Search), Resolved: q.Resolved, UserID: q.UserID,
		Offset: (q.Page - 1) * q.PerPage, Limit: q.PerPage,
	})
	return Page{Requests: found, Total: total}, err
}

// Waiting returns how many requests wait for an answer: those that are
// not resolved.
func (s Service) Waiting(ctx context.Context) (int, error) {
	open := false
	_, total, err := s.Store.Requests(ctx, connector.RequestQuery{Resolved: &open})
	return total, err
}

// Get returns the request whose id is id, whoever made it.
func (s Service) Get(ctx context.Context, id int64) (connector.StoredRequest, error) {
	return check.Lookup(ctx, id, s.Store.Request, ErrNoSuchRequest)
}

// Reply stores reply, trimmed, as the answer to the request whose id is
// id, resolved or not, and returns the request as it is now.
func (s Service) Reply(ctx context.Context, id int64, reply string) (connector.StoredRequest, error) {
	reply = strings.TrimSpace(reply)
	var found check.List
	found.Add("reply", check.Required(reply, "Write a reply."))
	found.Add("reply", check.TooLong(reply, MaxReplyLength, check.LongText))
	if err := found.Err(); err != nil {
		return connector.StoredRequest{}, err
	}
	return s.answered(ctx, id, func() (bool, error) { return s.Store.SetRequestReply(ctx, id, reply) })
}

// Resolve resolves the request whose id is id, which its user can then no
// longer change, and returns it. A resolved request keeps when it was
// resolved.
func (s Service) Resolve(ctx context.Context, id int64) (connector.StoredRequest, error) {
	r, err := s.Get(ctx, id)
	if err != nil || r.ResolvedAt != nil {
		return r, err
	}
	now := s.Clock.Now()
	return s.resolved(ctx, id, &now)
}

// Reopen makes the request whose id is id open again, so its user may
// change or withdraw it, and returns it.
func (s Service) Reopen(ctx context.Context, id int64) (connector.StoredRequest, error) {
	return s.resolved(ctx, id, nil)
}

// resolved stores at as when the request whose id is id was resolved.
func (s Service) resolved(ctx context.Context, id int64, at *time.Time) (connector.StoredRequest, error) {
	r, err := s.answered(ctx, id, func() (bool, error) { return s.Store.SetRequestResolved(ctx, id, at) })
	if err == nil {
		s.Changed()
	}
	return r, err
}

// answered makes the change set reports making to the request whose id
// is id, and returns the request as it is now.
func (s Service) answered(ctx context.Context, id int64, set func() (bool, error)) (connector.StoredRequest, error) {
	found, err := set()
	if err != nil {
		return connector.StoredRequest{}, err
	}
	if !found {
		return connector.StoredRequest{}, ErrNoSuchRequest
	}
	return s.Get(ctx, id)
}

// checked is f trimmed, or the problems with it.
func checked(f Fields) (Fields, error) {
	f = Fields{Title: strings.TrimSpace(f.Title), Body: strings.TrimSpace(f.Body)}
	var found check.List
	found.Add("title", check.Required(f.Title, "Give the request a title."))
	found.Add("title", check.TooLong(f.Title, MaxTitleLength, check.LongText))
	found.Add("body", check.TooLong(f.Body, MaxBodyLength, check.LongText))
	return f, found.Err()
}
