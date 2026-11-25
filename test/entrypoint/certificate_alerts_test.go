package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// certificateAlerts is certificateSite with alert mail set up as
// mailAlerts does and the certificate alert set to mail.
func certificateAlerts(t *testing.T, f *testsupport.Factory) *certificates {
	t.Helper()
	c := certificateSite(t, f)
	mailAlerts(t, c.api, "certificate")
	return c
}

// checkAt sets the clock to the RFC 3339 time at, checks every endpoint
// through the API, and has the server run, delivering what was mailed.
func (c *certificates) checkAt(at string) {
	c.t.Helper()
	c.f.ClockStub.Set(at)
	c.checkNow()
	run(c.t, c.a)
}

// brokerAlert is the subject of the certificate alert's mail about the
// broker for the reason named why.
func brokerAlert(why string) string {
	return "Certificate: Door broker broker.example.com:8883, " + why
}

// certificateFooter ends every message of the certificate alert.
var certificateFooter = alertFooter("Certificate")

func TestTheLeafsIssuerExpiringSoonLeavesTheRunwayToTheCrossSignAndRaisesNothing(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.pki.YE1.Leaf("2028-01-01", "2028-06-01", brokerHost), c.pki.YE1, c.pki.RootYEByX2)
	f.ClockStub.Set("2028-03-05T09:00:00Z")

	check := c.checkNow().(map[string]any)

	if diff := cmp.Diff([]any{true, 1641.0, []any{}}, []any{check["verified"], check["runway_days"], subjects(t, f)}); diff != "" {
		t.Errorf("verified, runway, subjects (-want +got):\n%s", diff)
	}
}

func TestARunwayBelowTheSettingMailsOnceTheClockIsWithinItOfTheCrossSignsExpiry(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	ye2 := c.pki.RootYE.Issue("YE2", "2025-09-03", "2034-01-01")
	c.serves(ye2.Leaf("2031-01-01", "2031-06-01", brokerHost), ye2, c.pki.RootYEByX2)
	c.checkAt("2031-03-02T09:00:00Z")
	early := subjects(t, f)

	c.checkAt("2031-03-04T09:00:00Z")
	c.checkAt("2031-03-05T09:00:00Z")
	c.set("certificate_runway_days", "500")
	c.checkAt("2031-03-06T09:00:00Z")

	raised := "Certificate at Door broker broker.example.com:8883, runway short, Tue 4 Mar 2031 09:00:00 UTC.\n\n" +
		"The runway of broker.example.com:8883, the days until the earliest expiry of the trust anchor and the certificates between it and the leaf's issuer, " +
		"is 547 days, fewer than the 548 days of certificate_runway_days.\n\n" +
		"Chain:\n" +
		"Leaf: CN=broker.example.com, issued by CN=YE2,O=Example Trust, expires Sun 1 Jun 2031 00:00:00 UTC (88 days)\n" +
		"Intermediate: CN=YE2,O=Example Trust, issued by CN=Root YE,O=Example Trust, expires Sun 1 Jan 2034 00:00:00 UTC (1033 days)\n" +
		"Intermediate: CN=Root YE,O=Example Trust, issued by CN=Root X2,O=Example Trust, expires Thu 2 Sep 2032 00:00:00 UTC (547 days)\n" +
		"Trust anchor: CN=Root X2,O=Example Trust, issued by CN=Root X2,O=Example Trust, expires Mon 17 Sep 2040 00:00:00 UTC (3484 days)\n" +
		"Runway: 547 days.\n" +
		"Leaf expires in 88 days.\n" + certificateFooter
	cleared := "Certificate at Door broker broker.example.com:8883, runway short cleared, Thu 6 Mar 2031 09:00:00 UTC.\n\n" +
		"It was raised Tue 4 Mar 2031 09:00:00 UTC.\n" + certificateFooter
	want := []any{[]any{}, [][]any{
		{recipients, brokerAlert("runway short"), raised},
		{recipients, "Cleared: " + brokerAlert("runway short"), cleared},
	}}
	if diff := cmp.Diff(want, []any{early, mailed(t, f)}); diff != "" {
		t.Errorf("subjects at 549 days, mail (-want +got):\n%s", diff)
	}
}

