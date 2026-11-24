package settings

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
)

// The keys of the alert mail settings.
const (
	keyAlertRecipients = "alert_recipients"
	keyAlertCleared    = "alert_mail_cleared"
	keyAlertHoldDown   = "alert_hold_down_minutes"
	recipientsExample  = "ops@example.com, Front desk <desk@example.com>"
)

// alertHoldDown parses how long a cleared alert condition stays quiet.
var alertHoldDown = length{whole{0, 10080, "minutes"}, minutesOf}

// alertsSection is the settings of whom alerts mail and when.
var alertsSection = section{Section{Name: "Alert mail", About: "Whom every enabled alert mails, whether it mails again when its condition clears, and how long a cleared condition stays quiet.", Tab: TabAlerts}, []definition{
	{key: keyAlertRecipients, fallback: "", validate: accepting(recipients).askedWith(Input{Kind: KindAddresses, Example: recipientsExample}), label: "Recipients",
		help: "The addresses every enabled alert mails, separated by commas or new lines, such as ops@example.com. Nothing is mailed while it is empty."},
	{key: keyAlertCleared, fallback: Yes, validate: accepting(boolean), label: "Mail when cleared",
		help: "Mails the recipients once more when a condition an alert mailed about clears."},
	{key: keyAlertHoldDown, fallback: "15", validate: alertHoldDown.rule(), label: "Quiet after clearing",
		help: "Minutes a cleared condition must stay clear before it mails again when it comes back; one that comes back sooner is followed without mail."},
}}

// Alerts is whom alerts mail and when: the bare addresses of the
// Recipients, whether a cleared condition is mailed too (MailCleared),
// and how long a cleared condition must stay clear before it mails again
// (HoldDown).
type Alerts struct {
	Recipients  []string
	MailCleared bool
	HoldDown    time.Duration
}

// Alerts returns whom alerts mail and when. It reads only the alert mail
// settings, so no other setting stops alerts.
func (s Service) Alerts(ctx context.Context) (Alerts, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return Alerts{}, err
	}
	values := s.valuesOf(alertsSection, stored)
	var errs [3]error
	var a Alerts
	a.Recipients, errs[0] = parsed(values, keyAlertRecipients, recipients)
	a.MailCleared, errs[1] = parsed(values, keyAlertCleared, boolean)
	a.HoldDown, errs[2] = parsed(values, keyAlertHoldDown, alertHoldDown.parse)
	return a, errors.Join(errs[:]...)
}

// recipients parses email addresses, each with or without a name,
// separated by commas or line breaks, into their bare addresses. A CR
// before a line feed is space around an entry.
func recipients(value string) ([]string, error) {
	out := []string{}
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' }) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		address, err := mail.ParseAddress(entry)
		if err != nil {
			return nil, errors.New("must be email addresses separated by commas or new lines, such as " + recipientsExample)
		}
		out = append(out, address.Address)
	}
	return out, nil
}

// The keys of the certificate check settings.
const (
	keyCertificateCheck    = "certificate_check_enabled"
	keyCertificateInterval = "certificate_check_interval_hours"
	KeyCertificateRunway   = "certificate_runway_days"
	KeyCertificateLeaf     = "certificate_leaf_days"
	keyCertificateFailures = "certificate_unreachable_checks"
)

// certificateInterval parses how long the certificate check waits between
// checks of an endpoint.
var certificateInterval = length{whole{1, 720, "hours"}, hoursOf}

// certificateRunway and certificateLeaf parse how few days left along a
// chain, and of its leaf, alert.
var (
	certificateRunway = length{whole{1, 3650, "days"}, daysOf}
	certificateLeaf   = length{whole{1, 365, "days"}, daysOf}
)

// unreachableChecks is how many checks in a row must reach no server
// before an endpoint alerts as unreachable.
var unreachableChecks = whole{1, 100, "checks"}

// hoursOf is n hours.
func hoursOf(n int) time.Duration { return time.Duration(n) * time.Hour }

// certificateSection is the settings of the certificate check.
var certificateSection = section{Section{Name: "Certificate check", About: "Checks the certificates the door broker and the device address serve against door_broker_ca_pem, as a door verifies them.", Tab: TabAlerts}, []definition{
	{key: keyCertificateCheck, fallback: No, validate: accepting(boolean), label: "Check certificates",
		help: "Checks each endpoint again once the interval has passed since its last check. Check now works while it is off."},
	{key: keyCertificateInterval, fallback: "24", validate: certificateInterval.rule(), label: "Check interval",
		help: "Hours, from 1 to 720, between two checks of an endpoint."},
	{key: KeyCertificateRunway, fallback: "548", validate: certificateRunway.rule(), label: "Runway alert",
		help: "Days, from 1 to 3650: the certificate alert is raised when the earliest expiry along a chain after its leaf, the trust anchor included, is fewer days away."},
	{key: KeyCertificateLeaf, fallback: "14", validate: certificateLeaf.rule(), label: "Leaf alert",
		help: "Days, from 1 to 365: the certificate alert is raised when the leaf expires in fewer days."},
	{key: keyCertificateFailures, fallback: "3", validate: unreachableChecks.rule(), label: "Unreachable alert",
		help: "Checks, from 1 to 100: the certificate alert is raised when this many checks in a row reach no server."},
}}

// CertificateCheck is whether the certificate check runs by itself
// (Enabled), how long it waits between two checks of an endpoint
// (Interval), and when an endpoint alerts: when the earliest expiry
// after its leaf is less than Runway away, when its leaf expires in less
// than Leaf, and when Unreachable checks in a row reached no server.
type CertificateCheck struct {
	Enabled     bool
	Interval    time.Duration
	Runway      time.Duration
	Leaf        time.Duration
	Unreachable int
}

// CertificateCheck returns whether the certificate check runs by itself
// and how often. It reads only its own settings.
func (s Service) CertificateCheck(ctx context.Context) (CertificateCheck, error) {
	stored, err := s.Store.Settings(ctx)
	if err != nil {
		return CertificateCheck{}, err
	}
	values := s.valuesOf(certificateSection, stored)
	var c CertificateCheck
	var errs [5]error
	c.Enabled, errs[0] = parsed(values, keyCertificateCheck, boolean)
	c.Interval, errs[1] = parsed(values, keyCertificateInterval, certificateInterval.parse)
	c.Runway, errs[2] = parsed(values, KeyCertificateRunway, certificateRunway.parse)
	c.Leaf, errs[3] = parsed(values, KeyCertificateLeaf, certificateLeaf.parse)
	c.Unreachable, errs[4] = parsed(values, keyCertificateFailures, unreachableChecks.parse)
	return c, errors.Join(errs[:]...)
}
