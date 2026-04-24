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

// XToolStatus represents the current state of an xTool laser machine.
type XToolStatus struct {
	State       string  // idle, running, paused, error
	LaserTemp   float64 // Laser module temperature in celsius
	JobProgress float64 // Job progress percentage (0-100)
	LastUpdate  time.Time
}

// XToolAdapter handles communication with xTool laser machines over WiFi/Ethernet
// via a reverse-engineered HTTP REST interface on port 8080.
type XToolAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	httpClient *http.Client
	baseURL    string
	connected  bool
	status     XToolStatus
	stopCh     chan struct{}

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewXToolAdapter creates a new xTool adapter.
func NewXToolAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *XToolAdapter {
	return &XToolAdapter{
		log:        log.WithField("adapter", "xtool"),
		definition: definition,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

// Connect establishes an HTTP connection to the xTool machine.
func (a *XToolAdapter) Connect(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if port == 0 {
		port = 8080
	}

	a.baseURL = fmt.Sprintf("http://%s:%d", host, port)

	// Verify connectivity by fetching device info.
	resp, err := a.httpClient.Get(a.baseURL + "/api/info")
	if err != nil {
		return fmt.Errorf("xtool connect failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("xtool connect error %d: %s", resp.StatusCode, string(body))
	}

	a.connected = true
	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to xTool machine")

	// Start status polling loop.
	go a.statusLoop()

	return nil
}

// Disconnect closes the connection to the xTool machine.
func (a *XToolAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	close(a.stopCh)
	a.connected = false
	a.baseURL = ""
	a.log.Info("Disconnected from xTool machine")
	return nil
}

// IsConnected returns true if the adapter has an active connection.
func (a *XToolAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns a snapshot of the current machine status.
func (a *XToolAdapter) GetStatus() XToolStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw command string to the xTool machine via the G-code
// endpoint. This implements the CommandExecutor interface.
func (a *XToolAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	client := &http.Client{Timeout: timeout}

	payload := fmt.Sprintf(`{"gcode":"%s"}`, command)
	req, err := http.NewRequest("POST", baseURL+"/api/gcode", strings.NewReader(payload))
	if err != nil {
		return fmt.Errorf("xtool request creation failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("xtool command failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("xtool command error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// MapCommand translates high-level command names to xTool HTTP REST actions.
// Supported commands: home, pause, resume, stop, gcode_line, get_status, set_laser_power.
func (a *XToolAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	if !a.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}

	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	switch command {
	case "home":
		return nil, a.SendCommand("G28", 30*time.Second)

	case "pause":
		return nil, a.postAction(baseURL + "/api/pause")

	case "resume":
		return nil, a.postAction(baseURL + "/api/start")

	case "stop":
		return nil, a.postAction(baseURL + "/api/stop")

	case "gcode_line":
		gcode, ok := params["gcode"].(string)
		if !ok {
			return nil, fmt.Errorf("gcode_line requires 'gcode' string parameter")
		}
		return nil, a.SendCommand(gcode, 30*time.Second)

	case "get_status":
		status, err := a.fetchStatus()
		if err != nil {
			return nil, err
		}
		return status, nil

	case "set_laser_power":
		power, ok := params["power"]
		if !ok {
			return nil, fmt.Errorf("set_laser_power requires 'power' parameter")
		}
		gcode := fmt.Sprintf("S%v", power)
		return nil, a.SendCommand(gcode, 5*time.Second)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// postAction sends an empty POST request to the given URL.
func (a *XToolAdapter) postAction(url string) error {
	resp, err := a.httpClient.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("xtool action failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("xtool action error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// xtoolStatusResponse represents the JSON response from GET /api/status.
type xtoolStatusResponse struct {
	State       string  `json:"state"`
	LaserTemp   float64 `json:"laser_temp"`
	JobProgress float64 `json:"job_progress"`
}

// fetchStatus retrieves the current machine status from the REST API.
func (a *XToolAdapter) fetchStatus() (*xtoolStatusResponse, error) {
	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	resp, err := a.httpClient.Get(baseURL + "/api/status")
	if err != nil {
		return nil, fmt.Errorf("xtool status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("xtool status error %d: %s", resp.StatusCode, string(body))
	}

	var status xtoolStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to parse xtool status: %w", err)
	}

	// Update internal status.
	a.mu.Lock()
	a.status.State = a.mapState(status.State)
	a.status.LaserTemp = status.LaserTemp
	a.status.JobProgress = status.JobProgress
	a.status.LastUpdate = time.Now()
	a.mu.Unlock()

	// Emit telemetry.
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "machine_state", Value: 0, Unit: a.status.State, Timestamp: now},
			{Type: "laser_temp", Value: status.LaserTemp, Unit: "celsius", Timestamp: now},
			{Type: "job_progress", Value: status.JobProgress, Unit: "percent", Timestamp: now},
		})
	}

	return &status, nil
}

// mapState converts xTool state strings to normalized status values.
func (a *XToolAdapter) mapState(state string) string {
	switch strings.ToLower(state) {
	case "idle":
		return "idle"
	case "running", "busy":
		return "running"
	case "paused":
		return "paused"
	case "error", "alarm":
		return "error"
	default:
		return "unknown"
	}
}

// statusLoop polls the machine status every 2 seconds.
func (a *XToolAdapter) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			if !a.IsConnected() {
				return
			}
			if _, err := a.fetchStatus(); err != nil {
				a.log.WithError(err).Debug("xTool status poll failed")
			}
		}
	}
}
