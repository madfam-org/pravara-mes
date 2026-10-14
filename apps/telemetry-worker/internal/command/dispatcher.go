// Package command provides command dispatch functionality for machine control.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
)

// MessagePublisher publishes a payload to an MQTT topic and reports whether
// the broker accepted it.
type MessagePublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// AckPublisher defines the interface for publishing command acknowledgments.
type AckPublisher interface {
	PublishCommandAck(ctx context.Context, tenantID, machineID uuid.UUID, ack CommandAckData) error
}

// DispatcherConfig configures the stream dispatcher.
type DispatcherConfig struct {
	StreamKey string
	Group     string
	Consumer  string
	// MaxAttempts bounds MQTT publish attempts per command.
	MaxAttempts int
	// RetryIdle is how long a pending stream entry must be idle before it
	// is reclaimed and retried.
	RetryIdle time.Duration
	// AckTimeout is the deadline for the machine to acknowledge.
	AckTimeout time.Duration
	// ReadBlock is how long one XREADGROUP call blocks.
	ReadBlock time.Duration
	// BatchSize is the maximum number of entries read or reclaimed per call.
	BatchSize int64
}

func (c *DispatcherConfig) applyDefaults() {
	if c.StreamKey == "" {
		c.StreamKey = DefaultStreamKey
	}
	if c.Group == "" {
		c.Group = "telemetry-worker"
	}
	if c.Consumer == "" {
		c.Consumer = "telemetry-worker"
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.RetryIdle <= 0 {
		c.RetryIdle = 30 * time.Second
	}
	if c.AckTimeout <= 0 {
		c.AckTimeout = 2 * time.Minute
	}
	if c.ReadBlock <= 0 {
		c.ReadBlock = 5 * time.Second
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 16
	}
}

// Dispatcher consumes machine commands from a Redis stream through a
// consumer group and publishes them to the machine's MQTT command topic.
//
// Delivery is at-least-once: an entry is acknowledged in the stream only
// after its outcome is recorded in the command ledger. An entry that is not
// acknowledged (failed attempt with budget left, worker crash, database
// unavailable) is reclaimed with XAUTOCLAIM once it has been idle for
// RetryIdle. Machines must treat command_id as an idempotency key.
type Dispatcher struct {
	redisClient *redis.Client
	publisher   MessagePublisher
	ledger      DispatchLedger
	cfg         DispatcherConfig
	log         *logrus.Logger
	now         func() time.Time
	// sparkplug delivers commands to Sparkplug-registered machines
	// (dispatcher_sparkplug.go); nil when the primary host is disabled.
	sparkplug SparkplugSender

	mu      sync.Mutex
	started bool
	closed  bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewDispatcher creates a new stream dispatcher.
func NewDispatcher(redisClient *redis.Client, publisher MessagePublisher, ledger DispatchLedger, cfg DispatcherConfig, log *logrus.Logger) *Dispatcher {
	cfg.applyDefaults()
	return &Dispatcher{
		redisClient: redisClient,
		publisher:   publisher,
		ledger:      ledger,
		cfg:         cfg,
		log:         log,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Start creates the consumer group if needed and begins consuming.
func (d *Dispatcher) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fmt.Errorf("dispatcher is closed")
	}
	if d.started {
		return fmt.Errorf("dispatcher already started")
	}

	if err := d.ensureGroup(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	d.started = true

	d.wg.Add(1)
	go d.run(runCtx)

	d.log.WithFields(logrus.Fields{
		"stream":   d.cfg.StreamKey,
		"group":    d.cfg.Group,
		"consumer": d.cfg.Consumer,
	}).Info("Command dispatcher consuming stream")
	return nil
}

// ensureGroup creates the consumer group at the start of the stream, so
// entries appended before the group existed are still delivered.
func (d *Dispatcher) ensureGroup(ctx context.Context) error {
	err := d.redisClient.XGroupCreateMkStream(ctx, d.cfg.StreamKey, d.cfg.Group, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}
	return nil
}

func (d *Dispatcher) run(ctx context.Context) {
	defer d.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}

		d.reclaim(ctx)

		streams, err := d.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    d.cfg.Group,
			Consumer: d.cfg.Consumer,
			Streams:  []string{d.cfg.StreamKey, ">"},
			Count:    d.cfg.BatchSize,
			Block:    d.cfg.ReadBlock,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			d.log.WithError(err).Warn("Command stream read failed")
			if strings.HasPrefix(err.Error(), "NOGROUP") {
				if gerr := d.ensureGroup(ctx); gerr != nil {
					d.log.WithError(gerr).Warn("Failed to recreate consumer group")
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				d.handleEntry(ctx, msg)
			}
		}
	}
}

// reclaim takes over entries that have been pending longer than RetryIdle,
// whichever consumer held them, and processes them again.
func (d *Dispatcher) reclaim(ctx context.Context) {
	start := "0-0"
	for i := 0; i < 10; i++ {
		msgs, next, err := d.redisClient.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   d.cfg.StreamKey,
			Group:    d.cfg.Group,
			Consumer: d.cfg.Consumer,
			MinIdle:  d.cfg.RetryIdle,
			Start:    start,
			Count:    d.cfg.BatchSize,
		}).Result()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, redis.Nil) {
				d.log.WithError(err).Warn("Command stream reclaim failed")
			}
			return
		}
		for _, msg := range msgs {
			observability.CommandStreamReclaimed.Inc()
			d.handleEntry(ctx, msg)
		}
		if next == "0-0" || next == "" {
			return
		}
		start = next
	}
}

