// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.bug.st/serial"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// RolandStatus represents the current state of a Roland CAMM-GL cutter.
type RolandStatus struct {
	State      string    // ready, cutting, error
	PositionX  float64   // Current X position in plotter units
	PositionY  float64   // Current Y position in plotter units
	LastUpdate time.Time
}

// RolandAdapter handles communication with Roland CAMM-GL vinyl cutters
// over a serial connection using the CAMM-GL III command language.
type RolandAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	port       serial.Port
	reader     *bufio.Reader
	status     RolandStatus
	connected  bool

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewRolandAdapter creates a new Roland CAMM-GL adapter.
func NewRolandAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *RolandAdapter {
	return &RolandAdapter{
		log:        log.WithField("adapter", "roland"),
		definition: definition,
	}
}

// Connect opens a serial connection to the Roland cutter.
func (a *RolandAdapter) Connect(portName string, baudRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if baudRate == 0 {
		baudRate = 9600 // Roland default baud rate
	}

	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	p, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("roland serial open failed: %w", err)
	}

	if err := p.SetReadTimeout(2 * time.Second); err != nil {
		p.Close()
		return fmt.Errorf("roland set read timeout failed: %w", err)
	}

	a.port = p
	a.reader = bufio.NewReader(p)
	a.connected = true
	a.status.State = "ready"

	a.log.WithFields(logrus.Fields{
		"port":     portName,
		"baudRate": baudRate,
	}).Info("Connected to Roland cutter")

	return nil
}

// Disconnect closes the serial connection.
func (a *RolandAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.connected = false
	if a.port != nil {
		if err := a.port.Close(); err != nil {
			return fmt.Errorf("roland serial close failed: %w", err)
		}
	}

	a.log.Info("Disconnected from Roland cutter")
	return nil
}

// IsConnected returns true if the serial port is open.
func (a *RolandAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns a snapshot of the current cutter status.
func (a *RolandAdapter) GetStatus() RolandStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw CAMM-GL command string to the cutter and reads
// the response if applicable. This implements the CommandExecutor interface.
func (a *RolandAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.port.SetReadTimeout(timeout); err != nil {
		return fmt.Errorf("roland set timeout failed: %w", err)
	}

	_, err := a.port.Write([]byte(command))
	if err != nil {
		return fmt.Errorf("roland write failed: %w", err)
	}

	a.log.WithField("command", strings.TrimSpace(command)).Debug("Sent CAMM-GL command")
	return nil
}

// MapCommand translates high-level command names to CAMM-GL III commands.
// Supported commands: init, move, cut, set_speed, set_force, select_pen,
// get_position, home.
func (a *RolandAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	if !a.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}

	switch command {
	case "init":
		return nil, a.SendCommand("IN;", 5*time.Second)

	case "move":
		x, y, err := a.extractXY(params)
		if err != nil {
			return nil, fmt.Errorf("move requires 'x' and 'y' parameters: %w", err)
		}
		cmd := fmt.Sprintf("PU %d,%d;", x, y)
		return nil, a.SendCommand(cmd, 10*time.Second)

	case "cut":
		x, y, err := a.extractXY(params)
		if err != nil {
			return nil, fmt.Errorf("cut requires 'x' and 'y' parameters: %w", err)
		}
		cmd := fmt.Sprintf("PD %d,%d;", x, y)
		return nil, a.SendCommand(cmd, 30*time.Second)

	case "set_speed":
		speed, ok := params["speed"]
		if !ok {
			return nil, fmt.Errorf("set_speed requires 'speed' parameter")
		}
		cmd := fmt.Sprintf("VS %v;", speed)
		return nil, a.SendCommand(cmd, 2*time.Second)

	case "set_force":
		force, ok := params["force"]
		if !ok {
			return nil, fmt.Errorf("set_force requires 'force' parameter")
		}
		cmd := fmt.Sprintf("FP %v;", force)
		return nil, a.SendCommand(cmd, 2*time.Second)

	case "select_pen":
		pen, ok := params["pen"]
		if !ok {
			return nil, fmt.Errorf("select_pen requires 'pen' parameter")
		}
		cmd := fmt.Sprintf("SP %v;", pen)
		return nil, a.SendCommand(cmd, 2*time.Second)

	case "get_position":
		pos, err := a.queryPosition()
		if err != nil {
			return nil, err
		}
		return pos, nil

	case "home":
		// Initialize then move to origin.
		if err := a.SendCommand("IN;", 5*time.Second); err != nil {
			return nil, fmt.Errorf("home init failed: %w", err)
		}
		return nil, a.SendCommand("PU 0,0;", 10*time.Second)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// extractXY parses integer x and y coordinates from the params map.
func (a *RolandAdapter) extractXY(params map[string]interface{}) (int, int, error) {
	xVal, ok := params["x"]
	if !ok {
		return 0, 0, fmt.Errorf("missing 'x' parameter")
	}
	yVal, ok := params["y"]
	if !ok {
		return 0, 0, fmt.Errorf("missing 'y' parameter")
	}

	x, err := toInt(xVal)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid 'x' value: %w", err)
	}
	y, err := toInt(yVal)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid 'y' value: %w", err)
	}

	return x, y, nil
}

// rolandPositionResponse holds the parsed OA; response.
type rolandPositionResponse struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// queryPosition sends OA; and parses the position response.
func (a *RolandAdapter) queryPosition() (*rolandPositionResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.port.SetReadTimeout(2 * time.Second); err != nil {
		return nil, fmt.Errorf("roland set timeout failed: %w", err)
	}

	_, err := a.port.Write([]byte("OA;"))
	if err != nil {
		return nil, fmt.Errorf("roland OA write failed: %w", err)
	}

	// OA; returns position as "x,y\r" (coordinates in plotter units).
	line, err := a.reader.ReadString('\r')
	if err != nil {
		return nil, fmt.Errorf("roland OA read failed: %w", err)
	}

	line = strings.TrimSpace(line)
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected OA response: %q", line)
	}

	x, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse X position: %w", err)
	}
	y, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Y position: %w", err)
	}

	// Update internal status.
	a.status.PositionX = x
	a.status.PositionY = y
	a.status.LastUpdate = time.Now()

	pos := &rolandPositionResponse{X: x, Y: y}

	// Emit telemetry.
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "position_x", Value: x, Unit: "plotter_units", Timestamp: now},
			{Type: "position_y", Value: y, Unit: "plotter_units", Timestamp: now},
		})
	}

	return pos, nil
}
