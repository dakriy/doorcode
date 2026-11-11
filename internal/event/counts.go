package event

import (
	"cmp"
	"maps"
	"slices"
	"sync"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/protocol"
)

// Kind is what an event is counted by: the door it happened at, its
// type, outcome, and reason, and the reason a controller gave for
// starting, as boundedStartReason keeps it, empty for any other event.
type Kind struct {
	DoorID      int64
	Type        string
	Outcome     string
	Reason      string
	StartReason string
}

// Count is how many events of a kind were recorded.
type Count struct {
	Kind
	N int
}

// Counts is how many events of each kind were recorded since the server
// started, kept in memory. It is safe for concurrent use, since the
// broker's lanes record events at once.
type Counts struct {
	mu sync.Mutex
	n  map[Kind]int
}

// NewCounts returns counts of no event.
func NewCounts() *Counts {
	return &Counts{n: map[Kind]int{}}
}

// add counts e.
func (c *Counts) add(e connector.StoredEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[Kind{DoorID: e.DoorID, Type: e.Type, Outcome: e.Outcome, Reason: e.Reason, StartReason: boundedStartReason(e.StartReason)}]++
}

// otherStartReason is what every start reason the server does not know
// is counted as, so no controller can grow the counts without limit.
const otherStartReason = "other"

// boundedStartReason is reason when it is empty or one the server
// knows, else otherStartReason.
func boundedStartReason(reason string) string {
	if reason == "" || slices.Contains(protocol.StartReasons, reason) {
		return reason
	}
	return otherStartReason
}

// Forget drops the counts of the door whose id is doorID, which is gone
// for good.
func (c *Counts) Forget(doorID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.DeleteFunc(c.n, func(k Kind, _ int) bool { return k.DoorID == doorID })
}

// All is the count of every kind recorded, by door id, then type,
// outcome, reason, and start reason.
func (c *Counts) All() []Count {
	c.mu.Lock()
	out := make([]Count, 0, len(c.n))
	for k, n := range c.n {
		out = append(out, Count{Kind: k, N: n})
	}
	c.mu.Unlock()
	slices.SortFunc(out, func(a, b Count) int {
		return cmp.Or(
			cmp.Compare(a.DoorID, b.DoorID), cmp.Compare(a.Type, b.Type), cmp.Compare(a.Outcome, b.Outcome),
			cmp.Compare(a.Reason, b.Reason), cmp.Compare(a.StartReason, b.StartReason))
	})
	return out
}
