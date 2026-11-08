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
	_, err = s.Store.SetPasswordHash(ctx, id, hash)
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
