// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.bug.st/serial"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// GraphtecConnectionMode represents the connection method for Graphtec cutters.
type GraphtecConnectionMode string

const (
	GraphtecSerial   GraphtecConnectionMode = "serial"
	GraphtecEthernet GraphtecConnectionMode = "ethernet"
)

// GraphtecStatus represents the current state of a Graphtec vinyl cutter.
type GraphtecStatus struct {
	State      string    // idle, cutting, paused, error
	PositionX  float64   // Current X position in mm
	PositionY  float64   // Current Y position in mm
	Speed      int       // Current cutting speed
	Force      int       // Current cutting force (grams)
	ActivePen  int       // Currently selected pen/tool
	LastUpdate time.Time
}

// GraphtecAdapter handles communication with Graphtec vinyl cutters using the
// GP-GL (Graphtec Plotter Graphic Language) protocol over serial or Ethernet.
type GraphtecAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	mode       GraphtecConnectionMode
	status     GraphtecStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Serial connection
	serialPort serial.Port
	reader     *bufio.Reader

	// Ethernet connection
	tcpConn net.Conn

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewGraphtecAdapter creates a new Graphtec GP-GL adapter.
func NewGraphtecAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *GraphtecAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &GraphtecAdapter{
		log:        log.WithField("adapter", "graphtec"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		status: GraphtecStatus{
			State: "idle",
		},
	}
}

// Connect establishes a serial connection to the Graphtec cutter.
func (a *GraphtecAdapter) Connect(portName string, baudRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if baudRate == 0 {
		baudRate = 9600 // Graphtec default baud rate
	}

	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port %s: %w", portName, err)
	}

	if err := port.SetReadTimeout(2 * time.Second); err != nil {
		port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	a.serialPort = port
	a.reader = bufio.NewReader(port)
	a.mode = GraphtecSerial
	a.connected = true

	a.log.WithFields(logrus.Fields{
		"port":     portName,
		"baudRate": baudRate,
	}).Info("Connected to Graphtec cutter via serial")

	// Initialize the cutter with a home command.
	if err := a.sendGPGL("H"); err != nil {
		a.log.WithError(err).Warn("Failed to send initial home command")
	}

	return nil
}

// ConnectEthernet establishes a TCP connection to the Graphtec cutter.
func (a *GraphtecAdapter) ConnectEthernet(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	a.tcpConn = conn
	a.reader = bufio.NewReader(conn)
	a.mode = GraphtecEthernet
	a.connected = true

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to Graphtec cutter via Ethernet")

	// Initialize the cutter with a home command.
	if err := a.sendGPGL("H"); err != nil {
		a.log.WithError(err).Warn("Failed to send initial home command")
	}

	return nil
}

// Disconnect closes the connection to the cutter.
func (a *GraphtecAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false

	switch a.mode {
	case GraphtecSerial:
		if a.serialPort != nil {
			if err := a.serialPort.Close(); err != nil {
				return fmt.Errorf("failed to close serial port: %w", err)
			}
			a.serialPort = nil
		}
	case GraphtecEthernet:
		if a.tcpConn != nil {
			if err := a.tcpConn.Close(); err != nil {
				return fmt.Errorf("failed to close TCP connection: %w", err)
			}
			a.tcpConn = nil
		}
	}

	a.log.Info("Disconnected from Graphtec cutter")
	return nil
}

