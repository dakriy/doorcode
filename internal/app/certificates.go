package app

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/dakriy/doorcode/internal/auth"
	"github.com/dakriy/doorcode/internal/certcheck"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/openapi"
)

// certificatesPath is where the Alerts page's Check now button posts.
const certificatesPath = "/admin/alerts/certificates"

// certificateCheckEvery is how often Run looks for endpoints whose
// certificates are due a check.
const certificateCheckEvery = time.Minute

// checkCertificatesWhenDue looks for endpoints whose certificates are
// due a check whenever the clock reaches its next turn, until stop
// closes.
func (a *App) checkCertificatesWhenDue(ctx context.Context, stop <-chan struct{}) {
	for {
		a.checkCertificatesIfDue(ctx)
		select {
		case <-stop:
			return
		case <-a.clock.Alarm(a.nextCertificateLook):
		}
	}
}

// checkCertificatesIfDue checks the endpoints that are due when its turn
// has come, and sets the next turn. A check that fails is logged.
func (a *App) checkCertificatesIfDue(ctx context.Context) {
	now := a.clock.Now()
	if now.Before(a.nextCertificateLook) {
		return
	}
	a.nextCertificateLook = now.Add(certificateCheckEvery)
	if err := a.certificates.CheckDue(ctx); err != nil {
		a.log.Error("certificate check failed", connector.Field{Key: "error", Value: err.Error()})
	}
}

type certificateView struct {
	Subject  string    `json:"subject" doc:"The certificate's subject, a distinguished name."`
	Issuer   string    `json:"issuer" doc:"Who issued it, a distinguished name."`
	NotAfter time.Time `json:"not_after" doc:"When it expires."`
}

type certificateCheckView struct {
	Name       string            `json:"name" doc:"What the endpoint is: Door broker, Device address, or both."`
	Host       string            `json:"host" doc:"The host it is reached at, which the check names as the server (SNI) and the leaf must be valid for."`
	Port       int               `json:"port" doc:"The port it is reached at."`
	Checked    bool              `json:"checked" doc:"True once it was checked at this address."`
	CheckedAt  *time.Time        `json:"checked_at,omitempty" doc:"When it was last checked; absent until it was."`
	Error      string            `json:"error,omitempty" doc:"What the connection or the TLS handshake reported, when the endpoint served no certificates."`
	Verified   bool              `json:"verified" doc:"True when what it served verifies against door_broker_ca_pem, for its host, at the time of the check."`
	Problem    string            `json:"problem,omitempty" doc:"Why what it served does not verify."`
	Chain      []certificateView `json:"chain" doc:"The path from the leaf to the anchor, leaf first, when one was found; else every certificate served, in order."`
	Anchor     *certificateView  `json:"anchor,omitempty" doc:"The certificate of door_broker_ca_pem the path ends at; absent when no path reaches one."`
	RunwayDays *int              `json:"runway_days,omitempty" doc:"Whole days from the check until the earliest expiry of the anchor and of the path's certificates above the leaf's issuer; the leaf and its issuer are left out."`
	LeafDays   *int              `json:"leaf_days,omitempty" doc:"Whole days from the check until the leaf expires."`
	Failures   int               `json:"failures" doc:"How many checks in a row, the latest included, reached no server; 0 once one does."`
	Baseline   *chainShapeView   `json:"baseline,omitempty" doc:"The shape of the chain the endpoint last served verified, whose certificates above the leaf's issuer, the anchor included, the next verified chain is compared with; absent until one verified."`
}

type chainLinkView struct {
	Subject string `json:"subject" doc:"The certificate's subject, a distinguished name."`
	Issuer  string `json:"issuer" doc:"Who issued it, a distinguished name."`
}

type chainShapeView struct {
	Links []chainLinkView `json:"links" doc:"Each certificate of the verified path, leaf first, then the trust anchor."`
	Since time.Time       `json:"since" doc:"When the check that first found the certificates of this shape above the leaf's issuer ran."`
}

type certificatesView struct {
	Enabled       bool                   `json:"enabled" doc:"True while the check runs by itself, from the setting certificate_check_enabled."`
	IntervalHours int                    `json:"interval_hours" doc:"Hours between two checks of an endpoint, from the setting certificate_check_interval_hours."`
	Endpoints     []certificateCheckView `json:"endpoints" doc:"The endpoints the general settings give now: the broker while door_broker_tls is on and door_broker_host is set, then the host of device_url while it is https; each with its latest check."`
}

func (a *App) certificateRoutes() []route {
	admin := holding(auth.RoleAdmin)
	return []route{
		pageRoute(http.MethodPost, certificatesPath, admin, a.checkCertificatesFromForm),
		apiRoute(http.MethodGet, "/alerts/certificates", admin, a.getCertificateChecks, apiDoc{
			id: "getCertificateChecks", summary: "Read the latest certificate check of each endpoint", tag: "alerts",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Whether and how often the check runs, and each endpoint with its latest check.", Body: certificatesView{}}},
		}),
		apiRoute(http.MethodPost, "/alerts/certificates/check", admin, a.checkCertificates, apiDoc{
			id: "checkCertificates", summary: "Check every endpoint's certificates now", tag: "alerts",
			responses: []openapi.Response{{Status: http.StatusOK, Description: "Every endpoint checked just now, whether or not the check runs by itself, once a check in progress has ended.", Body: certificatesView{}}},
		}),
	}
}

