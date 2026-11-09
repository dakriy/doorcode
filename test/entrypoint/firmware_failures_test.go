package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// fleetFailure is a failure a fleet site's store, broker, or settings are
// made to have.
type fleetFailure func(fl *fleet)

// The failures the firmware tests give a fleet site.
var (
	firmwareFails       fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailFirmware() }
	updatesFail         fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailUpdates() }
	firmwareWritesFail  fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailFirmwareWrites() }
	doorsFail           fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailDoors() }
	doorWritesFail      fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailDoorWrites() }
	settingsFail        fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailSettings() }
	settingWritesFail   fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailSettingWrites() }
	eventWritesFail     fleetFailure = func(fl *fleet) { fl.f.StoreStub.FailEventWrites() }
	publishingFails     fleetFailure = func(fl *fleet) { fl.f.BrokerStub.FailPublishing() }
	addressUnreadable   fleetFailure = func(fl *fleet) { fl.f.EnvStub["DOORCODE_PUBLIC_URL"] = "ftp://doorcode.example.com" }
	timezoneUnreadable  fleetFailure = func(fl *fleet) { fl.f.EnvStub["DOORCODE_SITE_TIMEZONE"] = "Mars/Olympus" }
	unusedFirmwareAdded fleetFailure = func(fl *fleet) { uploadFirmware(fl.t, fl.b, "1.6.0") }
	updateSentToLobby   fleetFailure = func(fl *fleet) { send(fl.t, fl.b, "POST", "/api/v1/doors/1/update", "", 200) }
)

// both is a then b.
func both(a, b fleetFailure) fleetFailure {
	return func(fl *fleet) {
		a(fl)
		b(fl)
	}
}

// fleetRequest is a request a test sends on a fleet site with b.
type fleetRequest func(fl *fleet, b *testsupport.Browser) testsupport.Response

// call is the request with method to target carrying body.
func call(method, target, body string) fleetRequest {
	return func(_ *fleet, b *testsupport.Browser) testsupport.Response { return b.Send(method, target, body) }
}

// uploading uploads version 2.0.0 through the API.
func uploading(_ *fleet, b *testsupport.Browser) testsupport.Response {
	return b.Upload("/api/v1/firmware", "image", "a.bin", imageOf("2.0.0"), "version", "2.0.0")
}

// downloading requests the URL of the last command Lobby's controller
// received.
func downloading(fl *fleet, b *testsupport.Browser) testsupport.Response {
	return b.Get(lastURL(fl, 1))
}

