// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// FormlabsStatus represents the current state of a Formlabs printer.
type FormlabsStatus struct {
	State         string  // idle, running, paused, error
	PrintProgress float64 // Print progress (0.0-1.0)
	ResinTemp     float64 // Current resin temperature (celsius)
	TankLevel     float64 // Resin tank level (0.0-1.0)
	PrinterState  string  // Raw printer state from the API
	LastUpdate    time.Time
}

// FormlabsAdapter handles communication with Formlabs printers (Form 4 and
// compatible) via the Fleet Control REST API. Formlabs printers offer limited
// direct control through the API -- primarily monitoring with pause/resume/stop.
type FormlabsAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	httpClient *http.Client
	baseURL    string
	printerID  string
	token      string
	connected  bool
	status     FormlabsStatus
	stopPoll   chan struct{}

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewFormlabsAdapter creates a new Formlabs adapter.
func NewFormlabsAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *FormlabsAdapter {
	return &FormlabsAdapter{
		log:        log.WithField("adapter", "formlabs"),
		definition: definition,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		stopPoll: make(chan struct{}),
	}
}

// Connect establishes a session with the Formlabs Fleet Control API. The host
// should be the Fleet Control server address. The token is an OAuth2 bearer
// token for API authentication. The printerID is discovered automatically
// from the fleet or can be set explicitly via SetPrinterID.
func (a *FormlabsAdapter) Connect(host string, token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("https://%s/api/v1", host)
	a.token = token

	// Discover printers in the fleet.
	if err := a.discoverPrinter(); err != nil {
		return fmt.Errorf("formlabs connect failed: %w", err)
	}

	a.connected = true
	a.log.WithFields(logrus.Fields{
		"host":       host,
		"printer_id": a.printerID,
	}).Info("Connected to Formlabs Fleet Control")

	// Start background status polling.
	go a.pollStatusLoop()

	return nil
}

// SetPrinterID explicitly sets the target printer ID. Call before Connect if
// the fleet contains multiple printers and you want a specific one.
func (a *FormlabsAdapter) SetPrinterID(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.printerID = id
}

// Disconnect stops polling and marks the adapter as disconnected.
func (a *FormlabsAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	close(a.stopPoll)
	a.connected = false
	a.stopPoll = make(chan struct{})
	a.log.Info("Disconnected from Formlabs Fleet Control")
	return nil
}

// IsConnected returns true if the adapter has an active connection.
func (a *FormlabsAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns a snapshot of the current printer status.
func (a *FormlabsAdapter) GetStatus() FormlabsStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a command action to the Formlabs printer. Because the
// Fleet Control API uses structured POST payloads rather than raw commands,
// only specific actions are supported (pause, resume, stop).
func (a *FormlabsAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	switch command {
	case "pause":
		return a.sendPrinterCommand("pause", timeout)
	case "resume":
		return a.sendPrinterCommand("resume", timeout)
	case "stop":
		return a.sendPrinterCommand("cancel", timeout)
	default:
		return fmt.Errorf("formlabs does not support raw command %q; use MapCommand", command)
	}
}

// MapCommand translates high-level command names to Formlabs API calls.
// Supported commands: get_status, pause, resume, stop.
func (a *FormlabsAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "get_status":
		status := a.GetStatus()
		return map[string]interface{}{
			"state":          status.State,
			"printer_state":  status.PrinterState,
			"print_progress": status.PrintProgress,
			"resin_temp":     status.ResinTemp,
			"tank_level":     status.TankLevel,
		}, nil

	case "pause":
		return nil, a.sendPrinterCommand("pause", 5*time.Second)

	case "resume":
		return nil, a.sendPrinterCommand("resume", 5*time.Second)

	case "stop":
		return nil, a.sendPrinterCommand("cancel", 10*time.Second)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// discoverPrinter fetches the printer list and selects the first printer if
// no explicit printerID has been set.
func (a *FormlabsAdapter) discoverPrinter() error {
	data, err := a.apiGet("/printers")
	if err != nil {
		return fmt.Errorf("failed to list printers: %w", err)
	}

	var printers []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(data, &printers); err != nil {
		return fmt.Errorf("failed to parse printer list: %w", err)
	}

	if len(printers) == 0 {
		return fmt.Errorf("no printers found in fleet")
	}

	// If no explicit ID, use the first printer.
	if a.printerID == "" {
		a.printerID = printers[0].ID
		a.log.WithFields(logrus.Fields{
			"printer_id":   printers[0].ID,
			"printer_name": printers[0].Name,
			"fleet_size":   len(printers),
		}).Info("Auto-selected printer from fleet")
	}

	return nil
}

// pollStatusLoop polls the Formlabs API every 3 seconds. Formlabs recommends
// a slightly longer interval than firmware-based printers.
func (a *FormlabsAdapter) pollStatusLoop() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopPoll:
			return
		case <-ticker.C:
			if !a.IsConnected() {
				return
			}
			if err := a.fetchStatus(); err != nil {
				a.log.WithError(err).Debug("Status poll failed")
			}
		}
	}
}

