package demo

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/dakriy/doorcode/internal/connector"
)

// ErrRefused is a connection whose username and password the broker does
// not hold.
var ErrRefused = errors.New("the broker refused the credentials")

// Broker is an in-memory stand-in for the MQTT broker: it keeps clients,
// retained messages, and subscriptions, and lets the simulated doors
// connect as controllers. The application reaches it as the Broker
// connector, allowed everything; a controller's connection may publish
// and subscribe only where its client allows. It delivers every message
// to every subscription it matches, in order, without ever blocking the
// publisher.
type Broker struct {
	mu       sync.Mutex
	clients  map[string]connector.BrokerClient
	retained map[string][]byte
	// listeners are the application's subscriptions and every
	// connection's.
	listeners []*listener
	conns     []*Conn
}

// NewBroker returns a broker with no clients and no messages.
func NewBroker() *Broker {
	return &Broker{clients: map[string]connector.BrokerClient{}, retained: map[string][]byte{}}
}

var _ connector.Broker = (*Broker)(nil)

// listener is one subscription: the filters it matches and where its
// messages wait to be taken.
type listener struct {
	filters []string
	box     *mailbox
}

// Publish delivers m as the application, which may publish anywhere.
func (b *Broker) Publish(_ context.Context, m connector.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deliver(m)
	return nil
}

// deliver keeps m when it is retained, an empty payload clearing the
// topic's retained message, and hands it to every listener it matches.
// The caller holds b.mu.
func (b *Broker) deliver(m connector.Message) {
	if m.Retained && len(m.Payload) == 0 {
		delete(b.retained, m.Topic)
	} else if m.Retained {
		b.retained[m.Topic] = slices.Clone(m.Payload)
	}
	for _, l := range b.listeners {
		if matchesAny(l.filters, m.Topic) {
			l.box.put(connector.Message{Topic: m.Topic, Payload: slices.Clone(m.Payload), Retained: false})
		}
	}
}

// Subscribe subscribes the application to filters. The channel closes
// when ctx ends.
func (b *Broker) Subscribe(ctx context.Context, filters []string) (<-chan connector.Message, error) {
	box := newMailbox()
	b.mu.Lock()
	b.listen(&listener{filters: slices.Clone(filters), box: box})
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.forget(box)
	}()
	return box.out, nil
}

// listen adds l and hands it the retained messages it matches. The
// caller holds b.mu.
func (b *Broker) listen(l *listener) {
	b.listeners = append(b.listeners, l)
	b.handRetained(l, l.filters)
}

// handRetained hands l the retained messages filters match, as retained.
// The caller holds b.mu.
func (b *Broker) handRetained(l *listener, filters []string) {
	topics := make([]string, 0, len(b.retained))
	for topic := range b.retained {
		topics = append(topics, topic)
	}
	slices.Sort(topics)
	for _, topic := range topics {
		if matchesAny(filters, topic) {
			l.box.put(connector.Message{Topic: topic, Payload: slices.Clone(b.retained[topic]), Retained: true})
		}
	}
}

// forget removes every listener delivering to box and closes it. The
// caller holds b.mu.
func (b *Broker) forget(box *mailbox) {
	b.listeners = slices.DeleteFunc(b.listeners, func(l *listener) bool { return l.box == box })
	box.close()
}

// CreateClient adds c, refusing a username the broker has.
func (b *Broker) CreateClient(_ context.Context, c connector.BrokerClient) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.clients[c.Username]; ok {
		return errors.New("the broker has a client named " + c.Username)
	}
	c.Publish = slices.Clone(c.Publish)
	c.Subscribe = slices.Clone(c.Subscribe)
	b.clients[c.Username] = c
	return nil
}

// DeleteClient removes the client named username and drops its
// connections, which publishes their last wills.
func (b *Broker) DeleteClient(_ context.Context, username string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.clients[username]; !ok {
		return errors.New("the broker has no client named " + username)
	}
	delete(b.clients, username)
	for _, c := range slices.Clone(b.conns) {
		if c.username == username {
			b.end(c, true)
		}
	}
	return nil
}

// Conn is one controller's connection to the broker.
type Conn struct {
	broker   *Broker
	clientID string
	username string
	will     connector.Message
	// listener is what the connection subscribed to.
	listener *listener
	closed   bool
}

