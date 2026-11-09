//go:build slow

package connector_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dakriy/doorcode/internal/connector"
)

// doorClient creates a client named after s's prefix, allowed to publish
// and subscribe below prefix door/, removed when the test ends, and
// returns its username and password.
func doorClient(t *testing.T, s brokerSession) (string, string) {
	t.Helper()
	username := "test-" + unique()
	topics := []string{s.prefix + "door/#"}
	err := s.app.CreateClient(context.Background(), connector.BrokerClient{Username: username, Password: "secret", Publish: topics, Subscribe: topics})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.app.DeleteClient(context.Background(), username) })
	return username, "secret"
}

// connectDoor connects as a new door client of s.
func connectDoor(t *testing.T, s brokerSession) peer {
	t.Helper()
	username, password := doorClient(t, s)
	p, err := s.connect(t, username, password)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBrokerReportsTheApplicationConnected(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)

			if !s.app.Connected() {
				t.Error("Connected reported false on an open connection")
			}
		})
	}
}

func TestBrokerDeliversWhatAClientPublishesOnItsTopics(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			next := s.inbound(t, []string{s.prefix + "door/+"})
			door := connectDoor(t, s)

			door.Publish(t, s.prefix+"door/online", []byte{0xa1, 0x61, 0x76, 0x01}, false)

			want := connector.Message{Topic: s.prefix + "door/online", Payload: []byte{0xa1, 0x61, 0x76, 0x01}}
			if diff := cmp.Diff(want, next(t)); diff != "" {
				t.Errorf("message (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBrokerDropsWhatAClientPublishesOutsideItsTopics(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			next := s.inbound(t, []string{s.prefix + "#"})
			door := connectDoor(t, s)

			door.Publish(t, s.prefix+"other/online", []byte("theirs"), false)
			door.Publish(t, s.prefix+"door/online", []byte("mine"), false)

			if diff := cmp.Diff(s.prefix+"door/online", next(t).Topic); diff != "" {
				t.Errorf("first topic delivered (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBrokerDeliversWhatTheApplicationPublishesToASubscribedClient(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			door := connectDoor(t, s)
			if err := door.Subscribe(t, s.prefix+"door/command"); err != nil {
				t.Fatal(err)
			}

			err := s.app.Publish(context.Background(), connector.Message{Topic: s.prefix + "door/command", Payload: []byte("open")})
			if err != nil {
				t.Fatal(err)
			}

			want := connector.Message{Topic: s.prefix + "door/command", Payload: []byte("open")}
			if diff := cmp.Diff(want, door.Next(t)); diff != "" {
				t.Errorf("message (-want +got):\n%s", diff)
			}
		})
	}
}

// publishRetained publishes payload retained on topic as the application,
// and clears it when the test ends.
func publishRetained(t *testing.T, s brokerSession, topic string, payload []byte) {
	t.Helper()
	ctx := context.Background()
	if err := s.app.Publish(ctx, connector.Message{Topic: topic, Payload: payload, Retained: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.app.Publish(ctx, connector.Message{Topic: topic, Retained: true}) })
}

func TestBrokerGivesALaterSubscriberTheRetainedMessage(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			door := connectDoor(t, s)
			publishRetained(t, s, s.prefix+"door/config", []byte("v1"))

			if err := door.Subscribe(t, s.prefix+"door/config"); err != nil {
				t.Fatal(err)
			}

			want := connector.Message{Topic: s.prefix + "door/config", Payload: []byte("v1"), Retained: true}
			if diff := cmp.Diff(want, door.Next(t)); diff != "" {
				t.Errorf("message (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBrokerGivesTheApplicationRetainedMessagesWhenItSubscribes(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			publishRetained(t, s, s.prefix+"door/online", []byte("online"))

			next := s.inbound(t, []string{s.prefix + "door/+"})

			want := connector.Message{Topic: s.prefix + "door/online", Payload: []byte("online"), Retained: true}
			if diff := cmp.Diff(want, next(t)); diff != "" {
				t.Errorf("message (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBrokerRefusesASubscriptionOutsideTheClientsTopics(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			door := connectDoor(t, s)

			if err := door.Subscribe(t, s.prefix+"other/command"); err == nil {
				t.Error("the broker accepted the subscription")
			}
		})
	}
}

func TestBrokerRefusesAWrongPassword(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			username, _ := doorClient(t, s)

			if _, err := s.connect(t, username, "wrong"); err == nil {
				t.Error("the broker accepted the connection")
			}
		})
	}
}

