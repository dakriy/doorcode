package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// everySettingsTab is what every tab of the settings page asks with, each
// input read from its markup.
func everySettingsTab(b *testsupport.Browser) testsupport.Page {
	page := testsupport.Page{Inputs: map[string]string{}, Placeholders: map[string]string{}, Limits: map[string]string{}, Descriptions: map[string]string{}}
	for _, tab := range []string{"site", "sign-in", "look", "doors"} {
		got := testsupport.ParsePage(b.Get("/admin/settings?tab=" + tab).Body)
		for name, v := range got.Inputs {
			page.Inputs[name] = v
		}
		for name, v := range got.Placeholders {
			page.Placeholders[name] = v
		}
		for name, v := range got.Limits {
			page.Limits[name] = v
		}
		for name, v := range got.Descriptions {
			page.Descriptions[name] = v
		}
	}
	return page
}

// entriesOf is the entries of m that keys name.
func entriesOf(m map[string]string, keys ...string) map[string]string {
	out := map[string]string{}
	for _, key := range keys {
		out[key] = m[key]
	}
	return out
}

func TestTheSettingsPageAsksForAddressesEmailsAndHostsWithInputsOfTheirType(t *testing.T) {
	t.Parallel()
	page := everySettingsTab(signedInAdmin(t, testsupport.NewFactory()))

	want := []any{
		map[string]string{
			"public_url": "url", "device_url": "url", "oidc_issuer": "url", "admin_email": "email",
			"door_ntp_server": "text", "door_broker_host": "text", "door_broker_ca_pem": "textarea", "enrollment_password": "password",
		},
		map[string]string{
			"public_url": "https://doorcode.example.com", "device_url": "https://devices.doorcode.example.com", "oidc_issuer": "https://id.example.com",
			"trusted_proxies": "10.0.0.0/8,proxy.example.com", "oidc_default_groups": "staff,contractors",
			"door_ntp_server": "pool.ntp.org", "door_broker_host": "broker.example.com", "door_broker_ca_pem": "-----BEGIN CERTIFICATE-----",
		},
	}
	got := []any{
		entriesOf(page.Inputs, "public_url", "device_url", "oidc_issuer", "admin_email", "door_ntp_server", "door_broker_host", "door_broker_ca_pem", "enrollment_password"),
		entriesOf(page.Placeholders, "public_url", "device_url", "oidc_issuer", "trusted_proxies", "oidc_default_groups", "door_ntp_server", "door_broker_host", "door_broker_ca_pem"),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, placeholders (-want +got):\n%s", diff)
	}
}

func TestTheSettingsPageBoundsEachNumberAndSaysItsRangeAndUnit(t *testing.T) {
	t.Parallel()
	page := everySettingsTab(signedInAdmin(t, testsupport.NewFactory()))

	want := []any{
		map[string]string{"update_window_minutes": "1 to 1440 by 1", "door_broker_port": "1 to 65535 by 1", "door_wifi_roam_rssi": "-100 to 0 by 1", "password_min_length": "8 to 128 by 1"},
		map[string]string{"update_window_minutes": "1 to 1440 minutes", "door_broker_port": "1 to 65535", "door_wifi_roam_rssi": "-100 to 0 dBm", "password_min_length": "8 to 128 characters"},
	}
	keys := []string{"update_window_minutes", "door_broker_port", "door_wifi_roam_rssi", "password_min_length"}
	if diff := cmp.Diff(want, []any{entriesOf(page.Limits, keys...), entriesOf(page.Descriptions, keys...)}); diff != "" {
		t.Errorf("limits, descriptions (-want +got):\n%s", diff)
	}
}

func TestARefusedNumberKeepsWhatWasTypedAndSaysWhatIsWrongBesideItsRange(t *testing.T) {
	t.Parallel()
	b := signedInAdmin(t, testsupport.NewFactory())
	b.Get("/")

	got := testsupport.ParsePage(b.Post("/admin/settings", "update_window_minutes", "0").Body)

	want := []any{"0", "Must be a whole number of minutes from 1 to 1440.", "1 to 1440 minutes Must be a whole number of minutes from 1 to 1440."}
	if diff := cmp.Diff(want, []any{got.Values["update_window_minutes"], got.FieldErrors["update_window_minutes"], got.Descriptions["update_window_minutes"]}); diff != "" {
		t.Errorf("value, field error, description (-want +got):\n%s", diff)
	}
}

