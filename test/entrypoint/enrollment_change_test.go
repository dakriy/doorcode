package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/testsupport"
)

// enrollingWith reports what the broker answers a controller connecting
// with username and password, as an error's text.
func enrollingWith(f *testsupport.Factory, username, password string) string {
	_, err := f.BrokerStub.Connect("Z9", username, password, "", nil)
	return testsupport.ErrorText(err)
}

func TestChangingTheEnrollmentPasswordLetsOnlyTheNewOneEnrollAtOnce(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := brokerSite(t, f)
	delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")

	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"rotated"}`, 200)

	got := []string{enrollingWith(f, enrollUser, enrollPassword), enrollingWith(f, enrollUser, "rotated")}
	if diff := cmp.Diff([]string{"stub: not authorized", "<nil>"}, got); diff != "" {
		t.Errorf("old password, new password (-want +got):\n%s", diff)
	}
}

func TestChangingTheEnrollmentPasswordSendsEveryDoorTheNewCredentialsInItsConfiguration(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := pushSite(t, f)
	delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")
	sent := len(f.BrokerStub.Published)

	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"rotated"}`, 200)

	want := []any{topicsOf("config", "3", "2", "1"), configWith(map[string]any{"enrollment_password": "rotated"})}
	got := []any{f.BrokerStub.Topics(sent, "config"), f.BrokerStub.RetainedDecoded(doorTopic("2", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("configurations sent, Dock's retained configuration (-want +got):\n%s", diff)
	}
}

func TestChangingTheEnrollmentUsernameReplacesTheEnrollmentAccount(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := brokerSite(t, f)

	send(t, b, "PUT", "/api/v1/settings/enrollment_username", `{"value":"enroll-2"}`, 200)

	want := []testsupport.BrokerClient{{
		Username: "enroll-2", Password: enrollPassword,
		Publish: []string{"doorcode/enroll/%c"}, Subscribe: []string{"doorcode/enroll/%c/credentials"},
	}}
	if diff := cmp.Diff(want, f.BrokerStub.Clients()); diff != "" {
		t.Errorf("clients (-want +got):\n%s", diff)
	}
}

func TestAnOldEnrollmentAccountTheBrokerKeepsIsLogged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := brokerSite(t, f)
	f.BrokerStub.FailDeleting()

	send(t, b, "PUT", "/api/v1/settings/enrollment_username", `{"value":"enroll-2"}`, 200)

	want := testsupport.LogEntry{Level: "error", Message: "could not change the enrollment credentials",
		Fields: map[string]string{"error": "remove the enrollment client doorcode-enroll: stub: unavailable"}}
	got := []any{lastLog(f), enrollingWith(f, "enroll-2", enrollPassword)}
	if diff := cmp.Diff([]any{want, "<nil>"}, got); diff != "" {
		t.Errorf("log, enrolling with the new username (-want +got):\n%s", diff)
	}
}

func TestClearingTheEnrollmentPasswordGeneratesANewOneTheBrokerAccepts(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	_, b := brokerSite(t, f)
	delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")
	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"rotated"}`, 200)

	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"clear":true}`, 200)

	generated, err := testsupport.OpenSecret("enrollment_password", f.StoreStub.StoredSetting("enrollment_password"))
	logged := lastLog(f)
	want := []any{"<nil>", "generated the enrollment password; controllers enroll with these credentials, and the enrollment_password setting changes the password",
		generated, "<nil>", "stub: not authorized"}
	got := []any{testsupport.ErrorText(err), logged.Message, logged.Fields["password"],
		enrollingWith(f, enrollUser, generated), enrollingWith(f, enrollUser, "rotated")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stored, log, password logged, generated password, cleared password (-want +got):\n%s", diff)
	}
}

func TestAnEnrollmentChangeTheBrokerRefusedTakesEffectWithTheNextPeriodicWorkAndReachesTheDoorsThen(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, b, _, _ := enrolledDoor(t, f)
	delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")
	f.BrokerStub.FailCreating()
	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"rotated"}`, 200)
	refused := []any{lastLog(f).Fields["error"], enrollingWith(f, enrollUser, enrollPassword),
		f.BrokerStub.RetainedDecoded(doorTopic("1", "config"))}
	f.BrokerStub.Recover()

	f.ClockStub.Set("2026-01-05T09:01:00Z")
	run(t, a)

	want := []any{
		[]any{"create the enrollment client: stub: unavailable", "stub: not authorized", configWith(nil)},
		"stub: not authorized", "<nil>", configWith(map[string]any{"enrollment_password": "rotated"}),
	}
	got := []any{refused, enrollingWith(f, enrollUser, enrollPassword), enrollingWith(f, enrollUser, "rotated"),
		f.BrokerStub.RetainedDecoded(doorTopic("1", "config"))}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("refused change, then old password, new password, Lobby's configuration (-want +got):\n%s", diff)
	}
}

func TestTheProvisioningFileCarriesTheEnrollmentCredentialsAsChanged(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	h, b := provisioningSite(t, f)
	delete(f.EnvStub, "DOORCODE_ENROLLMENT_PASSWORD")
	send(t, b, "PUT", "/api/v1/settings/enrollment_password", `{"value":"rotated"}`, 200)
	secret := createToken(t, b, `{"name":"bench"}`)

	got := tokenClient(h, secret).Get("/api/v1/firmware/provisioning")

	want := map[string]any{"username": enrollUser, "password": "rotated"}
	if diff := cmp.Diff(want, testsupport.JSONAt(got.Body, "/enrollment")); diff != "" {
		t.Errorf("enrollment (-want +got):\n%s", diff)
	}
}
