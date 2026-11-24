// Package certcheck checks the certificates the site's TLS endpoints
// serve as a door verifies them: against the certificates of the setting
// door_broker_ca_pem alone, and keeps the latest result of each endpoint.
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

// Service checks the endpoints' certificates and keeps the results. Lock
// lets one check of the endpoints run at a time, so a check never
// stores a result over the newer one of a check that began after it.
type Service struct {
	Probe    connector.CertificateProbe
	Store    connector.CertificatesStore
	Settings settings.Service
	Clock    connector.Clock
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
	return s.resultsOf(ctx, endpoints)
}

// resultsOf is each of endpoints with its latest check.
func (s Service) resultsOf(ctx context.Context, endpoints []Endpoint) ([]Result, error) {
	stored, err := s.Store.CertificateChecks(ctx)
	if err != nil {
		return nil, err
	}
	byAddress := map[string]connector.StoredCertificateCheck{}
	for _, c := range stored {
		byAddress[c.Address] = c
	}
	results := make([]Result, 0, len(endpoints))
	for _, e := range endpoints {
		r := Result{Endpoint: e}
		if c, checked := byAddress[e.Address()]; checked {
			r.Check = &c
		}
		results = append(results, r)
	}
	return results, nil
}

// CheckNow checks every endpoint now, whether or not the check runs by
// itself, and returns the results. It waits for a check in progress to
// end first.
func (s Service) CheckNow(ctx context.Context) ([]Result, error) {
	s.Lock.Lock()
	defer s.Lock.Unlock()
	endpoints, trust, err := s.target(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range endpoints {
		if err := s.check(ctx, e, trust); err != nil {
			return nil, err
		}
	}
	return s.resultsOf(ctx, endpoints)
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
	endpoints, trust, err := s.target(ctx)
	if err != nil {
		return err
	}
	results, err := s.resultsOf(ctx, endpoints)
	if err != nil {
		return err
	}
	for _, e := range dueOf(results, policy.Interval, s.Clock.Now()) {
		if err := s.check(ctx, e, trust); err != nil {
			return err
		}
	}
	return nil
}

// dueOf is the endpoints of results due a check at now: those never
// checked and those checked interval ago or longer.
func dueOf(results []Result, interval time.Duration, now time.Time) []Endpoint {
	var due []Endpoint
	for _, r := range results {
		if r.Check == nil || !now.Before(r.Check.CheckedAt.Add(interval)) {
			due = append(due, r.Endpoint)
		}
	}
	return due
}

// check checks e against trust and stores the result.
func (s Service) check(ctx context.Context, e Endpoint, trust []*x509.Certificate) error {
	served, err := s.Probe.Served(ctx, e.Host, e.Port)
	now := s.Clock.Now()
	result := connector.StoredCertificateCheck{Chain: []connector.StoredCertificate{}}
	if err != nil {
		result.Error = err.Error()
	} else {
		result = Judge(e.Host, served, trust, now)
	}
	result.Address, result.CheckedAt = e.Address(), now
	if err := s.Store.PutCertificateCheck(ctx, result); err != nil {
		return fmt.Errorf("store the certificate check of %s: %w", e.Address(), err)
	}
	return nil
}