func TestTheUpdateDaysAreAskedForWithABoxForEachWeekday(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		form    []string
		stored  string
		checked []string
	}{
		"some days ticked": {[]string{"update_window_days", "", "update_window_days", "sat", "update_window_days", "sun"}, "sat,sun",
			[]string{"update_window_days=sat", "update_window_days=sun"}},
		"every day ticked": {[]string{"update_window_days", "", "update_window_days", "mon", "update_window_days", "tue", "update_window_days", "wed",
			"update_window_days", "thu", "update_window_days", "fri", "update_window_days", "sat", "update_window_days", "sun"}, "daily",
			[]string{"update_window_days=mon", "update_window_days=tue", "update_window_days=wed", "update_window_days=thu", "update_window_days=fri",
				"update_window_days=sat", "update_window_days=sun"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			f.StoreStub.SeedSetting("update_window_days", "wed", testsupport.DefaultNow)
			b := signedInAdmin(t, f)
			b.Get("/")

			saved := b.Post("/admin/settings", c.form...)
			page := testsupport.ParsePage(b.Get("/admin/settings?tab=doors").Body)

			want := []any{303, c.stored, append(c.checked, "door_broker_tls=true"), "checkbox"}
			if diff := cmp.Diff(want, []any{saved.Status, f.StoreStub.StoredSetting("update_window_days"), page.Checked, page.Inputs["update_window_days"]}); diff != "" {
				t.Errorf("status, stored, ticked, input (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdateDaysWithNoBoxTickedAreRefusedBesideTheBoxes(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")

	got := b.Post("/admin/settings", "update_window_days", "")

	page := testsupport.ParsePage(got.Body)
	want := []any{422, "Must be daily or weekdays separated by commas, such as sat,sun.", []string{"door_broker_tls=true"}, ""}
	if diff := cmp.Diff(want, []any{got.Status, page.FieldErrors["update_window_days"], page.Checked, f.StoreStub.StoredSetting("update_window_days")}); diff != "" {
		t.Errorf("status, field error, ticked, stored (-want +got):\n%s", diff)
	}
}

func TestASecretLeftBlankOnTheSettingsPageKeepsItsValue(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	b := signedInAdmin(t, f)
	b.Get("/")
	send(t, b, "PUT", "/api/v1/settings/oidc_client_secret", `{"value":"s3cret"}`, 200)
	before := f.StoreStub.StoredSetting("oidc_client_secret")

	saved := b.Post("/admin/settings", "oidc_client_secret", "", "site_name", "North Campus")
	page := testsupport.ParsePage(b.Get("/admin/settings?tab=sign-in").Body)

	want := []any{303, before, storedSecret, ""}
	got := []any{saved.Status, f.StoreStub.StoredSetting("oidc_client_secret"), page.Descriptions["oidc_client_secret"], page.Values["oidc_client_secret"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("status, stored, description, value (-want +got):\n%s", diff)
	}
}

func TestAYesOrNoTheEnvironmentGivesIsShownAsOnOrOff(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ value, shown string }{
		"true":  {"true", "On"},
		"false": {"false", "Off"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			f.EnvStub["DOORCODE_PORTAL_DOOR_PICTURES"] = c.value
			b := signedInAdmin(t, f)

			rows := pageOf(b.Get("/admin/settings?tab=look")).Tables["User portal"]

			want := []string{c.shown + " Set by DOORCODE_PORTAL_DOOR_PICTURES in the environment; change it there.", "environment, read-only"}
			if diff := cmp.Diff(want, rows[1][1:]); diff != "" {
				t.Errorf("value, source (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheCertificatesASettingHoldsAreListedWithWhetherTheyAreValid(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ now, state string }{
		"before it is valid": {"2026-01-05T09:00:00Z", ", not yet valid"},
		"while it is valid":  {"2027-01-05T09:00:00Z", ""},
		"after it expires":   {"2127-01-05T09:00:00Z", ", expired"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			f.ClockStub.Set(c.now)
			b := signedInAdmin(t, f)
			b.Get("/")
			send(t, b, "PUT", "/api/v1/settings/door_broker_ca_pem", `{"value":`+testsupport.Quoted(testsupport.Certificate+testsupport.Certificate)+`}`, 200)

			page := testsupport.ParsePage(b.Get("/admin/settings?tab=doors").Body)

			listed := "CN=Example CA, valid 9 Nov 2026 to 16 Oct 2126 (UTC)" + c.state
			want := []any{[]string{listed, listed}, "Holds 2 certificates: " + listed + " " + listed}
			if diff := cmp.Diff(want, []any{page.Lists["Certificates of setting-door_broker_ca_pem"], page.Descriptions["door_broker_ca_pem"]}); diff != "" {
				t.Errorf("certificates, description (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageAsksForAYesOrNoAsTheDefaultOnOrOff(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		posted    string
		overrides map[string]any
		options   []string
	}{
		"on":          {"true", map[string]any{"wifi_11b_disabled": "true"}, []string{"Default (Off)", "On (selected)", "Off"}},
		"off":         {"false", map[string]any{"wifi_11b_disabled": "false"}, []string{"Default (Off)", "On", "Off (selected)"}},
		"the default": {"", map[string]any{}, []string{"Default (Off) (selected)", "On", "Off"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
			send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"wifi_11b_disabled":"true"}}`, 200)
			b.Get("/")

			saved := b.Post("/admin/doors/1/config", "wifi_11b_disabled", c.posted)
			page := testsupport.ParsePage(b.Get("/admin/doors/1?tab=configuration").Body)

			want := []any{303, c.overrides, c.options, "select"}
			got := []any{saved.Status, testsupport.JSONAt(b.Get("/api/v1/doors/1/config").Body, "/overrides"), page.Options["wifi_11b_disabled"], page.Inputs["wifi_11b_disabled"]}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("status, overrides, options, input (-want +got):\n%s", diff)
			}
		})
	}
}

func TestADoorsPageKeepsAnOverrideNoChoiceOffersSelected(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
	b.Get("/")

	page := testsupport.ParsePage(b.Post("/admin/doors/1/config", "log_level", "loud").Body)

	want := []string{"Default (info)", "loud (selected)", "error", "warn", "info", "debug"}
	if diff := cmp.Diff(want, page.Options["log_level"]); diff != "" {
		t.Errorf("options (-want +got):\n%s", diff)
	}
}

func TestADoorsPageBoundsEachNumberAndShowsTheDefaultInIt(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())

	page := testsupport.ParsePage(b.Get("/admin/doors/1?tab=configuration").Body)

	keys := []string{"keepalive_s", "broker_port"}
	want := []any{
		map[string]string{"keepalive_s": "number", "broker_port": "number"},
		map[string]string{"keepalive_s": "10 to 300 by 1", "broker_port": "1 to 65535 by 1"},
		map[string]string{"keepalive_s": "30", "broker_port": "8883"},
		map[string]string{"keepalive_s": "10 to 300 seconds", "broker_port": "1 to 65535"},
		defaultSecret,
	}
	got := []any{entriesOf(page.Inputs, keys...), entriesOf(page.Limits, keys...), entriesOf(page.Placeholders, keys...), entriesOf(page.Descriptions, keys...),
		page.Descriptions["wifi_password"]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("inputs, limits, placeholders, descriptions, Wi-Fi password description (-want +got):\n%s", diff)
	}
}

func TestADoorsPageListsTheCertificatesItsBrokerIsCheckedAgainst(t *testing.T) {
	t.Parallel()
	_, b, _, _ := enrolledDoor(t, testsupport.NewFactory())
	send(t, b, "PUT", "/api/v1/doors/1/config", `{"overrides":{"broker_ca_pem":`+testsupport.Quoted(testsupport.Certificate)+`}}`, 200)

	page := testsupport.ParsePage(b.Get("/admin/doors/1?tab=configuration").Body)

	listed := "CN=Example CA, valid 9 Nov 2026 to 16 Oct 2126 (UTC), not yet valid"
	want := []any{[]string{listed}, "Holds 1 certificate: " + listed}
	if diff := cmp.Diff(want, []any{page.Lists["Certificates of config-broker_ca_pem"], page.Descriptions["broker_ca_pem"]}); diff != "" {
		t.Errorf("certificates, description (-want +got):\n%s", diff)
	}
}
