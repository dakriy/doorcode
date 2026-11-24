package app

import (
	"slices"
	"strings"
	"time"

	"github.com/dakriy/doorcode/internal/settings"
)

// certificateView is a certificate as a page lists it: whom it names,
// when it is valid, and, when that is not now, why.
type certificateView struct {
	Subject string
	Valid   string
	State   string
}

// certificatesOf is each certificate value holds, as a page lists it at
// now, for an input asking for certificates; none for any other input.
// Their dates are in UTC, as certificates give them.
func certificatesOf(in settings.Input, value string, now time.Time) []certificateView {
	if in.Kind != settings.KindCertificates {
		return nil
	}
	var views []certificateView
	for _, c := range settings.Certificates(value) {
		view := certificateView{Subject: c.Subject,
			Valid: shownAt(time.UTC, c.NotBefore, longDateLayout) + " to " + shownAt(time.UTC, c.NotAfter, longDateLayout) + " (UTC)"}
		switch {
		case now.After(c.NotAfter):
			view.State = "expired"
		case now.Before(c.NotBefore):
			view.State = "not yet valid"
		}
		views = append(views, view)
	}
	return views
}

// describedBy is the ids, separated by spaces, of what describes the
// input id names, which asks with in: its range when it is bounded, the
// certificates it holds when it holds some, its state when it is a
// secret, and errorID, the id of what is wrong with its value, last, when
// it is not empty.
func describedBy(id string, in settings.Input, certificates []certificateView, secret bool, errorID string) string {
	var ids []string
	if in.Bounded() {
		ids = append(ids, id+"-range")
	}
	if len(certificates) > 0 {
		ids = append(ids, id+"-certificates")
	}
	if secret {
		ids = append(ids, id+"-state")
	}
	if errorID != "" {
		ids = append(ids, errorID)
	}
	return strings.Join(ids, " ")
}

// shownAs is value as a page shows the value of a setting asked for with
// in: a yes or no, which a setting holds only once it is checked, as On
// or Off, anything else as it is.
func shownAs(in settings.Input, value string) string {
	if in.Kind != settings.KindYesNo {
		return value
	}
	if value == settings.Yes {
		return "On"
	}
	return "Off"
}

// severalOptions is the checkboxes of an input asking with in for any
// number of its choices, those value chooses checked.
func severalOptions(in settings.Input, value string) []selectOption {
	chosen := in.Chosen(value)
	var options []selectOption
	for _, c := range in.Choices {
		options = append(options, selectOption{Value: c, Label: c, Selected: slices.Contains(chosen, c)})
	}
	return options
}

// overrideOptions is the options of the select a door's page asks for its
// own value of f with, when f is a yes or no or a choice, entered chosen:
// following the site's default, def, as the empty value, then entered
// when f does not take it, so that saving the form keeps it, then each
// value f takes. It is none for any other field.
func overrideOptions(f settings.DoorField, def, entered string) []selectOption {
	values := f.Input.Choices
	if f.Input.Kind == settings.KindYesNo {
		values = []string{settings.Yes, settings.No}
	}
	if len(values) == 0 {
		return nil
	}
	options := []selectOption{{Value: "", Label: "Default (" + shownAs(f.Input, def) + ")", Selected: entered == ""}}
	if entered != "" && !slices.Contains(values, entered) {
		options = append(options, selectOption{Value: entered, Label: entered, Selected: true})
	}
	for _, v := range values {
		options = append(options, selectOption{Value: v, Label: shownAs(f.Input, v), Selected: v == entered})
	}
	return options
}

// settingCell is a setting's row as a table of settings shows its value,
// with the site's accent color, which a color picker starts at while its
// setting is empty.
type settingCell struct {
	settingRow
	Accent string
}

// numberRange is the range of a bounded number beside the input ID names,
// which asks with Input.
type numberRange struct {
	ID    string
	Input settings.Input
}

// certificateList is the certificates a setting holds, beside the input
// ID names.
type certificateList struct {
	ID           string
	Certificates []certificateView
}
