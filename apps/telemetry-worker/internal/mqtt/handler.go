// Package mqtt provides MQTT message handling for telemetry ingestion.
package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/dlq"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// TelemetryPayload represents the expected MQTT telemetry message format.
type TelemetryPayload struct {
	Timestamp   *time.Time             `json:"timestamp"`
	MachineID   string                 `json:"machine_id"`
	MetricType  string                 `json:"metric_type"`
	Value       float64                `json:"value"`
	Unit        string                 `json:"unit"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// TelemetryStore defines the interface for storing telemetry data.
type TelemetryStore interface {
	CreateBatch(ctx context.Context, records []types.Telemetry) error
	GetMachineByCode(ctx context.Context, code string) (*types.Machine, error)
	UpdateMachineHeartbeat(ctx context.Context, machineID uuid.UUID) error
}

// Handler manages MQTT connections and message processing.
type Handler struct {
	client      mqtt.Client
	store       TelemetryStore
	publisher   *EventPublisher
	dlq         *dlq.DLQ
	cfg         *config.Config
	log         *logrus.Logger
	batch       []types.Telemetry
	batchMu     sync.Mutex
	batchTimer  *time.Timer
	stopChan    chan struct{}
	workerWg    sync.WaitGroup
	messageChan chan *TelemetryMessage
}

// TelemetryMessage wraps a telemetry payload with topic metadata.
type TelemetryMessage struct {
	Topic   string
	Payload TelemetryPayload
}

// NewHandler creates a new MQTT handler.
func NewHandler(cfg *config.Config, store TelemetryStore, log *logrus.Logger) *Handler {
	return &Handler{
		store:       store,
		cfg:         cfg,
		log:         log,
		batch:       make([]types.Telemetry, 0, cfg.Worker.BatchSize),
		stopChan:    make(chan struct{}),
		messageChan: make(chan *TelemetryMessage, cfg.Worker.BatchSize*cfg.Worker.NumWorkers),
	}
}

// SetPublisher sets the event publisher for real-time updates.
func (h *Handler) SetPublisher(p *EventPublisher) {
	h.publisher = p
}

// SetDLQ sets the dead-letter queue for failed batch recovery.
func (h *Handler) SetDLQ(d *dlq.DLQ) {
	h.dlq = d
}

// GetMQTTClient returns the underlying MQTT client for command dispatch.
func (h *Handler) GetMQTTClient() mqtt.Client {
	return h.client
}

// Connect establishes connection to the MQTT broker.
func (h *Handler) Connect() error {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(h.cfg.MQTT.BrokerURL())
	opts.SetClientID(h.cfg.MQTT.ClientID)

	if h.cfg.MQTT.Username != "" {
		opts.SetUsername(h.cfg.MQTT.Username)
		opts.SetPassword(h.cfg.MQTT.Password)
	}

	opts.SetCleanSession(h.cfg.MQTT.CleanStart)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		h.log.Info("Connected to MQTT broker")
		h.subscribe()
	})

	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		h.log.WithError(err).Warn("MQTT connection lost")
	})

	opts.SetReconnectingHandler(func(c mqtt.Client, opts *mqtt.ClientOptions) {
		h.log.Info("Attempting to reconnect to MQTT broker")
	})

	h.client = mqtt.NewClient(opts)

	token := h.client.Connect()
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("connection timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	return nil
}

// subscribe subscribes to the configured MQTT topics.
func (h *Handler) subscribe() {
	topic := h.cfg.MQTT.TopicRoot
	token := h.client.Subscribe(topic, byte(h.cfg.MQTT.QoS), h.messageHandler)
	if token.Wait() && token.Error() != nil {
		h.log.WithError(token.Error()).Error("Failed to subscribe to topic")
		return
	}
	h.log.WithField("topic", topic).Info("Subscribed to MQTT topic")
}

// messageHandler processes incoming MQTT messages.
func (h *Handler) messageHandler(client mqtt.Client, msg mqtt.Message) {
	var payload TelemetryPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		h.log.WithError(err).WithField("topic", msg.Topic()).Debug("Failed to parse telemetry payload")
		return
	}

	h.messageChan <- &TelemetryMessage{
		Topic:   msg.Topic(),
		Payload: payload,
	}
}

// Start begins processing telemetry messages.
func (h *Handler) Start(ctx context.Context) error {
	// Start batch timer
	h.batchTimer = time.NewTimer(time.Duration(h.cfg.Worker.BatchTimeout) * time.Millisecond)

	// Start worker goroutines
	for i := 0; i < h.cfg.Worker.NumWorkers; i++ {
		h.workerWg.Add(1)
		go h.worker(ctx, i)
	}

	// Start batch flusher
	go h.batchFlusher(ctx)

	h.log.WithField("workers", h.cfg.Worker.NumWorkers).Info("Telemetry workers started")
	return nil
}

// worker processes messages from the channel.
func (h *Handler) worker(ctx context.Context, id int) {
	defer h.workerWg.Done()

	log := h.log.WithField("worker_id", id)

	for {
		select {
		case <-ctx.Done():
			log.Debug("Worker shutting down")
			return
		case <-h.stopChan:
			log.Debug("Worker stopped")
			return
		case msg := <-h.messageChan:
			if msg == nil {
				continue
			}
			h.processMessage(ctx, msg)
		}
	}
}

// processMessage converts an MQTT message to a telemetry record.
func (h *Handler) processMessage(ctx context.Context, msg *TelemetryMessage) {
	// Parse topic to extract machine info
	// Format: {tenant}/{site}/{area}/{line}/{machine}/{metric}
	parts := strings.Split(msg.Topic, "/")
	if len(parts) < 6 {
		h.log.WithField("topic", msg.Topic).Debug("Invalid topic format")
		return
	}

	tenant := parts[0]
	machineCode := parts[4]
	metricType := msg.Payload.MetricType
	if metricType == "" && len(parts) > 5 {
		metricType = parts[5]
	}

	// Look up machine by code
	machine, err := h.store.GetMachineByCode(ctx, machineCode)
	if err != nil {
		h.log.WithError(err).WithField("machine_code", machineCode).Debug("Failed to lookup machine")
		return
	}
	if machine == nil {
		h.log.WithFields(logrus.Fields{
			"machine_code": machineCode,
			"tenant":       tenant,
		}).Debug("Machine not found")
		return
	}

	// Update machine heartbeat
	if err := h.store.UpdateMachineHeartbeat(ctx, machine.ID); err != nil {
		h.log.WithError(err).Debug("Failed to update machine heartbeat")
	}

	// Create telemetry record
	timestamp := time.Now()
	if msg.Payload.Timestamp != nil {
		timestamp = *msg.Payload.Timestamp
	}

	record := types.Telemetry{
		ID:         uuid.New(),
		TenantID:   machine.TenantID,
		MachineID:  machine.ID,
		Timestamp:  timestamp,
		MetricType: metricType,
		Value:      msg.Payload.Value,
		Unit:       msg.Payload.Unit,
		Metadata:   msg.Payload.Metadata,
	}

	h.addToBatch(record)
}

// addToBatch adds a telemetry record to the batch buffer.
func (h *Handler) addToBatch(record types.Telemetry) {
	h.batchMu.Lock()
	defer h.batchMu.Unlock()

	h.batch = append(h.batch, record)

	if len(h.batch) >= h.cfg.Worker.BatchSize {
		h.flushBatchLocked()
	}
}

// batchFlusher periodically flushes the batch buffer.
func (h *Handler) batchFlusher(ctx context.Context) {
	timeout := time.Duration(h.cfg.Worker.BatchTimeout) * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			h.flushBatch()
			return
		case <-h.stopChan:
			h.flushBatch()
			return
		case <-h.batchTimer.C:
			h.flushBatch()
			h.batchTimer.Reset(timeout)
		}
	}
}

// flushBatch writes the current batch to storage.
func (h *Handler) flushBatch() {
	h.batchMu.Lock()
	defer h.batchMu.Unlock()
	h.flushBatchLocked()
}

// flushBatchLocked writes the current batch to storage with retry logic (must be called with lock held).
func (h *Handler) flushBatchLocked() {
	if len(h.batch) == 0 {
		return
	}

	batch := h.batch
	h.batch = make([]types.Telemetry, 0, h.cfg.Worker.BatchSize)

	go func() {
		h.writeBatchWithRetry(batch)
	}()
}

// writeBatchWithRetry attempts to write a batch to storage with retry logic.
func (h *Handler) writeBatchWithRetry(batch []types.Telemetry) {
	maxRetries := h.cfg.Worker.RetryAttempts
	retryDelay := time.Duration(h.cfg.Worker.RetryDelay) * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		err := h.store.CreateBatch(ctx, batch)
		cancel()

		if err == nil {
			h.log.WithFields(logrus.Fields{
				"count":   len(batch),
				"attempt": attempt + 1,
			}).Debug("Flushed telemetry batch")

			// Publish events to Redis for real-time distribution
			if h.publisher != nil {
				pubCtx, pubCancel := context.WithTimeout(context.Background(), 5*time.Second)
				if pubErr := h.publisher.PublishTelemetryBatch(pubCtx, batch); pubErr != nil {
					h.log.WithError(pubErr).Debug("Failed to publish telemetry events to Redis")
				}
				pubCancel()
			}

			return
		}

		// Log the failure
		h.log.WithError(err).WithFields(logrus.Fields{
			"count":      len(batch),
			"attempt":    attempt + 1,
			"maxRetries": maxRetries,
		}).Warn("Failed to write telemetry batch")

		// Don't retry on the last attempt
		if attempt < maxRetries {
			// Exponential backoff: delay * 2^attempt
			backoff := retryDelay * time.Duration(1<<uint(attempt))
			if backoff > 30*time.Second {
				backoff = 30 * time.Second // Cap at 30 seconds
			}

			h.log.WithFields(logrus.Fields{
				"backoff_ms": backoff.Milliseconds(),
				"attempt":    attempt + 1,
			}).Debug("Retrying batch write after backoff")

			time.Sleep(backoff)
		}
	}

	// All retries exhausted - write to dead-letter queue for later recovery
	h.log.WithFields(logrus.Fields{
		"count":      len(batch),
		"maxRetries": maxRetries,
	}).Error("Failed to write telemetry batch after all retries")

	// Write to dead-letter queue if available
	if h.dlq != nil {
		dlqCtx, dlqCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := h.dlq.Push(dlqCtx, batch, "all retries exhausted after database write failures"); err != nil {
			h.log.WithError(err).WithField("count", len(batch)).Error("Failed to write batch to DLQ, data lost")
		} else {
			h.log.WithField("count", len(batch)).Info("Batch written to dead-letter queue for recovery")
		}
		dlqCancel()
	} else {
		h.log.WithField("count", len(batch)).Warn("No DLQ configured, data lost")
	}
}

// Stop gracefully shuts down the handler.
func (h *Handler) Stop() {
	close(h.stopChan)
	h.workerWg.Wait()

	if h.batchTimer != nil {
		h.batchTimer.Stop()
	}

	if h.client != nil && h.client.IsConnected() {
		h.client.Disconnect(5000)
	}

	// Flush any remaining records
	h.flushBatch()

	h.log.Info("MQTT handler stopped")
}
