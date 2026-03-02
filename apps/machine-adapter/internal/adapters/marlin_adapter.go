// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.bug.st/serial"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// MarlinStatus represents the current state of a Marlin 3D printer.
type MarlinStatus struct {
	State           string    // Printing, Idle, Paused, Error
	Progress        int       // Print progress percentage (0-100)
	ExtruderTemp    float64   // Current extruder temperature
	ExtruderTarget  float64   // Target extruder temperature
	BedTemp         float64   // Current bed temperature
	BedTarget       float64   // Target bed temperature
	Position        [3]float64 // Current X, Y, Z position
	FeedRate        float64   // Current feed rate
	FlowRate        int       // Flow rate percentage
	FanSpeed        int       // Fan speed (0-255)
	SDPrinting      bool      // Printing from SD card
	SDFileName      string    // Current SD file name
	PrintTime       int       // Print time in seconds
	FilamentUsed    float64   // Filament used in mm
	LastUpdate      time.Time
}

// MarlinAdapter handles communication with Marlin-based 3D printers.
type MarlinAdapter struct {
	mu            sync.RWMutex
	log           *logrus.Entry
	definition    *registry.MachineDefinition
	port          serial.Port
	reader        *bufio.Reader
	writer        io.Writer
	status        MarlinStatus
	connected     bool
	printing      bool
	commandQueue  chan CommandRequest
	responseQueue chan CommandResponse
	ctx           context.Context
	cancel        context.CancelFunc

	// Marlin-specific settings
	autoTempReport bool
	autoPosReport  bool
	echoEnabled    bool
	checksumMode   bool
	lineNumber     int
}

// MarlinCapabilities defines printer capabilities.
type MarlinCapabilities struct {
	HeatedBed       bool
	AutoLeveling    bool
	FilamentSensor  bool
	PowerRecovery   bool
	MultiExtruder   bool
	ExtruderCount   int
	BuildVolume     [3]float64 // X, Y, Z in mm
	MaxExtruderTemp float64
	MaxBedTemp      float64
}

// NewMarlinAdapter creates a new Marlin adapter.
func NewMarlinAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *MarlinAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &MarlinAdapter{
		log:           log.WithField("adapter", "marlin"),
		definition:    definition,
		commandQueue:  make(chan CommandRequest, 100),
		responseQueue: make(chan CommandResponse, 100),
		ctx:           ctx,
		cancel:        cancel,
		lineNumber:    0,
	}
}

