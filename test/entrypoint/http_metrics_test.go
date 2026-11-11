package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// healthzDurations is the duration histogram's samples of the GET
// requests to /healthz in a scrape's body.
func healthzDurations(body string) []string {
	var out []string
	for _, kind := range []string{`_bucket{method="GET",route="/healthz",`, `_sum{method="GET",route="/healthz"}`, `_count{method="GET",route="/healthz"}`} {
		out = append(out, testsupport.Samples(body, "doorcode_http_request_duration_seconds"+kind)...)
	}
	return out
}

func TestAScrapeCountsTheRequestsAnsweredByMethodRouteAndStatus(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	c := tokenClient(h, secret)
	testsupport.Get(h, "/healthz")
	testsupport.Get(h, "/healthz")
	c.Send("HEAD", "/healthz", "")
	c.Get("/api/v1/doors/7")
	c.Get("/api/v1/doors/8")
	c.Get("/api/v1/doors")
	testsupport.Get(h, "/no/such/page")
	testsupport.Get(h, "/api/v1/../healthz")
	c.Send("POST", "/healthz", "")
	c.Send("BREW", "/elsewhere", "")
	body := c.Get("/metrics").Body

	got := [][]string{
		testsupport.Samples(body, "# HELP doorcode_http_"),
		testsupport.Samples(body, "# TYPE doorcode_http_"),
		testsupport.Samples(body, "doorcode_http_requests_total"),
	}

	want := [][]string{
		{
			"# HELP doorcode_http_requests_total The HTTP requests answered since the server started, by method, route, and status.",
			"# HELP doorcode_http_request_duration_seconds How long the server took to answer HTTP requests, by method and route, in seconds.",
		},
		{"# TYPE doorcode_http_requests_total counter", "# TYPE doorcode_http_request_duration_seconds histogram"},
		{
			`doorcode_http_requests_total{method="GET",route="/api/v1/doors",status="200"} 1`,
			`doorcode_http_requests_total{method="GET",route="/api/v1/doors/{id}",status="404"} 2`,
			`doorcode_http_requests_total{method="POST",route="/api/v1/tokens",status="201"} 1`,
			`doorcode_http_requests_total{method="GET",route="/healthz",status="200"} 2`,
			`doorcode_http_requests_total{method="HEAD",route="/healthz",status="200"} 1`,
			`doorcode_http_requests_total{method="POST",route="/sign-in",status="303"} 1`,
			`doorcode_http_requests_total{method="GET",route="unmatched",status="307"} 1`,
			`doorcode_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
			`doorcode_http_requests_total{method="POST",route="unmatched",status="405"} 1`,
			`doorcode_http_requests_total{method="other",route="unmatched",status="404"} 1`,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("help, type, request counts (-want +got):\n%s", diff)
	}
}

func TestAScrapeShowsHowLongRequestsTookInBuckets(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	f.ClockStub.Pace("250ms")
	testsupport.Get(h, "/healthz")
	f.ClockStub.Pace("3s")
	testsupport.Get(h, "/healthz")
	f.ClockStub.Pace("0s")

	got := healthzDurations(tokenClient(h, secret).Get("/metrics").Body)

	want := []string{
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.005"} 0`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.01"} 0`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.025"} 0`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.05"} 0`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.1"} 0`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.25"} 1`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="0.5"} 1`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="1"} 1`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="2.5"} 1`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="5"} 2`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="10"} 2`,
		`doorcode_http_request_duration_seconds_bucket{method="GET",route="/healthz",le="+Inf"} 2`,
		`doorcode_http_request_duration_seconds_sum{method="GET",route="/healthz"} 3.25`,
		`doorcode_http_request_duration_seconds_count{method="GET",route="/healthz"} 2`,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestRequestsAnsweredAtOnceAreAllCounted(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	answered := make(chan testsupport.Response)
	for range 40 {
		go func() { answered <- testsupport.Get(h, "/healthz") }()
	}
	for range 40 {
		<-answered
	}

	got := testsupport.Samples(tokenClient(h, secret).Get("/metrics").Body, `doorcode_http_requests_total{method="GET",route="/healthz"`)

	want := []string{`doorcode_http_requests_total{method="GET",route="/healthz",status="200"} 40`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}

func TestANewlyStartedServerCountsRequestsFromZero(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, secret := adminWithToken(t, f, `{"name":"prometheus"}`)
	testsupport.Get(h, "/healthz")
	restarted := start(t, f).Handler()
	testsupport.Get(restarted, "/healthz")

	got := testsupport.Samples(tokenClient(restarted, secret).Get("/metrics").Body, "doorcode_http_requests_total")

	want := []string{`doorcode_http_requests_total{method="GET",route="/healthz",status="200"} 1`}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("samples (-want +got):\n%s", diff)
	}
}
