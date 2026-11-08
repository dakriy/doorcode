// Package systemclock implements connector.Clock over the system clock.
package systemclock

import "time"

// Clock reads the system clock.
type Clock struct{}

// Now returns the system time.
func (Clock) Now() time.Time {
	return time.Now()
}

// Alarm returns a channel that receives when the system clock reaches at.
func (Clock) Alarm(at time.Time) <-chan time.Time {
	return time.After(time.Until(at))
}
