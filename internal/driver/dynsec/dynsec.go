// Package dynsec implements connector.Broker over an MQTT broker with the
// Eclipse broker's dynamic-security plugin. It manages the clients doors
// connect as through the plugin, over $CONTROL/dynamic-security/v1, giving
// each client a role of the same name that carries its topics.
package dynsec

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/dakriy/doorcode/internal/connector"
)

const (
	controlTopic  = "$CONTROL/dynamic-security/v1"
	responseTopic = controlTopic + "/response"
	qos           = 1
)

// Broker is two connections to one broker as its administrator: one
// carries the application's messages, the other the plugin's commands and
// answers, so neither waits behind the other.
type Broker struct {
	messages  mqtt.Client
	control   mqtt.Client
	answers   chan []byte
	commandMu sync.Mutex
	sequence  int
	subsMu    sync.Mutex
	subs      []subscription
	stop      chan struct{}
}

// subscription is a set of topic filters and the inbox their messages go
// to.
type subscription struct {
	filters map[string]byte
	inbox   *inbox
}

// Open connects to the broker at rawURL, such as
// mqtt://admin:secret@localhost:1883 (mqtts:// for TLS), as the client ids
// clientID and clientID-control.
func Open(ctx context.Context, rawURL, clientID string) (*Broker, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	b := &Broker{answers: make(chan []byte, 16), stop: make(chan struct{})}
	b.messages = mqtt.NewClient(options(u, clientID).SetOnConnectHandler(b.resubscribe))
	b.control = mqtt.NewClient(options(u, clientID+"-control").SetOnConnectHandler(b.listen))
	if err := wait(ctx, b.messages.Connect()); err != nil {
		return nil, err
	}
	if err := wait(ctx, b.control.Connect()); err != nil {
		return nil, err
	}
	return b, wait(ctx, b.control.Subscribe(responseTopic, qos, b.answer))
}

func options(u *url.URL, clientID string) *mqtt.ClientOptions {
	password, _ := u.User.Password()
	return mqtt.NewClientOptions().
		AddBroker(u.Scheme + "://" + u.Host).
		SetClientID(clientID).
		SetUsername(u.User.Username()).
		SetPassword(password)
}

// Close disconnects, closing every channel Subscribe returned.
func (b *Broker) Close() {
	close(b.stop)
	b.messages.Disconnect(250)
	b.control.Disconnect(250)
}

