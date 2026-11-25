package certcheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/alert"
	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/settings"
)

// The reasons an endpoint alerts, each its own condition of the
// certificate alert: the key its subject ends in and how its mail names
// it.
var (
	untrusted    = reason{"untrusted", "not verified"}
	shortRunway  = reason{"runway", "runway short"}
	expiringLeaf = reason{"leaf", "leaf expiring"}
	unreachable  = reason{"unreachable", "unreachable"}
	changedShape = reason{"chain", "chain changed"}
)

// reason is why an endpoint alerts: the key its condition's subject
// carries and the name its mail calls it by.
type reason struct{ key, name string }

// heldReasons are the reasons whose conditions stay raised until a check
// clears them.
var heldReasons = []reason{untrusted, shortRunway, expiringLeaf, unreachable}

// subjectOf is the subject of the certificate alert's condition of the
// endpoint at address for why.
func subjectOf(address string, why reason) string {
	return address + " " + why.key
}

// conditionOf is the certificate alert's condition of e for why, its
// subject followed by occurrence when that is not empty.
func conditionOf(e Endpoint, why reason, occurrence string) alert.Condition {
	subject := subjectOf(e.Address(), why)
	if occurrence != "" {
		subject += " " + occurrence
	}
	return alert.Condition{Kind: alert.Certificate, Subject: subject, Name: e.Name + " " + e.Address() + ", " + why.name}
}

// forget drops address, an endpoint the settings no longer give, at at:
// it resolves its raised conditions without mail, since nothing checks it
// any more, then deletes its check, so it starts again as never checked
// should the settings give it again. A check whose conditions could not
// all be resolved, which is logged, is kept for the next check to try
// again.
func (s Service) forget(ctx context.Context, address string, at time.Time) error {
	withdrawn := true
	for _, why := range heldReasons {
		c := alert.Condition{Kind: alert.Certificate, Subject: subjectOf(address, why), Name: address}
		if err := s.Alerts.Withdraw(ctx, c, at); err != nil {
			withdrawn = false
		}
	}
	if !withdrawn {
		return nil
	}
	if err := s.Store.DeleteCertificateCheck(ctx, address); err != nil {
		return fmt.Errorf("delete the certificate check of %s: %w", address, err)
	}
	return nil
}

// follow raises and resolves the conditions of e that c, its latest
// check, says something of, as policy says: a check that reached no
// server judges only whether e is unreachable, and the runway and the
// leaf are judged only when c could work them out.
func (s Service) follow(ctx context.Context, e Endpoint, c connector.StoredCertificateCheck, policy settings.CertificateCheck) {
	at := c.CheckedAt
	if c.Error != "" {
		if c.Failures >= policy.Unreachable {
			detail := strconv.Itoa(c.Failures) + " checks in a row of " + e.Address() + " reached no server; the latest said: " + c.Error + "."
			s.Alerts.Raise(ctx, conditionOf(e, unreachable, ""), at, detail)
		}
		return
	}
	s.Alerts.Resolve(ctx, conditionOf(e, unreachable, ""), at)
	facts := chainFacts("Chain:", c)
	s.hold(ctx, conditionOf(e, untrusted, ""), !c.Verified, at,
		"What "+e.Address()+" served does not verify against door_broker_ca_pem: "+c.Problem+".\n\n"+facts)
	s.holdDays(ctx, conditionOf(e, shortRunway, ""), c.RunwayDays, policy.Runway, at, "The runway of "+e.Address()+
		", the days until the earliest expiry of the trust anchor and the certificates between it and the leaf's issuer, is", settings.KeyCertificateRunway, facts)
	s.holdDays(ctx, conditionOf(e, expiringLeaf, ""), c.LeafDays, policy.Leaf, at, "The leaf "+e.Address()+" serves expires in",
		settings.KeyCertificateLeaf, facts)
}

// holdDays holds c, at at, while fewer days than limit, which the setting
// key gives, are left, a mail saying what is left followed by facts; it
// does nothing when left is nil.
func (s Service) holdDays(ctx context.Context, c alert.Condition, left *int, limit time.Duration, at time.Time, what, key, facts string) {
	if left == nil {
		return
	}
	detail := what + " " + days(*left) + ", fewer than the " + days(wholeDays(limit)) + " of " + key + ".\n\n" + facts
	s.hold(ctx, c, below(*left, limit), at, detail)
}

