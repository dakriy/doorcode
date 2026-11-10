package testsupport

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/dakriy/doorcode/internal/connector"
)

var (
	// ErrRefused is what the stub broker answers a connection with
	// credentials it does not accept.
	ErrRefused = errors.New("stub: not authorized")
	// ErrNoSuchClient is what the stub broker reports for deleting a
	// client it does not have, as the dynamic-security plugin does.
	ErrNoSuchClient = errors.New("stub: client not found")
	// ErrDenied is what the stub broker answers a subscription the
	// client's topics do not allow.
	ErrDenied = errors.New("stub: subscription denied")
	// ErrDisconnected is what a closed connection reports.
	ErrDisconnected = errors.New("stub: not connected")
)

// BrokerClient is a client the broker accepts, as tests compare them.
type BrokerClient = connector.BrokerClient

type brokerMethod int

const (
	publishing brokerMethod = iota
	subscribing
	creating
	deleting
)

// Broker is the stub connector.Broker: an MQTT broker in memory, with the
// clients the application manages and connections a test opens as
// controllers. The application's subscriptions last, as a persistent
// session's would; the channel Subscribe returns holds what was delivered
// since the application last subscribed, each retained message of a filter
// it had not subscribed to first, and is closed, so the application's Run
// handles it and returns. Its connector methods are safe for concurrent
// use, as a broker connection is; a test reads Published once the
// application is done.
type Broker struct {
	mu       sync.Mutex
	clients  map[string]connector.BrokerClient
	conns    []*Connection
	retained map[string]connector.Message
	filters  []string
	queue    []connector.Message
	failing  map[brokerMethod]bool
	// lost says the application's connection to the broker is down.
	lost bool
	// Published is every message the application published, in order.
	Published []connector.Message
}

// NewBroker returns a broker with no clients.
func NewBroker() *Broker {
	return &Broker{clients: map[string]connector.BrokerClient{}, retained: map[string]connector.Message{}, failing: map[brokerMethod]bool{}}
}

// Publish records m and routes it as the broker would.
func (b *Broker) Publish(_ context.Context, m connector.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing[publishing] {
		return ErrUnavailable
	}
	m.Payload = slices.Clone(m.Payload)
	b.Published = append(b.Published, m)
	b.route(m)
	return nil
}

// route delivers m: the broker keeps it when it is retained, an empty
// payload clearing the topic, and passes it on to every connection and to
// the application when they are subscribed to its topic.
func (b *Broker) route(m connector.Message) {
	if m.Retained && len(m.Payload) == 0 {
		delete(b.retained, m.Topic)
	} else if m.Retained {
		b.retained[m.Topic] = m
	}
	live := connector.Message{Topic: m.Topic, Payload: m.Payload}
	for _, c := range b.conns {
		if c.open && anyMatch(c.subscriptions, m.Topic) {
			c.received = append(c.received, live)
		}
	}
	if anyMatch(b.filters, m.Topic) {
		b.queue = append(b.queue, live)
	}
}

// retainedFor is every retained message filter matches, in topic order.
func (b *Broker) retainedFor(filter string) []connector.Message {
	var out []connector.Message
	for _, topic := range slices.Sorted(maps.Keys(b.retained)) {
		if matches(filter, topic) {
			out = append(out, b.retained[topic])
		}
	}
	return out
}

// Subscribe adds filters to the application's subscriptions and returns
// what the broker has for it, closed. A failing subscription delivers
// nothing.
func (b *Broker) Subscribe(_ context.Context, filters []string) (<-chan connector.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing[subscribing] {
		nothing := make(chan connector.Message)
		close(nothing)
		return nothing, ErrUnavailable
	}
	var out []connector.Message
	for _, f := range filters {
		if !slices.Contains(b.filters, f) {
			b.filters = append(b.filters, f)
			out = append(out, b.retainedFor(f)...)
		}
	}
	out = append(out, b.queue...)
	b.queue = nil
	ch := make(chan connector.Message, len(out))
	for _, m := range out {
		ch <- m
	}
	close(ch)
	return ch, nil
}