// handleEntry processes one stream entry. It acknowledges the entry only
// once the command's outcome is durable in the ledger.
func (d *Dispatcher) handleEntry(ctx context.Context, msg redis.XMessage) {
	log := d.log.WithField("stream_id", msg.ID)

	tenantID, cmd, err := decodeEntry(msg)
	if err != nil {
		log.WithError(err).Error("Malformed command stream entry discarded")
		observability.CommandDispatchOutcomes.WithLabelValues("rejected").Inc()
		d.ackEntry(ctx, msg.ID)
		return
	}

	log = log.WithFields(logrus.Fields{
		"tenant_id":  tenantID,
		"command_id": cmd.CommandID,
		"machine_id": cmd.MachineID,
		"command":    cmd.Command,
	})

	lc, err := d.ledger.LoadForDispatch(ctx, tenantID, cmd.CommandID)
	if err != nil {
		// Transient: leave the entry pending; it is reclaimed later.
		log.WithError(err).Warn("Command ledger unavailable, dispatch deferred")
		return
	}
	if lc == nil {
		log.Error("Command not found in the issuing tenant's ledger, discarded")
		observability.CommandDispatchOutcomes.WithLabelValues("rejected").Inc()
		d.ackEntry(ctx, msg.ID)
		return
	}
	if lc.Status != StatusPending {
		// Already sent, acknowledged or final (redelivery after a crash,
		// or expired by the deadline sweep): never publish twice.
		log.WithField("status", lc.Status).Debug("Command no longer pending, entry acknowledged")
		observability.CommandDispatchOutcomes.WithLabelValues("duplicate").Inc()
		d.ackEntry(ctx, msg.ID)
		return
	}

	// Sparkplug-registered machines receive DCMD through the primary host
	// instead of the legacy {mqtt_topic}/cmd channel.
	if lc.SparkplugEdgeID != "" {
		d.dispatchSparkplug(ctx, log, msg.ID, tenantID, cmd, lc)
		return
	}

	var permanent string
	switch {
	case lc.MachineID != cmd.MachineID:
		permanent = "stream entry machine does not match the command ledger"
	case lc.MachineTopic == "":
		permanent = "machine has no MQTT topic configured"
	case lc.Attempts >= d.cfg.MaxAttempts:
		permanent = "dispatch attempts exhausted"
	}
	if permanent != "" {
		d.recordFailure(ctx, log, msg.ID, tenantID, cmd.CommandID, permanent, true)
		return
	}

	payload, err := json.Marshal(cmd.ToMQTTPayload())
	if err != nil {
		d.recordFailure(ctx, log, msg.ID, tenantID, cmd.CommandID, "failed to encode command payload", true)
		return
	}

	// The machine's topic comes from the ledger (machines.mqtt_topic), not
	// from the stream entry.
	topic := GetCommandTopic(lc.MachineTopic)
	if err := d.publisher.Publish(ctx, topic, payload); err != nil {
		d.recordFailure(ctx, log, msg.ID, tenantID, cmd.CommandID, "mqtt publish failed: "+err.Error(), false)
		return
	}

	sentAt := d.now()
	if err := d.ledger.MarkSent(ctx, tenantID, cmd.CommandID, sentAt, sentAt.Add(d.cfg.AckTimeout)); err != nil {
		// Leave the entry pending: it is redelivered and, because the
		// ledger still says pending, published again (at-least-once).
		log.WithError(err).Error("Command published but ledger update failed, redelivery pending")
		return
	}

	observability.CommandDispatchOutcomes.WithLabelValues("published").Inc()
	d.ackEntry(ctx, msg.ID)
	log.WithField("topic", topic).Info("Command dispatched to machine")
}

