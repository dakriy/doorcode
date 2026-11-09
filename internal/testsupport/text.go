package testsupport

import (
	"slices"
	"strings"
)

// Repeat is s n times, for a value just past a length limit.
func Repeat(s string, n int) string {
	return strings.Repeat(s, n)
}

// Listed reports whether status is one of statuses.
func Listed(statuses []int, status int) bool {
	return slices.Contains(statuses, status)
}
