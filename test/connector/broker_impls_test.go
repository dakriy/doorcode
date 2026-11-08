package connector_test

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/dakriy/doorcode/internal/connector"
	"github.com/dakriy/doorcode/internal/driver/cryptoentropy"
	"github.com/dakriy/doorcode/internal/driver/dynsec"
	"github.com/dakriy/doorcode/internal/testsupport"
)

// testBrokerEnv names the variable holding the URL, with the
// administrator's credentials, of a disposable local broker with the
// dynamic-security plugin, such as the one docker-compose.yml starts.
const testBrokerEnv = "DOORCODE_TEST_BROKER_URL"

// brokerWait is how long a test waits for the broker to deliver.
const brokerWait = 5 * time.Second

// peer is a client connected to a broker as a door controller would be.
type peer interface {
	// Publish sends a message at QoS 1.
	Publish(t *testing.T, topic string, payload []byte, retained bool)
	// Subscribe subscribes to filter, reporting a refusal.
	Subscribe(t *testing.T, filter string) error
	// Next is the next message the broker delivered to the peer.
	Next(t *testing.T) connector.Message
	// Lost reports whether the broker closed the connection.
	Lost(t *testing.T) bool
	Close()
}

// brokerSession is a broker the application is connected to, with a
// prefix for this test's topics and usernames so tests sharing a broker
// do not meet, and the ways a test reaches it.
type brokerSession struct {
	app    connector.Broker
	prefix string
	// inbound subscribes the application to filters and returns what
	// gives the next message delivered on them.
	inbound func(t *testing.T, filters []string) func(t *testing.T) connector.Message
	// connect connects as a controller with a client id of its own, and
	// connectAs with clientID.
	connect   func(t *testing.T, username, password string) (peer, error)
	connectAs func(t *testing.T, clientID, username, password string) (peer, error)
}

type brokerImpl struct {
	name string
	open func(t *testing.T) brokerSession
}

var brokerImpls = []brokerImpl{
	{name: "stub", open: openStubBroker},
	{name: "dynsec", open: openBroker},
}

func openStubBroker(*testing.T) brokerSession {
	b := testsupport.NewBroker()
	s := brokerSession{
		app:    b,
		prefix: "doorcode/test/",
		// The stub hands the application what was delivered since it last
		// subscribed, so asking for more subscribes again.
		inbound: func(t *testing.T, filters []string) func(t *testing.T) connector.Message {
			var held []connector.Message
			take := func() {
				ch, err := b.Subscribe(context.Background(), filters)
				if err != nil {
					t.Fatal(err)
				}
				for m := range ch {
					held = append(held, m)
				}
			}
			take()
			return func(t *testing.T) connector.Message {
				if len(held) == 0 {
					take()
				}
				if len(held) == 0 {
					t.Fatal("the broker delivered nothing to the application")
				}
				m := held[0]
				held = held[1:]
				return m
			}
		},
	}
	s.connectAs = func(_ *testing.T, clientID, username, password string) (peer, error) {
		conn, err := b.Connect(clientID, username, password, "", nil)
		if err != nil {
			return nil, err
		}
		return &stubPeer{conn: conn}, nil
	}
	return withConnect(s)
}

// withConnect is s connecting as a controller with a client id of its own
// through its connectAs.
func withConnect(s brokerSession) brokerSession {
	s.connect = func(t *testing.T, username, password string) (peer, error) {
		return s.connectAs(t, "door-"+unique(), username, password)
	}
	return s
}

type stubPeer struct {
	conn *testsupport.Connection
	read int
}

func (p *stubPeer) Publish(t *testing.T, topic string, payload []byte, retained bool) {
	if err := p.conn.Publish(topic, payload, retained); err != nil {
		t.Fatal(err)
	}
}

func (p *stubPeer) Subscribe(_ *testing.T, filter string) error { return p.conn.Subscribe(filter) }

func (p *stubPeer) Next(t *testing.T) connector.Message {
	received := p.conn.Received()
	if p.read >= len(received) {
		t.Fatal("the broker delivered nothing to the controller")
	}
	p.read++
	return received[p.read-1]
}

