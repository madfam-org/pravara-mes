// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// DuetStatus represents the current state of a Duet3D printer.
type DuetStatus struct {
	State          string     // idle, running, paused, error
	ExtruderTemp   float64    // Current extruder temperature (celsius)
	ExtruderTarget float64    // Target extruder temperature
	BedTemp        float64    // Current bed temperature
	BedTarget      float64    // Target bed temperature
	Position       [3]float64 // Current X, Y, Z position (mm)
	PrintProgress  float64    // Print progress (0.0-1.0)
	FanPercent     float64    // Part cooling fan speed (percent)
	LastUpdate     time.Time
}

// duetStatusResponse models the JSON returned by /rr_status?type=3.
type duetStatusResponse struct {
	Status string `json:"status"` // I, P, S, D, H, etc.
	Result int    `json:"result"` // Result code (only on some endpoints)
	Coords struct {
		XYZ []float64 `json:"xyz"`
	} `json:"coords"`
	Params struct {
		FanPercent []float64 `json:"fanPercent"`
	} `json:"params"`
	Temps struct {
		Current []float64 `json:"current"` // [bed, ext0, ext1, ...]
		Tools   struct {
			Active  [][]float64 `json:"active"`
			Standby [][]float64 `json:"standby"`
		} `json:"tools"`
		Bed struct {
			Current float64 `json:"current"`
			Active  float64 `json:"active"`
		} `json:"bed"`
	} `json:"temps"`
	CurrentTool int `json:"currentTool"`
	FractionPrinted float64 `json:"fractionPrinted"` // 0.0-1.0
}

// DuetAdapter handles communication with Duet3D / RepRapFirmware machines
// via their HTTP API. It supports both the legacy /rr_ endpoints (Duet 2)
// and the object model approach (Duet 3).
type DuetAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	httpClient *http.Client
	baseURL    string
	connected  bool
	status     DuetStatus
	stopPoll   chan struct{}

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewDuetAdapter creates a new Duet3D adapter.
func NewDuetAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *DuetAdapter {
	return &DuetAdapter{
		log:        log.WithField("adapter", "duet"),
		definition: definition,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopPoll: make(chan struct{}),
	}
}

