// Package mqtt provides MQTT message handling and Redis event publishing.
package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// EventPublisher handles publishing telemetry events to Redis for real-time distribution.
type EventPublisher struct {
	client *redis.Client
	log    *logrus.Logger
	mu     sync.RWMutex
	closed bool
}

// PublisherConfig contains configuration for the EventPublisher.
type PublisherConfig struct {
	RedisURL string
}

// NewEventPublisher creates a new Redis event publisher.
func NewEventPublisher(cfg PublisherConfig, log *logrus.Logger) (*EventPublisher, error) {
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

	log.Info("Event publisher connected to Redis")

	return &EventPublisher{
		client: client,
		log:    log,
	}, nil
}

// Close closes the Redis connection.
func (p *EventPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}

	p.closed = true
	return p.client.Close()
}

// TelemetryEvent represents a real-time telemetry event.
type TelemetryEvent struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	TenantID  uuid.UUID          `json:"tenant_id"`
	Timestamp time.Time          `json:"timestamp"`
	Data      TelemetryBatchData `json:"data"`
}

// TelemetryBatchData contains the batch of telemetry metrics.
type TelemetryBatchData struct {
	MachineID  uuid.UUID         `json:"machine_id"`
	Metrics    []TelemetryMetric `json:"metrics"`
	ReceivedAt time.Time         `json:"received_at"`
}

// TelemetryMetric represents a single telemetry metric in an event.
type TelemetryMetric struct {
	Type      string    `json:"type"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Timestamp time.Time `json:"timestamp"`
}

// MachineStatusEvent represents a machine status change event.
type MachineStatusEvent struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	TenantID  uuid.UUID         `json:"tenant_id"`
	Timestamp time.Time         `json:"timestamp"`
	Data      MachineStatusData `json:"data"`
}

// MachineStatusData contains machine status information.
type MachineStatusData struct {
	MachineID     uuid.UUID `json:"machine_id"`
	MachineName   string    `json:"machine_name"`
	NewStatus     string    `json:"new_status"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	IsOnline      bool      `json:"is_online"`
}

// PublishTelemetryBatch publishes a batch of telemetry records as an event.
func (p *EventPublisher) PublishTelemetryBatch(ctx context.Context, records []types.Telemetry) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	if len(records) == 0 {
		return nil
	}

	// Group records by tenant and machine for efficient publishing
	grouped := make(map[uuid.UUID]map[uuid.UUID][]types.Telemetry)
	for _, r := range records {
		if grouped[r.TenantID] == nil {
			grouped[r.TenantID] = make(map[uuid.UUID][]types.Telemetry)
		}
		grouped[r.TenantID][r.MachineID] = append(grouped[r.TenantID][r.MachineID], r)
	}

	// Publish events for each tenant/machine group
	for tenantID, machines := range grouped {
		for machineID, machineRecords := range machines {
			event := TelemetryEvent{
				ID:        uuid.New().String(),
				Type:      "machine.telemetry_batch",
				TenantID:  tenantID,
				Timestamp: time.Now().UTC(),
				Data: TelemetryBatchData{
					MachineID:  machineID,
					ReceivedAt: time.Now().UTC(),
					Metrics:    make([]TelemetryMetric, 0, len(machineRecords)),
				},
			}

			for _, r := range machineRecords {
				event.Data.Metrics = append(event.Data.Metrics, TelemetryMetric{
					Type:      r.MetricType,
					Value:     r.Value,
					Unit:      r.Unit,
					Timestamp: r.Timestamp,
				})
			}

			if err := p.publishEvent(ctx, "telemetry", tenantID, event); err != nil {
				p.log.WithError(err).WithFields(logrus.Fields{
					"tenant_id":  tenantID,
					"machine_id": machineID,
					"count":      len(machineRecords),
				}).Warn("Failed to publish telemetry event")
				// Continue with other events even if one fails
			}
		}
	}

	return nil
}

// PublishMachineHeartbeat publishes a machine heartbeat event.
func (p *EventPublisher) PublishMachineHeartbeat(ctx context.Context, tenantID, machineID uuid.UUID, machineName string) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	event := MachineStatusEvent{
		ID:        uuid.New().String(),
		Type:      "machine.heartbeat",
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data: MachineStatusData{
			MachineID:     machineID,
			MachineName:   machineName,
			NewStatus:     string(types.MachineStatusOnline),
			LastHeartbeat: time.Now().UTC(),
			IsOnline:      true,
		},
	}

	return p.publishEvent(ctx, "machines", tenantID, event)
}

// publishEvent publishes an event to the appropriate Centrifugo channel via Redis.
func (p *EventPublisher) publishEvent(ctx context.Context, namespace string, tenantID uuid.UUID, event interface{}) error {
	channel := fmt.Sprintf("%s:%s", namespace, tenantID.String())

	eventData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	publishCmd := map[string]interface{}{
		"channel": channel,
		"data":    json.RawMessage(eventData),
	}

	cmdData, err := json.Marshal(publishCmd)
	if err != nil {
		return fmt.Errorf("failed to marshal publish command: %w", err)
	}

	// Publish to Centrifugo's Redis channel
	if err := p.client.Publish(ctx, "centrifugo.api.publish", cmdData).Err(); err != nil {
		return fmt.Errorf("failed to publish to redis: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"channel":   channel,
		"tenant_id": tenantID,
	}).Debug("Event published")

	return nil
}

// HealthCheck performs a health check on the Redis connection.
func (p *EventPublisher) HealthCheck(ctx context.Context) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	return p.client.Ping(ctx).Err()
}

// GetRedisClient returns the underlying Redis client for command dispatch.
func (p *EventPublisher) GetRedisClient() *redis.Client {
	return p.client
}

// PublishCommandAck publishes a command acknowledgment event to Centrifugo.
// This method implements the command.AckPublisher interface.
func (p *EventPublisher) PublishCommandAck(ctx context.Context, tenantID, machineID uuid.UUID, ack command.CommandAckData) error {
	p.mu.RLock()
	if p.closed {
		p.mu.RUnlock()
		return fmt.Errorf("publisher is closed")
	}
	p.mu.RUnlock()

	event := command.CommandAckEvent{
		ID:        uuid.New().String(),
		Type:      "machine.command_ack",
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data:      ack,
	}

	if err := p.publishEvent(ctx, "machines", tenantID, event); err != nil {
		return fmt.Errorf("failed to publish command ack event: %w", err)
	}

	p.log.WithFields(logrus.Fields{
		"command_id": ack.CommandID,
		"machine_id": ack.MachineID,
		"success":    ack.Success,
	}).Debug("Command ack event published")

	return nil
}
