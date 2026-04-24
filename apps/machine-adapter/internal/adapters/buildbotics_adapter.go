// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// BuildboticsStatus represents the current state of a Buildbotics/Onefinity CNC.
type BuildboticsStatus struct {
	State        string  // ready, running, holding, stopping, estopped
	MachineState string  // Raw machine state string from the controller
	PositionX    float64 // Current X position in mm
	PositionY    float64 // Current Y position in mm
	PositionZ    float64 // Current Z position in mm
	SpindleSpeed float64 // Current spindle speed in RPM
	FeedRate     float64 // Current feed rate in mm/min
	Line         int     // Current G-code line number
	Progress     float64 // Job progress percentage
	LastUpdate   time.Time
}

// buildboticsStatusResponse represents the JSON response from GET /api/status.
type buildboticsStatusResponse struct {
	State    string  `json:"state"`
	Cycle    string  `json:"cycle"`
	Line     int     `json:"line"`
	Feed     float64 `json:"feed"`
	Speed    float64 `json:"speed"`
	Position struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		Z float64 `json:"z"`
	} `json:"position"`
	Progress float64 `json:"progress"`
}

// BuildboticsAdapter handles communication with Buildbotics-based CNC controllers
// (including Onefinity) via their HTTP REST API.
type BuildboticsAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	status     BuildboticsStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// HTTP connection
	baseURL    string
	httpClient *http.Client

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewBuildboticsAdapter creates a new Buildbotics CNC adapter.
func NewBuildboticsAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *BuildboticsAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &BuildboticsAdapter{
		log:        log.WithField("adapter", "buildbotics"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		status: BuildboticsStatus{
			State: "unknown",
		},
	}
}

// Connect establishes a connection to the Buildbotics REST API.
func (a *BuildboticsAdapter) Connect(host string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s", host)

	// Verify connectivity by querying status.
	if err := a.queryStatusLocked(); err != nil {
		return fmt.Errorf("failed to connect to Buildbotics at %s: %w", host, err)
	}

	a.connected = true

	a.log.WithField("host", host).Info("Connected to Buildbotics CNC controller")

	// Start background status polling.
	go a.statusLoop()

	return nil
}

// Disconnect closes the connection.
func (a *BuildboticsAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false
	a.baseURL = ""

	a.log.Info("Disconnected from Buildbotics CNC")
	return nil
}

