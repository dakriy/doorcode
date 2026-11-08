package decision

import "time"

// Limiter keeps a token bucket for each door, so a door cannot have codes
// checked faster than its rate limit: the bucket holds up to a burst of
// checks and refills at the door's checks a minute. Only the broker loop
// uses it.
type Limiter struct {
	buckets map[int64]bucket
}

// bucket is how many checks a door has left, as of at.
type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimiter returns a Limiter whose buckets are all full.
func NewLimiter() *Limiter {
	return &Limiter{buckets: map[int64]bucket{}}
}

// Take spends one check of the bucket of the door whose id is doorID,
// which refills at perMinute and holds burst, at now, and reports whether
// there was one to spend.
func (l *Limiter) Take(doorID int64, perMinute, burst int, now time.Time) bool {
	b, held := l.buckets[doorID]
	if !held {
		b = bucket{tokens: float64(burst), at: now}
	}
	refilled := b.tokens + now.Sub(b.at).Minutes()*float64(perMinute)
	b = bucket{tokens: min(refilled, float64(burst)), at: now}
	spent := b.tokens >= 1
	if spent {
		b.tokens--
	}
	l.buckets[doorID] = b
	return spent
}
