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

// UltimakerStatus represents the current state of an Ultimaker printer.
type UltimakerStatus struct {
	State          string  // idle, running, paused, error
	ExtruderTemp   float64 // Current hotend temperature (celsius)
	ExtruderTarget float64 // Target hotend temperature
	BedTemp        float64 // Current bed temperature
	BedTarget      float64 // Target bed temperature
	PrintProgress  float64 // Print progress (0.0-1.0)
	PrintState     string  // Raw print job state from the API
	LastUpdate     time.Time
}

// UltimakerAdapter handles communication with Ultimaker S5/S7 printers
// via their REST API on port 80. Authentication uses HTTP digest auth.
type UltimakerAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	httpClient *http.Client
	baseURL    string
	username   string
	password   string
	connected  bool
	status     UltimakerStatus
	stopPoll   chan struct{}

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewUltimakerAdapter creates a new Ultimaker adapter.
func NewUltimakerAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *UltimakerAdapter {
	return &UltimakerAdapter{
		log:        log.WithField("adapter", "ultimaker"),
		definition: definition,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopPoll: make(chan struct{}),
	}
}

// Connect establishes a session with the Ultimaker printer. The host should be
// an IP or hostname. Ultimaker uses digest auth with username/password obtained
// from the printer's developer portal or local API credentials.
func (a *UltimakerAdapter) Connect(host string, username string, password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s/api/v1", host)
	a.username = username
	a.password = password

	// Verify connectivity by fetching printer status.
	if err := a.verifyConnection(); err != nil {
		return fmt.Errorf("ultimaker connect failed: %w", err)
	}

	a.connected = true
	a.log.WithField("host", host).Info("Connected to Ultimaker printer")

	// Start background status polling.
	go a.pollStatusLoop()

	return nil
}

// Disconnect stops polling and marks the adapter as disconnected.
func (a *UltimakerAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	close(a.stopPoll)
	a.connected = false
	a.stopPoll = make(chan struct{})
	a.log.Info("Disconnected from Ultimaker printer")
	return nil
}

