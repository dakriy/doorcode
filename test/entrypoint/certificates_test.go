package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// nothingToCheck is what the Alerts page says while the settings give no
// endpoint to check.
const nothingToCheck = "Nothing to check: door_broker_tls is off or door_broker_host is empty, and device_url is not an https address."

// certificatesHead heads the Alerts page's table of certificate checks.
var certificatesHead = []string{"Endpoint", "Last checked", "Result", "Runway", "Leaf expires in", "Unreachable checks in a row", "Chain shape since"}

// shapeHead heads the Alerts page's table of a chain's shape.
var shapeHead = []string{"Subject", "Issued by"}

// chainHead heads the Alerts page's table of a chain.
var chainHead = []string{"Role", "Subject", "Issued by", "Expires"}

// certificateAbout is what the Alerts page and the API say raises and
// clears the certificate alert at the certificate check's default
// settings.
var certificateAbout = certificateAboutAt("548 days", "14 days", "3 checks")

// certificateAboutAt is what the Alerts page and the API say raises and
// clears the certificate alert when the runway alerts under runway, the
// leaf under leaf, and an endpoint after checks in a row reached no
// server.
func certificateAboutAt(runway, leaf, checks string) string {
	return "A certificate check of the door broker or the device address found that what it served does not verify against the broker CA certificates doors are given, " +
		"that its runway has fewer than " + runway + " left or its leaf fewer than " + leaf + ", or that it reached no server for " + checks + " in a row; " +
		"each clears once a later check passes it, or without mail once the settings no longer give the endpoint. " +
		"A verified chain whose certificates above the leaf's issuer, the anchor included, differ from those the endpoint served before mails once and becomes the shape compared with; " +
		"another intermediate under the same certificates does not mail."
}

// brokerHost is the broker the certificate tests check.
const brokerHost = "broker.example.com"

// certificates is a site whose door broker is broker.example.com, over
// TLS on 8883, whose doors trust Root X1 and Root X2 of the test PKI,
// and whose device address is not set; b is its administrator's browser
// and api a client holding the administrator's token, which outlasts the
// clock moving on.
type certificates struct {
	t   *testing.T
	f   *testsupport.Factory
	a   *app.App
	b   *testsupport.Browser
	api *testsupport.Browser
	pki testsupport.PKI
}

func certificateSite(t *testing.T, f *testsupport.Factory) *certificates {
	t.Helper()
	a, b := brokerSite(t, f)
	return certificatesOf(t, f, a, b)
}

// certificatesOf is the site a runs, b its administrator's browser, with
// the broker and trust list of certificateSite.
func certificatesOf(t *testing.T, f *testsupport.Factory, a *app.App, b *testsupport.Browser) *certificates {
	t.Helper()
	api := tokenClient(a.Handler(), createToken(t, b, `{"name":"certificates"}`))
	c := &certificates{t: t, f: f, a: a, b: b, api: api, pki: testsupport.TestPKI()}
	c.set("door_broker_host", brokerHost)
	c.trust(c.pki.X1, c.pki.X2)
	return c
}

// set stores a setting through the API.
func (c *certificates) set(key, value string) {
	c.t.Helper()
	putSetting(c.t, c.api, key, value)
}

// trust has the doors trust roots, and nothing else.
func (c *certificates) trust(roots ...testsupport.Cert) {
	c.t.Helper()
	c.set("door_broker_ca_pem", testsupport.PEMOf(roots...))
}

// leaf is a certificate for the broker that YE1 issued, valid from 1
// December 2025 to 1 March 2026.
func (c *certificates) leaf() testsupport.Cert {
	return c.pki.YE1.Leaf("2025-12-01", "2026-03-01", brokerHost)
}

// serves has the broker serve chain.
func (c *certificates) serves(chain ...testsupport.Cert) {
	c.f.ProbeStub.Serve(brokerHost, 8883, chain...)
}

// fullChain is what the broker serves during the root change: the leaf,
// YE1, Root YE cross-signed by X2, and X2 cross-signed by X1.
func (c *certificates) fullChain() []testsupport.Cert {
	return []testsupport.Cert{c.leaf(), c.pki.YE1, c.pki.RootYEByX2, c.pki.X2ByX1}
}