// IsConnected returns true if connected to the controller.
func (a *BuildboticsAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current controller status.
func (a *BuildboticsAdapter) GetStatus() BuildboticsStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw G-code line to the controller (implements CommandExecutor).
func (a *BuildboticsAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return a.sendGCode(command)
}

// MapCommand translates high-level command names to Buildbotics API calls.
// Supported commands: home, pause, resume, stop, gcode_line, get_status.
func (a *BuildboticsAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		return nil, a.apiPUT("/api/home", nil)

	case "pause":
		return nil, a.apiPUT("/api/pause", nil)

	case "resume":
		return nil, a.apiPUT("/api/unpause", nil)

	case "stop":
		return nil, a.apiPUT("/api/stop", nil)

	case "start":
		return nil, a.apiPUT("/api/start", nil)

	case "gcode_line":
		gcodeVal, ok := params["gcode"]
		if !ok {
			return nil, fmt.Errorf("gcode_line requires gcode parameter")
		}
		gcode, ok := gcodeVal.(string)
		if !ok {
			return nil, fmt.Errorf("gcode must be a string")
		}
		return nil, a.sendGCode(gcode)

	case "get_status":
		a.mu.Lock()
		err := a.queryStatusLocked()
		a.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("failed to query status: %w", err)
		}
		status := a.GetStatus()
		return map[string]interface{}{
			"state":         status.State,
			"machine_state": status.MachineState,
			"position": map[string]float64{
				"x": status.PositionX,
				"y": status.PositionY,
				"z": status.PositionZ,
			},
			"spindle_speed": status.SpindleSpeed,
			"feed_rate":     status.FeedRate,
			"line":          status.Line,
			"progress":      status.Progress,
		}, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendGCode sends a G-code string to the Buildbotics controller via PUT /api/gcode.
func (a *BuildboticsAdapter) sendGCode(gcode string) error {
	payload, err := json.Marshal(map[string]string{"gcode": gcode})
	if err != nil {
		return fmt.Errorf("failed to marshal gcode payload: %w", err)
	}

	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	req, err := http.NewRequestWithContext(a.ctx, "PUT", baseURL+"/api/gcode", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create gcode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gcode send failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcode send error %d: %s", resp.StatusCode, string(body))
	}

	a.log.WithField("gcode", gcode).Debug("Sent G-code to Buildbotics")
	return nil
}

// apiPUT sends a PUT request to the given API path with an optional JSON body.
func (a *BuildboticsAdapter) apiPUT(path string, body interface{}) error {
	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(a.ctx, "PUT", baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create PUT request for %s: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("PUT %s error %d: %s", path, resp.StatusCode, string(respBody))
	}

	a.log.WithField("path", path).Debug("Buildbotics API PUT successful")
	return nil
}

// queryStatusLocked queries the status endpoint and updates internal state.
// Caller must hold the write lock.
func (a *BuildboticsAdapter) queryStatusLocked() error {
	resp, err := a.httpClient.Get(a.baseURL + "/api/status")
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status query error: %d", resp.StatusCode)
	}

	var statusResp buildboticsStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return fmt.Errorf("failed to parse status response: %w", err)
	}

	a.status = BuildboticsStatus{
		State:        a.normalizeState(statusResp.State),
		MachineState: statusResp.State,
		PositionX:    statusResp.Position.X,
		PositionY:    statusResp.Position.Y,
		PositionZ:    statusResp.Position.Z,
		SpindleSpeed: statusResp.Speed,
		FeedRate:     statusResp.Feed,
		Line:         statusResp.Line,
		Progress:     statusResp.Progress,
		LastUpdate:   time.Now(),
	}

	return nil
}

// normalizeState maps Buildbotics state strings to normalized status values.
func (a *BuildboticsAdapter) normalizeState(state string) string {
	switch state {
	case "READY":
		return "idle"
	case "RUNNING":
		return "running"
	case "HOLDING", "STOPPING":
		return "paused"
	case "ESTOPPED":
		return "estop"
	default:
		return "unknown"
	}
}

// statusLoop periodically polls the controller for status.
func (a *BuildboticsAdapter) statusLoop() {
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

// pollStatus queries the controller status and publishes telemetry.
func (a *BuildboticsAdapter) pollStatus() {
	a.mu.Lock()
	err := a.queryStatusLocked()
	status := a.status
	a.mu.Unlock()

	if err != nil {
		a.log.WithError(err).Debug("Failed to poll Buildbotics status")
		return
	}

	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		stateVal := 0.0
		switch status.State {
		case "idle":
			stateVal = 0
		case "running":
			stateVal = 1
		case "paused":
			stateVal = 2
		case "estop":
			stateVal = -1
		}
		a.OnTelemetry([]TelemetryMetric{
			{Type: "machine_state", Value: stateVal, Unit: "enum", Timestamp: now},
			{Type: "position_x", Value: status.PositionX, Unit: "mm", Timestamp: now},
			{Type: "position_y", Value: status.PositionY, Unit: "mm", Timestamp: now},
			{Type: "position_z", Value: status.PositionZ, Unit: "mm", Timestamp: now},
			{Type: "spindle_speed", Value: status.SpindleSpeed, Unit: "rpm", Timestamp: now},
			{Type: "feed_rate", Value: status.FeedRate, Unit: "mm/min", Timestamp: now},
			{Type: "progress", Value: status.Progress, Unit: "percent", Timestamp: now},
		})
	}
}