// CreateClient adds c, refusing a username the broker has.
func (b *Broker) CreateClient(_ context.Context, c connector.BrokerClient) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing[creating] {
		return ErrUnavailable
	}
	if _, ok := b.clients[c.Username]; ok {
		return ErrDuplicate
	}
	c.Publish, c.Subscribe = slices.Clone(c.Publish), slices.Clone(c.Subscribe)
	b.clients[c.Username] = c
	return nil
}

// DeleteClient removes the client named username and closes its
// connections.
func (b *Broker) DeleteClient(_ context.Context, username string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failing[deleting] {
		return ErrUnavailable
	}
	if _, ok := b.clients[username]; !ok {
		return ErrNoSuchClient
	}
	delete(b.clients, username)
	for _, c := range b.conns {
		if c.client.Username == username {
			c.open = false
		}
	}
	return nil
}

// Inject delivers a message on topic as if a client had published it,
// without its client's topics limiting it.
func (b *Broker) Inject(topic string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.route(connector.Message{Topic: topic, Payload: slices.Clone(payload)})
}

// Clients is every client the broker accepts, by username.
func (b *Broker) Clients() []connector.BrokerClient {
	var out []connector.BrokerClient
	for _, name := range slices.Sorted(maps.Keys(b.clients)) {
		out = append(out, b.clients[name])
	}
	return out
}

// Usernames is the username of every client the broker accepts, in order.
func (b *Broker) Usernames() []string {
	return slices.Sorted(maps.Keys(b.clients))
}

// Retained is the message the broker keeps for topic, and whether it keeps
// one.
func (b *Broker) Retained(topic string) (connector.Message, bool) {
	m, ok := b.retained[topic]
	return m, ok
}

// Topics is the topic of each message the application published after
// its first since that ends in the level leaf, in order.
func (b *Broker) Topics(since int, leaf string) []string {
	out := []string{}
	for _, m := range b.Published[since:] {
		if strings.HasSuffix(m.Topic, "/"+leaf) {
			out = append(out, m.Topic)
		}
	}
	return out
}

// RetainedDecoded is the message the broker keeps for topic, decoded as
// Decoded does; nil when it keeps none.
func (b *Broker) RetainedDecoded(topic string) any {
	return Decoded(b.retained[topic].Payload)
}

// Connected reports whether the application's connection is up, which it
// is until Disconnect.
func (b *Broker) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.lost
}

// Disconnect makes Connected report false, as when the application loses
// its connection to the broker.
func (b *Broker) Disconnect() { b.lost = true }

// FailPublishing makes Publish report ErrUnavailable.
func (b *Broker) FailPublishing() { b.failing[publishing] = true }

// FailSubscribing makes Subscribe report ErrUnavailable.
func (b *Broker) FailSubscribing() { b.failing[subscribing] = true }

// FailCreating makes CreateClient report ErrUnavailable.
func (b *Broker) FailCreating() { b.failing[creating] = true }

// FailDeleting makes DeleteClient report ErrUnavailable.
func (b *Broker) FailDeleting() { b.failing[deleting] = true }

// Connect opens a connection as a controller would, with clientID,
// username, password, and a last will the broker publishes, retained, on
// willTopic if the connection drops; an empty willTopic sets none. The
// connection's topics are its client's, clientID in place of each %c.
func (b *Broker) Connect(clientID, username, password, willTopic string, willPayload []byte) (*Connection, error) {
	client, ok := b.clients[username]
	if !ok || client.Password != password {
		return nil, ErrRefused
	}
	client.Publish, client.Subscribe = withClientID(client.Publish, clientID), withClientID(client.Subscribe, clientID)
	c := &Connection{broker: b, client: client, open: true}
	if willTopic != "" {
		c.will = &connector.Message{Topic: willTopic, Payload: slices.Clone(willPayload), Retained: true}
	}
	b.conns = append(b.conns, c)
	return c, nil
}