// IsConnected returns true if the adapter has an active connection.
func (a *UltimakerAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns a snapshot of the current printer status.
func (a *UltimakerAdapter) GetStatus() UltimakerStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw command string to the Ultimaker. For the Ultimaker
// REST API this is less meaningful than MapCommand, but is provided to satisfy
// the CommandExecutor interface. The command is interpreted as a print job
// state transition (pause, resume, abort).
func (a *UltimakerAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Ultimaker does not accept arbitrary G-code over REST. Commands are
	// mapped to specific REST endpoints.
	switch command {
	case "pause":
		return a.setPrintJobState("pause", timeout)
	case "resume":
		return a.setPrintJobState("print", timeout)
	case "abort", "stop":
		return a.setPrintJobState("abort", timeout)
	default:
		return fmt.Errorf("ultimaker does not support raw command %q; use MapCommand", command)
	}
}

// MapCommand translates high-level command names to Ultimaker REST API calls.
// Supported commands: home, pause, resume, stop, get_status, get_temperature, set_temp.
func (a *UltimakerAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		// Ultimaker handles homing internally; not exposed via REST API in most
		// firmware versions. We return an informational response.
		return map[string]string{
			"info": "Ultimaker manages homing automatically; command acknowledged",
		}, nil

	case "pause":
		return nil, a.setPrintJobState("pause", 5*time.Second)

	case "resume":
		return nil, a.setPrintJobState("print", 5*time.Second)

	case "stop":
		return nil, a.setPrintJobState("abort", 10*time.Second)

	case "get_status":
		status := a.GetStatus()
		return map[string]interface{}{
			"state":          status.State,
			"print_state":    status.PrintState,
			"print_progress": status.PrintProgress,
		}, nil

	case "get_temperature":
		status := a.GetStatus()
		return map[string]float64{
			"extruder_current": status.ExtruderTemp,
			"extruder_target":  status.ExtruderTarget,
			"bed_current":      status.BedTemp,
			"bed_target":       status.BedTarget,
		}, nil

	case "set_temp":
		temp, err := extractFloat(params, "temp")
		if err != nil {
			return nil, fmt.Errorf("set_temp requires 'temp' numeric parameter: %w", err)
		}
		return nil, a.setExtruderTargetTemp(temp)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// verifyConnection tests the REST API by fetching the printer endpoint.
func (a *UltimakerAdapter) verifyConnection() error {
	req, err := http.NewRequest("GET", a.baseURL+"/printer", nil)
	if err != nil {
		return err
	}
	a.setDigestAuth(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("printer endpoint returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// setDigestAuth applies basic auth credentials to a request. The Ultimaker S5/S7
// REST API uses HTTP digest auth in production; basic auth is a simplified
// fallback used here. A production deployment should implement full digest
// negotiation or use an auth middleware.
func (a *UltimakerAdapter) setDigestAuth(req *http.Request) {
	if a.username != "" {
		req.SetBasicAuth(a.username, a.password)
	}
}

// pollStatusLoop polls the Ultimaker REST API every 2 seconds.
func (a *UltimakerAdapter) pollStatusLoop() {
	ticker := time.NewTicker(2 * time.Second)
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

// fetchStatus retrieves printer status, temperatures, and print job progress.
func (a *UltimakerAdapter) fetchStatus() error {
	// Fetch printer status.
	printerStatus, err := a.apiGet("/printer")
	if err != nil {
		return err
	}

	var printer struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(printerStatus, &printer); err != nil {
		return fmt.Errorf("failed to parse printer status: %w", err)
	}

	// Fetch hotend temperature.
	tempData, err := a.apiGet("/printer/heads/0/extruders/0/hotend/temperature")
	if err != nil {
		a.log.WithError(err).Debug("Failed to fetch hotend temperature")
	}

	var hotend struct {
		Current float64 `json:"current"`
		Target  float64 `json:"target"`
	}
	if tempData != nil {
		json.Unmarshal(tempData, &hotend)
	}

	// Fetch bed temperature.
	bedData, err := a.apiGet("/printer/bed/temperature")
	if err != nil {
		a.log.WithError(err).Debug("Failed to fetch bed temperature")
	}

	var bed struct {
		Current float64 `json:"current"`
		Target  float64 `json:"target"`
	}
	if bedData != nil {
		json.Unmarshal(bedData, &bed)
	}

	// Fetch print job progress.
	var progress float64
	var printState string
	jobData, err := a.apiGet("/print_job")
	if err == nil && jobData != nil {
		var job struct {
			State    string  `json:"state"`
			Progress float64 `json:"progress"`
		}
		if json.Unmarshal(jobData, &job) == nil {
			progress = job.Progress
			printState = job.State
		}
	}

	// Update internal state.
	a.mu.Lock()
	a.status.State = a.mapState(printer.Status)
	a.status.ExtruderTemp = hotend.Current
	a.status.ExtruderTarget = hotend.Target
	a.status.BedTemp = bed.Current
	a.status.BedTarget = bed.Target
	a.status.PrintProgress = progress
	a.status.PrintState = printState
	a.status.LastUpdate = time.Now()

	extTemp := a.status.ExtruderTemp
	extTarget := a.status.ExtruderTarget
	bedTemp := a.status.BedTemp
	bedTarget := a.status.BedTarget
	prog := a.status.PrintProgress
	state := a.status.PrintState

	a.mu.Unlock()

	// Emit telemetry.
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "extruder_temp", Value: extTemp, Unit: "celsius", Timestamp: now},
			{Type: "extruder_target", Value: extTarget, Unit: "celsius", Timestamp: now},
			{Type: "bed_temp", Value: bedTemp, Unit: "celsius", Timestamp: now},
			{Type: "bed_target", Value: bedTarget, Unit: "celsius", Timestamp: now},
			{Type: "print_progress", Value: prog * 100, Unit: "percent", Timestamp: now},
			{Type: "print_state", Value: stateToNumeric(state), Unit: "enum", Timestamp: now},
		})
	}

	return nil
}

// mapState converts Ultimaker printer status strings to normalized values.
func (a *UltimakerAdapter) mapState(status string) string {
	switch status {
	case "idle":
		return "idle"
	case "printing":
		return "running"
	case "paused":
		return "paused"
	case "error":
		return "error"
	case "maintenance":
		return "idle"
	case "booting":
		return "idle"
	default:
		return "unknown"
	}
}

// apiGet performs an authenticated GET to the Ultimaker API.
func (a *UltimakerAdapter) apiGet(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", a.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	a.setDigestAuth(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // Resource not available (e.g., no active print job).
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// apiPut performs an authenticated PUT to the Ultimaker API with a JSON body.
func (a *UltimakerAdapter) apiPut(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequest("PUT", a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.setDigestAuth(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// apiPost performs an authenticated POST to the Ultimaker API with a JSON body.
func (a *UltimakerAdapter) apiPost(path string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}

	req, err := http.NewRequest("POST", a.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.setDigestAuth(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST error %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// setPrintJobState changes the print job state via PUT /api/v1/print_job/state.
func (a *UltimakerAdapter) setPrintJobState(target string, timeout time.Duration) error {
	client := &http.Client{Timeout: timeout}
	body, _ := json.Marshal(map[string]string{"target": target})

	req, err := http.NewRequest("PUT", a.baseURL+"/print_job/state", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.setDigestAuth(req)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("set print job state failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set state error %d: %s", resp.StatusCode, string(respBody))
	}

	a.log.WithField("target", target).Debug("Set print job state")
	return nil
}

// setExtruderTargetTemp sets the hotend target temperature via POST.
func (a *UltimakerAdapter) setExtruderTargetTemp(temp float64) error {
	return a.apiPost(
		"/printer/heads/0/extruders/0/hotend/temperature/target",
		map[string]float64{"target": temp},
	)
}

// stateToNumeric converts a print state string to a numeric value for telemetry.
func stateToNumeric(state string) float64 {
	switch state {
	case "none":
		return 0
	case "printing":
		return 1
	case "paused":
		return 2
	case "pausing":
		return 3
	case "resuming":
		return 4
	case "pre_print", "post_print":
		return 5
	case "wait_cleanup":
		return 6
	default:
		return -1
	}
}
