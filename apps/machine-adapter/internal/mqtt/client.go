// Package mqtt provides MQTT client management for the machine adapter service.
package mqtt

import (
	"fmt"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// ClientConfig holds MQTT client configuration.
type ClientConfig struct {
	BrokerURL  string
	ClientID   string
	Username   string
	Password   string
	CleanStart bool
}

// Client wraps an MQTT client with reconnection and subscription management.
type Client struct {
	client   paho.Client
	cfg      ClientConfig
	log      *logrus.Logger
	handlers map[string]paho.MessageHandler
	mu       sync.RWMutex
}

// NewClient creates a new MQTT client.
func NewClient(cfg ClientConfig, log *logrus.Logger) *Client {
	return &Client{
		cfg:      cfg,
		log:      log,
		handlers: make(map[string]paho.MessageHandler),
	}
}

// Connect establishes connection to the MQTT broker.
func (c *Client) Connect() error {
	opts := paho.NewClientOptions()
	opts.AddBroker(c.cfg.BrokerURL)
	opts.SetClientID(c.cfg.ClientID)

	if c.cfg.Username != "" {
		opts.SetUsername(c.cfg.Username)
		opts.SetPassword(c.cfg.Password)
	}

	opts.SetCleanSession(c.cfg.CleanStart)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetKeepAlive(30 * time.Second)

	opts.SetOnConnectHandler(func(client paho.Client) {
		c.log.Info("Connected to MQTT broker")
		c.resubscribe()
	})

	opts.SetConnectionLostHandler(func(client paho.Client, err error) {
		c.log.WithError(err).Warn("MQTT connection lost")
	})

	opts.SetReconnectingHandler(func(client paho.Client, opts *paho.ClientOptions) {
		c.log.Info("Attempting to reconnect to MQTT broker")
	})

	c.client = paho.NewClient(opts)

	token := c.client.Connect()
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("MQTT connection timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("MQTT connection failed: %w", err)
	}

	return nil
}

// Subscribe subscribes to an MQTT topic with the given handler.
func (c *Client) Subscribe(topic string, qos byte, handler paho.MessageHandler) error {
	c.mu.Lock()
	c.handlers[topic] = handler
	c.mu.Unlock()

	token := c.client.Subscribe(topic, qos, handler)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topic, token.Error())
	}

	c.log.WithField("topic", topic).Info("Subscribed to MQTT topic")
	return nil
}

// Publish publishes a message to an MQTT topic.
func (c *Client) Publish(topic string, qos byte, payload []byte) error {
	token := c.client.Publish(topic, qos, false, payload)
	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to publish to %s: %w", topic, token.Error())
	}
	return nil
}

// resubscribe re-subscribes to all topics after reconnection.
func (c *Client) resubscribe() {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for topic, handler := range c.handlers {
		token := c.client.Subscribe(topic, 1, handler)
		if token.Wait() && token.Error() != nil {
			c.log.WithError(token.Error()).WithField("topic", topic).Error("Failed to resubscribe")
		}
	}
}

// Disconnect gracefully disconnects from the MQTT broker.
func (c *Client) Disconnect() {
	if c.client != nil && c.client.IsConnected() {
		c.client.Disconnect(5000)
		c.log.Info("Disconnected from MQTT broker")
	}
}

// IsConnected returns whether the client is currently connected.
func (c *Client) IsConnected() bool {
	return c.client != nil && c.client.IsConnected()
}