// withClientID is filters with clientID in place of each %c.
func withClientID(filters []string, clientID string) []string {
	var out []string
	for _, f := range filters {
		out = append(out, strings.ReplaceAll(f, "%c", clientID))
	}
	return out
}

// Connection is a controller's connection to the stub broker.
type Connection struct {
	broker        *Broker
	client        connector.BrokerClient
	will          *connector.Message
	subscriptions []string
	received      []connector.Message
	open          bool
}

// Publish sends a message as the controller. The broker drops, without a
// word, one the client's topics do not allow, as the real broker does.
func (c *Connection) Publish(topic string, payload []byte, retained bool) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if !c.open {
		return ErrDisconnected
	}
	if anyMatch(c.client.Publish, topic) {
		c.broker.route(connector.Message{Topic: topic, Payload: slices.Clone(payload), Retained: retained})
	}
	return nil
}

// Subscribe subscribes the controller to filter, which its client's topics
// must allow, and delivers what the broker retains for it.
func (c *Connection) Subscribe(filter string) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if !c.open {
		return ErrDisconnected
	}
	if !anyMatch(c.client.Subscribe, filter) {
		return ErrDenied
	}
	c.subscriptions = append(c.subscriptions, filter)
	c.received = append(c.received, c.broker.retainedFor(filter)...)
	return nil
}

// Received is every message the broker delivered to the controller, in
// order.
func (c *Connection) Received() []connector.Message {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	return slices.Clone(c.received)
}

// Connected reports whether the connection is open.
func (c *Connection) Connected() bool {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	return c.open
}

// Close disconnects cleanly, so the broker does not publish the last will.
func (c *Connection) Close() {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	c.open = false
}

// Drop loses the connection, so the broker publishes the last will.
func (c *Connection) Drop() {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	c.open = false
	if c.will != nil {
		c.broker.route(*c.will)
	}
}

// matches reports whether the MQTT topic filter matches topic.
func matches(filter, topic string) bool {
	filterLevels, topicLevels := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, level := range filterLevels {
		if level == "#" {
			return true
		}
		if i >= len(topicLevels) || (level != "+" && level != topicLevels[i]) {
			return false
		}
	}
	return len(filterLevels) == len(topicLevels)
}

func anyMatch(filters []string, topic string) bool {
	return slices.ContainsFunc(filters, func(f string) bool { return matches(f, topic) })
}

var (
	cborOut = func() cbor.EncMode {
		mode, _ := cbor.CoreDetEncOptions().EncMode()
		return mode
	}()
	cborIn = func() cbor.DecMode {
		mode, _ := cbor.DecOptions{DefaultMapType: reflect.TypeFor[map[string]any](), IntDec: cbor.IntDecConvertSignedOrFail}.DecMode()
		return mode
	}()
)

// CBOR is v encoded as CBOR, as a controller would encode a message.
func CBOR(v any) []byte {
	out, err := cborOut.Marshal(v)
	if err != nil {
		panic(err)
	}
	return out
}

// Decoded is a CBOR payload as plain values: maps with string keys, int64
// integers, strings, booleans; nil when it is not CBOR.
func Decoded(payload []byte) any {
	var v any
	if err := cborIn.Unmarshal(payload, &v); err != nil {
		return nil
	}
	return v
}

// awaitLimit is how long Await waits.
const awaitLimit = 2 * time.Second

// Await waits until the application has published on topic while it
// runs, and reports whether it did within two seconds.
func (b *Broker) Await(topic string) bool {
	deadline := time.Now().Add(awaitLimit)
	for time.Now().Before(deadline) {
		if b.hasPublished(topic) {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func (b *Broker) hasPublished(topic string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.ContainsFunc(b.Published, func(m connector.Message) bool { return m.Topic == topic })
}