// IsConnected returns true if connected to the cutter.
func (a *GraphtecAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current cutter status.
func (a *GraphtecAdapter) GetStatus() GraphtecStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw GP-GL command string to the cutter (implements CommandExecutor).
func (a *GraphtecAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return a.sendGPGL(command)
}

// MapCommand translates high-level command names to GP-GL protocol commands.
// Supported commands: home, move, cut, pause, resume, set_speed, set_force, select_pen.
func (a *GraphtecAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		return nil, a.sendGPGL("H")

	case "move":
		x, y, err := a.extractXY(params)
		if err != nil {
			return nil, fmt.Errorf("move requires x,y parameters: %w", err)
		}
		return nil, a.sendGPGL(fmt.Sprintf("M %d,%d", x, y))

	case "cut":
		x, y, err := a.extractXY(params)
		if err != nil {
			return nil, fmt.Errorf("cut requires x,y parameters: %w", err)
		}
		return nil, a.sendGPGL(fmt.Sprintf("D %d,%d", x, y))

	case "pause":
		return nil, a.sendGPGL("!")

	case "resume":
		return nil, a.sendGPGL("&")

	case "set_speed":
		speed, err := a.extractInt(params, "speed")
		if err != nil {
			return nil, fmt.Errorf("set_speed requires speed parameter: %w", err)
		}
		if speed < 1 || speed > 60 {
			return nil, fmt.Errorf("speed must be between 1 and 60 cm/s, got %d", speed)
		}
		return nil, a.sendGPGL(fmt.Sprintf("VS %d", speed))

	case "set_force":
		force, err := a.extractInt(params, "force")
		if err != nil {
			return nil, fmt.Errorf("set_force requires force parameter: %w", err)
		}
		if force < 1 || force > 38 {
			return nil, fmt.Errorf("force must be between 1 and 38, got %d", force)
		}
		return nil, a.sendGPGL(fmt.Sprintf("FC %d", force))

	case "select_pen":
		pen, err := a.extractInt(params, "pen")
		if err != nil {
			return nil, fmt.Errorf("select_pen requires pen parameter: %w", err)
		}
		if pen < 0 || pen > 8 {
			return nil, fmt.Errorf("pen must be between 0 and 8, got %d", pen)
		}
		a.mu.Lock()
		a.status.ActivePen = pen
		a.mu.Unlock()
		return nil, a.sendGPGL(fmt.Sprintf("SP %d", pen))

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendGPGL sends a GP-GL command string followed by a semicolon terminator.
func (a *GraphtecAdapter) sendGPGL(cmd string) error {
	a.mu.RLock()
	mode := a.mode
	a.mu.RUnlock()

	// GP-GL commands are terminated with ETX (0x03) for serial.
	data := []byte(cmd + "\x03")

	switch mode {
	case GraphtecSerial:
		a.mu.RLock()
		port := a.serialPort
		a.mu.RUnlock()
		if port == nil {
			return fmt.Errorf("serial port not available")
		}
		if _, err := port.Write(data); err != nil {
			return fmt.Errorf("serial write failed: %w", err)
		}
	case GraphtecEthernet:
		a.mu.RLock()
		conn := a.tcpConn
		a.mu.RUnlock()
		if conn == nil {
			return fmt.Errorf("TCP connection not available")
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return fmt.Errorf("failed to set write deadline: %w", err)
		}
		if _, err := conn.Write(data); err != nil {
			return fmt.Errorf("TCP write failed: %w", err)
		}
	default:
		return fmt.Errorf("no connection established")
	}

	a.log.WithField("command", cmd).Debug("Sent GP-GL command")
	return nil
}

// extractXY extracts integer x,y coordinates from the parameter map.
func (a *GraphtecAdapter) extractXY(params map[string]interface{}) (int, int, error) {
	xVal, ok := params["x"]
	if !ok {
		return 0, 0, fmt.Errorf("missing parameter: x")
	}
	yVal, ok := params["y"]
	if !ok {
		return 0, 0, fmt.Errorf("missing parameter: y")
	}

	x, err := toInt(xVal)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid x value: %w", err)
	}
	y, err := toInt(yVal)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid y value: %w", err)
	}

	return x, y, nil
}

// extractInt extracts an integer parameter by name from the parameter map.
func (a *GraphtecAdapter) extractInt(params map[string]interface{}, key string) (int, error) {
	val, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("missing parameter: %s", key)
	}
	return toInt(val)
}

// toInt converts an interface{} value to an int, supporting float64 and string inputs.
func toInt(val interface{}) (int, error) {
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as int: %w", v, err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for int conversion", val)
	}
}

// toFloat64 converts an interface{} value to a float64.
func toFloat64(val interface{}) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse %q as float64: %w", v, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unsupported type %T for float64 conversion", val)
	}
}
