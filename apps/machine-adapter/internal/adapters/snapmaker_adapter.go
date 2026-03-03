// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// ToolHeadType represents the active tool head on a Snapmaker.
type ToolHeadType string

const (
	ToolHead3DP   ToolHeadType = "3dp"
	ToolHeadLaser ToolHeadType = "laser"
	ToolHeadCNC   ToolHeadType = "cnc"
)

// SnapmakerConnectionMode represents the connection method.
type SnapmakerConnectionMode string

const (
	SnapmakerSerial SnapmakerConnectionMode = "serial"
	SnapmakerWiFi   SnapmakerConnectionMode = "wifi"
)

// SnapmakerAdapter handles communication with Snapmaker 2.0 A350 machines.
// It wraps MarlinAdapter for serial communication and adds Snapmaker-specific
// features: tool head detection, mode switching, enclosure control, and WiFi mode.
type SnapmakerAdapter struct {
	mu             sync.RWMutex
	log            *logrus.Entry
	definition     *registry.MachineDefinition
	marlin         *MarlinAdapter // Underlying Marlin adapter for serial
	mode           SnapmakerConnectionMode
	activeToolHead ToolHeadType
	enclosureOn    bool

	// WiFi mode fields
	wifiBaseURL string
	wifiAPIKey  string
	httpClient  *http.Client

	// Telemetry callback
	OnTelemetry TelemetryCallback
}

