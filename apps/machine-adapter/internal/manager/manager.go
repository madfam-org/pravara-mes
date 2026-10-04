// Package manager keeps the protocol adapters of the machines connected to
// this edge node. Every connected machine gets a protocol adapter as its
// Executor; commands are executed through it.
package manager

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// TelemetryMetric represents a single telemetry data point.
type TelemetryMetric struct {
	Type      string  `json:"type"` // "position_x", "temperature_extruder", etc.
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp string  `json:"timestamp"`
}

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

// disconnecter is implemented by adapters that hold a connection.
type disconnecter interface {
	Disconnect() error
}

// ConnectionParams says how to reach a machine on the site network.
type ConnectionParams struct {
	Host string
	Port int
	// Secret is the protocol credential: the Moonraker API key or the Bambu LAN access code.
	Secret string
	// Serial is the Bambu printer serial used in its local MQTT topics.
	Serial string
	// TLSPinSHA256 optionally pins a self-signed printer certificate (Bambu).
	TLSPinSHA256 string
	// FTPSPort overrides the Bambu FTPS port (990).
	FTPSPort int
}

// MachineSpec describes a machine to connect.
type MachineSpec struct {
	MachineID    string // pravara machine code (Sparkplug device_id)
	DefinitionID string // registry definition id, e.g. "voron_2_4"
	Protocol     string // overrides the definition's protocol when set
	TenantID     string
	Conn         ConnectionParams
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

// ExecutorFactory builds a protocol adapter for spec and connects it.
type ExecutorFactory func(ctx context.Context, spec MachineSpec, def *registry.MachineDefinition, log *logrus.Logger) (CommandExecutor, error)

// Manager tracks connected machines and their executors.
type Manager struct {
	registry *registry.Registry
	factory  ExecutorFactory
	adapters map[string]*Adapter
	mu       sync.RWMutex
	log      *logrus.Logger
}

// NewManager creates a manager. factory may be nil to use DefaultExecutorFactory.
func NewManager(reg *registry.Registry, factory ExecutorFactory, log *logrus.Logger) *Manager {
	if factory == nil {
		factory = DefaultExecutorFactory
	}
	return &Manager{
		registry: reg,
		factory:  factory,
		adapters: make(map[string]*Adapter),
		log:      log,
	}
}

// ConnectMachine builds the protocol adapter for a machine, connects it and
// wires it as the machine's Executor.
func (m *Manager) ConnectMachine(ctx context.Context, spec MachineSpec) (*Adapter, error) {
	if spec.MachineID == "" {
		return nil, fmt.Errorf("machine id is required")
	}
	m.mu.RLock()
	_, exists := m.adapters[spec.MachineID]
	m.mu.RUnlock()
	if exists {
		return nil, fmt.Errorf("machine %s is already connected", spec.MachineID)
	}

	var def *registry.MachineDefinition
	if spec.DefinitionID != "" {
		d, ok := m.registry.GetDefinition(spec.DefinitionID)
		if !ok {
			return nil, fmt.Errorf("unknown machine definition %q", spec.DefinitionID)
		}
		def = d
	}
	protocol := spec.Protocol
	if protocol == "" && def != nil {
		protocol = string(def.Protocol)
	}
	if protocol == "" {
		return nil, fmt.Errorf("machine %s: protocol is required", spec.MachineID)
	}
	spec.Protocol = protocol

	exec, err := m.factory(ctx, spec, def, m.log)
	if err != nil {
		return nil, fmt.Errorf("connect %s (%s): %w", spec.MachineID, protocol, err)
	}

	adapter := &Adapter{
		MachineID:   spec.MachineID,
		MachineType: spec.DefinitionID,
		Protocol:    protocol,
		Status:      "connected",
		TenantID:    spec.TenantID,
		Executor:    exec,
	}
	m.mu.Lock()
	if _, raced := m.adapters[spec.MachineID]; raced {
		m.mu.Unlock()
		disconnect(exec)
		return nil, fmt.Errorf("machine %s is already connected", spec.MachineID)
	}
	m.adapters[spec.MachineID] = adapter
	m.mu.Unlock()

	m.log.WithFields(logrus.Fields{
		"machine_id": spec.MachineID,
		"definition": spec.DefinitionID,
		"protocol":   protocol,
	}).Info("Machine connected")
	return adapter, nil
}

// DisconnectMachine closes and removes a machine's adapter.
func (m *Manager) DisconnectMachine(machineID string) error {
	m.mu.Lock()
	adapter, exists := m.adapters[machineID]
	if exists {
		delete(m.adapters, machineID)
	}
	m.mu.Unlock()
	if !exists {
		return fmt.Errorf("machine %s is not connected", machineID)
	}
	adapter.Status = "disconnected"
	disconnect(adapter.Executor)
	m.log.WithField("machine_id", machineID).Info("Machine disconnected")
	return nil
}

func disconnect(exec CommandExecutor) {
	if d, ok := exec.(disconnecter); ok {
		_ = d.Disconnect()
	}
}

// GetStatus returns a copy of a connected machine's adapter record.
func (m *Manager) GetStatus(machineID string) (*Adapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adapter, exists := m.adapters[machineID]
	if !exists {
		return nil, fmt.Errorf("machine %s is not connected", machineID)
	}
	cp := *adapter
	return &cp, nil
}

// Executor returns the executor wired for a connected machine.
func (m *Manager) Executor(machineID string) (CommandExecutor, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adapter, exists := m.adapters[machineID]
	if !exists || adapter.Executor == nil {
		return nil, false
	}
	return adapter.Executor, true
}

// ListConnected returns copies of all connected adapter records.
func (m *Manager) ListConnected() []Adapter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Adapter, 0, len(m.adapters))
	for _, a := range m.adapters {
		out = append(out, *a)
	}
	return out
}

// Execute runs a high-level command on a machine through its Executor:
// protocol-aware adapters translate it themselves (MapCommand); serial
// G-code adapters get the G-code mapping below.
func (m *Manager) Execute(machineID, command string, params map[string]interface{}) error {
	exec, ok := m.Executor(machineID)
	if !ok {
		return fmt.Errorf("machine %s is not connected", machineID)
	}
	if ma, ok := exec.(MachineAdapter); ok {
		_, err := ma.MapCommand(command, params)
		return err
	}
	gcode, timeout := mapCommandToGCode(command, params)
	if gcode == "" {
		return fmt.Errorf("unknown command %q", command)
	}
	return exec.SendCommand(gcode, timeout)
}

// Stop disconnects all machines.
func (m *Manager) Stop() {
	m.mu.Lock()
	adapters := m.adapters
	m.adapters = make(map[string]*Adapter)
	m.mu.Unlock()
	for _, a := range adapters {
		a.Status = "disconnected"
		disconnect(a.Executor)
	}
	m.log.Info("Adapter manager stopped")
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