// Connect establishes a session with the Duet controller. The host should be
// an IP or hostname (no scheme). If the board is password-protected, pass the
// password; otherwise use an empty string.
func (a *DuetAdapter) Connect(host string, password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s", host)

	// Authenticate via /rr_connect
	connectURL := fmt.Sprintf("%s/rr_connect?password=%s", a.baseURL, url.QueryEscape(password))
	resp, err := a.httpClient.Get(connectURL)
	if err != nil {
		return fmt.Errorf("duet connect failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("duet connect error %d: %s", resp.StatusCode, string(body))
	}

	var connectResp struct {
		Err int `json:"err"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&connectResp); err != nil {
		return fmt.Errorf("failed to parse connect response: %w", err)
	}
	if connectResp.Err != 0 {
		return fmt.Errorf("duet authentication failed (err=%d)", connectResp.Err)
	}

	a.connected = true
	a.log.WithField("host", host).Info("Connected to Duet controller")

	// Start background status polling.
	go a.pollStatusLoop()

	return nil
}

// Disconnect closes the session with the Duet controller.
func (a *DuetAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	// Signal the polling goroutine to stop.
	close(a.stopPoll)

	// Disconnect from the board.
	disconnectURL := fmt.Sprintf("%s/rr_disconnect", a.baseURL)
	resp, err := a.httpClient.Get(disconnectURL)
	if err != nil {
		a.log.WithError(err).Warn("Disconnect request failed")
	} else {
		resp.Body.Close()
	}

	a.connected = false
	a.stopPoll = make(chan struct{}) // Reset for potential reconnect.
	a.log.Info("Disconnected from Duet controller")
	return nil
}

// IsConnected returns true if the adapter has an active session.
func (a *DuetAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns a snapshot of the current printer status.
func (a *DuetAdapter) GetStatus() DuetStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw G-code command to the Duet via the /rr_gcode
// endpoint. This implements the CommandExecutor interface.
func (a *DuetAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	client := &http.Client{Timeout: timeout}
	gcodeURL := fmt.Sprintf("%s/rr_gcode?gcode=%s", a.baseURL, url.QueryEscape(command))

	resp, err := client.Get(gcodeURL)
	if err != nil {
		return fmt.Errorf("gcode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcode error %d: %s", resp.StatusCode, string(body))
	}

	a.log.WithField("gcode", command).Debug("Sent G-code command")
	return nil
}

// MapCommand translates high-level command names to Duet API calls.
// Supported commands: home, pause, resume, stop, emergency_stop, gcode_line,
// get_temperature, set_temp_extruder, set_temp_bed.
func (a *DuetAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		return nil, a.SendCommand("G28", 60*time.Second)

	case "pause":
		return nil, a.SendCommand("M25", 5*time.Second)

	case "resume":
		return nil, a.SendCommand("M24", 5*time.Second)

	case "stop":
		// M0 performs an orderly stop, turning off heaters.
		return nil, a.SendCommand("M0", 10*time.Second)

	case "emergency_stop":
		return nil, a.SendCommand("M112", 1*time.Second)

	case "gcode_line":
		gcode, ok := params["gcode"].(string)
		if !ok {
			return nil, fmt.Errorf("gcode_line requires 'gcode' string parameter")
		}
		return nil, a.SendCommand(gcode, 30*time.Second)

	case "get_temperature":
		status := a.GetStatus()
		return map[string]float64{
			"extruder_current": status.ExtruderTemp,
			"extruder_target":  status.ExtruderTarget,
			"bed_current":      status.BedTemp,
			"bed_target":       status.BedTarget,
		}, nil

	case "set_temp_extruder":
		temp, err := extractFloat(params, "temp")
		if err != nil {
			return nil, fmt.Errorf("set_temp_extruder requires 'temp' numeric parameter: %w", err)
		}
		if temp < 0 || temp > 300 {
			return nil, fmt.Errorf("extruder temp must be 0-300, got %.1f", temp)
		}
		gcode := fmt.Sprintf("M104 S%.1f", temp)
		return nil, a.SendCommand(gcode, 5*time.Second)

	case "set_temp_bed":
		temp, err := extractFloat(params, "temp")
		if err != nil {
			return nil, fmt.Errorf("set_temp_bed requires 'temp' numeric parameter: %w", err)
		}
		if temp < 0 || temp > 120 {
			return nil, fmt.Errorf("bed temp must be 0-120, got %.1f", temp)
		}
		gcode := fmt.Sprintf("M140 S%.1f", temp)
		return nil, a.SendCommand(gcode, 5*time.Second)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// pollStatusLoop polls /rr_status?type=3 every 2 seconds for telemetry.
func (a *DuetAdapter) pollStatusLoop() {
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

// fetchStatus retrieves the full status from /rr_status?type=3 and updates
// internal state and telemetry.
func (a *DuetAdapter) fetchStatus() error {
	statusURL := fmt.Sprintf("%s/rr_status?type=3", a.baseURL)
	resp, err := a.httpClient.Get(statusURL)
	if err != nil {
		return fmt.Errorf("status request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status error: %d", resp.StatusCode)
	}

	var sr duetStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return fmt.Errorf("failed to parse status: %w", err)
	}

	a.mu.Lock()

	// Map single-letter status to normalized state.
	a.status.State = a.mapState(sr.Status)

	// Position
	if len(sr.Coords.XYZ) >= 3 {
		a.status.Position = [3]float64{sr.Coords.XYZ[0], sr.Coords.XYZ[1], sr.Coords.XYZ[2]}
	}

	// Temperatures: current array is [bed, extruder0, extruder1, ...]
	if len(sr.Temps.Current) >= 2 {
		a.status.BedTemp = sr.Temps.Current[0]
		a.status.ExtruderTemp = sr.Temps.Current[1]
	}
	a.status.BedTarget = sr.Temps.Bed.Active
	if len(sr.Temps.Tools.Active) > 0 && len(sr.Temps.Tools.Active[0]) > 0 {
		a.status.ExtruderTarget = sr.Temps.Tools.Active[0][0]
	}

	// Fan
	if len(sr.Params.FanPercent) > 0 {
		a.status.FanPercent = sr.Params.FanPercent[0]
	}

	// Print progress
	a.status.PrintProgress = sr.FractionPrinted
	a.status.LastUpdate = time.Now()

	// Copy values for telemetry emission outside the lock.
	extTemp := a.status.ExtruderTemp
	extTarget := a.status.ExtruderTarget
	bedTemp := a.status.BedTemp
	bedTarget := a.status.BedTarget
	pos := a.status.Position
	progress := a.status.PrintProgress
	fan := a.status.FanPercent

	a.mu.Unlock()

	// Emit telemetry.
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.OnTelemetry([]TelemetryMetric{
			{Type: "extruder_temp", Value: extTemp, Unit: "celsius", Timestamp: now},
			{Type: "extruder_target", Value: extTarget, Unit: "celsius", Timestamp: now},
			{Type: "bed_temp", Value: bedTemp, Unit: "celsius", Timestamp: now},
			{Type: "bed_target", Value: bedTarget, Unit: "celsius", Timestamp: now},
			{Type: "position_x", Value: pos[0], Unit: "mm", Timestamp: now},
			{Type: "position_y", Value: pos[1], Unit: "mm", Timestamp: now},
			{Type: "position_z", Value: pos[2], Unit: "mm", Timestamp: now},
			{Type: "print_progress", Value: progress * 100, Unit: "percent", Timestamp: now},
			{Type: "fan_speed", Value: fan, Unit: "percent", Timestamp: now},
		})
	}

	return nil
}

// mapState converts Duet single-character status codes to normalized strings.
func (a *DuetAdapter) mapState(code string) string {
	switch code {
	case "I":
		return "idle"
	case "P":
		return "running"
	case "S":
		return "paused"
	case "D":
		return "paused" // Decelerating -> paused
	case "H":
		return "error" // Halted
	default:
		return "unknown"
	}
}

// extractFloat extracts a float64 value from a params map, accepting float64,
// int, and string representations.
func extractFloat(params map[string]interface{}, key string) (float64, error) {
	v, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("missing parameter %q", key)
	}
	switch val := v.(type) {
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("parameter %q has unsupported type %T", key, v)
	}
}