// NewSnapmakerAdapter creates a new Snapmaker adapter.
func NewSnapmakerAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *SnapmakerAdapter {
	return &SnapmakerAdapter{
		log:            log.WithField("adapter", "snapmaker"),
		definition:     definition,
		activeToolHead: ToolHead3DP, // Default assumption
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ConnectSerial establishes a serial connection, wrapping MarlinAdapter.
func (a *SnapmakerAdapter) ConnectSerial(portName string, baudRate int) error {
	if baudRate == 0 {
		baudRate = 115200 // Snapmaker default baud rate
	}

	a.marlin = NewMarlinAdapter(a.definition, a.log.Logger)
	a.marlin.OnTelemetry = a.OnTelemetry

	if err := a.marlin.Connect(portName, baudRate); err != nil {
		return fmt.Errorf("snapmaker serial connect failed: %w", err)
	}

	a.mu.Lock()
	a.mode = SnapmakerSerial
	a.mu.Unlock()

	// Detect active tool head
	if err := a.DetectToolHead(); err != nil {
		a.log.WithError(err).Warn("Failed to detect tool head, defaulting to 3DP")
	}

	return nil
}

// ConnectWiFi establishes a WiFi connection via HTTP API.
func (a *SnapmakerAdapter) ConnectWiFi(ipAddress string, apiKey string) error {
	a.mu.Lock()
	a.wifiBaseURL = fmt.Sprintf("http://%s:8080/api", ipAddress)
	a.wifiAPIKey = apiKey
	a.mode = SnapmakerWiFi
	a.mu.Unlock()

	// Test connection
	if err := a.wifiGetStatus(); err != nil {
		return fmt.Errorf("snapmaker wifi connect failed: %w", err)
	}

	a.log.WithField("ip", ipAddress).Info("Connected to Snapmaker via WiFi")

	// Start status polling for WiFi mode
	go a.wifiStatusLoop()

	return nil
}

// SendCommand sends a command to the Snapmaker (implements CommandExecutor).
func (a *SnapmakerAdapter) SendCommand(command string, timeout time.Duration) error {
	a.mu.RLock()
	mode := a.mode
	a.mu.RUnlock()

	switch mode {
	case SnapmakerSerial:
		if a.marlin == nil {
			return fmt.Errorf("serial adapter not initialized")
		}
		return a.marlin.SendCommand(command, timeout)
	case SnapmakerWiFi:
		return a.wifiSendCommand(command)
	default:
		return fmt.Errorf("no connection established")
	}
}

// Disconnect closes the connection.
func (a *SnapmakerAdapter) Disconnect() error {
	a.mu.RLock()
	mode := a.mode
	a.mu.RUnlock()

	if mode == SnapmakerSerial && a.marlin != nil {
		return a.marlin.Disconnect()
	}
	return nil
}

// DetectToolHead sends M1005 to identify the active tool head.
func (a *SnapmakerAdapter) DetectToolHead() error {
	// M1005 returns the tool head type
	if err := a.SendCommand("M1005", 5*time.Second); err != nil {
		return err
	}

	// The response is parsed asynchronously. For serial mode, we check
	// the response in the Marlin adapter's process loop. For WiFi, we
	// query the status endpoint.
	if a.mode == SnapmakerWiFi {
		return a.wifiDetectToolHead()
	}

	// For serial mode, we'll update the tool head from the M1005 response
	// which gets handled in processSnapmakerResponse
	a.log.Info("Tool head detection command sent (M1005)")
	return nil
}

// SwitchMode switches between 3DP/Laser/CNC modes.
func (a *SnapmakerAdapter) SwitchMode(toolHead ToolHeadType) error {
	var modeCode string
	switch toolHead {
	case ToolHead3DP:
		modeCode = "0"
	case ToolHeadLaser:
		modeCode = "1"
	case ToolHeadCNC:
		modeCode = "2"
	default:
		return fmt.Errorf("unknown tool head: %s", toolHead)
	}

	if err := a.SendCommand(fmt.Sprintf("M605 S%s", modeCode), 10*time.Second); err != nil {
		return fmt.Errorf("mode switch failed: %w", err)
	}

	a.mu.Lock()
	a.activeToolHead = toolHead
	a.mu.Unlock()

	a.log.WithField("mode", toolHead).Info("Switched tool head mode")
	return nil
}

// SetEnclosureLED controls the enclosure LED brightness (0-255).
func (a *SnapmakerAdapter) SetEnclosureLED(brightness int) error {
	if brightness < 0 {
		brightness = 0
	}
	if brightness > 255 {
		brightness = 255
	}
	return a.SendCommand(fmt.Sprintf("M2000 L%d", brightness), 2*time.Second)
}

// SetEnclosureFan controls the enclosure fan speed (0-255).
func (a *SnapmakerAdapter) SetEnclosureFan(speed int) error {
	if speed < 0 {
		speed = 0
	}
	if speed > 255 {
		speed = 255
	}
	return a.SendCommand(fmt.Sprintf("M2000 F%d", speed), 2*time.Second)
}

// GetActiveToolHead returns the currently active tool head.
func (a *SnapmakerAdapter) GetActiveToolHead() ToolHeadType {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.activeToolHead
}

// IsConnected returns whether the adapter has an active connection.
func (a *SnapmakerAdapter) IsConnected() bool {
	a.mu.RLock()
	mode := a.mode
	a.mu.RUnlock()

	if mode == SnapmakerSerial && a.marlin != nil {
		return a.marlin.IsConnected()
	}
	if mode == SnapmakerWiFi {
		return a.wifiBaseURL != ""
	}
	return false
}

// GetStatus returns the current Marlin status (serial mode only).
func (a *SnapmakerAdapter) GetStatus() MarlinStatus {
	if a.marlin != nil {
		return a.marlin.GetStatus()
	}
	return MarlinStatus{}
}

// WiFi mode helpers

func (a *SnapmakerAdapter) wifiSendCommand(command string) error {
	a.mu.RLock()
	baseURL := a.wifiBaseURL
	apiKey := a.wifiAPIKey
	a.mu.RUnlock()

	payload := fmt.Sprintf(`{"command":"%s"}`, command)
	req, err := http.NewRequest("POST", baseURL+"/gcode", strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wifi command failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("wifi command error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (a *SnapmakerAdapter) wifiGetStatus() error {
	a.mu.RLock()
	baseURL := a.wifiBaseURL
	a.mu.RUnlock()

	resp, err := a.httpClient.Get(baseURL + "/status")
	if err != nil {
		return fmt.Errorf("wifi status failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wifi status error: %d", resp.StatusCode)
	}

	var status struct {
		Model     string  `json:"model"`
		State     string  `json:"state"`
		ToolHead  string  `json:"toolhead"`
		Position  struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
			Z float64 `json:"z"`
		} `json:"position"`
		Temperatures struct {
			Nozzle struct{ Current, Target float64 } `json:"nozzle"`
			Bed    struct{ Current, Target float64 } `json:"bed"`
		} `json:"temperatures"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return fmt.Errorf("failed to parse wifi status: %w", err)
	}

	// Update tool head
	a.mu.Lock()
	switch status.ToolHead {
	case "printing", "3dp":
		a.activeToolHead = ToolHead3DP
	case "laser":
		a.activeToolHead = ToolHeadLaser
	case "cnc":
		a.activeToolHead = ToolHeadCNC
	}
	a.mu.Unlock()

	// Publish telemetry if callback is set
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "position_x", Value: status.Position.X, Unit: "mm", Timestamp: now},
			{Type: "position_y", Value: status.Position.Y, Unit: "mm", Timestamp: now},
			{Type: "position_z", Value: status.Position.Z, Unit: "mm", Timestamp: now},
			{Type: "temperature_extruder", Value: status.Temperatures.Nozzle.Current, Unit: "celsius", Timestamp: now},
			{Type: "temperature_extruder_target", Value: status.Temperatures.Nozzle.Target, Unit: "celsius", Timestamp: now},
			{Type: "temperature_bed", Value: status.Temperatures.Bed.Current, Unit: "celsius", Timestamp: now},
			{Type: "temperature_bed_target", Value: status.Temperatures.Bed.Target, Unit: "celsius", Timestamp: now},
		})
	}

	return nil
}

func (a *SnapmakerAdapter) wifiDetectToolHead() error {
	return a.wifiGetStatus() // Status response includes tool head info
}

func (a *SnapmakerAdapter) wifiStatusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !a.IsConnected() {
			return
		}
		if err := a.wifiGetStatus(); err != nil {
			a.log.WithError(err).Debug("WiFi status poll failed")
		}
	}
}
