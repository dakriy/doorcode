// Package certcheck checks the certificates the site's TLS endpoints
// serve as a door verifies them: against the certificates of the setting
// door_broker_ca_pem alone, and keeps the latest result of each endpoint.
// Each check follows the certificate alert's conditions of its endpoint.
// It never changes a setting or a door's configuration.
package certcheck

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dakriy/doorcode/internal/alert"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// The names of the endpoints the check reaches.
const (
	BrokerEndpoint = "Door broker"
	DeviceEndpoint = "Device address"
)

// httpsPort is the port of an https address that names none.
const httpsPort = 443

// Endpoint is a TLS endpoint doors reach: what it is (Name), its Host,
// and its Port.
type Endpoint struct {
	Name string
	Host string
	Port int
}

// Address is the endpoint as host:port, an IPv6 host in brackets.
func (e Endpoint) Address() string {
	host := e.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(e.Port)
}

// Endpoints is the endpoints of the site's general settings: the broker
// of config while it is reached over TLS, then the host of deviceURL
// while it is an https address, each address once.
func Endpoints(config settings.DoorConfig, deviceURL string) []Endpoint {
	var endpoints []Endpoint
	if config.BrokerTLS && config.BrokerHost != "" {
		endpoints = append(endpoints, Endpoint{Name: BrokerEndpoint, Host: config.BrokerHost, Port: config.BrokerPort})
	}
	device, secure := deviceEndpoint(deviceURL)
	if !secure {
		return endpoints
	}
	if len(endpoints) > 0 && endpoints[0].Address() == device.Address() {
		endpoints[0].Name += " and " + strings.ToLower(DeviceEndpoint)
		return endpoints
	}
	return append(endpoints, device)
}

// deviceEndpoint is the endpoint of deviceURL, its port or else 443, and
// whether deviceURL is an https address at all.
func deviceEndpoint(deviceURL string) (Endpoint, bool) {
	u, err := url.Parse(deviceURL)
	if err != nil || u.Scheme != "https" {
		return Endpoint{}, false
	}
	device := Endpoint{Name: DeviceEndpoint, Host: u.Hostname(), Port: httpsPort}
	if p := u.Port(); p != "" {
		device.Port, _ = strconv.Atoi(p)
	}
	return device, true
}

// Service checks the endpoints' certificates, keeps the results, and
// raises and resolves the certificate alert's conditions through Alerts.
// Lock lets one check of the endpoints run at a time, so a check never
// stores a result over the newer one of a check that began after it.
type Service struct {
	Probe    connector.CertificateProbe
	Store    connector.CertificatesStore
	Settings settings.Service
	Clock    connector.Clock
	Alerts   alert.Service
	Lock     *sync.Mutex
}

// Result is an endpoint and its latest check, nil before its first.
type Result struct {
	Endpoint
	Check *connector.StoredCertificateCheck
}

// target is the endpoints the settings give now and the trust list they
// are verified against.
func (s Service) target(ctx context.Context) ([]Endpoint, []*x509.Certificate, error) {
	defaults, defaultsErr := s.Settings.DoorDefaults(ctx)
	current, currentErr := s.Settings.Current(ctx)
	if err := errors.Join(defaultsErr, currentErr); err != nil {
		return nil, nil, err
	}
	config, err := defaults.For(nil)
	if err != nil {
		return nil, nil, err
	}
	return Endpoints(config, current.DeviceURL), TrustList(config.BrokerCAPEM), nil
}

// Results is every endpoint the settings give now, each with its latest
// check.
func (s Service) Results(ctx context.Context) ([]Result, error) {
	endpoints, _, err := s.target(ctx)
	if err != nil {
		return nil, err
	}
	results, _, err := s.resultsOf(ctx, endpoints)
	return results, err
}

