package entrypoint_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/app"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// checkAs is a code check the controller of a door publishes as request
// requestID.
func checkAs(requestID string) []byte {
	return testsupport.CBOR(map[string]any{"v": 1, "request_id": requestID, "digits": "999999"})
}

// runWhile runs a and does during while it handles what the broker holds,
// then waits for Run to return.
func runWhile(t *testing.T, a *app.App, during func()) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- a.Run(t.Context()) }()
	during()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// answered is the topic and request id of each answer to a code check f's
// broker carried after its first since, in order.
func answered(f *testsupport.Factory, since int) [][]any {
	out := [][]any{}
	for _, m := range f.BrokerStub.Published[since:] {
		reply, _ := testsupport.Decoded(m.Payload).(map[string]any)
		if m.Topic == doorTopic("1", "reply") || m.Topic == doorTopic("2", "reply") {
			out = append(out, []any{m.Topic, reply["request_id"]})
		}
	}
	return out
}

func TestACheckAtAnotherDoorIsAnsweredWhileOneDoorsCheckIsInProgress(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _ := pushSite(t, f)
	sent := len(f.BrokerStub.Published)
	held := f.StoreStub.HoldDoor(1)
	f.BrokerStub.Inject(doorTopic("1", "check"), checkAs("lobby"))
	f.BrokerStub.Inject(doorTopic("2", "check"), checkAs("dock"))

	var dockFirst bool
	runWhile(t, a, func() {
		dockFirst = f.BrokerStub.Await(doorTopic("2", "reply"))
		held.Release()
	})

	want := []any{true, [][]any{{doorTopic("2", "reply"), "dock"}, {doorTopic("1", "reply"), "lobby"}}}
	if diff := cmp.Diff(want, []any{dockFirst, answered(f, sent)}); diff != "" {
		t.Errorf("Dock answered while Lobby's check waited, answers (-want +got):\n%s", diff)
	}
}

func TestADoorsMessagesAreHandledInTheOrderItSentThemWhileOneWaits(t *testing.T) {
	t.Parallel()
	f := testsupport.NewFactory()
	a, _ := pushSite(t, f)
	sent := len(f.BrokerStub.Published)
	held := f.StoreStub.HoldDoor(1)
	for _, id := range []string{"r1", "r2", "r3"} {
		f.BrokerStub.Inject(doorTopic("1", "check"), checkAs(id))
	}
	f.BrokerStub.Inject(doorTopic("2", "check"), checkAs("dock"))

	runWhile(t, a, func() {
		f.BrokerStub.Await(doorTopic("2", "reply"))
		held.Release()
	})

	want := [][]any{
		{doorTopic("2", "reply"), "dock"},
		{doorTopic("1", "reply"), "r1"}, {doorTopic("1", "reply"), "r2"}, {doorTopic("1", "reply"), "r3"},
	}
	if diff := cmp.Diff(want, answered(f, sent)); diff != "" {
		t.Errorf("answers (-want +got):\n%s", diff)
	}
}