func (a *App) getCertificateChecks(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.writeCertificates(w, r, a.certificates.Results)
}

func (a *App) checkCertificates(w http.ResponseWriter, r *http.Request, _ auth.User) {
	a.writeCertificates(w, r, a.certificates.CheckNow)
}

// writeCertificates answers with the check's settings and the results
// results gives, or with the error that stopped reading either.
func (a *App) writeCertificates(w http.ResponseWriter, r *http.Request, results func(context.Context) ([]certcheck.Result, error)) {
	policy, err := a.settings.CertificateCheck(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	checks, err := results(r.Context())
	if err != nil {
		a.internalError(w, r, err)
		return
	}
	view := certificatesView{Enabled: policy.Enabled, IntervalHours: int(policy.Interval / time.Hour), Endpoints: []certificateCheckView{}}
	for _, c := range checks {
		view.Endpoints = append(view.Endpoints, checkViewOf(c))
	}
	writeJSON(w, http.StatusOK, view)
}

func checkViewOf(r certcheck.Result) certificateCheckView {
	v := certificateCheckView{Name: r.Name, Host: r.Host, Port: r.Port, Chain: []certificateView{}}
	c := r.Check
	if c == nil {
		return v
	}
	v.Checked, v.CheckedAt, v.Error, v.Verified, v.Problem = true, utc(&c.CheckedAt), c.Error, c.Verified, c.Problem
	v.RunwayDays, v.LeafDays, v.Failures = c.RunwayDays, c.LeafDays, c.Failures
	if c.Baseline != nil {
		v.Baseline = &chainShapeView{Links: []chainLinkView{}, Since: c.Baseline.Since.UTC()}
		for _, l := range c.Baseline.Links {
			v.Baseline.Links = append(v.Baseline.Links, chainLinkView{Subject: l.Subject, Issuer: l.Issuer})
		}
	}
	for _, cert := range c.Chain {
		v.Chain = append(v.Chain, certificateViewOf(cert))
	}
	if c.Anchor != nil {
		anchor := certificateViewOf(*c.Anchor)
		v.Anchor = &anchor
	}
	return v
}

func certificateViewOf(c connector.StoredCertificate) certificateView {
	return certificateView{Subject: c.Subject, Issuer: c.Issuer, NotAfter: c.NotAfter.UTC()}
}

// checkCertificatesFromForm checks every endpoint now and goes back to
// the Alerts page.
func (a *App) checkCertificatesFromForm(w http.ResponseWriter, r *http.Request, _ auth.User) {
	_, err := a.certificates.CheckNow(r.Context())
	a.changed(w, r, err, alertsPath, "certificates-checked")
}

// certificateRow is an endpoint as the Alerts page lists it, its times in
// the site timezone.
type certificateRow struct {
	Endpoint string
	Checked  string
	Result   string
	Runway   string
	Leaf     string
	Failures string
	Baseline string
	// Address labels the tables of Chain, which is empty before the first
	// check and while the endpoint served nothing, and of Links, the
	// baseline's certificates, empty before a chain verified.
	Address string
	Chain   []chainRow
	Links   []connector.StoredChainLink
}

// chainRow is a certificate as the Alerts page lists it.
type chainRow struct {
	Role    string
	Subject string
	Issuer  string
	Expires string
}

// certificateRowOf is r as the Alerts page lists it, its times in zone.
func certificateRowOf(r certcheck.Result, zone *time.Location) certificateRow {
	row := certificateRow{Endpoint: r.Name + " " + r.Address(), Address: r.Address(), Checked: "Not checked yet"}
	c := r.Check
	if c == nil {
		return row
	}
	row.Checked = shownAt(zone, c.CheckedAt, secondLayout)
	row.Result = resultText(*c)
	row.Runway, row.Leaf = daysLeftText(c.RunwayDays), daysLeftText(c.LeafDays)
	row.Failures = strconv.Itoa(c.Failures)
	if c.Baseline != nil {
		row.Baseline, row.Links = shownAt(zone, c.Baseline.Since, secondLayout), c.Baseline.Links
	}
	for _, f := range certcheck.FoundIn(*c) {
		row.Chain = append(row.Chain, chainRowOf(f, zone))
	}
	return row
}

func chainRowOf(f certcheck.Found, zone *time.Location) chainRow {
	return chainRow{Role: f.Role, Subject: f.Subject, Issuer: f.Issuer, Expires: shownAt(zone, f.NotAfter, longDateLayout)}
}

// resultText is what the Alerts page says c found.
func resultText(c connector.StoredCertificateCheck) string {
	if c.Error != "" {
		return "Unreachable: " + c.Error
	}
	if c.Verified {
		return "Verified up to " + c.Anchor.Subject
	}
	return "Not verified: " + c.Problem
}

// daysLeftText is days as the Alerts page writes it, empty when nil.
func daysLeftText(days *int) string {
	if days == nil {
		return ""
	}
	return strconv.Itoa(*days) + " days"
}
