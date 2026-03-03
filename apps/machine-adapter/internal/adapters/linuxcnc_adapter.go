// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// LinuxCNCStatus represents the current state of a LinuxCNC machine.
type LinuxCNCStatus struct {
	State         string    // idle, running, paused, estop, off
	Mode          string    // manual, auto, mdi
	InterpState   string    // idle, reading, paused, waiting
	PositionX     float64   // Commanded X position in mm
	PositionY     float64   // Commanded Y position in mm
	PositionZ     float64   // Commanded Z position in mm
	FeedRate      float64   // Current feed rate in mm/min
	SpindleSpeed  float64   // Current spindle speed in RPM
	SpindleOn     bool      // Whether spindle is running
	HomedAxes     [3]bool   // Whether X, Y, Z axes are homed
	EStop         bool      // Emergency stop active
	MachineOn     bool      // Machine power state
	LastUpdate    time.Time
}

// LinuxCNCAdapter handles communication with LinuxCNC via the linuxcncrsh
// remote shell interface over TCP. The protocol is text-based: send a command
// line and receive a text response.
type LinuxCNCAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	conn       net.Conn
	reader     *bufio.Reader
	status     LinuxCNCStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewLinuxCNCAdapter creates a new LinuxCNC adapter.
func NewLinuxCNCAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *LinuxCNCAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &LinuxCNCAdapter{
		log:        log.WithField("adapter", "linuxcnc"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		status: LinuxCNCStatus{
			State: "off",
		},
	}
}

// Connect establishes a TCP connection to the linuxcncrsh interface.
func (a *LinuxCNCAdapter) Connect(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if port == 0 {
		port = 5007 // Default linuxcncrsh port
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to LinuxCNC at %s: %w", addr, err)
	}

	a.conn = conn
	a.reader = bufio.NewReader(conn)
	a.connected = true

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to LinuxCNC via linuxcncrsh")

	// Read the initial greeting/prompt from linuxcncrsh.
	if err := a.readResponseLocked(); err != nil {
		a.log.WithError(err).Debug("No greeting received from linuxcncrsh")
	}

	// Set hello and enable command echo.
	if err := a.sendCommandLocked("hello EMC user 1.0"); err != nil {
		a.log.WithError(err).Warn("Failed to send hello handshake")
	}

	// Enable machine.
	if err := a.sendCommandLocked("set enable EMCTOO"); err != nil {
		a.log.WithError(err).Debug("Failed to enable control")
	}

	// Start background status polling.
	go a.statusLoop()

	return nil
}

// Disconnect closes the TCP connection.
func (a *LinuxCNCAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	// Send quit command before closing.
	_ = a.sendCommandLocked("quit")

	a.cancel()
	a.connected = false

	if a.conn != nil {
		if err := a.conn.Close(); err != nil {
			return fmt.Errorf("failed to close LinuxCNC connection: %w", err)
		}
		a.conn = nil
	}

	a.log.Info("Disconnected from LinuxCNC")
	return nil
}

