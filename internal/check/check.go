// Package check describes what is wrong with submitted values, field by
// field, so every form and API body reports problems the same way.
package check

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

// slugPattern is what a slug may look like: lowercase words of letters and
// digits joined by single dashes.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// MaxSlugLength is the longest a slug may be.
const MaxSlugLength = 64

// SlugHint says what a slug may look like.
var SlugHint = fmt.Sprintf("Use up to %d lowercase letters and digits, in words joined by single dashes.", MaxSlugLength)

// Slug reports whether s is a slug, such as a group's.
func Slug(s string) bool {
	return slugPattern.MatchString(s) && len(s) <= MaxSlugLength
}

// MaxHostLength is the longest a host name may be.
const MaxHostLength = 253

// HostHint says what a host name may be, as HostName takes it.
var HostHint = fmt.Sprintf("a host name of up to %d letters, digits, dots, and hyphens", MaxHostLength)

// HostName reports whether s is a host name: 1 to MaxHostLength letters,
// digits, dots, and hyphens.
func HostName(s string) bool {
	if s == "" || utf8.RuneCountInString(s) > MaxHostLength {
		return false
	}
	for _, r := range s {
		if !hostRune(r) {
			return false
		}
	}
	return true
}

// hostRune reports whether r may appear in a host name: a letter, a
// digit, a dot, or a hyphen.
func hostRune(r rune) bool {
	letter := ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
	digit := '0' <= r && r <= '9'
	return letter || digit || r == '.' || r == '-'
}

// Problem is what is wrong with one submitted field.
type Problem struct {
	Field   string
	Message string
}

// Problems is what is wrong with a submission, in field order. It is an
// error whose text is the first problem.
type Problems []Problem

func (p Problems) Error() string {
	return p[0].Message
}

// ByField maps each field to its problem.
func (p Problems) ByField() map[string]string {
	out := map[string]string{}
	for _, problem := range p {
		out[problem.Field] = problem.Message
	}
	return out
}

// List collects the problems found, in order, skipping fields without one.
type List Problems

// Add records message as the problem with field, unless it is empty.
func (l *List) Add(field, message string) {
	if message != "" {
		*l = append(*l, Problem{Field: field, Message: message})
	}
}

// Err is the problems as an error, nil when there are none.
func (l List) Err() error {
	if len(l) == 0 {
		return nil
	}
	return Problems(l)
}

// LongText is the problem with text longer than its limit.
const LongText = "Use at most %d characters."

// TooLong is message, with limit in place of its verb, when value has more
// than limit characters.
func TooLong(value string, limit int, message string) string {
	if utf8.RuneCountInString(value) > limit {
		return fmt.Sprintf(message, limit)
	}
	return ""
}

// Required is message when value is empty.
func Required(value, message string) string {
	if value == "" {
		return message
	}
	return ""
}

// Distinct is ids without repeats, ascending, and never nil.
func Distinct(ids []int64) []int64 {
	return append([]int64{}, slices.Compact(slices.Sorted(slices.Values(ids)))...)
}

// Unknown is message, with the id in place of its verb, for the first of
// given that is no id of items, which id reads.
func Unknown[T any](given []int64, items []T, id func(T) int64, message string) string {
	for _, g := range given {
		if !slices.ContainsFunc(items, func(item T) bool { return id(item) == g }) {
			return fmt.Sprintf(message, g)
		}
	}
	return ""
}
