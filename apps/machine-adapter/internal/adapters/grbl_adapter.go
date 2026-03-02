// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.bug.st/serial"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// GRBLStatus represents the current state of a GRBL machine.
type GRBLStatus struct {
	State      string    // Idle, Run, Hold, Jog, Alarm, Door, Check, Home, Sleep
	MachinePos [3]float64 // Machine position X, Y, Z
	WorkPos    [3]float64 // Work position X, Y, Z
	FeedRate   float64   // Current feed rate
	Spindle    float64   // Spindle speed
	Override   struct {
		Feed    int // Feed override percentage
		Rapids  int // Rapids override percentage
		Spindle int // Spindle override percentage
	}
	Accessories struct {
		SpindleOn bool
		FloodOn   bool
		MistOn    bool
	}
	LastUpdate time.Time
}

// GRBLAdapter handles communication with GRBL-based machines.
type GRBLAdapter struct {
	mu            sync.RWMutex
	log           *logrus.Entry
	definition    *registry.MachineDefinition
	port          serial.Port
	reader        *bufio.Reader
	writer        io.Writer
	status        GRBLStatus
	connected     bool
	commandQueue  chan CommandRequest
	responseQueue chan CommandResponse
	ctx           context.Context
	cancel        context.CancelFunc
}

// CommandRequest represents a command to send to the machine.
type CommandRequest struct {
	Command  string
	Response chan CommandResponse
	Timeout  time.Duration
}

// CommandResponse represents the machine's response to a command.
type CommandResponse struct {
	Success bool
	Message string
	Error   error
}

// NewGRBLAdapter creates a new GRBL adapter.
func NewGRBLAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *GRBLAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &GRBLAdapter{
		log:           log.WithField("adapter", "grbl"),
		definition:    definition,
		commandQueue:  make(chan CommandRequest, 100),
		responseQueue: make(chan CommandResponse, 100),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Connect establishes a connection to the GRBL machine.
func (a *GRBLAdapter) Connect(portName string, baudRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	// Configure serial port
	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	// Open serial port
	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port %s: %w", portName, err)
	}

	a.port = port
	a.reader = bufio.NewReader(port)
	a.writer = port

	// Set timeouts
	if err := port.SetReadTimeout(100 * time.Millisecond); err != nil {
		port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	a.connected = true
	a.log.WithFields(logrus.Fields{
		"port":     portName,
		"baudrate": baudRate,
	}).Info("Connected to GRBL machine")

	// Start background workers
	go a.readLoop()
	go a.commandLoop()
	go a.statusLoop()

	// Send initial commands
	time.Sleep(2 * time.Second) // Wait for GRBL to initialize

	// Request status
	if err := a.SendCommand("?", 1*time.Second); err != nil {
		a.log.WithError(err).Warn("Failed to get initial status")
	}

	// Get version
	if err := a.SendCommand("$I", 1*time.Second); err != nil {
		a.log.WithError(err).Warn("Failed to get version info")
	}

	return nil
}

// Disconnect closes the connection to the machine.
func (a *GRBLAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel() // Stop background workers
	a.connected = false

	if a.port != nil {
		if err := a.port.Close(); err != nil {
			return fmt.Errorf("failed to close port: %w", err)
		}
		a.port = nil
	}

	a.log.Info("Disconnected from GRBL machine")
	return nil
}

// SendCommand sends a command to the machine and waits for response.
func (a *GRBLAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	respChan := make(chan CommandResponse, 1)
	req := CommandRequest{
		Command:  command,
		Response: respChan,
		Timeout:  timeout,
	}

	select {
	case a.commandQueue <- req:
		// Command queued
	case <-time.After(timeout):
		return fmt.Errorf("command queue timeout")
	}

	select {
	case resp := <-respChan:
		if !resp.Success {
			return resp.Error
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("command response timeout")
	}
}

// SendGCode sends G-code to the machine.
func (a *GRBLAdapter) SendGCode(gcode string) error {
	lines := strings.Split(gcode, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "(") || strings.HasPrefix(line, ";") {
			continue // Skip empty lines and comments
		}

		if err := a.SendCommand(line, 30*time.Second); err != nil {
			return fmt.Errorf("failed to send line %q: %w", line, err)
		}
	}
	return nil
}

// GetStatus returns the current machine status.
func (a *GRBLAdapter) GetStatus() GRBLStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// IsConnected returns true if connected to the machine.
func (a *GRBLAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// Home performs a homing cycle.
func (a *GRBLAdapter) Home() error {
	return a.SendCommand("$H", 60*time.Second)
}

// Pause pauses the current operation.
func (a *GRBLAdapter) Pause() error {
	return a.SendCommand("!", 1*time.Second)
}

// Resume resumes the paused operation.
func (a *GRBLAdapter) Resume() error {
	return a.SendCommand("~", 1*time.Second)
}

// Stop performs an emergency stop.
func (a *GRBLAdapter) Stop() error {
	// Send Ctrl-X (0x18) for immediate stop
	if _, err := a.writer.Write([]byte{0x18}); err != nil {
		return fmt.Errorf("failed to send stop command: %w", err)
	}
	return nil
}

// Reset performs a soft reset.
func (a *GRBLAdapter) Reset() error {
	// Send Ctrl-X (0x18) for soft reset
	if _, err := a.writer.Write([]byte{0x18}); err != nil {
		return fmt.Errorf("failed to send reset command: %w", err)
	}
	time.Sleep(2 * time.Second) // Wait for GRBL to reset
	return nil
}

// readLoop continuously reads from the serial port.
func (a *GRBLAdapter) readLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
			line, err := a.reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					a.log.WithError(err).Debug("Read error")
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			a.log.WithField("line", line).Debug("Received")

			// Process the line
			a.processResponse(line)
		}
	}
}