func TestALeafWithFewerDaysLeftThanTheSettingMails(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		expires string
		want    []any
	}{
		"a leaf with 10 days left": {"2026-01-16", []any{10.0, []any{brokerAlert("leaf expiring")}}},
		"a leaf with 40 days left": {"2026-02-15", []any{40.0, []any{}}},
		"a leaf with 14 days left": {"2026-01-20", []any{14.0, []any{}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			c.serves(c.pki.YE1.Leaf("2025-12-01", tc.expires, brokerHost), c.pki.YE1, c.pki.RootYEByX2)

			leafDays := c.checkNow().(map[string]any)["leaf_days"]
			run(t, c.a)

			if diff := cmp.Diff(tc.want, []any{leafDays, subjects(t, f)}); diff != "" {
				t.Errorf("leaf days, subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheLeafMailNamesItsDaysAndTheSettingAndClearsOnceTheLeafIsRenewed(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.set("certificate_leaf_days", "60")
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	c.serves(c.pki.YE1.Leaf("2026-01-01", "2026-04-01", brokerHost), c.pki.YE1, c.pki.RootYEByX2, c.pki.X2ByX1)
	c.checkAt("2026-01-06T09:00:00Z")

	raised := "Certificate at Door broker broker.example.com:8883, leaf expiring, Mon 5 Jan 2026 09:00:00 UTC.\n\n" +
		"The leaf broker.example.com:8883 serves expires in 54 days, fewer than the 60 days of certificate_leaf_days.\n\n" +
		"Chain:\n" +
		"Leaf: CN=broker.example.com, issued by CN=YE1,O=Example Trust, expires Sun 1 Mar 2026 00:00:00 UTC (54 days)\n" +
		"Intermediate: CN=YE1,O=Example Trust, issued by CN=Root YE,O=Example Trust, expires Sat 2 Sep 2028 00:00:00 UTC (970 days)\n" +
		"Intermediate: CN=Root YE,O=Example Trust, issued by CN=Root X2,O=Example Trust, expires Thu 2 Sep 2032 00:00:00 UTC (2431 days)\n" +
		"Trust anchor: CN=Root X2,O=Example Trust, issued by CN=Root X2,O=Example Trust, expires Mon 17 Sep 2040 00:00:00 UTC (5368 days)\n" +
		"Runway: 2431 days.\n" +
		"Leaf expires in 54 days.\n" + certificateFooter
	want := [][]any{
		{recipients, brokerAlert("leaf expiring"), raised},
		{recipients, "Cleared: " + brokerAlert("leaf expiring"), "Certificate at Door broker broker.example.com:8883, leaf expiring cleared, " +
			"Tue 6 Jan 2026 09:00:00 UTC.\n\nIt was raised Mon 5 Jan 2026 09:00:00 UTC.\n" + certificateFooter},
	}
	if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
		t.Errorf("mail (-want +got):\n%s", diff)
	}
}

func TestWhatDoesNotVerifyMailsWhatWasServedAndClearsOnceItVerifies(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		serve func(c *certificates)
		want  string
	}{
		"a chain that reaches no trusted certificate": {
			func(c *certificates) { c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE) },
			"What broker.example.com:8883 served does not verify against door_broker_ca_pem: " + noPath + ".\n\n" +
				"Chain:\n" +
				"Served: CN=broker.example.com, issued by CN=YE1,O=Example Trust, expires Sun 1 Mar 2026 00:00:00 UTC (54 days)\n" +
				"Served: CN=YE1,O=Example Trust, issued by CN=Root YE,O=Example Trust, expires Sat 2 Sep 2028 00:00:00 UTC (970 days)\n" +
				"Served: CN=Root YE,O=Example Trust, issued by CN=Root YE,O=Example Trust, expires Sun 3 Sep 2045 00:00:00 UTC (7180 days)\n" +
				"Leaf expires in 54 days.\n",
		},
		"a server that serves no certificate": {
			func(c *certificates) { c.f.ProbeStub.ServeBytes(brokerHost, 8883) },
			"What broker.example.com:8883 served does not verify against door_broker_ca_pem: the server served no certificate.\n\nChain:\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			tc.serve(c)
			c.checkAt(testsupport.DefaultNow)
			c.serves(c.fullChain()...)
			c.checkAt("2026-01-05T10:00:00Z")

			want := [][]any{
				{recipients, brokerAlert("not verified"), "Certificate at Door broker broker.example.com:8883, not verified, " +
					"Mon 5 Jan 2026 09:00:00 UTC.\n\n" + tc.want + certificateFooter},
				{recipients, "Cleared: " + brokerAlert("not verified"), "Certificate at Door broker broker.example.com:8883, not verified cleared, " +
					"Mon 5 Jan 2026 10:00:00 UTC.\n\nIt was raised Mon 5 Jan 2026 09:00:00 UTC.\n" + certificateFooter},
			}
			if diff := cmp.Diff(want, mailed(t, f)); diff != "" {
				t.Errorf("mail (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAnEndpointUnreachableForTheSettingsChecksInARowMailsAndClearsOnceReached(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.fullChain()...)
	c.checkAt("2026-01-05T08:00:00Z")
	c.f.ProbeStub.Fail(brokerHost, 8883, "i/o timeout")
	c.checkAt("2026-01-05T09:00:00Z")
	c.checkAt("2026-01-05T10:00:00Z")
	early := []any{subjects(t, f), testsupport.JSONAt(send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body, "/endpoints/0/failures")}

	c.checkAt("2026-01-05T11:00:00Z")
	c.checkAt("2026-01-05T12:00:00Z")
	c.serves(c.fullChain()...)
	c.checkAt("2026-01-05T13:00:00Z")

	want := []any{[]any{[]any{}, 2.0}, [][]any{
		{recipients, brokerAlert("unreachable"), "Certificate at Door broker broker.example.com:8883, unreachable, Mon 5 Jan 2026 11:00:00 UTC.\n\n" +
			"3 checks in a row of broker.example.com:8883 reached no server; the latest said: i/o timeout.\n" + certificateFooter},
		{recipients, "Cleared: " + brokerAlert("unreachable"), "Certificate at Door broker broker.example.com:8883, unreachable cleared, " +
			"Mon 5 Jan 2026 13:00:00 UTC.\n\nIt was raised Mon 5 Jan 2026 11:00:00 UTC.\n" + certificateFooter},
	}}
	if diff := cmp.Diff(want, []any{early, mailed(t, f)}); diff != "" {
		t.Errorf("subjects and failures after two failed checks, mail (-want +got):\n%s", diff)
	}
}

func TestTheUnreachableAlertWaitsForTheChecksTheSettingAsksFor(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.set("certificate_unreachable_checks", "1")

	c.checkAt(testsupport.DefaultNow)

	if diff := cmp.Diff([]any{brokerAlert("unreachable")}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

// chainChangedText is the text of the mail saying, of the change at at,
// that the broker's chain, its shape baselined at 09:00, now ends at Root
// X1 when it first ran up to Root X2.
func chainChangedText(at string) string {
	return "Certificate at Door broker broker.example.com:8883, chain changed, " + at + ".\n\n" +
		"The chain broker.example.com:8883 serves verifies through other certificates above the leaf's issuer than the one it served since Mon 5 Jan 2026 09:00:00 UTC, " +
		"which it now replaces.\n\n" +
		"Before:\n" +
		"CN=broker.example.com, issued by CN=YE1,O=Example Trust\n" +
		"CN=YE1,O=Example Trust, issued by CN=Root YE,O=Example Trust\n" +
		"CN=Root YE,O=Example Trust, issued by CN=Root X2,O=Example Trust\n" +
		"CN=Root X2,O=Example Trust, issued by CN=Root X2,O=Example Trust\n\n" +
		"Now:\n" +
		"Leaf: CN=broker.example.com, issued by CN=YE1,O=Example Trust, expires Sun 1 Mar 2026 00:00:00 UTC (54 days)\n" +
		"Intermediate: CN=YE1,O=Example Trust, issued by CN=Root YE,O=Example Trust, expires Sat 2 Sep 2028 00:00:00 UTC (970 days)\n" +
		"Intermediate: CN=Root YE,O=Example Trust, issued by CN=Root X2,O=Example Trust, expires Thu 2 Sep 2032 00:00:00 UTC (2431 days)\n" +
		"Intermediate: CN=Root X2,O=Example Trust, issued by CN=Root X1,O=Example Trust, expires Thu 2 Sep 2032 00:00:00 UTC (2431 days)\n" +
		"Trust anchor: CN=Root X1,O=Example Trust, issued by CN=Root X1,O=Example Trust, expires Mon 4 Jun 2035 00:00:00 UTC (3436 days)\n" +
		"Runway: 2431 days.\n" +
		"Leaf expires in 54 days.\n" + certificateFooter
}

func TestAChainReachingTheListThroughOtherCertificatesMailsOnceAndAnotherIntermediateDoesNot(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	ye2 := c.pki.RootYE.Issue("YE2", "2025-09-03", "2034-01-01")
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	c.checkAt("2026-01-05T10:00:00Z")
	first := subjects(t, f)

	c.trust(c.pki.X1)
	c.checkAt("2026-01-05T11:00:00Z")
	c.checkAt("2026-01-05T12:00:00Z")
	c.serves(ye2.Leaf("2025-12-01", "2026-03-01", brokerHost), ye2, c.pki.RootYEByX2, c.pki.X2ByX1)
	c.checkAt("2026-01-05T13:00:00Z")
	baseline := testsupport.JSONAt(send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body, "/endpoints/0/baseline")

	want := []any{
		[]any{},
		[][]any{{recipients, brokerAlert("chain changed"), chainChangedText("Mon 5 Jan 2026 11:00:00 UTC")}},
		[]any{brokerAlert("chain changed")},
		testsupport.JSON(`{"links":[
			{"subject":"CN=broker.example.com","issuer":"CN=YE2,O=Example Trust"},
			{"subject":"CN=YE2,O=Example Trust","issuer":"CN=Root YE,O=Example Trust"},
			{"subject":"CN=Root YE,O=Example Trust","issuer":"CN=Root X2,O=Example Trust"},
			{"subject":"CN=Root X2,O=Example Trust","issuer":"CN=Root X1,O=Example Trust"},
			{"subject":"CN=Root X1,O=Example Trust","issuer":"CN=Root X1,O=Example Trust"}],"since":"2026-01-05T11:00:00Z"}`),
	}
	got := []any{first, mailed(t, f)[:1], subjects(t, f), baseline}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("subjects before, the anchor change's mail, every subject, the baseline after the intermediate change (-want +got):\n%s", diff)
	}
}

func TestALeafTheListIssuesMailsWhenItsAnchorIsIssuedByAnotherRoot(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.leaf())
	c.trust(c.pki.YE1)
	c.checkAt(testsupport.DefaultNow)
	c.trust(c.pki.X1.CrossSign(c.pki.YE1, "2025-09-03", "2028-09-02"))

	c.checkAt("2026-01-05T10:00:00Z")

	if diff := cmp.Diff([]any{brokerAlert("chain changed")}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestAChainThatDoesNotVerifyKeepsTheBaselineItCannotReplace(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE)
	c.checkAt("2026-01-05T10:00:00Z")
	c.f.ProbeStub.Fail(brokerHost, 8883, "i/o timeout")
	c.checkAt("2026-01-05T11:00:00Z")

	got := testsupport.JSONAt(send(t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body, "/endpoints/0/baseline/since")

	if diff := cmp.Diff([]any{"2026-01-05T09:00:00Z", []any{brokerAlert("not verified")}}, []any{got, subjects(t, f)}); diff != "" {
		t.Errorf("baseline since, subjects (-want +got):\n%s", diff)
	}
}

func TestAShapeChangeIsMailedOnceThoughItsCheckCouldNotBeStored(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	c.trust(c.pki.X1)
	f.StoreStub.FailCertificateWrites()
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	refused := c.api.Send("POST", "/api/v1/alerts/certificates/check", "").Status
	f.StoreStub.Recover()

	c.checkAt("2026-01-05T11:00:00Z")

	if diff := cmp.Diff([]any{500, []any{brokerAlert("chain changed")}}, []any{refused, subjects(t, f)}); diff != "" {
		t.Errorf("status of the check not stored, subjects (-want +got):\n%s", diff)
	}
}

func TestSettingTheCertificateAlertOffClearsEveryConditionOfItWithoutMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.set("certificate_runway_days", "3650")
	c.set("certificate_leaf_days", "365")
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	before := subjects(t, f)

	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"off"}`, 200)

	raised := pageOf(c.b.Get("/admin/alerts")).Tables["Raised now"]
	want := []any{[]any{brokerAlert("runway short"), brokerAlert("leaf expiring")}, [][]string{raisedHead, {"Nothing is raised."}}, 2}
	if diff := cmp.Diff(want, []any{before, raised, len(subjects(t, f))}); diff != "" {
		t.Errorf("subjects before, raised after, messages after (-want +got):\n%s", diff)
	}
}

func TestAShapeChangeFoundWhileTheAlertIsOffIsMailedWhenFoundAgainOnceItIsSetToMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"off"}`, 200)
	c.trust(c.pki.X1)
	f.StoreStub.FailCertificateWrites()
	f.ClockStub.Set("2026-01-05T10:00:00Z")
	c.api.Send("POST", "/api/v1/alerts/certificates/check", "")
	f.StoreStub.Recover()
	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"mail"}`, 200)

	c.checkAt("2026-01-05T11:00:00Z")

	if diff := cmp.Diff([]any{brokerAlert("chain changed")}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestAShapeChangeWhileTheAlertIsOnIsNotMailedOnceItIsSetToMail(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.serves(c.fullChain()...)
	c.checkAt(testsupport.DefaultNow)
	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"on"}`, 200)
	c.trust(c.pki.X1)
	c.checkAt("2026-01-05T10:00:00Z")
	send(t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"mail"}`, 200)
	c.checkAt("2026-01-05T11:00:00Z")
	quiet := subjects(t, f)

	c.trust(c.pki.X2)
	c.checkAt("2026-01-05T12:00:00Z")

	body := send(t, c.api, "GET", "/api/v1/alerts", "", 200).Body
	queued, outbox := testsupport.JSONAt(body, "/outbox/0/subject"), testsupport.JSONAt(body, "/outbox")

	want := []any{[]any{}, []any{brokerAlert("chain changed")}, brokerAlert("chain changed"), 1}
	if diff := cmp.Diff(want, []any{quiet, subjects(t, f), queued, len(outbox.([]any))}); diff != "" {
		t.Errorf("subjects while on, once set to mail, the outbox (-want +got):\n%s", diff)
	}
}

// notFollowedSubjects is the subject of each condition the log says was
// not followed.
func notFollowedSubjects(f *testsupport.Factory) []string {
	var out []string
	for _, e := range f.LogStub.Entries {
		if e.Message == "alert condition not followed" {
			out = append(out, e.Fields["subject"]+": "+e.Fields["error"])
		}
	}
	return out
}

func TestAShapeChangeTheAlertsCannotFollowIsLoggedAndTheCheckIsStored(t *testing.T) {
	t.Parallel()
	const occurrence = "broker.example.com:8883 chain 2026-01-05T09:00:00Z 194fa115e0eb4e99"
	cases := map[string]struct {
		arrange func(c *certificates)
		want    []string
	}{
		"when the conditions cannot be read": {
			func(c *certificates) { c.f.StoreStub.FailAlerts() },
			[]string{occurrence + ": stub: unavailable", "broker.example.com:8883 unreachable: stub: unavailable",
				"broker.example.com:8883 untrusted: stub: unavailable", "broker.example.com:8883 runway: stub: unavailable",
				"broker.example.com:8883 leaf: stub: unavailable"},
		},
		"when its mail cannot be queued": {
			func(c *certificates) { c.f.StoreStub.FailAlertWrites() },
			[]string{occurrence + ": stub: unavailable"},
		},
		"when its condition cannot be stored, mailing none": {
			func(c *certificates) {
				send(c.t, c.api, "PUT", "/api/v1/alerts/kinds/certificate", `{"state":"on"}`, 200)
				c.f.StoreStub.FailAlertWrites()
			},
			[]string{occurrence + ": stub: unavailable"},
		},
		"when the alert settings cannot be read": {
			func(c *certificates) { c.f.EnvStub["DOORCODE_ALERT_HOLD_DOWN_MINUTES"] = "soon" },
			[]string{occurrence + ": invalid value for alert_hold_down_minutes: must be a whole number of minutes from 0 to 10080"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			c.serves(c.fullChain()...)
			c.checkAt(testsupport.DefaultNow)
			c.trust(c.pki.X1)
			tc.arrange(c)
			f.ClockStub.Set("2026-01-05T10:00:00Z")

			anchor := testsupport.JSONAt(send(t, c.api, "POST", "/api/v1/alerts/certificates/check", "", 200).Body, "/endpoints/0/anchor/subject")

			if diff := cmp.Diff([]any{tc.want, x1Subject}, []any{notFollowedSubjects(f), anchor}); diff != "" {
				t.Errorf("not followed, the anchor stored (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAShapeChangeWhileMailCannotBeSentIsMailedOnceItCanSayingWhenItHappened(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ key, blocked, allowed string }{
		"with no recipients":   {"alert_recipients", "", "ops@example.com,\nFront desk <desk@example.com>"},
		"with mail not set up": {"mail_host", "", "smtp.example.com"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			c.set(tc.key, tc.blocked)
			c.serves(c.fullChain()...)
			c.checkAt(testsupport.DefaultNow)
			c.trust(c.pki.X1)
			c.checkAt("2026-01-05T10:00:00Z")
			waiting := pageOf(c.b.Get("/admin/alerts")).Tables["Raised now"]
			f.ClockStub.Set("2026-01-05T11:00:00Z")
			c.set(tc.key, tc.allowed)
			run(t, c.a)

			c.checkAt("2026-01-05T12:00:00Z")

			want := []any{
				[][]string{raisedHead, {"Certificate", "Door broker broker.example.com:8883, chain changed", "Mon 5 Jan 2026 10:00:00", "Waiting: mailed once it can be"}},
				[][]any{{recipients, brokerAlert("chain changed"), chainChangedText("Mon 5 Jan 2026 10:00:00 UTC")}},
				"2026-01-05T11:00:00Z",
			}
			if diff := cmp.Diff(want, []any{waiting, mailed(t, f), testsupport.JSONAt(send(t, c.api, "GET", "/api/v1/alerts", "", 200).Body, "/outbox/0/created_at")}); diff != "" {
				t.Errorf("raised while waiting, mail, when it was queued (-want +got):\n%s", diff)
			}
		})
	}
}

// endpointsListed is the host and port of each endpoint the certificate
// checks API lists, and whether each was checked.
func (c *certificates) endpointsListed() []any {
	c.t.Helper()
	out := []any{}
	for _, e := range testsupport.JSONAt(send(c.t, c.api, "GET", "/api/v1/alerts/certificates", "", 200).Body, "/endpoints").([]any) {
		e := e.(map[string]any)
		out = append(out, []any{e["host"], e["port"], e["checked"]})
	}
	return out
}

func TestAnEndpointTheSettingsNoLongerGiveResolvesWithoutMailAndStartsAgainUnchecked(t *testing.T) {
	t.Parallel()
	const device = "devices.example.com"
	untrustedBroker := func(c *certificates) { c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE) }
	cases := map[string]struct {
		serve         func(c *certificates)
		drop, restore [2]string
		subject       string
		gone, back    []any
	}{
		"door_broker_tls turned off": {
			untrustedBroker, [2]string{"door_broker_tls", "false"}, [2]string{"door_broker_tls", "true"},
			brokerAlert("not verified"), []any{}, []any{[]any{"broker.example.com", 8883.0, false}},
		},
		"door_broker_host changed": {
			untrustedBroker, [2]string{"door_broker_host", "other.example.com"}, [2]string{"door_broker_host", brokerHost},
			brokerAlert("not verified"), []any{[]any{"other.example.com", 8883.0, true}}, []any{[]any{"broker.example.com", 8883.0, false}},
		},
		"device_url made http": {
			func(c *certificates) {
				c.set("device_url", "https://"+device)
				c.serves(c.fullChain()...)
				c.f.ProbeStub.Serve(device, 443, c.pki.YE1.Leaf("2025-12-01", "2026-03-01", device), c.pki.YE1, c.pki.RootYE)
			},
			[2]string{"device_url", "http://" + device}, [2]string{"device_url", "https://" + device},
			"Certificate: Device address devices.example.com:443, not verified",
			[]any{[]any{"broker.example.com", 8883.0, true}},
			[]any{[]any{"broker.example.com", 8883.0, true}, []any{"devices.example.com", 443.0, false}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			tc.serve(c)
			c.checkAt("2026-01-05T09:00:00Z")
			c.set(tc.drop[0], tc.drop[1])
			c.checkAt("2026-01-05T10:00:00Z")
			gone := c.endpointsListed()
			c.set(tc.restore[0], tc.restore[1])
			back := c.endpointsListed()
			c.checkAt("2026-01-05T11:00:00Z")

			want := []any{tc.gone, tc.back, []any{tc.subject, tc.subject}}
			if diff := cmp.Diff(want, []any{gone, back, subjects(t, f)}); diff != "" {
				t.Errorf("endpoints once dropped, once given again, subjects (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAForgottenEndpointsHeldConditionsClearSilentlyAndItsOthersStartFresh(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	c := certificateAlerts(t, f)
	c.f.ProbeStub.Fail(brokerHost, 8883, "i/o timeout")
	for _, at := range []string{"2026-01-05T08:00:00Z", "2026-01-05T08:30:00Z", "2026-01-05T09:00:00Z"} {
		c.checkAt(at)
	}
	c.set("door_broker_tls", "false")
	c.checkAt("2026-01-05T10:00:00Z")
	c.set("door_broker_tls", "true")
	c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE)
	c.checkAt("2026-01-05T10:05:00Z")

	if diff := cmp.Diff([]any{brokerAlert("unreachable"), brokerAlert("not verified")}, subjects(t, f)); diff != "" {
		t.Errorf("subjects (-want +got):\n%s", diff)
	}
}

func TestAnEndpointThatCannotBeForgottenIsForgottenByALaterCheck(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   func(s *testsupport.Store)
		status int
	}{
		"when its conditions cannot be read": {(*testsupport.Store).FailAlerts, 200},
		"when its check cannot be deleted":   {(*testsupport.Store).FailCertificateWrites, 500},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			c := certificateAlerts(t, f)
			c.serves(c.leaf(), c.pki.YE1, c.pki.RootYE)
			c.checkAt("2026-01-05T09:00:00Z")
			c.set("door_broker_tls", "false")
			tc.fail(f.StoreStub)
			f.ClockStub.Set("2026-01-05T10:00:00Z")
			status := c.api.Send("POST", "/api/v1/alerts/certificates/check", "").Status
			f.StoreStub.Recover()
			c.checkAt("2026-01-05T10:30:00Z")
			c.set("door_broker_tls", "true")
			c.checkAt("2026-01-05T11:00:00Z")

			want := []any{tc.status, []any{brokerAlert("not verified"), brokerAlert("not verified")}}
			if diff := cmp.Diff(want, []any{status, subjects(t, f)}); diff != "" {
				t.Errorf("status of the check that could not forget, subjects (-want +got):\n%s", diff)
			}
		})
	}
}