// wait waits for tok to complete, or for ctx to be done.
func wait(ctx context.Context, tok mqtt.Token) error {
	select {
	case <-tok.Done():
		return tok.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Publish sends m at QoS 1.
func (b *Broker) Publish(ctx context.Context, m connector.Message) error {
	return wait(ctx, b.messages.Publish(m.Topic, qos, m.Retained, m.Payload))
}

// Subscribe subscribes to filters at QoS 1, again after every reconnect.
func (b *Broker) Subscribe(ctx context.Context, filters []string) (<-chan connector.Message, error) {
	s := subscription{filters: map[string]byte{}, inbox: newInbox()}
	for _, f := range filters {
		s.filters[f] = qos
	}
	go s.inbox.run(b.stop)
	b.subsMu.Lock()
	b.subs = append(b.subs, s)
	b.subsMu.Unlock()
	return s.inbox.out, wait(ctx, b.messages.SubscribeMultiple(s.filters, s.handler))
}

func (s subscription) handler(_ mqtt.Client, m mqtt.Message) {
	s.inbox.put(connector.Message{Topic: m.Topic(), Payload: m.Payload(), Retained: m.Retained()})
}

// resubscribe subscribes again to every filter after a reconnect, since
// the broker forgets a clean session's subscriptions.
func (b *Broker) resubscribe(c mqtt.Client) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, s := range b.subs {
		c.SubscribeMultiple(s.filters, s.handler)
	}
}

// listen subscribes again to the plugin's answers after a reconnect.
func (b *Broker) listen(c mqtt.Client) {
	c.Subscribe(responseTopic, qos, b.answer)
}

func (b *Broker) answer(_ mqtt.Client, m mqtt.Message) {
	select {
	case b.answers <- m.Payload():
	default:
	}
}

// acl is one entry of a role's access control list.
type acl struct {
	Type  string `json:"acltype"`
	Topic string `json:"topic"`
	Allow bool   `json:"allow"`
}

func acls(c connector.BrokerClient) []acl {
	out := []acl{}
	for _, topic := range c.Publish {
		out = append(out, acl{Type: "publishClientSend", Topic: topic, Allow: true})
	}
	for _, topic := range c.Subscribe {
		out = append(out, acl{Type: "subscribePattern", Topic: topic, Allow: true})
	}
	return out
}

// CreateClient creates a role named after c's username carrying its
// topics, and the client with that role.
func (b *Broker) CreateClient(ctx context.Context, c connector.BrokerClient) error {
	return b.command(ctx,
		map[string]any{"command": "createRole", "rolename": c.Username, "acls": acls(c)},
		map[string]any{"command": "createClient", "username": c.Username, "password": c.Password,
			"roles": []map[string]any{{"rolename": c.Username}}})
}

// DeleteClient deletes the client named username, which the plugin
// disconnects, and its role.
func (b *Broker) DeleteClient(ctx context.Context, username string) error {
	return b.command(ctx,
		map[string]any{"command": "deleteClient", "username": username},
		map[string]any{"command": "deleteRole", "rolename": username})
}

// response is the plugin's answer to a list of commands.
type response struct {
	Responses []struct {
		Command         string `json:"command"`
		Error           string `json:"error"`
		CorrelationData string `json:"correlationData"`
	} `json:"responses"`
}

// command sends commands to the plugin, one list at a time, and reports
// every error it answers with.
func (b *Broker) command(ctx context.Context, commands ...map[string]any) error {
	b.commandMu.Lock()
	defer b.commandMu.Unlock()
	b.sequence++
	id := strconv.Itoa(b.sequence)
	for _, c := range commands {
		c["correlationData"] = id
	}
	payload, _ := json.Marshal(map[string]any{"commands": commands})
	if err := wait(ctx, b.control.Publish(controlTopic, qos, false, payload)); err != nil {
		return err
	}
	return b.answered(ctx, id)
}

// answered waits for the answer to the commands sent as id, passing over
// answers to commands given up on, and reports its errors.
func (b *Broker) answered(ctx context.Context, id string) error {
	for {
		r, err := b.nextAnswer(ctx)
		if err != nil || r.answers(id) {
			return errors.Join(err, r.errors())
		}
	}
}

// nextAnswer waits for the plugin's next answer, or for ctx to be done.
func (b *Broker) nextAnswer(ctx context.Context) (response, error) {
	select {
	case raw := <-b.answers:
		return decode(raw), nil
	case <-ctx.Done():
		return response{}, ctx.Err()
	}
}

func decode(raw []byte) response {
	var r response
	_ = json.Unmarshal(raw, &r)
	return r
}

func (r response) answers(id string) bool {
	return len(r.Responses) > 0 && r.Responses[0].CorrelationData == id
}

func (r response) errors() error {
	var errs []error
	for _, answer := range r.Responses {
		if answer.Error != "" {
			errs = append(errs, errors.New(answer.Command+": "+answer.Error))
		}
	}
	return errors.Join(errs...)
}

// inbox hands messages on in the order they arrived without making the
// client wait for its reader, which may itself be waiting on the client.
type inbox struct {
	mu      sync.Mutex
	pending []connector.Message
	wake    chan struct{}
	out     chan connector.Message
}

func newInbox() *inbox {
	return &inbox{wake: make(chan struct{}, 1), out: make(chan connector.Message)}
}

func (i *inbox) put(m connector.Message) {
	i.mu.Lock()
	i.pending = append(i.pending, m)
	i.mu.Unlock()
	select {
	case i.wake <- struct{}{}:
	default:
	}
}

func (i *inbox) take() []connector.Message {
	i.mu.Lock()
	defer i.mu.Unlock()
	taken := i.pending
	i.pending = nil
	return taken
}

// run hands pending messages on until stop is closed, then closes out.
func (i *inbox) run(stop <-chan struct{}) {
	defer close(i.out)
	for {
		select {
		case <-i.wake:
		case <-stop:
			return
		}
		if !i.send(i.take(), stop) {
			return
		}
	}
}

func (i *inbox) send(messages []connector.Message, stop <-chan struct{}) bool {
	for _, m := range messages {
		select {
		case i.out <- m:
		case <-stop:
			return false
		}
	}
	return true
}