// commandLoop processes queued commands.
func (a *GRBLAdapter) commandLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case req := <-a.commandQueue:
			// Send command
			cmd := req.Command + "\n"
			if _, err := a.writer.Write([]byte(cmd)); err != nil {
				req.Response <- CommandResponse{
					Success: false,
					Error:   fmt.Errorf("write error: %w", err),
				}
				continue
			}

			a.log.WithField("command", req.Command).Debug("Sent")

			// Wait for response with timeout
			go func() {
				timer := time.NewTimer(req.Timeout)
				defer timer.Stop()

				select {
				case resp := <-a.responseQueue:
					req.Response <- resp
				case <-timer.C:
					req.Response <- CommandResponse{
						Success: false,
						Error:   fmt.Errorf("response timeout"),
					}
				}
			}()
		}
	}
}

// statusLoop periodically requests machine status.
func (a *GRBLAdapter) statusLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.IsConnected() {
				// Send status request (?)
				if _, err := a.writer.Write([]byte("?")); err != nil {
					a.log.WithError(err).Debug("Failed to request status")
				}
			}
		}
	}
}

// processResponse processes responses from the machine.
func (a *GRBLAdapter) processResponse(line string) {
	// Check for status report
	if strings.HasPrefix(line, "<") && strings.HasSuffix(line, ">") {
		a.parseStatus(line)
		return
	}

	// Check for standard responses
	switch line {
	case "ok":
		a.responseQueue <- CommandResponse{Success: true, Message: "ok"}
	case "error":
		a.responseQueue <- CommandResponse{Success: false, Error: fmt.Errorf("GRBL error")}
	default:
		// Check for error messages
		if strings.HasPrefix(line, "error:") {
			a.responseQueue <- CommandResponse{
				Success: false,
				Error:   fmt.Errorf("GRBL error: %s", line[6:]),
			}
		} else if strings.HasPrefix(line, "ALARM:") {
			a.responseQueue <- CommandResponse{
				Success: false,
				Error:   fmt.Errorf("GRBL alarm: %s", line[6:]),
			}
		} else {
			// Other messages (version info, settings, etc.)
			a.log.WithField("message", line).Info("GRBL message")
			a.responseQueue <- CommandResponse{Success: true, Message: line}
		}
	}
}

// parseStatus parses a GRBL status report.
func (a *GRBLAdapter) parseStatus(line string) {
	// Remove < and >
	line = strings.TrimPrefix(line, "<")
	line = strings.TrimSuffix(line, ">")

	// Parse status format: <State|MPos:x,y,z|WPos:x,y,z|...>
	parts := strings.Split(line, "|")
	if len(parts) == 0 {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse state
	a.status.State = parts[0]

	// Parse other fields
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "MPos:") {
			// Machine position
			coords := strings.TrimPrefix(part, "MPos:")
			if vals := parseCoordinates(coords); len(vals) == 3 {
				copy(a.status.MachinePos[:], vals)
			}
		} else if strings.HasPrefix(part, "WPos:") {
			// Work position
			coords := strings.TrimPrefix(part, "WPos:")
			if vals := parseCoordinates(coords); len(vals) == 3 {
				copy(a.status.WorkPos[:], vals)
			}
		} else if strings.HasPrefix(part, "F:") {
			// Feed rate
			if val, err := strconv.ParseFloat(strings.TrimPrefix(part, "F:"), 64); err == nil {
				a.status.FeedRate = val
			}
		} else if strings.HasPrefix(part, "S:") {
			// Spindle speed
			if val, err := strconv.ParseFloat(strings.TrimPrefix(part, "S:"), 64); err == nil {
				a.status.Spindle = val
			}
		}
	}

	a.status.LastUpdate = time.Now()
}

// parseCoordinates parses comma-separated coordinates.
func parseCoordinates(coords string) []float64 {
	parts := strings.Split(coords, ",")
	vals := make([]float64, 0, len(parts))
	for _, part := range parts {
		if val, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
			vals = append(vals, val)
		}
	}
	return vals
}