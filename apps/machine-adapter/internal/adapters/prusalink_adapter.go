// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PrusaLinkTelemetryMetric represents a single telemetry data point for PrusaLink.
type PrusaLinkTelemetryMetric struct {
	Type      string  `json:"type"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp string  `json:"timestamp"`
}

// PrusaLinkTelemetryCallback is called when new PrusaLink telemetry data is available.
type PrusaLinkTelemetryCallback func(metrics []PrusaLinkTelemetryMetric)

// PrusaLinkStatus represents the current state of a PrusaLink-connected printer.
type PrusaLinkStatus struct {
	State         string    // IDLE, PRINTING, PAUSED, ERROR, FINISHED, BUSY
	NozzleTemp    float64   // Current nozzle temperature
	NozzleTarget  float64   // Target nozzle temperature
	BedTemp       float64   // Current bed temperature
	BedTarget     float64   // Target bed temperature
	PrintProgress float64   // Print progress percentage (0-100)
	PrintSpeed    int       // Current print speed percentage
	FlowFactor    int       // Current flow factor percentage
	LastUpdate    time.Time // Timestamp of last status update
}

// PrusaLinkAdapter handles communication with PrusaLink-enabled printers via REST API.
// PrusaLink is used on Prusa MINI+, MK4, XL, Core One, and SL1S printers.
type PrusaLinkAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	baseURL    string
	apiKey     string
	httpClient *http.Client
	status     PrusaLinkStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Telemetry callback for publishing metrics.
	OnTelemetry PrusaLinkTelemetryCallback
}

// NewPrusaLinkAdapter creates a new PrusaLink adapter.
func NewPrusaLinkAdapter(log *logrus.Logger) *PrusaLinkAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &PrusaLinkAdapter{
		log: log.WithField("adapter", "prusalink"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Connect establishes a connection to the PrusaLink instance.
// PrusaLink uses HTTP with X-Api-Key authentication. The host should include
// the port if non-standard (e.g. "192.168.1.50:8080").
func (a *PrusaLinkAdapter) Connect(host string, apiKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s", host)
	a.apiKey = apiKey

	// Verify connectivity by fetching the status endpoint.
	req, err := http.NewRequest("GET", a.baseURL+"/api/v1/status", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Api-Key", a.apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("prusalink connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prusalink auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	a.connected = true
	a.log.WithField("host", host).Info("Connected to PrusaLink")

	// Start background telemetry polling.
	go a.pollLoop()

	return nil
}

// Disconnect stops the adapter and releases resources.
func (a *PrusaLinkAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false
	a.log.Info("Disconnected from PrusaLink")
	return nil
}

// IsConnected returns whether the adapter is connected.
func (a *PrusaLinkAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current printer status.
func (a *PrusaLinkAdapter) GetStatus() PrusaLinkStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw G-code command to PrusaLink.
// PrusaLink has limited G-code passthrough; this attempts a POST to the
// gcode endpoint if available, falling back to an error for unsupported operations.
func (a *PrusaLinkAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	payload := map[string]string{"command": command}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	defer cancel()

	a.mu.RLock()
	baseURL := a.baseURL
	apiKey := a.apiKey
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "POST", baseURL+"/api/v1/gcode", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("prusalink command failed: %w", err)
	}
	defer resp.Body.Close()

	// Accept both 200 OK and 204 No Content as success.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("prusalink command error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// MapCommand translates a high-level command into PrusaLink REST API calls.
func (a *PrusaLinkAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	if !a.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}

	switch command {
	case "home":
		return nil, a.SendCommand("G28", 60*time.Second)

	case "pause":
		return a.putJobAction("pause")

	case "resume":
		return a.putJobAction("resume")

	case "stop":
		return a.deleteJob()

	case "get_status":
		return a.fetchStatus()

	case "get_temperature":
		return a.fetchStatus()

	case "upload_file":
		fileName, ok := params["path"].(string)
		if !ok {
			return nil, fmt.Errorf("upload_file requires string 'path' parameter")
		}
		fileContent, ok := params["content"].([]byte)
		if !ok {
			return nil, fmt.Errorf("upload_file requires []byte 'content' parameter")
		}
		return a.uploadFile(fileName, fileContent)

	default:
		return nil, fmt.Errorf("unsupported command: %s", command)
	}
}

// doRequest performs an authenticated HTTP request against the PrusaLink API.
func (a *PrusaLinkAdapter) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	a.mu.RLock()
	baseURL := a.baseURL
	apiKey := a.apiKey
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(a.ctx, method, baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return a.httpClient.Do(req)
}

// fetchStatus retrieves printer status from GET /api/v1/status.
func (a *PrusaLinkAdapter) fetchStatus() (interface{}, error) {
	resp, err := a.doRequest("GET", "/api/v1/status", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse status: %w", err)
	}
	return result, nil
}

// putJobAction sends a job control action (pause/resume) via PUT /api/v1/job.
func (a *PrusaLinkAdapter) putJobAction(action string) (interface{}, error) {
	payload := map[string]string{"command": action}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job action: %w", err)
	}

	resp, err := a.doRequest("PUT", "/api/v1/job", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to send job action: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("job action error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return map[string]string{"status": "ok"}, nil
}

// deleteJob cancels the current print via DELETE /api/v1/job.
func (a *PrusaLinkAdapter) deleteJob() (interface{}, error) {
	resp, err := a.doRequest("DELETE", "/api/v1/job", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to cancel job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cancel job error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return map[string]string{"status": "ok"}, nil
}

// uploadFile uploads a G-code file via POST /api/v1/files.
func (a *PrusaLinkAdapter) uploadFile(fileName string, content []byte) (interface{}, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("failed to create multipart field: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("failed to write file content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	a.mu.RLock()
	baseURL := a.baseURL
	apiKey := a.apiKey
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(a.ctx, "POST", baseURL+"/api/v1/files", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Api-Key", apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("file upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("file upload error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse upload response: %w", err)
	}
	return result, nil
}

// pollLoop periodically polls PrusaLink for telemetry data.
func (a *PrusaLinkAdapter) pollLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if !a.IsConnected() {
				return
			}
			a.pollTelemetry()
		}
	}
}

// pollTelemetry fetches printer status, updates internal state, and invokes callback.
func (a *PrusaLinkAdapter) pollTelemetry() {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var metrics []PrusaLinkTelemetryMetric

	// Fetch printer status from /api/v1/status.
	resp, err := a.doRequest("GET", "/api/v1/status", nil)
	if err != nil {
		a.log.WithError(err).Debug("Telemetry poll: failed to get status")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		a.log.WithField("status_code", resp.StatusCode).Debug("Telemetry poll: non-200 status response")
		return
	}

	var statusData struct {
		Printer struct {
			State        string  `json:"state"`
			TempNozzle   float64 `json:"temp_nozzle"`
			TargetNozzle float64 `json:"target_nozzle"`
			TempBed      float64 `json:"temp_bed"`
			TargetBed    float64 `json:"target_bed"`
			Speed        int     `json:"speed"`
			Flow         int     `json:"flow"`
		} `json:"printer"`
		Job struct {
			Progress float64 `json:"progress"`
		} `json:"job"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		a.log.WithError(err).Debug("Telemetry poll: failed to parse status")
		return
	}

	a.mu.Lock()
	a.status.State = statusData.Printer.State
	a.status.NozzleTemp = statusData.Printer.TempNozzle
	a.status.NozzleTarget = statusData.Printer.TargetNozzle
	a.status.BedTemp = statusData.Printer.TempBed
	a.status.BedTarget = statusData.Printer.TargetBed
	a.status.PrintProgress = statusData.Job.Progress
	a.status.PrintSpeed = statusData.Printer.Speed
	a.status.FlowFactor = statusData.Printer.Flow
	a.status.LastUpdate = time.Now()
	a.mu.Unlock()

	metrics = append(metrics,
		PrusaLinkTelemetryMetric{Type: "nozzle_temp", Value: statusData.Printer.TempNozzle, Unit: "celsius", Timestamp: now},
		PrusaLinkTelemetryMetric{Type: "bed_temp", Value: statusData.Printer.TempBed, Unit: "celsius", Timestamp: now},
		PrusaLinkTelemetryMetric{Type: "print_progress", Value: statusData.Job.Progress, Unit: "percent", Timestamp: now},
	)

	// Map PrusaLink state to a normalized print_state value.
	stateVal := 0.0
	switch statusData.Printer.State {
	case "PRINTING":
		stateVal = 1.0
	case "PAUSED":
		stateVal = 2.0
	case "ERROR":
		stateVal = 3.0
	case "IDLE", "FINISHED":
		stateVal = 0.0
	case "BUSY":
		stateVal = 4.0
	}
	metrics = append(metrics,
		PrusaLinkTelemetryMetric{Type: "print_state", Value: stateVal, Unit: "enum", Timestamp: now},
	)

	// Invoke the telemetry callback.
	if a.OnTelemetry != nil && len(metrics) > 0 {
		a.OnTelemetry(metrics)
	}
}
