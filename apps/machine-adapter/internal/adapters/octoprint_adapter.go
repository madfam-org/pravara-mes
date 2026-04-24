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

// OctoPrintTelemetryMetric represents a single telemetry data point for OctoPrint.
type OctoPrintTelemetryMetric struct {
	Type      string  `json:"type"`
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
	Timestamp string  `json:"timestamp"`
}

// OctoPrintTelemetryCallback is called when new OctoPrint telemetry data is available.
type OctoPrintTelemetryCallback func(metrics []OctoPrintTelemetryMetric)

// OctoPrintStatus represents the current state of the OctoPrint-connected printer.
type OctoPrintStatus struct {
	State          string    // Operational, Printing, Paused, Error, Offline
	NozzleTemp     float64   // Current nozzle temperature
	NozzleTarget   float64   // Target nozzle temperature
	BedTemp        float64   // Current bed temperature
	BedTarget      float64   // Target bed temperature
	PrintProgress  float64   // Print progress percentage (0-100)
	PrintFileName  string    // Name of the file being printed
	PrintTimeLeft  int       // Estimated time remaining in seconds
	PrintTimeSpent int       // Time spent printing in seconds
	LastUpdate     time.Time // Timestamp of last status update
}

// OctoPrintAdapter handles communication with OctoPrint instances via REST API.
// It communicates over HTTP at port 5000 with X-Api-Key authentication and uses
// polling for telemetry collection.
type OctoPrintAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	baseURL    string
	apiKey     string
	httpClient *http.Client
	status     OctoPrintStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Telemetry callback for publishing metrics.
	OnTelemetry OctoPrintTelemetryCallback
}

// NewOctoPrintAdapter creates a new OctoPrint adapter.
func NewOctoPrintAdapter(log *logrus.Logger) *OctoPrintAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &OctoPrintAdapter{
		log: log.WithField("adapter", "octoprint"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		ctx:    ctx,
		cancel: cancel,
	}
}

// Connect establishes a connection to the OctoPrint instance.
func (a *OctoPrintAdapter) Connect(host string, port int, apiKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s:%d", host, port)
	a.apiKey = apiKey

	// Verify connectivity by fetching the version endpoint.
	req, err := http.NewRequest("GET", a.baseURL+"/api/version", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Api-Key", a.apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("octoprint connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("octoprint auth failed (status %d): %s", resp.StatusCode, string(body))
	}

	a.connected = true
	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to OctoPrint")

	// Start background telemetry polling.
	go a.pollLoop()

	return nil
}

// Disconnect stops the adapter and releases resources.
func (a *OctoPrintAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false
	a.log.Info("Disconnected from OctoPrint")
	return nil
}

