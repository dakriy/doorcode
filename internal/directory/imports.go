package directory

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/check"
	"github.com/dakriy/doorcode/internal/connector"
)

// What an import does with a row: create a user who joins the groups
// listed, add an existing user to the groups listed they are not in,
// leave an existing user who is in all of them, or skip a row with
// problems.
const (
	ImportCreate    = "create"
	ImportUpdate    = "update"
	ImportUnchanged = "unchanged"
	ImportSkip      = "skip"
)

// ImportRow is one row of an import file and what importing it does.
type ImportRow struct {
	// Line is the line of the file the row starts on; the header is
	// line 1.
	Line   int
	UserID string
	Email  string
	// Groups is the slugs the row lists, and Joins those of the groups the
	// user joins by importing it: for a new user, also the groups whose
	// pre-assigned user ids hold theirs.
	Groups []string
	Joins  []string
	// Outcome is ImportCreate, ImportUpdate, ImportUnchanged, or
	// ImportSkip, and Problems what makes a row skipped, each naming its
	// column (empty for the row as a whole).
	Outcome  string
	Problems check.Problems
	// User is the user's id and Name their display name: the existing
	// user's, or the new user's once created (until then User is zero).
	User int64
	Name string
	// Applied is true once applying the import made the row's change.
	Applied bool
	profile Profile
	joinIDs []int64
}

// Import is what importing a file does, row by row.
type Import struct {
	Rows []ImportRow
}

// Count is how many of the rows have outcome.
func (imp Import) Count(outcome string) int {
	n := 0
	for _, row := range imp.Rows {
		if row.Outcome == outcome {
			n++
		}
	}
	return n
}

// importer is what planning an import has seen so far: the groups, the
// person importing, the site timezone dates are read in, and the line
// each user id and each new user's email was first on.
type importer struct {
	viewer  auth.User
	groups  []connector.StoredGroup
	zone    *time.Location
	userIDs map[string]int
	emails  map[string]int
}

// PreviewImport is what importing file on behalf of viewer would do, with
// its dates in zone, without changing anything: the file is read, then
// each of its rows planned. It reports check.Problems for the field
// ImportFileField, or ErrImportTooBig, when the file cannot be read at all.
func (s Service) PreviewImport(ctx context.Context, viewer auth.User, file []byte, zone *time.Location) (Import, error) {
	records, err := readImport(file)
	if err != nil {
		return Import{}, err
	}
	return s.planImport(ctx, viewer, records, zone)
}

// ApplyImport imports file on behalf of viewer as PreviewImport describes
// it, row by row: it creates users, through the same path as CreateUser,
// and adds existing users to groups, skipping the rows with problems. When
// the store fails, it returns the import so far, its rows applied until
// then marked Applied, with the error. Applying a file again is safe: users
// created already are found by their user id and gain only what they lack.
func (s Service) ApplyImport(ctx context.Context, viewer auth.User, file []byte, zone *time.Location) (Import, error) {
	imp, err := s.PreviewImport(ctx, viewer, file, zone)
	if err != nil {
		return imp, err
	}
	for i, row := range imp.Rows {
		imp.Rows[i], err = s.applyRow(ctx, row)
		if err != nil {
			return imp, err
		}
	}
	return imp, nil
}

// Planning: what each row of a file that could be read does.

// planImport is what importing records on behalf of viewer does, with
// their dates in zone.
func (s Service) planImport(ctx context.Context, viewer auth.User, records []record, zone *time.Location) (Import, error) {
	groups, err := s.Store.Groups(ctx)
	if err != nil {
		return Import{}, err
	}
	im := importer{viewer: viewer, groups: groups, zone: zone, userIDs: map[string]int{}, emails: map[string]int{}}
	imp := Import{Rows: []ImportRow{}}
	for _, rec := range records {
		row, err := s.plan(ctx, &im, rec)
		if err != nil {
			return Import{}, err
		}
		imp.Rows = append(imp.Rows, row)
	}
	return imp, nil
}

// plan is what importing rec does.
func (s Service) plan(ctx context.Context, im *importer, rec record) (ImportRow, error) {
	row := ImportRow{
		Line: rec.line, UserID: rec.values[ColumnUserID], Email: strings.ToLower(rec.values[ColumnEmail]),
		Groups: slugsOf(rec.values[ColumnGroups]), Joins: []string{}, Outcome: ImportSkip,
	}
	problems := rec.problems
	problems.Add(ColumnUserID, im.userIDProblem(row))
	listed := im.listedGroups(row.Groups, &problems)
	if len(problems) > 0 {
		row.Problems = check.Problems(problems)
		return row, nil
	}
	u, found, err := s.Store.UserByUserID(ctx, row.UserID)
	if err != nil {
		return row, err
	}
	if found {
		return planExisting(row, u, listed), nil
	}
	return s.planNew(ctx, im, row, rec, listed)
}

// userIDProblem says what is wrong with row's user id; empty when nothing
// is. It remembers the line of each user id it is given.
func (im *importer) userIDProblem(row ImportRow) string {
	if row.UserID == "" {
		return "Enter the user's id."
	}
	if !userIDPattern.MatchString(row.UserID) {
		return userIDHint
	}
	if first, seen := im.userIDs[row.UserID]; seen {
		return fmt.Sprintf("This user id is also on line %d; list each user once.", first)
	}
	im.userIDs[row.UserID] = row.Line
	return ""
}