// resultsOf is each of endpoints with its latest check, and the address
// of each check stored for an endpoint not among them.
func (s Service) resultsOf(ctx context.Context, endpoints []Endpoint) ([]Result, []string, error) {
	stored, err := s.Store.CertificateChecks(ctx)
	if err != nil {
		return nil, nil, err
	}
	byAddress := map[string]connector.StoredCertificateCheck{}
	for _, c := range stored {
		byAddress[c.Address] = c
	}
	results := make([]Result, 0, len(endpoints))
	given := map[string]bool{}
	for _, e := range endpoints {
		r := Result{Endpoint: e}
		if c, checked := byAddress[e.Address()]; checked {
			r.Check = &c
		}
		results = append(results, r)
		given[e.Address()] = true
	}
	var gone []string
	for _, c := range stored {
		if !given[c.Address] {
			gone = append(gone, c.Address)
		}
	}
	return results, gone, nil
}

// prepare is what a check of the endpoints starts from: the endpoints
// the settings give now, each with its latest check, and the trust list
// they are verified against. It forgets every endpoint checked before
// that the settings no longer give.
func (s Service) prepare(ctx context.Context) ([]Result, []*x509.Certificate, error) {
	endpoints, trust, err := s.target(ctx)
	if err != nil {
		return nil, nil, err
	}
	results, gone, err := s.resultsOf(ctx, endpoints)
	if err != nil {
		return nil, nil, err
	}
	now := s.Clock.Now()
	for _, address := range gone {
		if err := s.forget(ctx, address, now); err != nil {
			return nil, nil, err
		}
	}
	return results, trust, nil
}

// CheckNow checks every endpoint now, whether or not the check runs by
// itself, and returns the results. It waits for a check in progress to
// end first.
func (s Service) CheckNow(ctx context.Context) ([]Result, error) {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	policy, err := s.Settings.CertificateCheck(ctx)
	if err != nil {
		return nil, err
	}
	results, trust, err := s.prepare(ctx)
	if err != nil {
		return nil, err
	}
	endpoints := make([]Endpoint, 0, len(results))
	for _, r := range results {
		if err := s.check(ctx, r, trust, policy); err != nil {
			return nil, err
		}
		endpoints = append(endpoints, r.Endpoint)
	}
	checked, _, err := s.resultsOf(ctx, endpoints)
	return checked, err
}

// CheckDue checks, while the check runs by itself, every endpoint whose
// latest check is at least the interval old or that was never checked.
// It waits for a check in progress to end first.
func (s Service) CheckDue(ctx context.Context) error {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	policy, err := s.Settings.CertificateCheck(ctx)
	if err != nil || !policy.Enabled {
		return err
	}
	results, trust, err := s.prepare(ctx)
	if err != nil {
		return err
	}
	for _, r := range dueOf(results, policy.Interval, s.Clock.Now()) {
		if err := s.check(ctx, r, trust, policy); err != nil {
			return err
		}
	}
	return nil
}

// dueOf is the results due a check at now: those never checked and
// those checked interval ago or longer.
func dueOf(results []Result, interval time.Duration, now time.Time) []Result {
	var due []Result
	for _, r := range results {
		if r.Check == nil || !now.Before(r.Check.CheckedAt.Add(interval)) {
			due = append(due, r)
		}
	}
	return due
}

// check checks the endpoint of r against trust, carrying on from its
// latest check, stores the result, and follows the endpoint's alert
// conditions as policy says. A chain shape that changed is mailed before
// the result is stored, so it is mailed even when storing fails.
func (s Service) check(ctx context.Context, r Result, trust []*x509.Certificate, policy settings.CertificateCheck) error {
	served, err := s.Probe.Served(ctx, r.Host, r.Port)
	now := s.Clock.Now()
	before := connector.StoredCertificateCheck{}
	if r.Check != nil {
		before = *r.Check
	}
	result := connector.StoredCertificateCheck{Chain: []connector.StoredCertificate{}, Failures: before.Failures + 1}
	if err != nil {
		result.Error = err.Error()
	} else {
		result = Judge(r.Host, served, trust, now)
	}
	result.Address, result.CheckedAt = r.Address(), now
	result.Baseline = s.followShape(ctx, r.Endpoint, before.Baseline, result)
	if err := s.Store.PutCertificateCheck(ctx, result); err != nil {
		return fmt.Errorf("store the certificate check of %s: %w", r.Address(), err)
	}
	s.follow(ctx, r.Endpoint, result, policy)
	return nil
}