func (d *Dispatcher) recordFailure(ctx context.Context, log *logrus.Entry, entryID string, tenantID, commandID uuid.UUID, reason string, permanent bool) {
	failed, err := d.ledger.RecordDispatchFailure(ctx, DispatchFailure{
		TenantID:    tenantID,
		CommandID:   commandID,
		Error:       reason,
		MaxAttempts: d.cfg.MaxAttempts,
		Permanent:   permanent,
	})
	if err != nil {
		log.WithError(err).WithField("reason", reason).Error("Failed to record dispatch failure, retry pending")
		return
	}
	if failed {
		log.WithField("reason", reason).Error("Command failed")
		observability.CommandDispatchOutcomes.WithLabelValues("failed").Inc()
		d.ackEntry(ctx, entryID)
		return
	}
	log.WithField("reason", reason).Warn("Command dispatch attempt failed, will retry")
	observability.CommandDispatchOutcomes.WithLabelValues("retry").Inc()
}

func (d *Dispatcher) ackEntry(ctx context.Context, id string) {
	if err := d.redisClient.XAck(ctx, d.cfg.StreamKey, d.cfg.Group, id).Err(); err != nil {
		d.log.WithError(err).WithField("stream_id", id).Warn("Failed to acknowledge stream entry")
	}
}

// decodeEntry parses a stream entry into the issuing tenant and the command.
func decodeEntry(msg redis.XMessage) (uuid.UUID, *MachineCommand, error) {
	rawTenant, _ := msg.Values[StreamFieldTenantID].(string)
	tenantID, err := uuid.Parse(rawTenant)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("invalid tenant_id: %w", err)
	}
	rawPayload, _ := msg.Values[StreamFieldPayload].(string)
	var cmd MachineCommand
	if err := json.Unmarshal([]byte(rawPayload), &cmd); err != nil {
		return uuid.Nil, nil, fmt.Errorf("invalid payload: %w", err)
	}
	if cmd.CommandID == uuid.Nil || cmd.MachineID == uuid.Nil || cmd.Command == "" {
		return uuid.Nil, nil, fmt.Errorf("payload missing command_id, machine_id or command")
	}
	return tenantID, &cmd, nil
}

// Stop stops consuming and waits for the in-flight entry to finish.
func (d *Dispatcher) Stop() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	cancel := d.cancel
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	d.wg.Wait()
	d.log.Info("Command dispatcher stopped")
}

// HealthCheck verifies the stream is reachable.
func (d *Dispatcher) HealthCheck(ctx context.Context) error {
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return fmt.Errorf("dispatcher is closed")
	}
	if err := d.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis health check failed: %w", err)
	}
	return nil
}
