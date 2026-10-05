package host

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// ClientConfig is the host application's own broker connection. Its
// credential is separate from every edge-node credential.
type ClientConfig struct {
	BrokerURL string // ssl://host:8883 (tcp:// only for local tests)
	ClientID  string
	Username  string
	Password  string
	TLSConfig *tls.Config
	// ConnectTimeout bounds CONNECT and SUBSCRIBE (default 10s).
	ConnectTimeout time.Duration
	// ReconnectDelay is the wait after a lost or failed connection (default 5s).
	ReconnectDelay time.Duration
	// TickInterval is how often device liveness is refreshed (default 60s).
	TickInterval time.Duration
	// HandlerTimeout bounds the Store work for one message (default 10s).
	HandlerTimeout time.Duration
}

func (c *ClientConfig) applyDefaults() {
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 10 * time.Second
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = 5 * time.Second
	}
	if c.TickInterval <= 0 {
		c.TickInterval = time.Minute
	}
	if c.HandlerTimeout <= 0 {
		c.HandlerTimeout = 10 * time.Second
	}
	if c.ClientID == "" {
		c.ClientID = "pravara-mes-host"
	}
}

// Subscriptions are the host's topic filters (all groups): births, deaths and
// data of every edge node and device. Commands are never subscribed.
func Subscriptions() []string {
	return []string{
		sparkplug.Namespace + "/+/" + string(sparkplug.NBIRTH) + "/+",
		sparkplug.Namespace + "/+/" + string(sparkplug.NDEATH) + "/+",
		sparkplug.Namespace + "/+/" + string(sparkplug.NDATA) + "/+",
		sparkplug.Namespace + "/+/" + string(sparkplug.DBIRTH) + "/+/+",
		sparkplug.Namespace + "/+/" + string(sparkplug.DDATA) + "/+/+",
		sparkplug.Namespace + "/+/" + string(sparkplug.DDEATH) + "/+/+",
	}
}

// Run connects the host to the broker and processes messages until ctx is
// done, reconnecting after a loss. Every CONNECT carries the will
// spBv1.0/STATE/{host} {"online":false,"timestamp":T} (retained, QoS 1);
// after subscribing, the host publishes {"online":true,"timestamp":T} with
// the same T. On shutdown it publishes the offline STATE with that T and
// disconnects. Run returns nil when ctx ends.
func (e *Engine) Run(ctx context.Context, cfg ClientConfig) error {
	cfg.applyDefaults()
	for {
		err := e.runSession(ctx, cfg)
		if ctx.Err() != nil {
			return nil
		}
		e.opts.Observe("broker_session_ended")
		e.opts.Logger.Warn("host broker session ended; reconnecting", "error", err, "delay", cfg.ReconnectDelay.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(cfg.ReconnectDelay):
		}
	}
}

type pahoPublisher struct{ client paho.Client }

// Publish hands the message to paho without waiting for the broker.
func (p pahoPublisher) Publish(topic string, qos byte, retained bool, payload []byte) error {
	if !p.client.IsConnectionOpen() {
		return ErrNotConnected
	}
	tok := p.client.Publish(topic, qos, retained, payload)
	select {
	case <-tok.Done():
		return tok.Error()
	default:
		return nil
	}
}

func (e *Engine) runSession(ctx context.Context, cfg ClientConfig) error {
	ts := sessionTimestamp(e.opts.Now())
	stateTopic, err := sparkplug.StateTopic(e.opts.HostID)
	if err != nil {
		return err
	}
	offline := sparkplug.EncodeState(sparkplug.HostState{Online: false, Timestamp: ts})
	online := sparkplug.EncodeState(sparkplug.HostState{Online: true, Timestamp: ts})

	lost := make(chan error, 1)
	opts := paho.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID(cfg.ClientID).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetOrderMatters(true).
		SetConnectTimeout(cfg.ConnectTimeout).
		SetKeepAlive(30*time.Second).
		SetBinaryWill(stateTopic, offline, 1, true).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			select {
			case lost <- err:
			default:
			}
		})
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}
	if cfg.TLSConfig != nil {
		opts.SetTLSConfig(cfg.TLSConfig)
	}
	client := paho.NewClient(opts)
	if err := wait(client.Connect(), cfg.ConnectTimeout); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	handler := func(_ paho.Client, m paho.Message) {
		hctx, cancel := context.WithTimeout(ctx, cfg.HandlerTimeout)
		defer cancel()
		e.HandleMessage(hctx, m.Topic(), m.Payload(), e.opts.Now().UTC())
	}
	filters := map[string]byte{stateTopic: 1}
	for _, f := range Subscriptions() {
		filters[f] = 1
	}
	if err := wait(client.SubscribeMultiple(filters, handler), cfg.ConnectTimeout); err != nil {
		client.Disconnect(250)
		return fmt.Errorf("subscribe: %w", err)
	}

	e.attach(pahoPublisher{client}, ts)
	defer e.detach()
	if err := wait(client.Publish(stateTopic, 1, true, online), cfg.ConnectTimeout); err != nil {
		client.Disconnect(250)
		return fmt.Errorf("publish online STATE: %w", err)
	}
	e.opts.Observe("state_online")
	e.opts.Logger.Info("primary host online", "host_id", e.opts.HostID, "timestamp", ts)

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Graceful stop: the offline STATE carries the session timestamp.
			_ = wait(client.Publish(stateTopic, 1, true, offline), cfg.ConnectTimeout)
			client.Disconnect(250)
			e.opts.Logger.Info("primary host offline", "host_id", e.opts.HostID)
			return nil
		case err := <-lost:
			if err == nil {
				err = errors.New("connection lost")
			}
			return err
		case <-ticker.C:
			tctx, cancel := context.WithTimeout(ctx, cfg.HandlerTimeout)
			e.Tick(tctx)
			cancel()
		}
	}
}

func wait(tok paho.Token, timeout time.Duration) error {
	if !tok.WaitTimeout(timeout) {
		return errors.New("timed out")
	}
	return tok.Error()
}
