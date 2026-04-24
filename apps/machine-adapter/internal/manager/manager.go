// Package manager coordinates machine adapters and MQTT communication.
package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/mqtt"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// TelemetryMetric represents a single telemetry data point.
type TelemetryMetric struct {
	Type      string  `json:"type"` // "position_x", "temperature_extruder", etc.
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp string  `json:"timestamp"`
}

// TelemetryCallback is invoked by adapters when new telemetry data is available.
type TelemetryCallback func(metrics []TelemetryMetric)

// CommandExecutor is implemented by protocol adapters to execute machine commands.
type CommandExecutor interface {
	SendCommand(command string, timeout time.Duration) error
}

// MachineAdapter extends CommandExecutor for non-G-code adapters (MQTT JSON, REST, UDP).
// Adapters implementing this interface handle protocol-aware command translation internally.
type MachineAdapter interface {
	CommandExecutor
	// MapCommand translates a high-level command name and params into a protocol-specific
	// payload and executes it. Returns a response map or error.
	MapCommand(command string, params map[string]interface{}) (interface{}, error)
}

// CommandResponse represents the result of a command execution.
type CommandResponse struct {
	MachineID string `json:"machine_id"`
	Command   string `json:"command"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Adapter represents a connected machine protocol adapter.
type Adapter struct {
	MachineID   string          `json:"machine_id"`
	MachineType string          `json:"machine_type"`
	Protocol    string          `json:"protocol"`
	Status      string          `json:"status"` // connected, disconnected, error
	TenantID    string          `json:"tenant_id"`
	Executor    CommandExecutor `json:"-"`
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

// PublishTelemetry publishes telemetry metrics to MQTT for a specific machine.
func (m *Manager) PublishTelemetry(tenantID, machineID string, metrics []TelemetryMetric) {
	topic := fmt.Sprintf("pravara/%s/machines/%s/telemetry", tenantID, machineID)

	payload, err := json.Marshal(map[string]interface{}{
		"machine_id": machineID,
		"metrics":    metrics,
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		m.log.WithError(err).Error("Failed to marshal telemetry payload")
		return
	}

	if err := m.mqttClient.Publish(topic, 0, payload); err != nil {
		m.log.WithError(err).WithField("machine_id", machineID).Warn("Failed to publish telemetry")
	}
}

// MakeTelemetryCallback creates a TelemetryCallback bound to a specific tenant and machine.
func (m *Manager) MakeTelemetryCallback(tenantID, machineID string) TelemetryCallback {
	return func(metrics []TelemetryMetric) {
		m.PublishTelemetry(tenantID, machineID, metrics)
	}
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

	if adapter.Executor == nil {
		m.log.WithField("machine_id", cmd.MachineID).Warn("No executor available for machine")
		m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, false, "no executor available")
		return
	}

	// Use protocol-aware MapCommand for adapters that implement MachineAdapter,
	// fall back to G-code mapping for serial adapters (GRBL, Marlin).
	if ma, ok := adapter.Executor.(MachineAdapter); ok {
		if _, err := ma.MapCommand(cmd.Command, cmd.Params); err != nil {
			m.log.WithError(err).WithFields(logrus.Fields{
				"machine_id": cmd.MachineID,
				"command":    cmd.Command,
			}).Error("Command execution failed")
			m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, false, err.Error())
			return
		}
		m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, true, "")
		return
	}

	gcode, timeout := mapCommandToGCode(cmd.Command, cmd.Params)
	if gcode == "" {
		m.log.WithField("command", cmd.Command).Warn("Unknown command, cannot map to G-code")
		m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, false, "unknown command")
		return
	}

	if err := adapter.Executor.SendCommand(gcode, timeout); err != nil {
		m.log.WithError(err).WithFields(logrus.Fields{
			"machine_id": cmd.MachineID,
			"command":    cmd.Command,
			"gcode":      gcode,
		}).Error("Command execution failed")
		m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, false, err.Error())
		return
	}

	m.publishCommandResponse(adapter.TenantID, cmd.MachineID, cmd.Command, true, "")
}

// publishCommandResponse sends a command ACK/NACK to MQTT.
func (m *Manager) publishCommandResponse(tenantID, machineID, command string, success bool, errMsg string) {
	topic := fmt.Sprintf("pravara/%s/machines/%s/command/response", tenantID, machineID)

	resp := CommandResponse{
		MachineID: machineID,
		Command:   command,
		Success:   success,
	}
	if success {
		resp.Message = "ok"
	} else {
		resp.Error = errMsg
	}

	payload, _ := json.Marshal(resp)
	if err := m.mqttClient.Publish(topic, 1, payload); err != nil {
		m.log.WithError(err).Warn("Failed to publish command response")
	}
}

// mapCommandToGCode maps a high-level command name to G-code and timeout.
func mapCommandToGCode(command string, params map[string]interface{}) (string, time.Duration) {
	switch command {
	case "home":
		return "G28", 60 * time.Second
	case "pause":
		return "M25", 5 * time.Second
	case "resume":
		return "M24", 5 * time.Second
	case "stop":
		return "M524", 5 * time.Second
	case "emergency_stop":
		return "M112", 1 * time.Second
	case "preheat":
		temp := 200.0
		if t, ok := params["temperature"]; ok {
			if tf, ok := t.(float64); ok {
				temp = tf
			}
		}
		return fmt.Sprintf("M104 S%.0f", temp), 5 * time.Second
	case "preheat_bed":
		temp := 60.0
		if t, ok := params["temperature"]; ok {
			if tf, ok := t.(float64); ok {
				temp = tf
			}
		}
		return fmt.Sprintf("M140 S%.0f", temp), 5 * time.Second
	case "cooldown":
		return "M104 S0\nM140 S0", 5 * time.Second
	case "get_position":
		return "M114", 2 * time.Second
	case "get_temperature":
		return "M105", 2 * time.Second
	case "auto_level":
		return "G29", 120 * time.Second
	default:
		return "", 0
	}
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