func (p *stubPeer) Lost(*testing.T) bool { return !p.conn.Connected() }

func (p *stubPeer) Close() { p.conn.Close() }

// unique is a random name part, so tests sharing a broker do not meet.
func unique() string {
	return hex.EncodeToString(cryptoentropy.Source{}.Bytes(6))
}

// brokerURL is the test broker's URL, failing the test without one.
func brokerURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv(testBrokerEnv)
	if raw == "" {
		t.Fatalf("%s must name a disposable broker with its administrator's credentials (see AGENTS.md)", testBrokerEnv)
	}
	return raw
}

func openBroker(t *testing.T) brokerSession {
	t.Helper()
	raw := brokerURL(t)
	b, err := dynsec.Open(context.Background(), raw, "doorcode-test-"+unique(), brokerWait)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return withConnect(brokerSession{
		app:    b,
		prefix: "doorcode/test-" + unique() + "/",
		inbound: func(t *testing.T, filters []string) func(t *testing.T) connector.Message {
			ch, err := b.Subscribe(context.Background(), filters)
			if err != nil {
				t.Fatal(err)
			}
			return func(t *testing.T) connector.Message {
				select {
				case m := <-ch:
					return m
				case <-time.After(brokerWait):
					t.Fatal("the broker delivered nothing to the application")
					return connector.Message{}
				}
			}
		},
		connectAs: func(t *testing.T, clientID, username, password string) (peer, error) {
			return connectPeer(t, raw, clientID, username, password)
		},
	})
}

// mqttPeer is a controller connected to the test broker.
type mqttPeer struct {
	client   mqtt.Client
	received chan connector.Message
	lost     chan struct{}
}

func connectPeer(t *testing.T, raw, clientID, username, password string) (peer, error) {
	t.Helper()
	p := &mqttPeer{received: make(chan connector.Message, 64), lost: make(chan struct{})}
	u := mustParseURL(t, raw)
	opts := mqtt.NewClientOptions().AddBroker(u.Scheme + "://" + u.Host).SetClientID(clientID).
		SetUsername(username).SetPassword(password).SetAutoReconnect(false).
		SetDefaultPublishHandler(func(_ mqtt.Client, m mqtt.Message) {
			p.received <- connector.Message{Topic: m.Topic(), Payload: m.Payload(), Retained: m.Retained()}
		}).
		SetConnectionLostHandler(func(mqtt.Client, error) { close(p.lost) })
	p.client = mqtt.NewClient(opts)
	if err := waitFor(t, p.client.Connect()); err != nil {
		return nil, err
	}
	t.Cleanup(p.Close)
	return p, nil
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func waitFor(t *testing.T, tok mqtt.Token) error {
	t.Helper()
	if !tok.WaitTimeout(brokerWait) {
		t.Fatal("the broker did not answer")
	}
	return tok.Error()
}

func (p *mqttPeer) Publish(t *testing.T, topic string, payload []byte, retained bool) {
	if err := waitFor(t, p.client.Publish(topic, 1, retained, payload)); err != nil {
		t.Fatal(err)
	}
}

// errSubscriptionRefused is how a peer reports the broker refusing a
// subscription, which it answers with return code 0x80.
var errSubscriptionRefused = errors.New("subscription refused")

func (p *mqttPeer) Subscribe(t *testing.T, filter string) error {
	tok := p.client.Subscribe(filter, 1, nil)
	if err := waitFor(t, tok); err != nil {
		return err
	}
	if tok.(*mqtt.SubscribeToken).Result()[filter] == 0x80 {
		return errSubscriptionRefused
	}
	return nil
}

func (p *mqttPeer) Next(t *testing.T) connector.Message {
	select {
	case m := <-p.received:
		return m
	case <-time.After(brokerWait):
		t.Fatal("the broker delivered nothing to the controller")
		return connector.Message{}
	}
}

func (p *mqttPeer) Lost(*testing.T) bool {
	select {
	case <-p.lost:
		return true
	case <-time.After(brokerWait):
		return false
	}
}

func (p *mqttPeer) Close() {
	if p.client.IsConnected() {
		p.client.Disconnect(100)
	}
}
