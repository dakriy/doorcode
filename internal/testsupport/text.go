package testsupport

import (
	"slices"
	"strconv"
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

// Itoa is n in decimal.
func Itoa(n int) string {
	return strconv.Itoa(n)
}

// Upper is s upper-cased, as a setting's key is in its variable's name.
func Upper(s string) string {
	return strings.ToUpper(s)
}

// Samples is the sample lines of a metrics scrape's body that start with
// prefix, in order, leaving out the HELP and TYPE comments.
func Samples(body, prefix string) []string {
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

// WithoutFamilies is a metrics scrape's body with every family whose name
// starts with prefix left out, its HELP and TYPE comments with it.
func WithoutFamilies(body, prefix string) string {
	var kept []string
	for _, line := range strings.SplitAfter(body, "\n") {
		if strings.HasPrefix(line, prefix) || strings.HasPrefix(line, "# HELP "+prefix) || strings.HasPrefix(line, "# TYPE "+prefix) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}
