// Package command provides command dispatch and acknowledgment handling.
package command

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

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
)

// AckHandler handles command acknowledgments from machines via MQTT.
//
// An ack is bound to the machine its topic belongs to: it only changes a
// command that was issued to that machine, inside that machine's tenant.
type AckHandler struct {
	mqttClient mqtt.Client
	publisher  AckPublisher
	ledger     AckLedger
	hook       JobCompletionHook
	log        *logrus.Logger
	topicRoot  string
	mu         sync.RWMutex
	closed     bool
}

// NewAckHandler creates a new acknowledgment handler.
func NewAckHandler(mqttClient mqtt.Client, publisher AckPublisher, log *logrus.Logger, topicRoot string) *AckHandler {
	return &AckHandler{
		mqttClient: mqttClient,
		publisher:  publisher,
		log:        log,
		topicRoot:  topicRoot,
	}
}

// SetLedger sets the command ledger used to apply acks.
func (h *AckHandler) SetLedger(ledger AckLedger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ledger = ledger
}

// SetCompletionHook registers the hook called after each committed job
// completion (production genealogy / product passport recording).
func (h *AckHandler) SetCompletionHook(hook JobCompletionHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hook = hook
}

// Start subscribes to ack topics and begins processing.
func (h *AckHandler) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("ack handler is closed")
	}
	h.mu.Unlock()

	// Format: {tenant}/{site}/{area}/{line}/{machine}/ack
	ackTopic := buildAckTopic(h.topicRoot)

	token := h.mqttClient.Subscribe(ackTopic, 1, h.handleAckMessage)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("ack subscription timeout")
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to subscribe to ack topic: %w", err)
	}

	h.log.WithField("topic", ackTopic).Info("Ack handler subscribed to MQTT")
	return nil
}

// buildAckTopic constructs the ack topic pattern from the telemetry topic root.
func buildAckTopic(topicRoot string) string {
	// Input: "madfam/+/+/+/+/+" or "madfam/#"; output: "madfam/+/+/+/+/ack"
	if topicRoot == "" {
		return "+/+/+/+/+/ack"
	}

	parts := strings.Split(topicRoot, "/")
	tenant := parts[0]
	if tenant == "" || tenant == "+" || tenant == "#" {
		tenant = "+"
	}

	return fmt.Sprintf("%s/+/+/+/+/ack", tenant)
}

// handleAckMessage is the paho callback for ack topics.
func (h *AckHandler) handleAckMessage(_ mqtt.Client, msg mqtt.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h.ProcessAck(ctx, msg.Topic(), msg.Payload())
}

// ProcessAck applies one ack message received on topic. It returns the
// disposition for observability and tests.
func (h *AckHandler) ProcessAck(ctx context.Context, topic string, payload []byte) AckDisposition {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return ""
	}
	ledger := h.ledger
	publisher := h.publisher
	hook := h.hook
	h.mu.RUnlock()

	log := h.log.WithField("topic", topic)

	var ack CommandAck
	if err := json.Unmarshal(payload, &ack); err != nil {
		log.WithError(err).Debug("Failed to parse ack payload")
		observability.CommandAcks.WithLabelValues("invalid").Inc()
		return "invalid"
	}
	if ack.Timestamp.IsZero() {
		ack.Timestamp = time.Now().UTC()
	}

	commandID, err := uuid.Parse(ack.CommandID)
	if err != nil {
		log.WithField("command_id", ack.CommandID).Debug("Invalid command ID in ack")
		observability.CommandAcks.WithLabelValues("invalid").Inc()
		return "invalid"
	}

	topicBase, machineCode := splitAckTopic(topic)
	if machineCode == "" {
		log.Debug("Could not extract machine code from ack topic")
		observability.CommandAcks.WithLabelValues("invalid").Inc()
		return "invalid"
	}

	log = log.WithFields(logrus.Fields{
		"command_id":   commandID,
		"machine_code": machineCode,
		"success":      ack.Success,
	})

	if ledger == nil {
		log.Warn("Ack received but no command ledger is configured")
		return ""
	}

	machine, err := ledger.ResolveAckMachine(ctx, topicBase, machineCode)
	if err != nil {
		log.WithError(err).Warn("Failed to resolve ack machine")
		return ""
	}
	if machine == nil {
		log.Warn("Ack from an unknown machine topic dropped")
		observability.CommandAcks.WithLabelValues("unknown_machine").Inc()
		return "unknown_machine"
	}

	outcome, err := ledger.ApplyAck(ctx, AckApplication{
		CommandID:    commandID,
		Machine:      *machine,
		Success:      ack.Success,
		JobCompleted: ack.JobCompleted,
		Message:      ack.Message,
		AckedAt:      ack.Timestamp,
	})
	if err != nil {
		log.WithError(err).Error("Failed to apply command ack")
		return ""
	}

	observability.CommandAcks.WithLabelValues(string(outcome.Disposition)).Inc()

	switch outcome.Disposition {
	case AckApplied:
	case AckMachineMismatch:
		log.WithField("machine_id", machine.ID).Warn("Ack dropped: command was issued to a different machine")
		return outcome.Disposition
	case AckUnknownCommand:
		log.WithField("machine_id", machine.ID).Warn("Ack dropped: no such command for this machine's tenant")
		return outcome.Disposition
	default:
		log.WithField("disposition", outcome.Disposition).Info("Ack ignored")
		return outcome.Disposition
	}

	if outcome.Completion != nil && hook != nil {
		if err := hook.OnJobCompleted(ctx, *outcome.Completion); err != nil {
			observability.CommandCompletionHookErrors.Inc()
			log.WithError(err).Error("Job completion hook failed")
		}
	}

	// Real-time UI notification (best-effort; the ledger is the record).
	if publisher != nil {
		ackData := CommandAckData{
			CommandID: commandID,
			MachineID: machine.ID,
			Success:   ack.Success,
			Message:   ack.Message,
			AckedAt:   ack.Timestamp,
		}
		if err := publisher.PublishCommandAck(ctx, machine.TenantID, machine.ID, ackData); err != nil {
			log.WithError(err).Warn("Failed to publish command ack event")
		}
	}

	log.WithFields(logrus.Fields{
		"status":        outcome.Status,
		"job_completed": ack.JobCompleted,
	}).Info("Command acknowledgment processed")
	return outcome.Disposition
}

// splitAckTopic returns the machine's base topic and machine code from an
// ack topic of the form {tenant}/{site}/{area}/{line}/{machine}/ack.
func splitAckTopic(topic string) (base, code string) {
	base = strings.TrimSuffix(topic, AckTopicSuffix)
	parts := strings.Split(topic, "/")
	if len(parts) < 6 || base == topic {
		return "", ""
	}
	return base, parts[4]
}

// extractMachineCode extracts the machine code from an ack topic.
func extractMachineCode(topic string) string {
	_, code := splitAckTopic(topic)
	return code
}

// Stop gracefully shuts down the ack handler.
func (h *AckHandler) Stop() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.mu.Unlock()

	h.log.Info("Ack handler stopped")
}