// IsConnected returns true if connected to LinuxCNC.
func (a *LinuxCNCAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current machine status.
func (a *LinuxCNCAdapter) GetStatus() LinuxCNCStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw linuxcncrsh command (implements CommandExecutor).
func (a *LinuxCNCAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	return a.sendCommandLocked(command)
}

// MapCommand translates high-level command names to linuxcncrsh commands.
// Supported commands: home, gcode_line, pause, resume, stop, emergency_stop, get_status.
func (a *LinuxCNCAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		axis := 0 // Default to axis 0 (X)
		if axisVal, ok := params["axis"]; ok {
			a, err := toInt(axisVal)
			if err == nil {
				axis = a
			}
		}
		// Sequence: set mode manual, then home the axis.
		cmds := []string{
			"set mode manual",
			fmt.Sprintf("set home %d", axis),
		}
		for _, cmd := range cmds {
			if err := a.sendLockedCommand(cmd); err != nil {
				return nil, fmt.Errorf("home axis %d failed at '%s': %w", axis, cmd, err)
			}
		}
		return map[string]interface{}{"homed_axis": axis}, nil

	case "gcode_line":
		gcodeVal, ok := params["gcode"]
		if !ok {
			return nil, fmt.Errorf("gcode_line requires gcode parameter")
		}
		gcode, ok := gcodeVal.(string)
		if !ok {
			return nil, fmt.Errorf("gcode must be a string")
		}
		// Switch to MDI mode and send the G-code line.
		cmds := []string{
			"set mode mdi",
			fmt.Sprintf("set mdi %s", gcode),
		}
		for _, cmd := range cmds {
			if err := a.sendLockedCommand(cmd); err != nil {
				return nil, fmt.Errorf("gcode_line failed at '%s': %w", cmd, err)
			}
		}
		return nil, nil

	case "pause":
		return nil, a.sendLockedCommand("set pause")

	case "resume":
		return nil, a.sendLockedCommand("set resume")

	case "stop":
		return nil, a.sendLockedCommand("set abort")

	case "emergency_stop":
		if err := a.sendLockedCommand("set estop on"); err != nil {
			return nil, err
		}
		a.mu.Lock()
		a.status.EStop = true
		a.status.State = "estop"
		a.mu.Unlock()
		return nil, nil

	case "clear_estop":
		cmds := []string{
			"set estop off",
			"set machine on",
		}
		for _, cmd := range cmds {
			if err := a.sendLockedCommand(cmd); err != nil {
				return nil, fmt.Errorf("clear_estop failed at '%s': %w", cmd, err)
			}
		}
		a.mu.Lock()
		a.status.EStop = false
		a.status.MachineOn = true
		a.mu.Unlock()
		return nil, nil

	case "get_status":
		return a.queryFullStatus()

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendLockedCommand acquires the lock and sends a command.
func (a *LinuxCNCAdapter) sendLockedCommand(cmd string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sendCommandLocked(cmd)
}

// sendCommandLocked sends a command string to linuxcncrsh. Caller must hold the lock.
func (a *LinuxCNCAdapter) sendCommandLocked(cmd string) error {
	if a.conn == nil {
		return fmt.Errorf("not connected")
	}

	data := []byte(cmd + "\r\n")
	if _, err := a.conn.Write(data); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	a.log.WithField("command", cmd).Debug("Sent linuxcncrsh command")

	// Read response.
	if err := a.readResponseLocked(); err != nil {
		return fmt.Errorf("response error for '%s': %w", cmd, err)
	}

	return nil
}

// readResponseLocked reads a line response from linuxcncrsh. Caller must hold the lock.
func (a *LinuxCNCAdapter) readResponseLocked() error {
	if a.reader == nil {
		return fmt.Errorf("reader not initialized")
	}

	if err := a.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	line, err := a.reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read failed: %w", err)
	}

	line = strings.TrimSpace(line)
	a.log.WithField("response", line).Debug("LinuxCNC response")

	// Check for error responses.
	if strings.HasPrefix(line, "NAK") || strings.HasPrefix(line, "ERROR") {
		return fmt.Errorf("linuxcncrsh error: %s", line)
	}

	return nil
}

// queryFullStatus queries all status fields from linuxcncrsh.
func (a *LinuxCNCAdapter) queryFullStatus() (interface{}, error) {
	status := a.GetStatus()
	return map[string]interface{}{
		"state":        status.State,
		"mode":         status.Mode,
		"interp_state": status.InterpState,
		"position": map[string]float64{
			"x": status.PositionX,
			"y": status.PositionY,
			"z": status.PositionZ,
		},
		"feed_rate":    status.FeedRate,
		"spindle_speed": status.SpindleSpeed,
		"spindle_on":   status.SpindleOn,
		"estop":        status.EStop,
		"machine_on":   status.MachineOn,
	}, nil
}

// statusLoop periodically polls LinuxCNC for status.
func (a *LinuxCNCAdapter) statusLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.IsConnected() {
				continue
			}
			a.pollStatus()
		}
	}
}

// pollStatus queries the current machine state from linuxcncrsh.
func (a *LinuxCNCAdapter) pollStatus() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn == nil {
		return
	}

	now := time.Now()

	// Query task state.
	if _, err := a.conn.Write([]byte("get estop\r\n")); err != nil {
		a.log.WithError(err).Debug("Failed to query estop")
		return
	}
	if err := a.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return
	}
	line, err := a.reader.ReadString('\n')
	if err != nil {
		a.log.WithError(err).Debug("Failed to read estop response")
		return
	}
	a.status.EStop = strings.Contains(strings.ToLower(line), "on")

	// Query machine mode.
	if _, err := a.conn.Write([]byte("get mode\r\n")); err == nil {
		if err := a.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err == nil {
			if modeLine, err := a.reader.ReadString('\n'); err == nil {
				modeLine = strings.TrimSpace(strings.ToLower(modeLine))
				if strings.Contains(modeLine, "manual") {
					a.status.Mode = "manual"
				} else if strings.Contains(modeLine, "auto") {
					a.status.Mode = "auto"
				} else if strings.Contains(modeLine, "mdi") {
					a.status.Mode = "mdi"
				}
			}
		}
	}

	// Determine state from estop and machine status.
	if a.status.EStop {
		a.status.State = "estop"
	} else if !a.status.MachineOn {
		a.status.State = "off"
	} else {
		a.status.State = "idle"
	}

	a.status.LastUpdate = now

	// Publish telemetry.
	if a.OnTelemetry != nil {
		ts := now.UTC().Format(time.RFC3339Nano)
		estopVal := 0.0
		if a.status.EStop {
			estopVal = 1.0
		}
		spindleVal := 0.0
		if a.status.SpindleOn {
			spindleVal = 1.0
		}
		a.OnTelemetry([]TelemetryMetric{
			{Type: "position_x", Value: a.status.PositionX, Unit: "mm", Timestamp: ts},
			{Type: "position_y", Value: a.status.PositionY, Unit: "mm", Timestamp: ts},
			{Type: "position_z", Value: a.status.PositionZ, Unit: "mm", Timestamp: ts},
			{Type: "feed_rate", Value: a.status.FeedRate, Unit: "mm/min", Timestamp: ts},
			{Type: "spindle_speed", Value: a.status.SpindleSpeed, Unit: "rpm", Timestamp: ts},
			{Type: "spindle_on", Value: spindleVal, Unit: "bool", Timestamp: ts},
			{Type: "estop", Value: estopVal, Unit: "bool", Timestamp: ts},
		})
	}
}