// Connect connects as a controller with client id clientID, username, and
// password, leaving will to be published when the connection drops. A
// connection with the same client id is dropped first, as MQTT takes it
// over.
func (b *Broker) Connect(clientID, username, password string, will connector.Message) (*Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	client, ok := b.clients[username]
	if !ok || client.Password != password {
		return nil, ErrRefused
	}
	for _, c := range slices.Clone(b.conns) {
		if c.clientID == clientID {
			b.end(c, true)
		}
	}
	c := &Conn{broker: b, clientID: clientID, username: username, will: will, listener: &listener{box: newMailbox()}}
	b.conns = append(b.conns, c)
	b.listeners = append(b.listeners, c.listener)
	return c, nil
}

// end closes c, publishing its last will when dropped says it went away
// without a word. The caller holds b.mu.
func (b *Broker) end(c *Conn, dropped bool) {
	if c.closed {
		return
	}
	c.closed = true
	b.conns = slices.DeleteFunc(b.conns, func(o *Conn) bool { return o == c })
	b.forget(c.listener.box)
	if dropped && c.will.Topic != "" {
		b.deliver(c.will)
	}
}

// allowed is the client's topic filters with the connection's client id
// in place of %c. The caller holds b.mu.
func (c *Conn) allowed(publish bool) []string {
	client := c.broker.clients[c.username]
	filters := client.Subscribe
	if publish {
		filters = client.Publish
	}
	out := make([]string, len(filters))
	for i, f := range filters {
		out[i] = strings.ReplaceAll(f, "%c", c.clientID)
	}
	return out
}

// Publish publishes m as the controller. The broker drops without a word
// what the client may not publish, as the real broker does.
func (c *Conn) Publish(m connector.Message) {
	b := c.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.closed || !matchesAny(c.allowed(true), m.Topic) {
		return
	}
	b.deliver(m)
}

// Subscribe adds filters to what the connection receives, handing it the
// retained messages they match first. It refuses a filter the client may
// not subscribe to.
func (c *Conn) Subscribe(filters ...string) error {
	b := c.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	if c.closed {
		return errors.New("the connection is closed")
	}
	allowed := c.allowed(false)
	for _, f := range filters {
		if !matchesAny(allowed, f) {
			return errors.New("the broker denied the subscription to " + f)
		}
	}
	c.listener.filters = append(c.listener.filters, filters...)
	b.handRetained(c.listener, filters)
	return nil
}

// Messages is where the connection receives what it subscribed to. It
// closes when the connection ends.
func (c *Conn) Messages() <-chan connector.Message {
	return c.listener.box.out
}

// Close disconnects cleanly, without the last will.
func (c *Conn) Close() {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	c.broker.end(c, false)
}

// Drop disconnects as a controller losing power does, publishing the last
// will.
func (c *Conn) Drop() {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	c.broker.end(c, true)
}

// matchesAny reports whether any of filters matches topic.
func matchesAny(filters []string, topic string) bool {
	for _, f := range filters {
		if matches(f, topic) {
			return true
		}
	}
	return false
}

// matches reports whether the MQTT topic filter matches topic: + stands
// for one level and a final # for any number, none included.
func matches(filter, topic string) bool {
	want := strings.Split(filter, "/")
	got := strings.Split(topic, "/")
	for i, level := range want {
		if level == "#" {
			return true
		}
		if i >= len(got) || (level != "+" && level != got[i]) {
			return false
		}
	}
	return len(want) == len(got)
}

// mailbox holds messages for one subscriber until it takes them, so a
// publisher never waits for a subscriber.
type mailbox struct {
	mu     sync.Mutex
	queue  []connector.Message
	wake   chan struct{}
	done   chan struct{}
	out    chan connector.Message
	closed bool
}

func newMailbox() *mailbox {
	m := &mailbox{wake: make(chan struct{}, 1), done: make(chan struct{}), out: make(chan connector.Message)}
	go m.pump()
	return m
}

// put adds msg to the queue.
func (m *mailbox) put(msg connector.Message) {
	m.mu.Lock()
	m.queue = append(m.queue, msg)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// close stops the mailbox; its channel closes, dropping what waits.
func (m *mailbox) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.done)
	}
}

// pump hands the queued messages over one at a time until the mailbox
// closes.
func (m *mailbox) pump() {
	defer close(m.out)
	for {
		next, ok := m.take()
		if !ok {
			select {
			case <-m.wake:
				continue
			case <-m.done:
				return
			}
		}
		select {
		case m.out <- next:
		case <-m.done:
			return
		}
	}
}

// take removes the oldest queued message, reporting whether there was
// one.
func (m *mailbox) take() (connector.Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return connector.Message{}, false
	}
	next := m.queue[0]
	m.queue = m.queue[1:]
	return next, true
}
