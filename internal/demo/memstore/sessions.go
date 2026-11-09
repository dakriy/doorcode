package memstore

import (
	"context"
	"slices"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// CreateSession stores ses.
func (s *Store) CreateSession(_ context.Context, ses connector.StoredSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[ses.UserID]; !ok {
		return errNoReference
	}
	key := string(ses.TokenHash)
	if _, taken := s.sessions[key]; taken {
		return errDuplicate
	}
	ses.TokenHash = list(ses.TokenHash)
	s.sessions[key] = ses
	return nil
}

// SessionUser returns the session whose token hash is tokenHash and its
// user.
func (s *Store) SessionUser(_ context.Context, tokenHash []byte) (connector.StoredSession, connector.StoredUser, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.sessions[string(tokenHash)]
	if !ok {
		return connector.StoredSession{}, connector.StoredUser{}, false, nil
	}
	ses.TokenHash = list(ses.TokenHash)
	return ses, s.userOut(ses.UserID), true, nil
}

// DeleteSession removes the session whose token hash is tokenHash.
func (s *Store) DeleteSession(_ context.Context, tokenHash []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, string(tokenHash))
	return nil
}

// DeleteExpiredSessions removes every session that expires at or before
// now.
func (s *Store) DeleteExpiredSessions(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for key, ses := range s.sessions {
		if !ses.ExpiresAt.After(now) {
			delete(s.sessions, key)
			removed++
		}
	}
	return removed, nil
}

// tokenOut is a copy of t.
func tokenOut(t connector.StoredToken) connector.StoredToken {
	t.TokenHash = list(t.TokenHash)
	t.ExpiresAt = cloneTime(t.ExpiresAt)
	return t
}

// CreateToken stores t and returns its id.
func (s *Store) CreateToken(_ context.Context, t connector.StoredToken) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[t.UserID]; !ok {
		return 0, errNoReference
	}
	for _, other := range s.tokens {
		if slices.Equal(other.TokenHash, t.TokenHash) {
			return 0, errDuplicate
		}
	}
	t = tokenOut(t)
	t.ID = s.next("tokens")
	s.tokens[t.ID] = t
	return t.ID, nil
}

// UserTokens returns the user's tokens, oldest first.
func (s *Store) UserTokens(_ context.Context, userID int64) ([]connector.StoredToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens := []connector.StoredToken{}
	for _, t := range s.tokens {
		if t.UserID == userID {
			tokens = append(tokens, tokenOut(t))
		}
	}
	byID(tokens, func(t connector.StoredToken) int64 { return t.ID })
	return tokens, nil
}

// TokenUser returns the token whose hash is tokenHash and its user.
func (s *Store) TokenUser(_ context.Context, tokenHash []byte) (connector.StoredToken, connector.StoredUser, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if slices.Equal(t.TokenHash, tokenHash) {
			return tokenOut(t), s.userOut(t.UserID), true, nil
		}
	}
	return connector.StoredToken{}, connector.StoredUser{}, false, nil
}

// DeleteToken removes the user's token with id.
func (s *Store) DeleteToken(_ context.Context, userID, id int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[id]
	if !ok || t.UserID != userID {
		return false, nil
	}
	delete(s.tokens, id)
	return true, nil
}
