// Package pubsub provides real-time event publishing for PravaraMES.
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Publisher handles publishing events to Redis for Centrifugo distribution.
type Publisher struct {
	client *redis.Client
	log    *logrus.Logger
	mu     sync.RWMutex
	closed bool
}

// PublisherConfig contains configuration for the Publisher.
type PublisherConfig struct {
	RedisURL string
}

// NewPublisher creates a new Publisher connected to Redis.
func NewPublisher(cfg PublisherConfig, log *logrus.Logger) (*Publisher, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	log.Info("Redis publisher connected")

	return &Publisher{
		client: client,
		log:    log,
	}, nil
}

// Close closes the Redis connection.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	return p.client.Close()
}

// buildChannel creates a Centrifugo channel name.
// Format: namespace:tenant_id or namespace:tenant_id:entity_id
func buildChannel(namespace ChannelNamespace, tenantID uuid.UUID, entityID *uuid.UUID) string {
	if entityID != nil {
		return fmt.Sprintf("%s:%s:%s", namespace, tenantID.String(), entityID.String())
	}
	return fmt.Sprintf("%s:%s", namespace, tenantID.String())
}

// CentrifugoMessage represents the message format expected by Centrifugo Redis Engine.
type CentrifugoMessage struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// Publish publishes an event to the appropriate Centrifugo channel via Redis.
func (p *Publisher) Publish(ctx context.Context, namespace ChannelNamespace, tenantID uuid.UUID, event *Event) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	channel := buildChannel(namespace, tenantID, nil)
	return p.publishToChannel(ctx, channel, event)
}

// PublishToEntity publishes an event to an entity-specific channel.
func (p *Publisher) PublishToEntity(ctx context.Context, namespace ChannelNamespace, tenantID, entityID uuid.UUID, event *Event) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	channel := buildChannel(namespace, tenantID, &entityID)
	return p.publishToChannel(ctx, channel, event)
}

// publishToChannel publishes an event to a specific Centrifugo channel.
func (p *Publisher) publishToChannel(ctx context.Context, channel string, event *Event) error {
	// Serialize event data
	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Centrifugo expects messages on the centrifugo.api.publish channel
	// with a specific format for Redis Engine
	publishCmd := map[string]interface{}{
		"channel": channel,
		"data":    json.RawMessage(eventData),
	}

	cmdData, err := json.Marshal(publishCmd)
	if err != nil {
		return fmt.Errorf("failed to marshal publish command: %w", err)
	}

	// Publish to Centrifugo's Redis channel
	// Centrifugo Redis Engine listens on "centrifugo.api.publish" by default
	if err := p.client.Publish(ctx, "centrifugo.api.publish", cmdData).Err(); err != nil {
		p.log.WithError(err).WithFields(logrus.Fields{
			"channel": channel,
			"event":   event.Type,
		}).Error("Failed to publish event to Redis")
		return fmt.Errorf("failed to publish to redis: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"channel":   channel,
		"event":     event.Type,
		"tenant_id": event.TenantID,
	}).Debug("Event published")

	return nil
}

// PublishMachineStatus publishes a machine status change event.
func (p *Publisher) PublishMachineStatus(ctx context.Context, tenantID uuid.UUID, data MachineStatusData) error {
	event := NewEvent(EventMachineStatusChanged, tenantID, data)
	return p.Publish(ctx, NamespaceMachines, tenantID, event)
}

// PublishMachineHeartbeat publishes a machine heartbeat event.
func (p *Publisher) PublishMachineHeartbeat(ctx context.Context, tenantID uuid.UUID, data MachineHeartbeatData) error {
	event := NewEvent(EventMachineHeartbeat, tenantID, data)
	return p.Publish(ctx, NamespaceMachines, tenantID, event)
}

// PublishMachineCommand publishes a machine command event.
// This notifies the UI and any listeners that a command was issued.
func (p *Publisher) PublishMachineCommand(ctx context.Context, tenantID uuid.UUID, data MachineCommandData) error {
	event := NewEvent(EventMachineCommandSent, tenantID, data)
	return p.PublishToEntity(ctx, NamespaceMachines, tenantID, data.MachineID, event)
}

// PublishMachineCommandAck publishes a command acknowledgement event.
func (p *Publisher) PublishMachineCommandAck(ctx context.Context, tenantID uuid.UUID, data MachineCommandAckData) error {
	event := NewEvent(EventMachineCommandAck, tenantID, data)
	return p.PublishToEntity(ctx, NamespaceMachines, tenantID, data.MachineID, event)
}

// PublishTelemetryBatch publishes a telemetry batch event.
func (p *Publisher) PublishTelemetryBatch(ctx context.Context, tenantID uuid.UUID, data TelemetryBatchData) error {
	event := NewEvent(EventMachineTelemetryBatch, tenantID, data)
	return p.Publish(ctx, NamespaceTelemetry, tenantID, event)
}

// PublishTaskMove publishes a task move event.
func (p *Publisher) PublishTaskMove(ctx context.Context, tenantID uuid.UUID, data TaskMoveData) error {
	event := NewEvent(EventTaskMoved, tenantID, data)
	return p.Publish(ctx, NamespaceTasks, tenantID, event)
}

// PublishTaskAssign publishes a task assignment event.
func (p *Publisher) PublishTaskAssign(ctx context.Context, tenantID uuid.UUID, data TaskAssignData) error {
	event := NewEvent(EventTaskAssigned, tenantID, data)
	return p.Publish(ctx, NamespaceTasks, tenantID, event)
}

// PublishOrderStatus publishes an order status change event.
func (p *Publisher) PublishOrderStatus(ctx context.Context, tenantID uuid.UUID, data OrderStatusData) error {
	event := NewEvent(EventOrderStatus, tenantID, data)
	return p.Publish(ctx, NamespaceOrders, tenantID, event)
}

// PublishNotification publishes a notification event.
func (p *Publisher) PublishNotification(ctx context.Context, tenantID uuid.UUID, data NotificationData) error {
	var eventType EventType
	switch data.Severity {
	case "critical", "error":
		eventType = EventNotificationAlert
	case "warning":
		eventType = EventNotificationWarning
	default:
		eventType = EventNotificationInfo
	}

	event := NewEvent(eventType, tenantID, data)
	return p.Publish(ctx, NamespaceNotifications, tenantID, event)
}

// PublishEntityCreated publishes a generic entity creation event.
func (p *Publisher) PublishEntityCreated(ctx context.Context, namespace ChannelNamespace, tenantID uuid.UUID, eventType EventType, data EntityCreatedData) error {
	event := NewEvent(eventType, tenantID, data)
	return p.Publish(ctx, namespace, tenantID, event)
}

// PublishEntityUpdated publishes a generic entity update event.
func (p *Publisher) PublishEntityUpdated(ctx context.Context, namespace ChannelNamespace, tenantID uuid.UUID, eventType EventType, data EntityUpdatedData) error {
	event := NewEvent(eventType, tenantID, data)
	return p.Publish(ctx, namespace, tenantID, event)
}

// PublishEntityDeleted publishes a generic entity deletion event.
func (p *Publisher) PublishEntityDeleted(ctx context.Context, namespace ChannelNamespace, tenantID uuid.UUID, eventType EventType, data EntityDeletedData) error {
	event := NewEvent(eventType, tenantID, data)
	return p.Publish(ctx, namespace, tenantID, event)
}

// HealthCheck performs a health check on the Redis connection.
func (p *Publisher) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	return p.client.Ping(ctx).Err()
}
