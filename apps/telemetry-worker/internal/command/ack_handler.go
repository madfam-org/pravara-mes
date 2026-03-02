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
)

// AckHandler handles command acknowledgments from machines via MQTT.
type AckHandler struct {
	mqttClient mqtt.Client
	publisher  AckPublisher
	store      AckStore
	log        *logrus.Logger
	topicRoot  string
	mu         sync.RWMutex
	closed     bool
}

// AckStore defines the interface for looking up command and machine information.
type AckStore interface {
	// GetMachineByCode retrieves machine info by its code.
	GetMachineByCode(ctx context.Context, code string) (*MachineInfo, error)
	// UpdateCommandStatus updates the status of a command.
	UpdateCommandStatus(ctx context.Context, commandID uuid.UUID, status string, message string) error
}

// MachineInfo contains the information needed to process acks.
type MachineInfo struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Code     string
	Name     string
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

// SetStore sets the store for machine lookups.
func (h *AckHandler) SetStore(store AckStore) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.store = store
}

// Start subscribes to ack topics and begins processing.
func (h *AckHandler) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("ack handler is closed")
	}
	h.mu.Unlock()

	// Subscribe to ack topics using wildcard
	// Format: {tenant}/{site}/{area}/{line}/{machine}/ack
	// The topic root is typically: madfam/+/+/+/+/+ (for telemetry)
	// We want: {tenant}/+/+/+/+/ack
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
	// Remove trailing wildcards and rebuild for acks
	// Input: "madfam/+/+/+/+/+" or "madfam/#"
	// Output: "madfam/+/+/+/+/ack"
	if topicRoot == "" {
		return "+/+/+/+/+/ack"
	}

	parts := strings.Split(topicRoot, "/")
	if len(parts) == 0 {
		return "+/+/+/+/+/ack"
	}

	// Take the first part (org/tenant) and build ack pattern
	tenant := parts[0]
	if tenant == "" || tenant == "+" || tenant == "#" {
		tenant = "+"
	}

	return fmt.Sprintf("%s/+/+/+/+/ack", tenant)
}

// handleAckMessage processes an acknowledgment message from a machine.
func (h *AckHandler) handleAckMessage(client mqtt.Client, msg mqtt.Message) {
	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return
	}
	store := h.store
	publisher := h.publisher
	h.mu.RUnlock()

	log := h.log.WithField("topic", msg.Topic())

	// Parse the ack payload
	var ack CommandAck
	if err := json.Unmarshal(msg.Payload(), &ack); err != nil {
		log.WithError(err).Debug("Failed to parse ack payload")
		return
	}

	// Set timestamp if not provided
	if ack.Timestamp.IsZero() {
		ack.Timestamp = time.Now().UTC()
	}

	// Parse command ID
	commandID, err := uuid.Parse(ack.CommandID)
	if err != nil {
		log.WithError(err).WithField("command_id", ack.CommandID).Debug("Invalid command ID in ack")
		return
	}

	log = log.WithFields(logrus.Fields{
		"command_id": commandID,
		"success":    ack.Success,
	})

	// Extract machine code from topic
	// Format: {tenant}/{site}/{area}/{line}/{machine}/ack
	machineCode := extractMachineCode(msg.Topic())
	if machineCode == "" {
		log.Debug("Could not extract machine code from topic")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Look up machine to get tenant ID
	var tenantID, machineID uuid.UUID
	if store != nil {
		machine, err := store.GetMachineByCode(ctx, machineCode)
		if err != nil {
			log.WithError(err).WithField("machine_code", machineCode).Warn("Failed to lookup machine")
			return
		}
		if machine != nil {
			tenantID = machine.TenantID
			machineID = machine.ID
		}
	}

	// Update command status if we have a store
	if store != nil {
		status := "acknowledged"
		if !ack.Success {
			status = "failed"
		}
		if err := store.UpdateCommandStatus(ctx, commandID, status, ack.Message); err != nil {
			log.WithError(err).Warn("Failed to update command status")
			// Continue to publish ack event anyway
		}
	}

	// Publish ack event to Centrifugo for real-time UI updates
	if publisher != nil && tenantID != uuid.Nil {
		ackData := CommandAckData{
			CommandID: commandID,
			MachineID: machineID,
			Success:   ack.Success,
			Message:   ack.Message,
			AckedAt:   ack.Timestamp,
		}

		if err := publisher.PublishCommandAck(ctx, tenantID, machineID, ackData); err != nil {
			log.WithError(err).Warn("Failed to publish command ack event")
			return
		}
	}

	log.WithField("machine_code", machineCode).Info("Command acknowledgment processed")
}

// extractMachineCode extracts the machine code from an ack topic.
// Expected format: {tenant}/{site}/{area}/{line}/{machine}/ack
func extractMachineCode(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) < 6 {
		return ""
	}
	// Machine code is at index 4 (5th element)
	return parts[4]
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