// Connect establishes a connection to the Marlin printer.
func (a *MarlinAdapter) Connect(portName string, baudRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	// Standard baud rate for Marlin is 250000
	if baudRate == 0 {
		baudRate = 250000
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

	// DTR/RTS control for Arduino reset
	if err := port.SetDTR(false); err != nil {
		a.log.WithError(err).Warn("Failed to set DTR")
	}
	time.Sleep(100 * time.Millisecond)
	if err := port.SetDTR(true); err != nil {
		a.log.WithError(err).Warn("Failed to set DTR")
	}

	a.connected = true
	a.log.WithFields(logrus.Fields{
		"port":     portName,
		"baudrate": baudRate,
	}).Info("Connected to Marlin printer")

	// Start background workers
	go a.readLoop()
	go a.commandLoop()
	go a.statusLoop()

	// Wait for Marlin to initialize (shows "start" message)
	time.Sleep(3 * time.Second)

	// Initialize printer settings
	if err := a.initialize(); err != nil {
		a.log.WithError(err).Warn("Failed to initialize printer settings")
	}

	return nil
}

// initialize sets up printer communication settings.
func (a *MarlinAdapter) initialize() error {
	// Get firmware info
	if err := a.SendCommand("M115", 2*time.Second); err != nil {
		return fmt.Errorf("failed to get firmware info: %w", err)
	}

	// Enable auto-temperature reporting (if supported)
	if err := a.SendCommand("M155 S1", 1*time.Second); err == nil {
		a.autoTempReport = true
		a.log.Info("Auto-temperature reporting enabled")
	}

	// Enable auto-position reporting (if supported)
	if err := a.SendCommand("M114", 1*time.Second); err == nil {
		a.autoPosReport = true
	}

	// Set absolute positioning
	if err := a.SendCommand("G90", 1*time.Second); err != nil {
		return fmt.Errorf("failed to set absolute positioning: %w", err)
	}

	// Set units to millimeters
	if err := a.SendCommand("G21", 1*time.Second); err != nil {
		return fmt.Errorf("failed to set units to mm: %w", err)
	}

	// Get current status
	a.updateStatus()

	return nil
}

// Disconnect closes the connection to the printer.
func (a *MarlinAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	// Stop auto-reporting if enabled
	if a.autoTempReport {
		a.SendCommand("M155 S0", 1*time.Second)
	}

	a.cancel() // Stop background workers
	a.connected = false

	if a.port != nil {
		if err := a.port.Close(); err != nil {
			return fmt.Errorf("failed to close port: %w", err)
		}
		a.port = nil
	}

	a.log.Info("Disconnected from Marlin printer")
	return nil
}

// SendCommand sends a command to the printer and waits for response.
func (a *MarlinAdapter) SendCommand(command string, timeout time.Duration) error {
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

// SendGCode sends G-code to the printer.
func (a *MarlinAdapter) SendGCode(gcode string) error {
	lines := strings.Split(gcode, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue // Skip empty lines and comments
		}

		if err := a.SendCommand(line, 30*time.Second); err != nil {
			return fmt.Errorf("failed to send line %q: %w", line, err)
		}
	}
	return nil
}

// GetStatus returns the current printer status.
func (a *MarlinAdapter) GetStatus() MarlinStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// IsConnected returns true if connected to the printer.
func (a *MarlinAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// IsPrinting returns true if the printer is currently printing.
func (a *MarlinAdapter) IsPrinting() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.printing
}

// Home performs a homing cycle.
func (a *MarlinAdapter) Home(axes string) error {
	if axes == "" {
		axes = "G28" // Home all axes
	} else {
		axes = fmt.Sprintf("G28 %s", axes) // Home specific axes
	}
	return a.SendCommand(axes, 60*time.Second)
}

// SetExtruderTemp sets the extruder temperature.
func (a *MarlinAdapter) SetExtruderTemp(temp float64, wait bool) error {
	var cmd string
	if wait {
		cmd = fmt.Sprintf("M109 S%.1f", temp) // Set and wait
	} else {
		cmd = fmt.Sprintf("M104 S%.1f", temp) // Set without waiting
	}
	timeout := 1 * time.Second
	if wait {
		timeout = 10 * time.Minute // Heating can take time
	}
	return a.SendCommand(cmd, timeout)
}

// SetBedTemp sets the bed temperature.
func (a *MarlinAdapter) SetBedTemp(temp float64, wait bool) error {
	var cmd string
	if wait {
		cmd = fmt.Sprintf("M190 S%.1f", temp) // Set and wait
	} else {
		cmd = fmt.Sprintf("M140 S%.1f", temp) // Set without waiting
	}
	timeout := 1 * time.Second
	if wait {
		timeout = 10 * time.Minute // Heating can take time
	}
	return a.SendCommand(cmd, timeout)
}

// StartPrint starts printing from SD card.
func (a *MarlinAdapter) StartPrint(filename string) error {
	// Select file
	if err := a.SendCommand(fmt.Sprintf("M23 %s", filename), 5*time.Second); err != nil {
		return fmt.Errorf("failed to select file: %w", err)
	}

	// Start print
	if err := a.SendCommand("M24", 5*time.Second); err != nil {
		return fmt.Errorf("failed to start print: %w", err)
	}

	a.mu.Lock()
	a.printing = true
	a.status.SDPrinting = true
	a.status.SDFileName = filename
	a.mu.Unlock()

	return nil
}

// PausePrint pauses the current print.
func (a *MarlinAdapter) PausePrint() error {
	err := a.SendCommand("M25", 5*time.Second)
	if err == nil {
		a.mu.Lock()
		a.status.State = "Paused"
		a.mu.Unlock()
	}
	return err
}

// ResumePrint resumes a paused print.
func (a *MarlinAdapter) ResumePrint() error {
	err := a.SendCommand("M24", 5*time.Second)
	if err == nil {
		a.mu.Lock()
		a.status.State = "Printing"
		a.mu.Unlock()
	}
	return err
}

// StopPrint stops the current print.
func (a *MarlinAdapter) StopPrint() error {
	// Stop print
	if err := a.SendCommand("M524", 5*time.Second); err != nil {
		// Fallback for older Marlin versions
		a.SendCommand("M25", 1*time.Second)  // Pause
		a.SendCommand("M410", 1*time.Second) // Quick stop
	}

	a.mu.Lock()
	a.printing = false
	a.status.SDPrinting = false
	a.status.State = "Idle"
	a.mu.Unlock()

	// Turn off heaters
	a.SetExtruderTemp(0, false)
	a.SetBedTemp(0, false)

	// Home axes
	return a.Home("")
}

// EmergencyStop performs an emergency stop.
func (a *MarlinAdapter) EmergencyStop() error {
	// M112 - Emergency stop
	if _, err := a.writer.Write([]byte("M112\n")); err != nil {
		return fmt.Errorf("failed to send emergency stop: %w", err)
	}

	a.mu.Lock()
	a.printing = false
	a.status.State = "Error"
	a.mu.Unlock()

	return nil
}

// readLoop continuously reads from the serial port.
func (a *MarlinAdapter) readLoop() {
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
func (a *MarlinAdapter) commandLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case req := <-a.commandQueue:
			// Add checksum if enabled
			cmd := req.Command
			if a.checksumMode {
				cmd = a.addChecksum(cmd)
			}
			cmd += "\n"

			// Send command
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

// statusLoop periodically requests printer status.
func (a *MarlinAdapter) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.IsConnected() && !a.autoTempReport {
				a.updateStatus()
			}
		}
	}
}

