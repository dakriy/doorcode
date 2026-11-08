package directory

import (
	"cmp"
	"context"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

const (
	maxEmailLength = 254
	maxNameLength  = 100
	minCodeDigits  = 6
	maxCodeDigits  = 20
	// A code a user chooses is their prefix followed by minOwnDigits to
	// maxOwnDigits digits.
	minOwnDigits = 4
	maxOwnDigits = 8
	digits       = "0123456789"
	codeConflict = "Another user has this code. Choose a different one."
)

// Profile is what an administrator says about a user. Every field is
// trimmed and the email lowercased before it is checked; an empty display
// name becomes the first and last names.
type Profile struct {
	Email       string
	UserID      string
	FirstName   string
	LastName    string
	DisplayName string
	// ExpiresAt is nil for an account that never expires.
	ExpiresAt *time.Time
}

// Query selects a page of users; Page counts from 1.
type Query struct {
	Search  string
	Deleted bool
	Page    int
	PerPage int
}

// UserPage is one page of users and how many users the query selects.
type UserPage struct {
	Users []connector.StoredUser
	Total int
}

// Users returns the page of users q selects.
func (s Service) Users(ctx context.Context, q Query) (UserPage, error) {
	users, total, err := s.Store.Users(ctx, connector.UserQuery{
		Search:  strings.TrimSpace(q.Search),
		Deleted: q.Deleted,
		Offset:  (q.Page - 1) * q.PerPage,
		Limit:   q.PerPage,
	})
	return UserPage{Users: users, Total: total}, err
}

// User returns the user whose id is id.
func (s Service) User(ctx context.Context, id int64) (connector.StoredUser, error) {
	u, found, err := s.Store.User(ctx, id)
	if err != nil {
		return connector.StoredUser{}, err
	}
	if !found {
		return connector.StoredUser{}, ErrNoUser
	}
	return u, nil
}

// FindUser returns the user whose email or, failing that, user id is
// handle, and whether there is one.
func (s Service) FindUser(ctx context.Context, handle string) (connector.StoredUser, bool, error) {
	handle = strings.TrimSpace(handle)
	u, found, err := s.Store.UserByEmail(ctx, strings.ToLower(handle))
	if err != nil || found {
		return u, found, err
	}
	return s.Store.UserByUserID(ctx, handle)
}

// Match returns the user whose user id is userID or, when nobody has it,
// whose email is email ignoring case, and whether there is one. An empty
// userID is nobody's.
func (s Service) Match(ctx context.Context, userID, email string) (connector.StoredUser, bool, error) {
	if userID != "" {
		u, found, err := s.Store.UserByUserID(ctx, userID)
		if err != nil || found {
			return u, found, err
		}
	}
	return s.Store.UserByEmail(ctx, strings.ToLower(email))
}

// CreateUser creates a user from p. A user whose user id is on a group's
// pre-assigned list joins that group as they are created; every way of
// creating a user goes through here.
func (s Service) CreateUser(ctx context.Context, p Profile) (connector.StoredUser, error) {
	p, err := s.checkProfile(ctx, 0, p)
	if err != nil {
		return connector.StoredUser{}, err
	}
	groups, err := s.preassigned(ctx, p.UserID)
	if err != nil {
		return connector.StoredUser{}, err
	}
	u := stored(p)
	u.CreatedAt = s.Clock.Now()
	id, err := s.Store.CreateUser(ctx, u, groups)
	if err != nil {
		return connector.StoredUser{}, err
	}
	return s.User(ctx, id)
}

// preassigned returns the ids of the groups whose pre-assigned list holds
// userID; none when userID is empty.
func (s Service) preassigned(ctx context.Context, userID string) ([]int64, error) {
	if userID == "" {
		return nil, nil
	}
	groups, err := s.Store.Groups(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, g := range groups {
		if slices.Contains(g.PreassignedUserIDs, userID) {
			ids = append(ids, g.ID)
		}
	}
	return ids, nil
}

// UpdateUser replaces the profile of the user whose id is id with p.
func (s Service) UpdateUser(ctx context.Context, id int64, p Profile) (connector.StoredUser, error) {
	p, err := s.checkProfile(ctx, id, p)
	if err != nil {
		return connector.StoredUser{}, err
	}
	u := stored(p)
	u.ID = id
	found, err := s.Store.UpdateUser(ctx, u)
	if err != nil {
		return connector.StoredUser{}, err
	}
	if !found {
		return connector.StoredUser{}, ErrNoUser
	}
	return s.User(ctx, id)
}

// ChangeUser is UpdateUser made by viewer, who may not make their own
// account expire now or earlier: that would end it, as deleting it would.
func (s Service) ChangeUser(ctx context.Context, viewer auth.User, id int64, p Profile) (connector.StoredUser, error) {
	if id == viewer.ID && p.ExpiresAt != nil && !p.ExpiresAt.After(s.Clock.Now()) {
		return connector.StoredUser{}, ErrOwnExpiry
	}
	return s.UpdateUser(ctx, id, p)
}

// DeleteUser soft-deletes the user whose id is id, which viewer may not be.
// A user deleted already keeps their deletion time.
func (s Service) DeleteUser(ctx context.Context, viewer auth.User, id int64) error {
	if id == viewer.ID {
		return ErrOwnAccount
	}
	u, err := s.User(ctx, id)
	if err != nil || u.DeletedAt != nil {
		return err
	}
	now := s.Clock.Now()
	return s.changed(s.Store.SetUserDeleted(ctx, id, &now))
}

// RestoreUser undoes the deletion of the user whose id is id.
func (s Service) RestoreUser(ctx context.Context, id int64) error {
	return s.changed(s.Store.SetUserDeleted(ctx, id, nil))
}

// SetCode gives the user whose id is id the door code code, 6 to 20 digits
// that no other user has.
func (s Service) SetCode(ctx context.Context, id int64, code string) error {
	if problem := digitsProblem(code, minCodeDigits, maxCodeDigits); problem != "" {
		return check.Problems{{Field: "code", Message: problem}}
	}
	return s.storeCode(ctx, id, code, "code")
}

// CodePrefix is what every door code u chooses starts with: u's user id
// with its leading zeros stripped, which must leave digits.
func CodePrefix(u connector.StoredUser) (string, error) {
	if u.UserID == nil {
		return "", ErrNoUserID
	}
	prefix := strings.TrimLeft(*u.UserID, "0")
	if prefix == "" || strings.Trim(prefix, digits) != "" {
		return "", ErrUserIDNotDigits
	}
	return prefix, nil
}

// SetOwnCode gives the user whose id is id the door code they chose: their
// prefix followed by chosen, 4 to 8 digits, which no other user has.
func (s Service) SetOwnCode(ctx context.Context, id int64, chosen string) error {
	u, err := s.User(ctx, id)
	if err != nil {
		return err
	}
	prefix, err := CodePrefix(u)
	if err != nil {
		return err
	}
	if problem := digitsProblem(chosen, minOwnDigits, maxOwnDigits); problem != "" {
		return check.Problems{{Field: "digits", Message: problem}}
	}
	return s.storeCode(ctx, id, prefix+chosen, "digits")
}

// storeCode stores the hash of code as the door code of the user whose id
// is id, unless another user has it, which is a problem with field.
func (s Service) storeCode(ctx context.Context, id int64, code, field string) error {
	hash, err := s.Auth.CodeHash(ctx, code)
	if err != nil {
		return err
	}
	holder, taken, err := s.Store.UserByCodeHash(ctx, hash)
	if err != nil {
		return err
	}
	if problem := conflict(id, holder.ID, taken, codeConflict); problem != "" {
		return check.Problems{{Field: field, Message: problem}}
	}
	return s.changed(s.Store.SetCodeHash(ctx, id, &hash))
}

// ClearCode takes the door code from the user whose id is id.
func (s Service) ClearCode(ctx context.Context, id int64) error {
	return s.changed(s.Store.SetCodeHash(ctx, id, nil))
}

// changed turns a store's report of whether the user was there into an
// error.
func (s Service) changed(found bool, err error) error {
	if err == nil && !found {
		return ErrNoUser
	}
	return err
}

// digitsProblem says what is wrong with s as least to most digits; empty
// when nothing is.
func digitsProblem(s string, least, most int) string {
	if strings.Trim(s, digits) != "" || len(s) < least || len(s) > most {
		return fmt.Sprintf("Enter %d to %d digits.", least, most)
	}
	return ""
}

func stored(p Profile) connector.StoredUser {
	u := connector.StoredUser{
		Email: p.Email, FirstName: p.FirstName, LastName: p.LastName,
		DisplayName: p.DisplayName, ExpiresAt: p.ExpiresAt,
	}
	if p.UserID != "" {
		u.UserID = &p.UserID
	}
	return u
}

// checkProfile returns p tidied, or the problems with it. id is the user p
// describes, zero for a new one, whose own email and user id are no
// conflict.
func (s Service) checkProfile(ctx context.Context, id int64, p Profile) (Profile, error) {
	p = tidy(p)
	emailProblem, emailErr := s.emailProblem(ctx, id, p.Email)
	userIDProblem, userIDErr := s.userIDProblem(ctx, id, p.UserID)
	if err := cmp.Or(emailErr, userIDErr); err != nil {
		return p, err
	}
	var found check.List
	found.Add("email", emailProblem)
	found.Add("user_id", userIDProblem)
	found.Add("first_name", check.TooLong(p.FirstName, maxNameLength, check.LongText))
	found.Add("last_name", check.TooLong(p.LastName, maxNameLength, check.LongText))
	found.Add("display_name", check.TooLong(p.DisplayName, maxNameLength, check.LongText))
	if p.DisplayName == "" {
		found.Add("display_name", "Enter a display name, or a first or last name.")
	}
	return p, found.Err()
}

func tidy(p Profile) Profile {
	p.Email = strings.ToLower(strings.TrimSpace(p.Email))
	p.UserID = strings.TrimSpace(p.UserID)
	p.FirstName = strings.TrimSpace(p.FirstName)
	p.LastName = strings.TrimSpace(p.LastName)
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if p.DisplayName == "" {
		p.DisplayName = strings.TrimSpace(p.FirstName + " " + p.LastName)
	}
	return p
}

func (s Service) emailProblem(ctx context.Context, id int64, email string) (string, error) {
	if email == "" {
		return "Enter an email address.", nil
	}
	if !validEmail(email) {
		return "Enter an email address like name@example.com.", nil
	}
	if long := check.TooLong(email, maxEmailLength, "Use at most %d characters for the email address."); long != "" {
		return long, nil
	}
	holder, taken, err := s.Store.UserByEmail(ctx, email)
	return conflict(id, holder.ID, taken, "Another user has this email address."), err
}

func validEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email
}

func (s Service) userIDProblem(ctx context.Context, id int64, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}
	if !userIDPattern.MatchString(userID) {
		return userIDHint, nil
	}
	holder, taken, err := s.Store.UserByUserID(ctx, userID)
	return conflict(id, holder.ID, taken, "Another user has this user id."), err
}
