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
	KeyAlertRecipients = "alert_recipients"
	KeyAlertCleared    = "alert_mail_cleared"
	KeyAlertHoldDown   = "alert_hold_down_minutes"
	recipientsExample  = "ops@example.com, Front desk <desk@example.com>"
)

// TabAlerts is where the alert mail settings are shown: the Alerts page,
// apart from the settings page's tabs.
var TabAlerts = Tab{Slug: "alerts", Name: "Alerts"}

// alertHoldDown parses how long a cleared alert condition stays quiet.
var alertHoldDown = length{whole{0, 10080, "minutes"}, minutesOf}

// alertsSection is the settings of whom alerts mail and when.
var alertsSection = section{Section{Name: "Alert mail", About: "Whom every enabled alert mails, whether it mails again when its condition clears, and how long a cleared condition stays quiet.", Tab: TabAlerts}, []definition{
	{key: KeyAlertRecipients, fallback: "", validate: accepting(recipients).askedWith(Input{Kind: KindAddresses, Example: recipientsExample}), label: "Recipients",
		help: "The addresses every enabled alert mails, separated by commas or new lines, such as ops@example.com. Nothing is mailed while it is empty."},
	{key: KeyAlertCleared, fallback: Yes, validate: accepting(boolean), label: "Mail when cleared",
		help: "Mails the recipients once more when a condition an alert mailed about clears."},
	{key: KeyAlertHoldDown, fallback: "15", validate: alertHoldDown.rule(), label: "Quiet after clearing",
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
	values := map[string]string{}
	for _, def := range alertsSection.definitions {
		values[def.key] = s.resolve(def, stored).Value
	}
	var errs [3]error
	var a Alerts
	a.Recipients, errs[0] = parsed(values, KeyAlertRecipients, recipients)
	a.MailCleared, errs[1] = parsed(values, KeyAlertCleared, boolean)
	a.HoldDown, errs[2] = parsed(values, KeyAlertHoldDown, alertHoldDown.parse)
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