// updateStatus requests current status from the printer.
func (a *MarlinAdapter) updateStatus() {
	// Get temperatures
	if !a.autoTempReport {
		a.SendCommand("M105", 1*time.Second)
	}

	// Get position
	a.SendCommand("M114", 1*time.Second)

	// Get SD print status if printing
	if a.status.SDPrinting {
		a.SendCommand("M27", 1*time.Second)
	}
}

// processResponse processes responses from the printer.
func (a *MarlinAdapter) processResponse(line string) {
	// Check for standard responses
	if line == "ok" || strings.HasPrefix(line, "ok ") {
		a.responseQueue <- CommandResponse{Success: true, Message: line}
		return
	}

	// Check for errors
	if strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "error:") {
		a.responseQueue <- CommandResponse{
			Success: false,
			Error:   fmt.Errorf("printer error: %s", line),
		}
		return
	}

	// Parse temperature reports
	if strings.Contains(line, "T:") || strings.Contains(line, "B:") {
		a.parseTemperature(line)
	}

	// Parse position reports
	if strings.Contains(line, "X:") && strings.Contains(line, "Y:") && strings.Contains(line, "Z:") {
		a.parsePosition(line)
	}

	// Parse SD progress
	if strings.HasPrefix(line, "SD printing byte") {
		a.parseSDProgress(line)
	}

	// Parse other messages
	if strings.Contains(line, "echo:") {
		a.echoEnabled = true
		a.log.WithField("echo", line).Debug("Echo message")
	}

	// Send generic success for other messages
	if !strings.HasPrefix(line, "echo:") && !strings.HasPrefix(line, "//") {
		a.responseQueue <- CommandResponse{Success: true, Message: line}
	}
}

// parseTemperature parses temperature reports.
func (a *MarlinAdapter) parseTemperature(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse extruder temperature: T:200.5 /200.0
	if match := regexp.MustCompile(`T:([\d.]+)\s*/\s*([\d.]+)`).FindStringSubmatch(line); len(match) > 2 {
		if temp, err := strconv.ParseFloat(match[1], 64); err == nil {
			a.status.ExtruderTemp = temp
		}
		if target, err := strconv.ParseFloat(match[2], 64); err == nil {
			a.status.ExtruderTarget = target
		}
	}

	// Parse bed temperature: B:60.2 /60.0
	if match := regexp.MustCompile(`B:([\d.]+)\s*/\s*([\d.]+)`).FindStringSubmatch(line); len(match) > 2 {
		if temp, err := strconv.ParseFloat(match[1], 64); err == nil {
			a.status.BedTemp = temp
		}
		if target, err := strconv.ParseFloat(match[2], 64); err == nil {
			a.status.BedTarget = target
		}
	}

	a.status.LastUpdate = time.Now()
}

// parsePosition parses position reports.
func (a *MarlinAdapter) parsePosition(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse position: X:100.00 Y:100.00 Z:5.00 E:0.00
	if match := regexp.MustCompile(`X:([\d.-]+)`).FindStringSubmatch(line); len(match) > 1 {
		if pos, err := strconv.ParseFloat(match[1], 64); err == nil {
			a.status.Position[0] = pos
		}
	}
	if match := regexp.MustCompile(`Y:([\d.-]+)`).FindStringSubmatch(line); len(match) > 1 {
		if pos, err := strconv.ParseFloat(match[1], 64); err == nil {
			a.status.Position[1] = pos
		}
	}
	if match := regexp.MustCompile(`Z:([\d.-]+)`).FindStringSubmatch(line); len(match) > 1 {
		if pos, err := strconv.ParseFloat(match[1], 64); err == nil {
			a.status.Position[2] = pos
		}
	}

	a.status.LastUpdate = time.Now()
}

// parseSDProgress parses SD card print progress.
func (a *MarlinAdapter) parseSDProgress(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Parse: SD printing byte 12345/67890
	if match := regexp.MustCompile(`SD printing byte (\d+)/(\d+)`).FindStringSubmatch(line); len(match) > 2 {
		if current, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			if total, err := strconv.ParseInt(match[2], 10, 64); err == nil {
				if total > 0 {
					a.status.Progress = int(current * 100 / total)
				}
			}
		}
	}
}

// addChecksum adds line number and checksum to a command.
func (a *MarlinAdapter) addChecksum(cmd string) string {
	a.lineNumber++
	numbered := fmt.Sprintf("N%d %s", a.lineNumber, cmd)

	// Calculate checksum (XOR of all bytes)
	checksum := 0
	for _, b := range []byte(numbered) {
		checksum ^= int(b)
	}

	return fmt.Sprintf("%s*%d", numbered, checksum)
}