// Package systemclock implements connector.Clock over the system clock.
package systemclock

import "time"

// Clock reads the system clock.
type Clock struct{}

// Now returns the system time.
func (Clock) Now() time.Time {
	return time.Now()
}
