// Package request keeps access requests: what a user asks an administrator
// in their own words, and the administrator's reply. A user may change or
// withdraw their request until it is resolved.
package request

import (
	"context"
	"errors"
	"strings"

	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

const (
	// MaxTitleLength and MaxBodyLength are the most characters a
	// request's title and body may have.
	MaxTitleLength = 120
	MaxBodyLength  = 4000
)

var (
	// ErrNoRequest reports an id that names none of the user's requests.
	ErrNoRequest = errors.New("you have no access request with this id")
	// ErrResolved reports a change to a request that is resolved.
	ErrResolved = errors.New("this access request is resolved and can no longer be changed")
)

// Service reads and changes access requests.
type Service struct {
	Store connector.RequestsStore
	Clock connector.Clock
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
	r, found, err := s.Store.Request(ctx, id)
	if err != nil {
		return connector.StoredRequest{}, err
	}
	if !found || r.UserID != userID {
		return connector.StoredRequest{}, ErrNoRequest
	}
	return r, nil
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
	return r, err
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
	_, err := s.Store.DeleteRequest(ctx, id)
	return err
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

// checked is f trimmed, or the problems with it.
func checked(f Fields) (Fields, error) {
	f = Fields{Title: strings.TrimSpace(f.Title), Body: strings.TrimSpace(f.Body)}
	var found check.List
	found.Add("title", check.Required(f.Title, "Give the request a title."))
	found.Add("title", check.TooLong(f.Title, MaxTitleLength, check.LongText))
	found.Add("body", check.TooLong(f.Body, MaxBodyLength, check.LongText))
	return f, found.Err()
}