func TestFirmwareOperationsAnswerAFailureWithARequestID(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail    fleetFailure
		request fleetRequest
	}{
		"listing, when firmware cannot be read":             {firmwareFails, call("GET", "/api/v1/firmware", "")},
		"reading one, when firmware cannot be read":         {firmwareFails, call("GET", "/api/v1/firmware/1", "")},
		"reading one, when doors cannot be read":            {doorsFail, call("GET", "/api/v1/firmware/1", "")},
		"deleting, when firmware cannot be read":            {firmwareFails, call("DELETE", "/api/v1/firmware/1", "")},
		"deleting, when doors cannot be read":               {doorsFail, call("DELETE", "/api/v1/firmware/1", "")},
		"deleting, when updates cannot be read":             {updatesFail, call("DELETE", "/api/v1/firmware/1", "")},
		"deleting, when it cannot be removed":               {both(unusedFirmwareAdded, firmwareWritesFail), call("DELETE", "/api/v1/firmware/2", "")},
		"the rollout, when firmware cannot be read":         {firmwareFails, call("GET", "/api/v1/firmware/rollout", "")},
		"the default, when firmware cannot be read":         {firmwareFails, call("PUT", "/api/v1/firmware/default", `{"firmware_id":1}`)},
		"the default, when it cannot be stored":             {firmwareWritesFail, call("PUT", "/api/v1/firmware/default", `{"firmware_id":1}`)},
		"the rollout settings, when they cannot be read":    {settingsFail, call("GET", "/api/v1/firmware/settings", "")},
		"the rollout settings, when they cannot be stored":  {settingWritesFail, call("PUT", "/api/v1/firmware/settings", `{"update_timeout_minutes":"3"}`)},
		"a door's, when doors cannot be read":               {doorsFail, call("GET", "/api/v1/doors/1/firmware", "")},
		"a door's, when firmware cannot be read":            {firmwareFails, call("GET", "/api/v1/doors/1/firmware", "")},
		"a door's choice, when it cannot be stored":         {doorWritesFail, call("PUT", "/api/v1/doors/1/firmware", `{"firmware_id":null}`)},
		"a door's choice, when firmware cannot be read":     {firmwareFails, call("PUT", "/api/v1/doors/1/firmware", `{"firmware_id":1}`)},
		"updating now, when updates cannot be read":         {updatesFail, call("POST", "/api/v1/doors/1/update", "")},
		"updating now, when the settings cannot be parsed":  {addressUnreadable, call("POST", "/api/v1/doors/1/update", "")},
		"updating now, when the command cannot be sent":     {publishingFails, call("POST", "/api/v1/doors/1/update", "")},
		"updating now, when the update cannot be stored":    {firmwareWritesFail, call("POST", "/api/v1/doors/1/update", "")},
		"updating now, when the event cannot be stored":     {eventWritesFail, call("POST", "/api/v1/doors/1/update", "")},
		"downloading, when updates cannot be read":          {updatesFail, call("GET", "/api/v1/doors/1/firmware/1.5.0?token=x", "")},
		"downloading, when firmware cannot be read":         {both(updateSentToLobby, firmwareFails), downloading},
		"uploading, when firmware cannot be read":           {firmwareFails, uploading},
		"uploading, when the upload cannot be stored":       {firmwareWritesFail, uploading},
		"the firmware page, when settings cannot be read":   {settingsFail, call("GET", "/admin/firmware", "")},
		"the firmware page, when settings cannot be parsed": {addressUnreadable, call("GET", "/admin/firmware", "")},
		"the firmware page, when firmware cannot be read":   {firmwareFails, call("GET", "/admin/firmware", "")},
		"a version's page, when the timezone is wrong":      {timezoneUnreadable, call("GET", "/admin/firmware/1", "")},
		"a version's page, when doors cannot be read":       {doorsFail, call("GET", "/admin/firmware/1", "")},
		"a door's page, when firmware cannot be read":       {firmwareFails, call("GET", "/admin/doors/1", "")},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			b := fl.browser()
			c.fail(fl)

			got := c.request(fl, b)

			if diff := cmp.Diff(500, got.Status); diff != "" {
				t.Errorf("status (-want +got):\n%s\n%s", diff, got.Body)
			}
		})
	}
}

