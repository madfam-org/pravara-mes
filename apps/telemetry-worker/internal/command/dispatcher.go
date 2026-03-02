// Package command provides command dispatch functionality for machine control.
package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Dispatcher handles receiving commands from Redis and dispatching them to machines via MQTT.
type Dispatcher struct {
	redisClient *redis.Client
	mqttClient  mqtt.Client
	publisher   AckPublisher
	log         *logrus.Logger
	mu          sync.RWMutex
	closed      bool
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// AckPublisher defines the interface for publishing command acknowledgments.
type AckPublisher interface {
	PublishCommandAck(ctx context.Context, tenantID, machineID uuid.UUID, ack CommandAckData) error
}

// DispatcherConfig contains configuration for the command dispatcher.
type DispatcherConfig struct {
	RedisURL string
}

// NewDispatcher creates a new command dispatcher.
func NewDispatcher(redisClient *redis.Client, mqttClient mqtt.Client, log *logrus.Logger) *Dispatcher {
	return &Dispatcher{
		redisClient: redisClient,
		mqttClient:  mqttClient,
		log:         log,
		stopChan:    make(chan struct{}),
	}
}

// SetPublisher sets the acknowledgment publisher.
func (d *Dispatcher) SetPublisher(p AckPublisher) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.publisher = p
}

// Start begins listening for commands on Redis and dispatching to MQTT.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return fmt.Errorf("dispatcher is closed")
	}
	d.mu.Unlock()

	// Subscribe to command channels using pattern subscription
	pattern := CommandChannelWildcard()
	pubsub := d.redisClient.PSubscribe(ctx, pattern)

	// Verify subscription
	_, err := pubsub.Receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe to command channels: %w", err)
	}

	d.log.WithField("pattern", pattern).Info("Command dispatcher subscribed to Redis")

	// Start message processing goroutine
	d.wg.Add(1)
	go d.processMessages(ctx, pubsub)

	return nil
}

// processMessages handles incoming Redis messages.
func (d *Dispatcher) processMessages(ctx context.Context, pubsub *redis.PubSub) {
	defer d.wg.Done()
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			d.log.Debug("Command dispatcher context cancelled")
			return
		case <-d.stopChan:
			d.log.Debug("Command dispatcher stopped")
			return
		case msg, ok := <-ch:
			if !ok {
				d.log.Debug("Command channel closed")
				return
			}
			d.handleMessage(ctx, msg)
		}
	}
}

// handleMessage processes a single Redis message.
func (d *Dispatcher) handleMessage(ctx context.Context, msg *redis.Message) {
	log := d.log.WithFields(logrus.Fields{
		"channel": msg.Channel,
	})

	// Parse the command from the message payload
	var cmd MachineCommand
	if err := json.Unmarshal([]byte(msg.Payload), &cmd); err != nil {
		log.WithError(err).Debug("Failed to parse command message")
		return
	}

	log = log.WithFields(logrus.Fields{
		"command_id": cmd.CommandID,
		"machine_id": cmd.MachineID,
		"command":    cmd.Command,
		"mqtt_topic": cmd.MQTTTopic,
	})

	// Validate command has required fields
	if cmd.MQTTTopic == "" {
		log.Warn("Command missing MQTT topic, cannot dispatch")
		return
	}

	// Dispatch the command to the machine via MQTT
	if err := d.dispatchToMQTT(ctx, &cmd); err != nil {
		log.WithError(err).Error("Failed to dispatch command to MQTT")
		return
	}

	log.Info("Command dispatched to machine")
}

// dispatchToMQTT publishes a command to the machine's MQTT topic.
func (d *Dispatcher) dispatchToMQTT(ctx context.Context, cmd *MachineCommand) error {
	// Build the command topic
	topic := GetCommandTopic(cmd.MQTTTopic)

	// Convert to MQTT payload format
	payload := cmd.ToMQTTPayload()

	// Serialize to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal command payload: %w", err)
	}

	// Publish to MQTT with QoS 1 for reliable delivery
	token := d.mqttClient.Publish(topic, 1, false, data)

	// Wait for publish with timeout
	done := make(chan bool, 1)
	go func() {
		token.Wait()
		done <- true
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return fmt.Errorf("mqtt publish timeout")
	case <-done:
		if err := token.Error(); err != nil {
			return fmt.Errorf("mqtt publish failed: %w", err)
		}
	}

	d.log.WithFields(logrus.Fields{
		"topic":      topic,
		"command_id": cmd.CommandID,
		"command":    cmd.Command,
	}).Debug("Command published to MQTT")

	return nil
}

// Stop gracefully shuts down the dispatcher.
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.mu.Unlock()

	close(d.stopChan)
	d.wg.Wait()

	d.log.Info("Command dispatcher stopped")
}

// HealthCheck performs a health check on the dispatcher.
func (d *Dispatcher) HealthCheck(ctx context.Context) error {
	d.mu.RLock()
	if d.closed {
		d.mu.RUnlock()
		return fmt.Errorf("dispatcher is closed")
	}
	d.mu.RUnlock()

	// Check Redis connection
	if err := d.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis health check failed: %w", err)
	}

	// Check MQTT connection
	if !d.mqttClient.IsConnected() {
		return fmt.Errorf("mqtt client not connected")
	}

	return nil
}
