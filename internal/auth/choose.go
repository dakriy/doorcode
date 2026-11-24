package auth

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/dakriy/doorcode/internal/check"
)

// ErrPasswordsHidden reports choosing a password on a site where everyone
// signs in at the identity provider, so no password is ever asked for.
var ErrPasswordsHidden = errors.New("passwords are not used here; everyone signs in with single sign-on")

// maxPasswordLength is the most characters a chosen password may have.
const maxPasswordLength = 1024

// ChoosePassword makes chosen the password of the user whose id is id. A
// user who has a password already must give it as current. Neither is
// trimmed.
func (s Service) ChoosePassword(ctx context.Context, id int64, current, chosen string) error {
	c, err := s.Settings.Current(ctx)
	if err != nil {
		return err
	}
	if c.OIDC.HidesPasswordForms() {
		return ErrPasswordsHidden
	}
	u, _, err := s.Store.User(ctx, id)
	if err != nil {
		return err
	}
	var problems check.List
	if u.PasswordHash != nil && !passwordMatches(*u.PasswordHash, current) {
		problems.Add("current_password", "That is not your current password.")
	}
	problems.Add("new_password", passwordProblem(chosen, c.PasswordMinLength))
	if err := problems.Err(); err != nil {
		return err
	}
	hash := hashPassword(chosen, s.Entropy.Bytes(saltBytes), c.PasswordHashMemoryKiB)
	_, err = s.Store.SetPasswordHash(ctx, id, &hash)
	return err
}

// SetPassword makes password the password of the user whose id is id on
// someone else's word, without asking for the one they have, and ends
// every session they have, so only the new password signs them in. It is
// not trimmed. Auto sign-in does not refuse it: a person reaches the
// password form at /sign-in?auto_sign_in=false. The user must exist.
func (s Service) SetPassword(ctx context.Context, id int64, password string) error {
	c, err := s.Settings.Current(ctx)
	if err != nil {
		return err
	}
	var problems check.List
	problems.Add("password", passwordProblem(password, c.PasswordMinLength))
	if err := problems.Err(); err != nil {
		return err
	}
	hash := hashPassword(password, s.Entropy.Bytes(saltBytes), c.PasswordHashMemoryKiB)
	return s.replacePassword(ctx, id, &hash)
}

// ClearPassword removes the password of the user whose id is id and ends
// every session they have, so they sign in only at the identity provider.
// The user must exist.
func (s Service) ClearPassword(ctx context.Context, id int64) error {
	return s.replacePassword(ctx, id, nil)
}

// replacePassword stores hash as the password hash of the user whose id
// is id and ends their sessions. Their API tokens stay.
func (s Service) replacePassword(ctx context.Context, id int64, hash *string) error {
	if _, err := s.Store.SetPasswordHash(ctx, id, hash); err != nil {
		return err
	}
	_, err := s.Store.DeleteUserSessions(ctx, id)
	return err
}

// passwordProblem says what is wrong with chosen as a password at least
// minLength characters long; empty when nothing is.
func passwordProblem(chosen string, minLength int) string {
	if utf8.RuneCountInString(chosen) < minLength {
		return fmt.Sprintf("Use at least %d characters.", minLength)
	}
	return check.TooLong(chosen, maxPasswordLength, check.LongText)
}