func TestBrokerRefusesToCreateAClientTwice(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			username, _ := doorClient(t, s)

			err := s.app.CreateClient(context.Background(), connector.BrokerClient{Username: username, Password: "other"})

			if err == nil {
				t.Error("the broker created the client again")
			}
		})
	}
}

func TestBrokerDisconnectsADeletedClientAndRefusesItAfter(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			username, password := doorClient(t, s)
			door, err := s.connect(t, username, password)
			if err != nil {
				t.Fatal(err)
			}

			if err := s.app.DeleteClient(context.Background(), username); err != nil {
				t.Fatal(err)
			}
			_, again := s.connect(t, username, password)

			if diff := cmp.Diff([]bool{true, true}, []bool{door.Lost(t), again != nil}); diff != "" {
				t.Errorf("disconnected, refused after (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBrokerRefusesToDeleteAClientItDoesNotHave(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)

			if err := s.app.DeleteClient(context.Background(), "test-"+unique()); err == nil {
				t.Error("the broker deleted a client it does not have")
			}
		})
	}
}

// sharedClient creates a client of s allowed to publish to prefix
// enroll/%c and to subscribe to prefix enroll/%c/credentials, removed when
// the test ends, and returns its username; its password is secret.
func sharedClient(t *testing.T, s brokerSession) string {
	t.Helper()
	ctx := context.Background()
	username := "test-" + unique()
	err := s.app.CreateClient(ctx, connector.BrokerClient{
		Username: username, Password: "secret", Publish: []string{s.prefix + "enroll/%c"}, Subscribe: []string{s.prefix + "enroll/%c/credentials"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.app.DeleteClient(ctx, username) })
	return username
}

// publishCredentials publishes, as the application, the credentials of
// each of hwids on its credentials topic below s's prefix.
func publishCredentials(t *testing.T, s brokerSession, hwids ...string) {
	t.Helper()
	for _, hwid := range hwids {
		m := connector.Message{Topic: s.prefix + "enroll/" + hwid + "/credentials", Payload: []byte("for " + hwid)}
		if err := s.app.Publish(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrokerConfinesEachConnectionOfASharedClientToTopicsNamingItsClientID(t *testing.T) {
	for _, impl := range brokerImpls {
		t.Run(impl.name, func(t *testing.T) {
			s := impl.open(t)
			username := sharedClient(t, s)
			next := s.inbound(t, []string{s.prefix + "enroll/+"})
			mine, theirs := "A1-"+unique(), "B2-"+unique()
			controller, err := s.connectAs(t, mine, username, "secret")
			if err != nil {
				t.Fatal(err)
			}

			refusedTheirs := controller.Subscribe(t, s.prefix+"enroll/"+theirs+"/credentials") != nil
			refusedEvery := controller.Subscribe(t, s.prefix+"enroll/+/credentials") != nil
			acceptedMine := controller.Subscribe(t, s.prefix+"enroll/"+mine+"/credentials") == nil
			controller.Publish(t, s.prefix+"enroll/"+theirs, []byte("as them"), false)
			controller.Publish(t, s.prefix+"enroll/"+mine, []byte("as me"), false)
			publishCredentials(t, s, theirs, mine)

			want := []any{true, true, true, s.prefix + "enroll/" + mine, "for " + mine}
			got := []any{refusedTheirs, refusedEvery, acceptedMine, next(t).Topic, string(controller.Next(t).Payload)}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("refused theirs, refused every, accepted mine, announcement delivered, credentials received (-want +got):\n%s", diff)
			}
		})
	}
}