// checkNow checks every endpoint through the API and returns what it
// answered for the first.
func (c *certificates) checkNow() any {
	c.t.Helper()
	return testsupport.JSONAt(send(c.t, c.api, "POST", "/api/v1/alerts/certificates/check", "", 200).Body, "/endpoints/0")
}

// summary is what a check found of endpoint, a JSON object: whether it
// verified, the problem, each subject of its chain, its anchor's subject
// (empty without one), and its runway (nil without one).
func summary(endpoint any) []any {
	e := endpoint.(map[string]any)
	var subjects []any
	for _, cert := range e["chain"].([]any) {
		subjects = append(subjects, cert.(map[string]any)["subject"])
	}
	anchor := ""
	if a, anchored := e["anchor"].(map[string]any); anchored {
		anchor = a["subject"].(string)
	}
	return []any{e["verified"], e["problem"], subjects, anchor, e["runway_days"]}
}

// The subjects of the test PKI's certificates.
const (
	leafSubject   = "CN=broker.example.com"
	ye1Subject    = "CN=YE1,O=Example Trust"
	rootYESubject = "CN=Root YE,O=Example Trust"
	x1Subject     = "CN=Root X1,O=Example Trust"
	x2Subject     = "CN=Root X2,O=Example Trust"
)

// noPath is the problem of a chain that reaches no trusted certificate.
const noPath = "no chain from the certificates served reaches a certificate of door_broker_ca_pem"

func TestCheckingNowReportsThePathFromTheLeafToTheFirstTrustedIssuer(t *testing.T) {
	t.Parallel()
	c := certificateSite(t, testsupport.NewFactory())
	c.serves(c.fullChain()...)

	got := c.checkNow()

	want := testsupport.JSON(`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
		"verified":true,"chain":[
			{"subject":"CN=broker.example.com","issuer":"CN=YE1,O=Example Trust","not_after":"2026-03-01T00:00:00Z"},
			{"subject":"CN=YE1,O=Example Trust","issuer":"CN=Root YE,O=Example Trust","not_after":"2028-09-02T00:00:00Z"},
			{"subject":"CN=Root YE,O=Example Trust","issuer":"CN=Root X2,O=Example Trust","not_after":"2032-09-02T00:00:00Z"}],
		"anchor":{"subject":"CN=Root X2,O=Example Trust","issuer":"CN=Root X2,O=Example Trust","not_after":"2040-09-17T00:00:00Z"},
		"runway_days":2431,"leaf_days":54,"failures":0,"baseline":{"links":[
			{"subject":"CN=broker.example.com","issuer":"CN=YE1,O=Example Trust"},
			{"subject":"CN=YE1,O=Example Trust","issuer":"CN=Root YE,O=Example Trust"},
			{"subject":"CN=Root YE,O=Example Trust","issuer":"CN=Root X2,O=Example Trust"},
			{"subject":"CN=Root X2,O=Example Trust","issuer":"CN=Root X2,O=Example Trust"}],"since":"2026-01-05T09:00:00Z"}}`)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("broker (-want +got):\n%s", diff)
	}
}

