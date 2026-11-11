// Package traffic counts the HTTP requests the server answers, by method,
// route, and status, and how long they took, kept in memory from when the
// server starts, and gives them as metric families.
package traffic

import (
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/dakriy/doorcode/internal/exposition"
)

// Unmatched is the route of a request no route of the table matched, so
// a path a client makes up never becomes a label value.
const Unmatched = "unmatched"

// otherMethod is the method of a request whose method is none of HTTP's
// own, so a method a client makes up never becomes a label value.
const otherMethod = "other"

// buckets are the upper bounds, in seconds, of the duration histogram's
// buckets, Prometheus's defaults.
var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// methods are HTTP's own methods.
var methods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
}

// key is what a request's duration is told apart by.
type key struct {
	method, route string
}

// series is what the requests of one method and route came to: how many
// answered each status, and their durations.
type series struct {
	statuses map[int]int
	// inBucket counts, for each of buckets, the requests that took no
	// longer than its bound.
	inBucket []int
	seconds  float64
	count    int
}

// Requests is the requests counted since it was made. It is safe for
// concurrent use, since the server answers requests at once.
type Requests struct {
	mu     sync.Mutex
	series map[key]*series
}

// New returns a count of no request.
func New() *Requests {
	return &Requests{series: map[key]*series{}}
}

// Add counts a request of method to route that answered status after
// seconds.
func (r *Requests) Add(method, route string, status int, seconds float64) {
	if !slices.Contains(methods, method) {
		method = otherMethod
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key{method: method, route: route}
	s := r.series[k]
	if s == nil {
		s = &series{statuses: map[int]int{}, inBucket: make([]int, len(buckets))}
		r.series[k] = s
	}
	s.statuses[status]++
	s.count++
	s.seconds += seconds
	for i, bound := range buckets {
		if seconds <= bound {
			s.inBucket[i]++
		}
	}
}

// Families is the request counts and the duration histogram, by route,
// then method, then status.
func (r *Requests) Families() []exposition.Family {
	total := exposition.Family{
		Name: "doorcode_http_requests_total", Help: "The HTTP requests answered since the server started, by method, route, and status.", Type: exposition.Counter,
	}
	duration := exposition.Family{
		Name: "doorcode_http_request_duration_seconds", Help: "How long the server took to answer HTTP requests, by method and route, in seconds.", Type: exposition.Histogram,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range r.keys() {
		s := r.series[k]
		labels := []exposition.Label{{Name: "method", Value: k.method}, {Name: "route", Value: k.route}}
		for _, status := range sortedStatuses(s.statuses) {
			withStatus := append(slices.Clone(labels), exposition.Label{Name: "status", Value: strconv.Itoa(status)})
			total.Samples = append(total.Samples, exposition.Sample{Labels: withStatus, Value: float64(s.statuses[status])})
		}
		duration.Samples = append(duration.Samples, histogram(labels, s)...)
	}
	return []exposition.Family{total, duration}
}

// keys is every key counted, by route, then method.
func (r *Requests) keys() []key {
	out := make([]key, 0, len(r.series))
	for k := range r.series {
		out = append(out, k)
	}
	slices.SortFunc(out, func(a, b key) int {
		return cmp.Or(cmp.Compare(a.route, b.route), cmp.Compare(a.method, b.method))
	})
	return out
}

// sortedStatuses is the statuses counted in statuses, in ascending order.
func sortedStatuses(statuses map[int]int) []int {
	out := make([]int, 0, len(statuses))
	for status := range statuses {
		out = append(out, status)
	}
	slices.Sort(out)
	return out
}

// histogram is the bucket, sum, and count samples of s, labelled labels.
func histogram(labels []exposition.Label, s *series) []exposition.Sample {
	var out []exposition.Sample
	for i, bound := range buckets {
		out = append(out, bucket(labels, strconv.FormatFloat(bound, 'f', -1, 64), s.inBucket[i]))
	}
	return append(out,
		bucket(labels, "+Inf", s.count),
		exposition.Sample{Suffix: "_sum", Labels: labels, Value: s.seconds},
		exposition.Sample{Suffix: "_count", Labels: labels, Value: float64(s.count)})
}

// bucket is the sample of a bucket whose bound is le, labelled labels and
// le, holding count requests.
func bucket(labels []exposition.Label, le string, count int) exposition.Sample {
	withBound := append(slices.Clone(labels), exposition.Label{Name: "le", Value: le})
	return exposition.Sample{Suffix: "_bucket", Labels: withBound, Value: float64(count)}
}