func TestFirmwareFormsShowAFailure(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail   fleetFailure
		submit func(b *testsupport.Browser) testsupport.Response
	}{
		"uploading": {firmwareWritesFail, func(b *testsupport.Browser) testsupport.Response {
			return b.Upload("/admin/firmware", "image", "a.bin", imageOf("2.0.0"), "version", "2.0.0")
		}},
		"choosing the default": {firmwareWritesFail, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/firmware/default", "firmware_id", "1")
		}},
		"saving the update window": {settingWritesFail, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/firmware/settings", "repeat", "daily", "update_window_start", "04:00", "update_window_minutes", "120",
				"update_timeout_minutes", "30")
		}},
		"deleting": {both(unusedFirmwareAdded, firmwareWritesFail), func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/firmware/2/delete")
		}},
		"choosing a door's": {doorWritesFail, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/firmware", "firmware_id", "")
		}},
		"updating a door": {publishingFails, func(b *testsupport.Browser) testsupport.Response {
			return b.Post("/admin/doors/1/update")
		}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			b := fl.browser()
			c.fail(fl)

			got := c.submit(b)

			if diff := cmp.Diff([]any{500, "Something went wrong"}, []any{got.Status, pageOf(got).Heading}); diff != "" {
				t.Errorf("status, heading (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheRolloutLogsWhatItCannotDo(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		fail fleetFailure
		at   string
	}{
		"when firmware cannot be read":       {firmwareFails, "2026-01-06T02:00:00Z"},
		"when an update cannot be logged":    {eventWritesFail, "2026-01-06T02:00:00Z"},
		"when a timed out update cannot end": {both(func(fl *fleet) { fl.at("2026-01-06T02:00:00Z") }, firmwareWritesFail), "2026-01-06T02:30:00Z"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			c.fail(fl)
			logged := len(fl.f.LogStub.Entries)

			fl.at(c.at)

			want := []testsupport.LogEntry{{Level: "error", Message: "periodic work failed", Fields: map[string]string{"error": "stub: unavailable"}}}
			if diff := cmp.Diff(want, logsSince(fl.f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTheRolloutLogsSettingsItCannotRead(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ key, value, error string }{
		"the public address": {"public_url", "ftp://doorcode.example.com",
			"invalid value for public_url: must be empty or an https:// or http:// address without a query, such as https://doorcode.example.com"},
		"the weekdays":    {"update_window_days", "mon,someday", "invalid value for update_window_days: must be daily or weekdays separated by commas, such as sat,sun"},
		"the start":       {"update_window_start", "2am", "invalid value for update_window_start: must be a time of day as HH:MM, such as 02:00"},
		"the concurrency": {"update_concurrency", "many", "invalid value for update_concurrency: must be a whole number of doors from 1 to 1000"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := testsupport.NewFactory()
			fl := fleetSite(t, f)
			delete(f.EnvStub, "DOORCODE_PUBLIC_URL")
			delete(f.EnvStub, "DOORCODE_UPDATE_CONCURRENCY")
			f.StoreStub.SeedSetting(c.key, c.value, testsupport.DefaultNow)
			logged := len(f.LogStub.Entries)

			fl.at("2026-01-06T02:00:00Z")

			want := []testsupport.LogEntry{{Level: "error", Message: "periodic work failed", Fields: map[string]string{"error": c.error}}}
			if diff := cmp.Diff(want, logsSince(f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAControllerMessageAboutAnUpdateThatCannotBeHandledIsLogged(t *testing.T) {
	t.Parallel()
	failed := func(fl *fleet) { fl.acks(1, "failed", "") }
	back := func(fl *fleet) { fl.reconnects(1, "1.5.0") }
	unavailable := "stub: unavailable"
	cases := map[string]struct {
		fail   fleetFailure
		answer func(fl *fleet)
		topic  string
		error  string
	}{
		"an answer, when updates cannot be read":            {updatesFail, failed, "doorcode/doors/1/ack", unavailable},
		"an answer, when the failure cannot be logged":      {eventWritesFail, failed, "doorcode/doors/1/ack", unavailable},
		"a reconnection, when updates cannot be read":       {updatesFail, back, "doorcode/doors/1/online", unavailable},
		"a reconnection, when the success cannot be stored": {firmwareWritesFail, back, "doorcode/doors/1/online", unavailable},
		"an answer that is not one": {func(*fleet) {}, func(fl *fleet) {
			fl.publish(1, "ack", testsupport.CBOR(map[string]any{"v": 2}))
		}, "doorcode/doors/1/ack", "unsupported protocol version 2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fl := fleetSite(t, testsupport.NewFactory())
			fl.at("2026-01-06T02:00:00Z")
			c.fail(fl)
			logged := len(fl.f.LogStub.Entries)

			c.answer(fl)

			want := []testsupport.LogEntry{{Level: "error", Message: "ignored a broker message", Fields: map[string]string{"topic": c.topic, "error": c.error}}}
			if diff := cmp.Diff(want, logsSince(fl.f, logged)); diff != "" {
				t.Errorf("log (-want +got):\n%s", diff)
			}
		})
	}
}
