package auth

import (
	"context"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

// apiTokenPrefix starts every personal API token, so a leaked one is easy to
// recognize.
const apiTokenPrefix = "dct_"

const apiTokenBytes = 32

// Token is a personal API token as its owner sees it, without the secret.
type Token struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	// ExpiresAt is nil for a token that never expires.
	ExpiresAt *time.Time
	Expired   bool
}

// CreateToken issues the user whose id is ownerID a token called name that expires after days, or
// never when days is nil. It returns the token and its secret, which is
// shown once and kept only as a hash.
func (s Service) CreateToken(ctx context.Context, ownerID int64, name string, days *int) (Token, string, error) {
	now := s.Clock.Now()
	secret := apiTokenPrefix + tokenEncoding.EncodeToString(s.Entropy.Bytes(apiTokenBytes))
	stored := connector.StoredToken{UserID: ownerID, Name: name, TokenHash: tokenHash(secret), CreatedAt: now}
	if days != nil {
		expires := now.AddDate(0, 0, *days)
		stored.ExpiresAt = &expires
	}
	id, err := s.Store.CreateToken(ctx, stored)
	if err != nil {
		return Token{}, "", err
	}
	stored.ID = id
	return tokenOf(stored, now), secret, nil
}

// Tokens returns the tokens of the user whose id is ownerID, oldest first.
func (s Service) Tokens(ctx context.Context, ownerID int64) ([]Token, error) {
	stored, err := s.Store.UserTokens(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	now := s.Clock.Now()
	tokens := make([]Token, 0, len(stored))
	for _, t := range stored {
		tokens = append(tokens, tokenOf(t, now))
	}
	return tokens, nil
}

// RevokeToken removes the token with id of the user whose id is ownerID,
// and reports whether they had one.
func (s Service) RevokeToken(ctx context.Context, ownerID, id int64) (bool, error) {
	return s.Store.DeleteToken(ctx, ownerID, id)
}

// AuthenticateToken returns the user a personal API token secret belongs
// to, and whether the token and the user are still good. The token acts as
// its user, with that user's roles.
func (s Service) AuthenticateToken(ctx context.Context, secret string) (User, bool, error) {
	t, u, found, err := s.Store.TokenUser(ctx, tokenHash(secret))
	if err != nil {
		return User{}, false, err
	}
	return s.admit(u, found, t.ExpiresAt)
}

func tokenOf(t connector.StoredToken, now time.Time) Token {
	return Token{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, Expired: reached(t.ExpiresAt, now)}
}