// IsConnected returns whether the adapter is connected.
func (a *OctoPrintAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current printer status.
func (a *OctoPrintAdapter) GetStatus() OctoPrintStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw G-code command to OctoPrint via POST /api/printer/command.
func (a *OctoPrintAdapter) SendCommand(command string, timeout time.Duration) error {
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

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/api/printer/command", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", a.apiKey)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("octoprint command failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("octoprint command error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// MapCommand translates a high-level command into OctoPrint REST API calls.
func (a *OctoPrintAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	if !a.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}

	switch command {
	case "home":
		return nil, a.SendCommand("G28", 60*time.Second)

	case "pause":
		return a.postJob(map[string]interface{}{
			"command": "pause",
			"action":  "pause",
		})

	case "resume":
		return a.postJob(map[string]interface{}{
			"command": "pause",
			"action":  "resume",
		})

	case "stop":
		return a.postJob(map[string]interface{}{
			"command": "cancel",
		})

	case "emergency_stop":
		return nil, a.SendCommand("M112", 1*time.Second)

	case "gcode_line":
		line, ok := params["line"].(string)
		if !ok {
			return nil, fmt.Errorf("gcode_line requires string 'line' parameter")
		}
		return nil, a.SendCommand(line, 30*time.Second)

	case "get_temperature":
		return a.getPrinterState()

	case "upload_file":
		filePath, ok := params["path"].(string)
		if !ok {
			return nil, fmt.Errorf("upload_file requires string 'path' parameter")
		}
		fileContent, ok := params["content"].([]byte)
		if !ok {
			return nil, fmt.Errorf("upload_file requires []byte 'content' parameter")
		}
		return a.uploadFile(filePath, fileContent)

	case "start_print":
		fileName, ok := params["file"].(string)
		if !ok {
			return nil, fmt.Errorf("start_print requires string 'file' parameter")
		}
		return a.postJob(map[string]interface{}{
			"command": "start",
			"file":    fileName,
		})

	default:
		return nil, fmt.Errorf("unsupported command: %s", command)
	}
}

// doRequest performs an authenticated HTTP request against the OctoPrint API.
func (a *OctoPrintAdapter) doRequest(method, path string, body io.Reader) (*http.Response, error) {
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

// getPrinterState fetches printer state and temperatures from GET /api/printer.
func (a *OctoPrintAdapter) getPrinterState() (interface{}, error) {
	resp, err := a.doRequest("GET", "/api/printer", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get printer state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		// Printer is not connected or not operational.
		return map[string]string{"state": "offline"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("printer state error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse printer state: %w", err)
	}
	return result, nil
}

// getJobState fetches print job information from GET /api/job.
func (a *OctoPrintAdapter) getJobState() (map[string]interface{}, error) {
	resp, err := a.doRequest("GET", "/api/job", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get job state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("job state error (status %d): %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse job state: %w", err)
	}
	return result, nil
}

// postJob sends a job command via PATCH /api/job.
func (a *OctoPrintAdapter) postJob(payload map[string]interface{}) (interface{}, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal job payload: %w", err)
	}

	resp, err := a.doRequest("POST", "/api/job", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to post job command: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("job command error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return map[string]string{"status": "ok"}, nil
}

// uploadFile uploads a G-code file via POST /api/files/local.
func (a *OctoPrintAdapter) uploadFile(fileName string, content []byte) (interface{}, error) {
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

	req, err := http.NewRequestWithContext(a.ctx, "POST", baseURL+"/api/files/local", &buf)
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

// pollLoop periodically polls OctoPrint for telemetry data.
func (a *OctoPrintAdapter) pollLoop() {
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

// pollTelemetry fetches printer state and job progress, updates internal status,
// and invokes the telemetry callback.
func (a *OctoPrintAdapter) pollTelemetry() {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var metrics []OctoPrintTelemetryMetric

	// Fetch printer state (temperatures + state).
	printerResp, err := a.doRequest("GET", "/api/printer", nil)
	if err != nil {
		a.log.WithError(err).Debug("Telemetry poll: failed to get printer state")
		return
	}
	defer printerResp.Body.Close()

	if printerResp.StatusCode == http.StatusOK {
		var printerData struct {
			State struct {
				Text string `json:"text"`
			} `json:"state"`
			Temperature struct {
				Tool0 struct {
					Actual float64 `json:"actual"`
					Target float64 `json:"target"`
				} `json:"tool0"`
				Bed struct {
					Actual float64 `json:"actual"`
					Target float64 `json:"target"`
				} `json:"bed"`
			} `json:"temperature"`
		}
		if err := json.NewDecoder(printerResp.Body).Decode(&printerData); err == nil {
			a.mu.Lock()
			a.status.State = printerData.State.Text
			a.status.NozzleTemp = printerData.Temperature.Tool0.Actual
			a.status.NozzleTarget = printerData.Temperature.Tool0.Target
			a.status.BedTemp = printerData.Temperature.Bed.Actual
			a.status.BedTarget = printerData.Temperature.Bed.Target
			a.status.LastUpdate = time.Now()
			a.mu.Unlock()

			metrics = append(metrics,
				OctoPrintTelemetryMetric{Type: "nozzle_temp", Value: printerData.Temperature.Tool0.Actual, Unit: "celsius", Timestamp: now},
				OctoPrintTelemetryMetric{Type: "bed_temp", Value: printerData.Temperature.Bed.Actual, Unit: "celsius", Timestamp: now},
			)
		}
	}

	// Fetch job state (progress).
	jobResp, err := a.doRequest("GET", "/api/job", nil)
	if err != nil {
		a.log.WithError(err).Debug("Telemetry poll: failed to get job state")
	} else {
		defer jobResp.Body.Close()
		if jobResp.StatusCode == http.StatusOK {
			var jobData struct {
				State    string `json:"state"`
				Progress struct {
					Completion    float64 `json:"completion"`
					PrintTime     int     `json:"printTime"`
					PrintTimeLeft int     `json:"printTimeLeft"`
				} `json:"progress"`
				Job struct {
					File struct {
						Name string `json:"name"`
					} `json:"file"`
				} `json:"job"`
			}
			if err := json.NewDecoder(jobResp.Body).Decode(&jobData); err == nil {
				a.mu.Lock()
				a.status.PrintProgress = jobData.Progress.Completion
				a.status.PrintTimeSpent = jobData.Progress.PrintTime
				a.status.PrintTimeLeft = jobData.Progress.PrintTimeLeft
				a.status.PrintFileName = jobData.Job.File.Name
				a.mu.Unlock()

				metrics = append(metrics,
					OctoPrintTelemetryMetric{Type: "print_progress", Value: jobData.Progress.Completion, Unit: "percent", Timestamp: now},
				)

				// Map OctoPrint state to a normalized print_state value.
				stateVal := 0.0
				switch jobData.State {
				case "Printing":
					stateVal = 1.0
				case "Paused", "Pausing":
					stateVal = 2.0
				case "Error":
					stateVal = 3.0
				case "Operational":
					stateVal = 0.0
				}
				metrics = append(metrics,
					OctoPrintTelemetryMetric{Type: "print_state", Value: stateVal, Unit: "enum", Timestamp: now},
				)
			}
		}
	}

	// Invoke the telemetry callback.
	if a.OnTelemetry != nil && len(metrics) > 0 {
		a.OnTelemetry(metrics)
	}
}
