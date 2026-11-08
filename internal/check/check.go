// Package check describes what is wrong with submitted values, field by
// field, so every form and API body reports problems the same way.
package check

import (
	"fmt"
	"unicode/utf8"
)

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