func TestTheCheckTakesTheShortestPathTheTrustListAllows(t *testing.T) {
	t.Parallel()
	pki := testsupport.TestPKI()
	ye2 := pki.RootYE.Issue("YE2", "2025-09-03", "2034-01-01")
	rootZ := testsupport.Root("Root Z", "2020-01-01", "2027-01-01")
	z1 := rootZ.Issue("Z1", "2025-01-01", "2030-01-01")
	cases := map[string]struct {
		served func(c *certificates) []testsupport.Cert
		trust  []testsupport.Cert
		want   []any
	}{
		"the full chain served out of order": {
			func(c *certificates) []testsupport.Cert {
				return []testsupport.Cert{c.leaf(), pki.X2ByX1, pki.RootYEByX2, pki.YE1}
			},
			[]testsupport.Cert{pki.X1, pki.X2},
			[]any{true, nil, []any{leafSubject, ye1Subject, rootYESubject}, x2Subject, 2431.0},
		},
		"the full chain with only X1 trusted": {
			(*certificates).fullChain, []testsupport.Cert{pki.X1},
			[]any{true, nil, []any{leafSubject, ye1Subject, rootYESubject, x2Subject}, x1Subject, 2431.0},
		},
		"the direct chain with X1 and X2 trusted": {
			func(c *certificates) []testsupport.Cert { return []testsupport.Cert{c.leaf(), pki.YE1, pki.RootYE} },
			[]testsupport.Cert{pki.X1, pki.X2},
			[]any{false, noPath, []any{leafSubject, ye1Subject, rootYESubject}, "", nil},
		},
		"the direct chain with Root YE trusted": {
			func(c *certificates) []testsupport.Cert { return []testsupport.Cert{c.leaf(), pki.YE1, pki.RootYE} },
			[]testsupport.Cert{pki.RootYE},
			[]any{true, nil, []any{leafSubject, ye1Subject}, rootYESubject, 7180.0},
		},
		"the full chain with an impostor of X2 trusted": {
			(*certificates).fullChain, []testsupport.Cert{testsupport.Impostor("Root X2", "2020-09-04", "2040-09-17")},
			[]any{false, noPath, []any{leafSubject, ye1Subject, rootYESubject, x2Subject}, "", nil},
		},
		"the leaf alone with YE1 trusted": {
			func(c *certificates) []testsupport.Cert { return []testsupport.Cert{c.leaf()} },
			[]testsupport.Cert{pki.YE1},
			[]any{true, nil, []any{leafSubject}, ye1Subject, 970.0},
		},
		"an intermediate that outlives the cross-sign": {
			func(*certificates) []testsupport.Cert {
				return []testsupport.Cert{ye2.Leaf("2025-12-01", "2026-03-01", brokerHost), ye2, pki.RootYEByX2}
			},
			[]testsupport.Cert{pki.X1, pki.X2},
			[]any{true, nil, []any{leafSubject, "CN=YE2,O=Example Trust", rootYESubject}, x2Subject, 2431.0},
		},
		"an anchor that expires first": {
			func(*certificates) []testsupport.Cert {
				return []testsupport.Cert{z1.Leaf("2025-12-01", "2026-03-01", brokerHost), z1}
			},
			[]testsupport.Cert{pki.X1, rootZ},
			[]any{true, nil, []any{leafSubject, "CN=Z1,O=Example Trust"}, "CN=Root Z,O=Example Trust", 360.0},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := certificateSite(t, testsupport.NewFactory())
			c.trust(tc.trust...)
			c.serves(tc.served(c)...)

			got := summary(c.checkNow())

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("verified, problem, chain, anchor, runway (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheCheckFailsALeafForAnotherHostOrACertificateOutOfDate(t *testing.T) {
	t.Parallel()
	pki := testsupport.TestPKI()
	rootZ := testsupport.Root("Root Z", "2020-01-01", "2025-12-31")
	z1 := rootZ.Issue("Z1", "2025-01-01", "2030-01-01")
	cases := map[string]struct {
		now    string
		served []testsupport.Cert
		trust  []testsupport.Cert
		want   []any
	}{
		"a leaf for another host": {
			testsupport.DefaultNow, []testsupport.Cert{pki.YE1.Leaf("2025-12-01", "2026-03-01", "other.example.com"), pki.YE1, pki.RootYEByX2},
			[]testsupport.Cert{pki.X2},
			[]any{"the certificate is not valid for broker.example.com: x509: certificate is valid for other.example.com, not broker.example.com", 54.0},
		},
		"a leaf that expired": {
			"2026-03-02T12:00:00Z", []testsupport.Cert{pki.YE1.Leaf("2025-12-01", "2026-03-01", brokerHost), pki.YE1, pki.RootYEByX2},
			[]testsupport.Cert{pki.X2},
			[]any{"CN=broker.example.com expired 2026-03-01T00:00:00Z", -1.0},
		},
		"a leaf not valid yet": {
			testsupport.DefaultNow, []testsupport.Cert{pki.YE1.Leaf("2026-02-01", "2026-05-01", brokerHost), pki.YE1, pki.RootYEByX2},
			[]testsupport.Cert{pki.X2},
			[]any{"CN=broker.example.com is not valid before 2026-02-01T00:00:00Z", 115.0},
		},
		"an anchor that expired": {
			testsupport.DefaultNow, []testsupport.Cert{z1.Leaf("2025-12-01", "2026-03-01", brokerHost), z1},
			[]testsupport.Cert{rootZ},
			[]any{"CN=Root Z,O=Example Trust expired 2025-12-31T00:00:00Z", 54.0},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := certificateSite(t, testsupport.NewFactory())
			c.trust(tc.trust...)
			c.serves(tc.served...)
			c.f.ClockStub.Set(tc.now)

			got := c.checkNow().(map[string]any)

			want := append([]any{false}, tc.want...)
			if diff := cmp.Diff(want, []any{got["verified"], got["problem"], got["leaf_days"]}); diff != "" {
				t.Errorf("verified, problem, leaf days (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheCheckReportsWhatStopsItReadingTheChain(t *testing.T) {
	t.Parallel()
	pki := testsupport.TestPKI()
	cases := map[string]struct {
		arrange func(c *certificates)
		want    string
	}{
		"a broker that cannot be reached": {
			func(*certificates) {},
			`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
				"error":"dial tcp broker.example.com:8883: connect: connection refused","verified":false,"chain":[],"failures":1}`,
		},
		"a broker whose handshake fails": {
			func(c *certificates) {
				c.f.ProbeStub.Fail(brokerHost, 8883, "remote error: tls: protocol version not supported")
			},
			`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
				"error":"remote error: tls: protocol version not supported","verified":false,"chain":[],"failures":1}`,
		},
		"a broker serving no certificate": {
			func(c *certificates) { c.f.ProbeStub.ServeBytes(brokerHost, 8883) },
			`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
				"verified":false,"problem":"the server served no certificate","chain":[],"failures":0}`,
		},
		"a broker serving what is no certificate": {
			func(c *certificates) { c.f.ProbeStub.ServeBytes(brokerHost, 8883, c.leaf().DER, []byte("junk")) },
			`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
				"verified":false,"problem":"the server served a certificate that cannot be read: x509: malformed certificate","chain":[],"failures":0}`,
		},
		"doors that trust nothing": {
			func(c *certificates) {
				c.set("door_broker_ca_pem", "")
				c.serves(c.leaf(), pki.YE1)
			},
			`{"name":"Door broker","host":"broker.example.com","port":8883,"checked":true,"checked_at":"2026-01-05T09:00:00Z",
				"verified":false,"problem":"door_broker_ca_pem holds no certificate to verify against","chain":[
				{"subject":"CN=broker.example.com","issuer":"CN=YE1,O=Example Trust","not_after":"2026-03-01T00:00:00Z"},
				{"subject":"CN=YE1,O=Example Trust","issuer":"CN=Root YE,O=Example Trust","not_after":"2028-09-02T00:00:00Z"}],"leaf_days":54,"failures":0}`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := certificateSite(t, testsupport.NewFactory())
			tc.arrange(c)

			got := c.checkNow()

			if diff := cmp.Diff(testsupport.JSON(tc.want), got); diff != "" {
				t.Errorf("broker (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheEndpointsComeFromTheBrokerAndDeviceAddressSettings(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		settings []string
		want     string
	}{
		"nothing set": {nil, `[]`},
		"a broker host": {[]string{"door_broker_host", brokerHost},
			`[{"name":"Door broker","host":"broker.example.com","port":8883}]`},
		"a broker in the clear": {[]string{"door_broker_host", brokerHost, "door_broker_tls", "false"}, `[]`},
		"a broker at an IPv6 address and another port": {[]string{"door_broker_host", "2001:db8::1", "door_broker_port", "8884"},
			`[{"name":"Door broker","host":"2001:db8::1","port":8884}]`},
		"an https device address": {[]string{"device_url", "https://devices.example.com"},
			`[{"name":"Device address","host":"devices.example.com","port":443}]`},
		"an https device address with a port": {[]string{"device_url", "https://devices.example.com:8443/"},
			`[{"name":"Device address","host":"devices.example.com","port":8443}]`},
		"an http device address": {[]string{"door_broker_host", brokerHost, "device_url", "http://devices.example.com"},
			`[{"name":"Door broker","host":"broker.example.com","port":8883}]`},
		"both": {[]string{"door_broker_host", brokerHost, "device_url", "https://devices.example.com"},
			`[{"name":"Door broker","host":"broker.example.com","port":8883},{"name":"Device address","host":"devices.example.com","port":443}]`},
		"both at one address": {[]string{"door_broker_host", brokerHost, "door_broker_port", "443", "device_url", "https://broker.example.com"},
			`[{"name":"Door broker and device address","host":"broker.example.com","port":443}]`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			b := signedInAdmin(t, f)
			for i := 0; i < len(tc.settings); i += 2 {
				putSetting(t, b, tc.settings[i], tc.settings[i+1])
			}

			got := testsupport.JSONAt(send(t, b, "GET", "/api/v1/alerts/certificates", "", 200).Body, "/endpoints")

			var want []any
			for _, e := range testsupport.JSON(tc.want).([]any) {
				want = append(want, with(e.(map[string]any), map[string]any{"checked": false, "verified": false, "chain": []any{}, "failures": 0.0}))
			}
			if diff := cmp.Diff(append([]any{}, want...), got); diff != "" {
				t.Errorf("endpoints (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunChecksEachEndpointOnceItsIntervalHasPassedWhileTheCheckIsOn(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateSite(t, f)
	c.serves(c.fullChain()...)
	run(t, c.a)
	c.set("certificate_check_enabled", "true")
	c.set("certificate_check_interval_hours", "12")
	f.ClockStub.Set("2026-01-05T09:01:00Z")
	run(t, c.a)
	c.set("device_url", "https://devices.example.com")
	for _, at := range []string{"2026-01-05T21:00:00Z", "2026-01-05T21:01:00Z", "2026-01-06T09:02:00Z"} {
		f.ClockStub.Set(at)
		run(t, c.a)
	}
	f.ClockStub.Set("2026-01-06T09:03:00Z")
	send(t, c.api, "POST", "/api/v1/alerts/certificates/check", "", 200)

	body := send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body
	got := []any{f.ProbeStub.Probed(), testsupport.JSONAt(body, "/enabled"), testsupport.JSONAt(body, "/interval_hours"),
		testsupport.JSONAt(body, "/endpoints/0/checked_at"), testsupport.JSONAt(body, "/endpoints/1/checked_at")}

	broker, device := "broker.example.com:8883", "devices.example.com:443"
	want := []any{[]string{broker, device, broker, broker, device, broker, device}, true, 12.0, "2026-01-06T09:03:00Z", "2026-01-06T09:03:00Z"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("probes, enabled, interval, checked at (-want +got):\n%s", diff)
	}
}

func TestADoorIsAnsweredWhileACertificateCheckWaitsOnItsServer(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b := pushSite(t, f)
	c := certificatesOf(t, f, a, b)
	c.serves(c.fullChain()...)
	c.set("certificate_check_enabled", "true")
	f.ClockStub.Set("2026-01-05T09:01:00Z")
	sent := len(f.BrokerStub.Published)
	release := f.ProbeStub.Hold(brokerHost, 8883)
	f.BrokerStub.Inject(doorTopic("1", "check"), checkAs("lobby"))

	var whileHeld [][]any
	runWhile(t, c.a, func() {
		f.ProbeStub.Await(brokerHost, 8883)
		f.BrokerStub.Await(doorTopic("1", "reply"))
		whileHeld = answered(f, sent)
		release()
	})

	body := send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body
	want := []any{[][]any{{doorTopic("1", "reply"), "lobby"}}, true, "2026-01-05T09:01:00Z"}
	got := []any{whileHeld, testsupport.JSONAt(body, "/endpoints/0/verified"), testsupport.JSONAt(body, "/endpoints/0/checked_at")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("answers while the probe waited, then the check (-want +got):\n%s", diff)
	}
}

func TestCheckNowWaitsForACheckInProgressAndKeepsItsOwnNewerResult(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateSite(t, f)
	c.serves(c.fullChain()...)
	c.set("certificate_check_enabled", "true")
	f.ClockStub.Set("2026-01-05T09:01:00Z")
	release := f.ProbeStub.Hold(brokerHost, 8883)

	checked := make(chan testsupport.Response, 1)
	runWhile(t, c.a, func() {
		f.ProbeStub.Await(brokerHost, 8883)
		f.ProbeStub.Fail(brokerHost, 8883, "i/o timeout")
		go func() { checked <- c.api.Send("POST", "/api/v1/alerts/certificates/check", "") }()
		release()
	})

	now := <-checked
	later := send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body
	want := []any{200, "i/o timeout", "i/o timeout", 2}
	got := []any{now.Status, testsupport.JSONAt(now.Body, "/endpoints/0/error"), testsupport.JSONAt(later, "/endpoints/0/error"),
		len(f.ProbeStub.Probed())}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("check now, the stored check, probes (-want +got):\n%s", diff)
	}
}

func TestTheAlertsPageShowsEachEndpointsLatestCheckAndItsChain(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateSite(t, f)
	c.set("site_timezone", "Europe/Berlin")
	c.set("device_url", "https://devices.example.com")
	c.serves(c.fullChain()...)
	before := pageOf(c.b.Get("/admin/alerts")).Tables["Certificate checks"]
	c.f.ProbeStub.ServeBytes("devices.example.com", 443, c.pki.RootYE.DER, c.leaf().DER)

	checked := c.b.Post("/admin/alerts/certificates")
	page := pageOf(c.b.Get(checked.Location))
	c.f.ProbeStub.Fail("devices.example.com", 443, "i/o timeout")
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	send(t, c.b, "POST", "/api/v1/alerts/certificates/check", "", 200)
	later := pageOf(c.b.Get("/admin/alerts")).Tables

	want := []any{
		[][]string{certificatesHead, {"Door broker broker.example.com:8883", "Not checked yet", "", "", "", "", ""},
			{"Device address devices.example.com:443", "Not checked yet", "", "", "", "", ""}},
		303, "/admin/alerts", []string{"The certificates have been checked."},
		[][]string{certificatesHead,
			{"Door broker broker.example.com:8883", "Mon 5 Jan 2026 10:00:00", "Verified up to " + x2Subject, "2431 days", "54 days", "0", "Mon 5 Jan 2026 10:00:00"},
			{"Device address devices.example.com:443", "Mon 5 Jan 2026 10:00:00", "Not verified: " + noPath, "", "7180 days", "0", ""}},
		[][]string{chainHead,
			{"Leaf", leafSubject, ye1Subject, "1 Mar 2026"},
			{"Intermediate", ye1Subject, rootYESubject, "2 Sep 2028"},
			{"Intermediate", rootYESubject, x2Subject, "2 Sep 2032"},
			{"Trust anchor", x2Subject, x2Subject, "17 Sep 2040"}},
		[][]string{chainHead,
			{"Served", rootYESubject, rootYESubject, "3 Sep 2045"},
			{"Served", leafSubject, ye1Subject, "1 Mar 2026"}},
		[][]string{shapeHead, {leafSubject, ye1Subject}, {ye1Subject, rootYESubject}, {rootYESubject, x2Subject}, {x2Subject, x2Subject}},
		[]string{"Door broker broker.example.com:8883", "Mon 5 Jan 2026 11:00:00", "Verified up to " + x2Subject, "2431 days", "54 days", "0", "Mon 5 Jan 2026 10:00:00"},
		[]string{"Device address devices.example.com:443", "Mon 5 Jan 2026 11:00:00", "Unreachable: i/o timeout", "", "", "1", ""},
		false,
	}
	_, deviceChain := later["Chain served by devices.example.com:443"]
	got := []any{before, checked.Status, checked.Location, page.Messages, page.Tables["Certificate checks"],
		page.Tables["Chain served by broker.example.com:8883"], page.Tables["Chain served by devices.example.com:443"],
		page.Tables["Chain shape of broker.example.com:8883"], later["Certificate checks"][1], later["Certificate checks"][2], deviceChain}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("before, check now, after, chains, an hour later (-want +got):\n%s", diff)
	}
}

func TestCheckingCertificatesFailsWhenTheStoreOrTheSettingsDo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		method  string
		target  string
	}{
		"reading the checks for the API":   {func(f *testsupport.Factory) { f.StoreStub.FailCertificates() }, "GET", "/api/v1/alerts/certificates"},
		"reading the checks for the page":  {func(f *testsupport.Factory) { f.StoreStub.FailCertificates() }, "GET", "/admin/alerts"},
		"storing a check from the API":     {func(f *testsupport.Factory) { f.StoreStub.FailCertificateWrites() }, "POST", "/api/v1/alerts/certificates/check"},
		"storing a check from the page":    {func(f *testsupport.Factory) { f.StoreStub.FailCertificateWrites() }, "POST", "/admin/alerts/certificates"},
		"reading the checks after storing": {func(f *testsupport.Factory) { f.StoreStub.FailCertificates() }, "POST", "/api/v1/alerts/certificates/check"},
		"reading the door defaults": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_DOOR_BROKER_PORT"] = "many" },
			"POST", "/api/v1/alerts/certificates/check"},
		"reading the site's settings": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Nowhere/Else" },
			"GET", "/api/v1/alerts/certificates"},
		"reading the check's settings": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_CERTIFICATE_CHECK_INTERVAL_HOURS"] = "often" },
			"GET", "/api/v1/alerts/certificates"},
		"reading the alert thresholds before checking": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_CERTIFICATE_RUNWAY_DAYS"] = "long" },
			"POST", "/admin/alerts/certificates"},
		"reading the stored settings": {func(f *testsupport.Factory) { f.StoreStub.FailSettings() }, "GET", "/api/v1/alerts/certificates"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateSite(t, f)
			c.serves(c.fullChain()...)
			tc.arrange(f)

			got := c.b.Send(tc.method, tc.target, "")

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRunLogsWhatStopsTheCertificateCheckWhenTheStoreOrTheSettingsFail(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		arrange func(f *testsupport.Factory)
		want    string
	}{
		"reading the check's settings": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_CERTIFICATE_CHECK_ENABLED"] = "maybe" },
			"invalid value for certificate_check_enabled: must be true or false"},
		"reading the door defaults": {func(f *testsupport.Factory) { f.EnvStub["DOORCODE_DOOR_BROKER_TLS"] = "maybe" },
			"invalid value for door_broker_tls: must be true or false"},
		"reading the checks": {func(f *testsupport.Factory) { f.StoreStub.FailCertificates() }, "stub: unavailable"},
		"storing a check": {func(f *testsupport.Factory) { f.StoreStub.FailCertificateWrites() },
			"store the certificate check of broker.example.com:8883: stub: unavailable"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateSite(t, f)
			c.set("certificate_check_enabled", "true")
			f.ClockStub.Set("2026-01-05T09:01:00Z")
			tc.arrange(f)

			run(t, c.a)

			want := testsupport.LogEntry{Level: "error", Message: "certificate check failed", Fields: map[string]string{"error": tc.want}}
			if diff := cmp.Diff(want, lastLogged(f, "certificate check failed")); diff != "" {
				t.Errorf("logged (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheCertificateCheckSettingsRefuseValuesOutsideTheirRange(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ key, value, want string }{
		"an interval below an hour":             {"certificate_check_interval_hours", "0", "must be a whole number of hours from 1 to 720"},
		"an interval above thirty days":         {"certificate_check_interval_hours", "721", "must be a whole number of hours from 1 to 720"},
		"a check neither on nor off":            {"certificate_check_enabled", "sometimes", "must be true or false"},
		"no runway":                             {"certificate_runway_days", "0", "must be a whole number of days from 1 to 3650"},
		"a runway above ten years":              {"certificate_runway_days", "3651", "must be a whole number of days from 1 to 3650"},
		"no leaf days":                          {"certificate_leaf_days", "0", "must be a whole number of days from 1 to 365"},
		"leaf days above a year":                {"certificate_leaf_days", "366", "must be a whole number of days from 1 to 365"},
		"no unreachable checks":                 {"certificate_unreachable_checks", "0", "must be a whole number of checks from 1 to 100"},
		"over a hundred unreachable checks":     {"certificate_unreachable_checks", "101", "must be a whole number of checks from 1 to 100"},
		"unreachable checks that are no number": {"certificate_unreachable_checks", "three", "must be a whole number of checks from 1 to 100"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := signedInAdmin(t, testsupport.NewFactory())

			got := b.PutJSON("/api/v1/settings/"+tc.key, `{"value":`+testsupport.Quoted(tc.value)+`}`)

			if diff := cmp.Diff(apiError(400, "invalid value for "+tc.key+": "+tc.want), got); diff != "" {
				t.Errorf("response (-want +got):\n%s", diff)
			}
		})
	}
}