// hold raises c at at, saying detail, while holds, and resolves it
// otherwise.
func (s Service) hold(ctx context.Context, c alert.Condition, holds bool, at time.Time, detail string) {
	if holds {
		s.Alerts.Raise(ctx, c, at, detail)
		return
	}
	s.Alerts.Resolve(ctx, c, at)
}

// below reports whether n days is less than limit.
func below(n int, limit time.Duration) bool {
	return n < wholeDays(limit)
}

// followShape is the baseline of e after c, its latest check, given the
// one before it, baseline: the shape c verified, unless it is baseline's
// shape, and then baseline itself. A shape whose links above the leaf's
// issuer, the anchor's included (trustedLinks), differ from baseline's
// is mailed once, the change from that baseline to that shape being its
// condition's occurrence. One that differs only in the leaf or its
// issuer, as when the issuing CA signs a renewal with another
// intermediate under the same roots, replaces baseline without mail,
// keeping the time baseline's links above the issuer were first served.
// The first shape c verifies replaces none and is not mailed.
func (s Service) followShape(ctx context.Context, e Endpoint, baseline *connector.StoredChainShape, c connector.StoredCertificateCheck) *connector.StoredChainShape {
	if !c.Verified {
		return baseline
	}
	shape := shapeOf(c)
	if baseline == nil {
		return &shape
	}
	if slices.Equal(baseline.Links, shape.Links) {
		return baseline
	}
	if slices.Equal(trustedLinks(baseline.Links), trustedLinks(shape.Links)) {
		shape.Since = baseline.Since
		return &shape
	}
	occurrence := baseline.Since.UTC().Format(time.RFC3339Nano) + " " + fingerprint(shape.Links)
	detail := "The chain " + e.Address() + " serves verifies through other certificates above the leaf's issuer than the one it served since " +
		alert.Stamp(baseline.Since) + ", which it now replaces.\n\nBefore:\n" + linksText(baseline.Links) + "\n\n" + chainFacts("Now:", c)
	s.Alerts.Notify(ctx, conditionOf(e, changedShape, occurrence), c.CheckedAt, detail)
	return &shape
}

// shapeOf is the shape of the chain c verified, found at c's time: the
// subject and issuer of each certificate of the path, then the anchor's.
func shapeOf(c connector.StoredCertificateCheck) connector.StoredChainShape {
	var links []connector.StoredChainLink
	for _, cert := range append(slices.Clone(c.Chain), *c.Anchor) {
		links = append(links, connector.StoredChainLink{Subject: cert.Subject, Issuer: cert.Issuer})
	}
	return connector.StoredChainShape{Links: links, Since: c.CheckedAt}
}

// trustedLinks is the part of links, a shape's, that ties its chain to
// the anchor: the links above the leaf's issuer, the anchor's last.
func trustedLinks(links []connector.StoredChainLink) []connector.StoredChainLink {
	return links[min(2, len(links)-1):]
}

// fingerprint tells shapes apart in a few characters: the start of the
// SHA-256 of their links.
func fingerprint(links []connector.StoredChainLink) string {
	h := sha256.New()
	for _, l := range links {
		h.Write([]byte(l.Subject + "\x00" + l.Issuer + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}

// linksText is links as a mail lists them, one per line.
func linksText(links []connector.StoredChainLink) string {
	lines := make([]string, 0, len(links))
	for _, l := range links {
		lines = append(lines, l.Subject+", issued by "+l.Issuer)
	}
	return strings.Join(lines, "\n")
}

// chainFacts is what a mail says of the chain c found, headed by head:
// each certificate with its role, subject, issuer, expiry and days left,
// then the runway and the leaf's days when c worked them out.
func chainFacts(head string, c connector.StoredCertificateCheck) string {
	lines := []string{head}
	for _, f := range FoundIn(c) {
		lines = append(lines, certificateText(f, c.CheckedAt))
	}
	if c.RunwayDays != nil {
		lines = append(lines, "Runway: "+days(*c.RunwayDays)+".")
	}
	if c.LeafDays != nil {
		lines = append(lines, "Leaf expires in "+days(*c.LeafDays)+".")
	}
	return strings.Join(lines, "\n")
}

// certificateText is f as a mail lists it at now.
func certificateText(f Found, now time.Time) string {
	return f.Role + ": " + f.Subject + ", issued by " + f.Issuer + ", expires " + alert.Stamp(f.NotAfter) +
		" (" + days(daysLeft(f.NotAfter, now)) + ")"
}

// days is n days as a mail writes it.
func days(n int) string {
	return strconv.Itoa(n) + " days"
}