// fetchStatus retrieves printer status and active print job data.
func (a *FormlabsAdapter) fetchStatus() error {
	a.mu.RLock()
	printerID := a.printerID
	a.mu.RUnlock()

	// Fetch printer status.
	printerData, err := a.apiGet(fmt.Sprintf("/printers/%s", printerID))
	if err != nil {
		return err
	}

	var printer struct {
		State     string  `json:"state"`
		ResinTemp float64 `json:"resin_temperature"`
		TankLevel float64 `json:"tank_level"`
	}
	if err := json.Unmarshal(printerData, &printer); err != nil {
		return fmt.Errorf("failed to parse printer status: %w", err)
	}

	// Fetch print job progress.
	var progress float64
	jobData, err := a.apiGet(fmt.Sprintf("/printers/%s/print-job", printerID))
	if err == nil && jobData != nil {
		var job struct {
			Progress float64 `json:"progress"`
		}
		if json.Unmarshal(jobData, &job) == nil {
			progress = job.Progress
		}
	}

	// Update internal state.
	a.mu.Lock()
	a.status.State = a.mapState(printer.State)
	a.status.PrinterState = printer.State
	a.status.ResinTemp = printer.ResinTemp
	a.status.TankLevel = printer.TankLevel
	a.status.PrintProgress = progress
	a.status.LastUpdate = time.Now()

	state := a.status.State
	resinTemp := a.status.ResinTemp
	tankLevel := a.status.TankLevel
	prog := a.status.PrintProgress

	a.mu.Unlock()

	// Emit telemetry.
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "print_progress", Value: prog * 100, Unit: "percent", Timestamp: now},
			{Type: "printer_state", Value: stateToNumericFormlabs(state), Unit: "enum", Timestamp: now},
			{Type: "resin_temp", Value: resinTemp, Unit: "celsius", Timestamp: now},
			{Type: "tank_level", Value: tankLevel * 100, Unit: "percent", Timestamp: now},
		})
	}

	return nil
}

// mapState converts Formlabs printer state strings to normalized values.
func (a *FormlabsAdapter) mapState(state string) string {
	switch state {
	case "idle":
		return "idle"
	case "printing":
		return "running"
	case "paused":
		return "paused"
	case "error":
		return "error"
	default:
		return "unknown"
	}
}

// apiGet performs an authenticated GET to the Formlabs Fleet Control API.
func (a *FormlabsAdapter) apiGet(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", a.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// sendPrinterCommand sends a command action to the printer via POST.
func (a *FormlabsAdapter) sendPrinterCommand(action string, timeout time.Duration) error {
	a.mu.RLock()
	printerID := a.printerID
	a.mu.RUnlock()

	client := &http.Client{Timeout: timeout}
	body, _ := json.Marshal(map[string]string{"action": action})

	url := fmt.Sprintf("%s/printers/%s/command", a.baseURL, printerID)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("command request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("command error %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.WithField("action", action).Debug("Sent printer command")
	return nil
}

// stateToNumericFormlabs converts a Formlabs state to a numeric telemetry value.
func stateToNumericFormlabs(state string) float64 {
	switch state {
	case "idle":
		return 0
	case "running":
		return 1
	case "paused":
		return 2
	case "error":
		return 3
	default:
		return -1
	}
}
