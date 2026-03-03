// Package manager coordinates machine adapters and MQTT communication.
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/mqtt"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// Adapter represents a connected machine protocol adapter.
type Adapter struct {
	MachineID   string `json:"machine_id"`
	MachineType string `json:"machine_type"`
	Protocol    string `json:"protocol"`
	Status      string `json:"status"` // connected, disconnected, error
	TenantID    string `json:"tenant_id"`
}

// CommandRequest represents an incoming machine command.
type CommandRequest struct {
	MachineID string                 `json:"machine_id"`
	Command   string                 `json:"command"`
	Params    map[string]interface{} `json:"params"`
}

// Manager coordinates machine adapters and MQTT communication.
type Manager struct {
	mqttClient *mqtt.Client
	registry   *registry.Registry
	adapters   map[string]*Adapter
	mu         sync.RWMutex
	log        *logrus.Logger
}

// NewManager creates a new adapter manager.
func NewManager(mqttClient *mqtt.Client, reg *registry.Registry, log *logrus.Logger) *Manager {
	return &Manager{
		mqttClient: mqttClient,
		registry:   reg,
		adapters:   make(map[string]*Adapter),
		log:        log,
	}
}

// Start begins listening for MQTT commands and telemetry.
func (m *Manager) Start(ctx context.Context) error {
	// Subscribe to command topics for all tenants
	if err := m.mqttClient.Subscribe("pravara/+/machines/+/command", 1, m.handleCommand); err != nil {
		return fmt.Errorf("failed to subscribe to command topic: %w", err)
	}

	m.log.Info("Adapter manager started, listening for commands")
	return nil
}

// ConnectMachine creates an adapter for a machine and establishes the connection.
func (m *Manager) ConnectMachine(machineID, machineType, protocol, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.adapters[machineID]; exists {
		return fmt.Errorf("machine %s is already connected", machineID)
	}

	adapter := &Adapter{
		MachineID:   machineID,
		MachineType: machineType,
		Protocol:    protocol,
		Status:      "connected",
		TenantID:    tenantID,
	}

	m.adapters[machineID] = adapter

	// Subscribe to telemetry for this specific machine
	telemetryTopic := fmt.Sprintf("pravara/%s/machines/%s/telemetry", tenantID, machineID)
	if err := m.mqttClient.Subscribe(telemetryTopic, 1, m.handleTelemetry); err != nil {
		m.log.WithError(err).WithField("machine_id", machineID).Error("Failed to subscribe to telemetry")
	}

	// Publish status update
	statusTopic := fmt.Sprintf("pravara/%s/machines/%s/status", tenantID, machineID)
	statusPayload, _ := json.Marshal(map[string]string{
		"machine_id": machineID,
		"status":     "connected",
	})
	if err := m.mqttClient.Publish(statusTopic, 1, statusPayload); err != nil {
		m.log.WithError(err).WithField("machine_id", machineID).Warn("Failed to publish status update")
	}

	m.log.WithFields(logrus.Fields{
		"machine_id": machineID,
		"type":       machineType,
		"protocol":   protocol,
	}).Info("Machine connected")

	return nil
}

// DisconnectMachine removes the adapter for a machine.
func (m *Manager) DisconnectMachine(machineID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	adapter, exists := m.adapters[machineID]
	if !exists {
		return fmt.Errorf("machine %s is not connected", machineID)
	}

	adapter.Status = "disconnected"
	delete(m.adapters, machineID)

	m.log.WithField("machine_id", machineID).Info("Machine disconnected")
	return nil
}

// GetStatus returns the status of a connected machine.
func (m *Manager) GetStatus(machineID string) (*Adapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapter, exists := m.adapters[machineID]
	if !exists {
		return nil, fmt.Errorf("machine %s is not connected", machineID)
	}

	return adapter, nil
}

// ListConnected returns all connected adapters.
func (m *Manager) ListConnected() []*Adapter {
	m.mu.RLock()
	defer m.mu.RUnlock()

	adapters := make([]*Adapter, 0, len(m.adapters))
	for _, a := range m.adapters {
		adapters = append(adapters, a)
	}
	return adapters
}

// handleCommand processes incoming MQTT command messages.
func (m *Manager) handleCommand(_ paho.Client, msg paho.Message) {
	var cmd CommandRequest
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		m.log.WithError(err).Debug("Failed to parse command payload")
		return
	}

	m.mu.RLock()
	adapter, exists := m.adapters[cmd.MachineID]
	m.mu.RUnlock()

	if !exists {
		m.log.WithField("machine_id", cmd.MachineID).Debug("Command received for unconnected machine")
		return
	}

	m.log.WithFields(logrus.Fields{
		"machine_id": adapter.MachineID,
		"command":    cmd.Command,
		"protocol":   adapter.Protocol,
	}).Info("Routing command to adapter")
}

// handleTelemetry processes incoming MQTT telemetry messages.
func (m *Manager) handleTelemetry(_ paho.Client, msg paho.Message) {
	m.log.WithField("topic", msg.Topic()).Debug("Telemetry received")
}

// Stop gracefully shuts down the manager and disconnects all machines.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id := range m.adapters {
		m.adapters[id].Status = "disconnected"
		delete(m.adapters, id)
	}

	m.log.Info("Adapter manager stopped")
}
