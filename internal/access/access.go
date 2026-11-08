// Package access decides whether a door code grants entry.
package access

import "strings"

// Allowed reports whether code appears as a line of codes. An empty code is
// never allowed.
func Allowed(codes []byte, code string) bool {
	if code == "" {
		return false
	}
	for _, line := range strings.Split(string(codes), "\n") {
		if strings.TrimSpace(line) == code {
			return true
		}
	}
	return false
}
