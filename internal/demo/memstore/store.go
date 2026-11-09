// Package memstore is an in-memory stand-in for the Store that the demo
// binary runs the application over. It keeps what the Postgres store keeps,
// in the same orders and under the same constraints, in memory guarded by
// one lock; nothing is kept when the process ends.
package memstore

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/connector"
)

var _ connector.Store = (*Store)(nil)

// The refusals the store answers with where the database's constraints
// would refuse.
var (
	errDuplicate   = errors.New("memstore: a unique value is taken")
	errNoReference = errors.New("memstore: a referenced row does not exist")
	errReferenced  = errors.New("memstore: the row is still referenced")
	errNil         = errors.New("memstore: a required value is nil")
)

// Store is the in-memory store. Its zero value is not ready; use New.
type Store struct {
	mu       sync.Mutex
	migrated bool

	settings    map[string]connector.StoredSetting
	codeHashing *string
	signInImage *connector.StoredImage

	users    map[int64]connector.StoredUser
	groups   map[int64]connector.StoredGroup
	members  map[pair]bool
	sessions map[string]connector.StoredSession
	tokens   map[int64]connector.StoredToken

	schedules map[int64]connector.StoredSchedule
	doors     map[int64]connector.StoredDoor
	// zoneDoors is the one relation of zones and their doors, keyed by
	// zone first.
	zoneDoors   map[pair]bool
	pictures    map[int64]picture
	zones       map[int64]connector.StoredZone
	rules       map[int64]connector.StoredRule
	overrides   map[int64]connector.StoredOverride
	credentials map[int64]connector.StoredCredential

	events  []connector.StoredEvent
	samples []connector.StoredSample
	rollups map[rollupKey]connector.StoredRollup

	firmware map[int64]firmwareRow
	updates  map[int64]connector.StoredUpdate
	requests map[int64]connector.StoredRequest

	// last holds the last id each aggregate gave, keyed by its name.
	last map[string]int64
}

// pair links two rows by id, such as a group and a member or a zone and a
// door.
type pair struct {
	a, b int64
}

// New returns an empty store, ready for Migrate.
func New() *Store {
	return &Store{
		settings:    map[string]connector.StoredSetting{},
		users:       map[int64]connector.StoredUser{},
		groups:      map[int64]connector.StoredGroup{},
		members:     map[pair]bool{},
		sessions:    map[string]connector.StoredSession{},
		tokens:      map[int64]connector.StoredToken{},
		schedules:   map[int64]connector.StoredSchedule{},
		doors:       map[int64]connector.StoredDoor{},
		zoneDoors:   map[pair]bool{},
		pictures:    map[int64]picture{},
		zones:       map[int64]connector.StoredZone{},
		rules:       map[int64]connector.StoredRule{},
		overrides:   map[int64]connector.StoredOverride{},
		credentials: map[int64]connector.StoredCredential{},
		rollups:     map[rollupKey]connector.StoredRollup{},
		firmware:    map[int64]firmwareRow{},
		updates:     map[int64]connector.StoredUpdate{},
		requests:    map[int64]connector.StoredRequest{},
		last:        map[string]int64{},
	}
}

// Migrate seeds what the migrations seed, once: the administrators group
// and the Always schedule.
func (s *Store) Migrate(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.migrated {
		return nil
	}
	s.migrated = true
	groupID := s.next("groups")
	s.groups[groupID] = connector.StoredGroup{
		ID: groupID, Slug: "administrators", Name: "Administrators", Description: "Members may do everything.",
		Roles: []string{"admin"}, PreassignedUserIDs: []string{},
	}
	scheduleID := s.next("schedules")
	everyDay := []time.Weekday{time.Sunday, time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday}
	s.schedules[scheduleID] = connector.StoredSchedule{
		ID: scheduleID, Name: "Always", Windows: []connector.StoredWindow{{Days: everyDay}},
		Excluded: []time.Time{}, Builtin: true,
	}
	return nil
}

// next gives the next id of the aggregate named name.
func (s *Store) next(name string) int64 {
	s.last[name]++
	return s.last[name]
}

// byName orders items by name compared byte by byte after lowercasing, and
// then by id, as every name-ordered list is.
func byName[T any](items []T, name func(T) string, id func(T) int64) {
	slices.SortFunc(items, func(x, y T) int {
		if c := strings.Compare(strings.ToLower(name(x)), strings.ToLower(name(y))); c != 0 {
			return c
		}
		return cmp.Compare(id(x), id(y))
	})
}

// byID orders items by id.
func byID[T any](items []T, id func(T) int64) {
	slices.SortFunc(items, func(x, y T) int { return cmp.Compare(id(x), id(y)) })
}

// newestFirst orders items by time, latest first, and then by id, highest
// first, as every newest-first list is.
func newestFirst[T any](items []T, at func(T) time.Time, id func(T) int64) {
	slices.SortFunc(items, func(x, y T) int {
		if c := at(y).Compare(at(x)); c != 0 {
			return c
		}
		return cmp.Compare(id(y), id(x))
	})
}

// nameTaken reports whether a row of rows other than the one whose id is
// id has name, ignoring case, as the unique indexes on lower(name) refuse.
func nameTaken[T any](rows map[int64]T, rowName func(T) string, name string, id int64) bool {
	for other, row := range rows {
		if other != id && strings.EqualFold(rowName(row), name) {
			return true
		}
	}
	return false
}

// allExist reports whether rows holds a row for each of ids, as the
// foreign keys require.
func allExist[T any](rows map[int64]T, ids []int64) bool {
	for _, id := range ids {
		if _, ok := rows[id]; !ok {
			return false
		}
	}
	return true
}

// within reports whether t is at or after from and before to.
func within(t, from, to time.Time) bool {
	return !t.Before(from) && t.Before(to)
}

// page is the part of items that skips offset and holds at most limit.
func page[T any](items []T, offset, limit int) []T {
	start := min(max(offset, 0), len(items))
	end := min(start+max(limit, 0), len(items))
	return append([]T{}, items[start:end]...)
}

// contains reports whether search is in any of fields, joined by single
// spaces, ignoring case.
func contains(search string, fields ...string) bool {
	joined := strings.ToLower(strings.Join(fields, " "))
	return strings.Contains(joined, strings.ToLower(search))
}

// clone is a copy of what v points at, nil for nil.
func clone[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// list is a copy of items that is never nil, as the database returns an
// empty array.
func list[T any](items []T) []T {
	return append([]T{}, items...)
}

// sortedIDs is a copy of ids, ascending and without repeats, as a link
// table holds them.
func sortedIDs(ids []int64) []int64 {
	c := list(ids)
	slices.Sort(c)
	return slices.Compact(c)
}