// listedGroups returns the groups slugs names, adding to problems each slug
// that names none and each group carrying roles the viewer does not hold.
func (im *importer) listedGroups(slugs []string, problems *check.List) []connector.StoredGroup {
	var listed []connector.StoredGroup
	for _, slug := range slugs {
		i := slices.IndexFunc(im.groups, func(g connector.StoredGroup) bool { return g.Slug == slug })
		if i < 0 {
			problems.Add(ColumnGroups, fmt.Sprintf("No group has the slug “%s”.", slug))
			continue
		}
		if !mayChange(im.viewer, im.groups[i].Roles) {
			problems.Add(ColumnGroups, fmt.Sprintf("The group “%s” carries roles you do not hold, so you cannot add people to it.", slug))
			continue
		}
		listed = append(listed, im.groups[i])
	}
	return listed
}

// joining adds g to the groups row's user joins by importing it.
func (row *ImportRow) joining(g connector.StoredGroup) {
	row.Joins = append(row.Joins, g.Slug)
	row.joinIDs = append(row.joinIDs, g.ID)
}

// planExisting is what importing row does to u, who exists: they join the
// groups listed they are not in. A deleted user is skipped.
func planExisting(row ImportRow, u connector.StoredUser, listed []connector.StoredGroup) ImportRow {
	row.User, row.Name = u.ID, u.DisplayName
	if u.DeletedAt != nil {
		row.Problems = check.Problems{{Field: ColumnUserID, Message: "This user is deleted. Restore them before importing them."}}
		return row
	}
	for _, g := range listed {
		if !slices.ContainsFunc(u.Groups, func(m connector.UserGroup) bool { return m.ID == g.ID }) {
			row.joining(g)
		}
	}
	row.Outcome = ImportUpdate
	if len(row.Joins) == 0 {
		row.Outcome = ImportUnchanged
	}
	return row
}

// planNew is what importing row, read from rec, does when nobody has its
// user id: a user is created from its columns who joins the groups listed
// and, as CreateUser has them, those whose pre-assigned user ids hold
// theirs.
func (s Service) planNew(ctx context.Context, im *importer, row ImportRow, rec record, listed []connector.StoredGroup) (ImportRow, error) {
	p, problems, err := s.newUser(ctx, im, row, rec)
	if err != nil {
		return row, err
	}
	if len(problems) > 0 {
		row.Problems = check.Problems(problems)
		return row, nil
	}
	row.Outcome, row.Name, row.profile = ImportCreate, p.DisplayName, p
	for _, g := range listed {
		row.joining(g)
	}
	for _, g := range preassignedTo(im.groups, row.UserID) {
		if !slices.Contains(row.Joins, g.Slug) {
			row.Joins = append(row.Joins, g.Slug)
		}
	}
	return row, nil
}

// newUser is the checked profile of the user row, read from rec, creates,
// or what is wrong with it.
func (s Service) newUser(ctx context.Context, im *importer, row ImportRow, rec record) (Profile, check.List, error) {
	var problems check.List
	expires, expiryProblem := im.expiry(rec.values[ColumnExpiresAt])
	problems.Add(ColumnExpiresAt, expiryProblem)
	if row.Email == "" {
		problems.Add(ColumnEmail, "Nobody has this user id yet, and a new user needs an email address.")
		return Profile{}, problems, nil
	}
	p, err := s.checkProfile(ctx, 0, profileOf(row, rec, expires))
	var profileProblems check.Problems
	if errors.As(err, &profileProblems) {
		problems = append(problems, profileProblems...)
	} else if err != nil {
		return Profile{}, nil, err
	}
	if len(problems) == 0 {
		problems.Add(ColumnEmail, im.emailProblem(row))
	}
	return p, problems, nil
}

// profileOf is the profile of the user row, read from rec, creates,
// expiring at expires: called by their names, or else their email.
func profileOf(row ImportRow, rec record, expires *time.Time) Profile {
	first, last := rec.values[ColumnFirstName], rec.values[ColumnLastName]
	return Profile{
		Email: row.Email, UserID: row.UserID, FirstName: first, LastName: last,
		DisplayName: cmp.Or(strings.TrimSpace(first+" "+last), row.Email), ExpiresAt: expires,
	}
}

// expiry reads an expires_at value: the start of that date in the site
// timezone, nil when it is empty, or what is wrong with it.
func (im *importer) expiry(value string) (*time.Time, string) {
	if value == "" {
		return nil, ""
	}
	day, err := time.ParseInLocation(time.DateOnly, value, im.zone)
	if err != nil {
		return nil, "Enter a date like 2030-06-30, or leave it empty for an account that never expires."
	}
	return &day, ""
}

// emailProblem says when row's email, which a new user is created with,
// is on an earlier line for another new user; it remembers the line of
// each email it is given.
func (im *importer) emailProblem(row ImportRow) string {
	if first, seen := im.emails[row.Email]; seen {
		return fmt.Sprintf("This email is also on line %d for another new user.", first)
	}
	im.emails[row.Email] = row.Line
	return ""
}

// Applying: making each planned row's change.

// applyRow makes row's change, when it has one, and returns it marked
// Applied once made; a new user's row then names them.
func (s Service) applyRow(ctx context.Context, row ImportRow) (ImportRow, error) {
	var err error
	switch row.Outcome {
	case ImportCreate:
		var u connector.StoredUser
		u, err = s.create(ctx, row.profile, row.joinIDs)
		row.User = u.ID
	case ImportUpdate:
		err = s.join(ctx, row.User, row.joinIDs)
	default:
		return row, nil
	}
	row.Applied = err == nil
	return row, err
}

// join makes the user whose id is userID a member of the groups whose ids
// are groupIDs.
func (s Service) join(ctx context.Context, userID int64, groupIDs []int64) error {
	for _, id := range groupIDs {
		if err := s.Store.AddMember(ctx, id, userID); err != nil {
			return err
		}
	}
	return nil
}
