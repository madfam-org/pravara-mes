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

// OpenPnPStatus represents the current state of an OpenPnP pick-and-place machine.
type OpenPnPStatus struct {
	State      string  // idle, running, paused, error
	PositionX  float64 // Nozzle X position in mm
	PositionY  float64 // Nozzle Y position in mm
	PositionZ  float64 // Nozzle Z position in mm
	Rotation   float64 // Nozzle rotation in degrees
	VacuumOn   bool    // Whether vacuum/suction is active
	LastUpdate time.Time
}

// openPnPScriptRequest represents a request to the OpenPnP scripting API.
type openPnPScriptRequest struct {
	Script string `json:"script"`
}

// openPnPStatusResponse represents the status response from the OpenPnP API.
type openPnPStatusResponse struct {
	State    string `json:"state"`
	Position struct {
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Z        float64 `json:"z"`
		Rotation float64 `json:"rotation"`
	} `json:"position"`
	Nozzle struct {
		VacuumOn bool `json:"vacuumOn"`
	} `json:"nozzle"`
}

// OpenPnPAdapter handles communication with OpenPnP pick-and-place machines.
// OpenPnP exposes an HTTP scripting API and uses G-code for motion control.
type OpenPnPAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	status     OpenPnPStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// HTTP connection
	baseURL    string
	httpClient *http.Client

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewOpenPnPAdapter creates a new OpenPnP pick-and-place adapter.
func NewOpenPnPAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *OpenPnPAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &OpenPnPAdapter{
		log:        log.WithField("adapter", "openpnp"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		status: OpenPnPStatus{
			State: "idle",
		},
	}
}

// Connect establishes a connection to the OpenPnP HTTP API.
func (a *OpenPnPAdapter) Connect(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.baseURL = fmt.Sprintf("http://%s:%d", host, port)

	// Verify connectivity by querying status.
	if err := a.queryStatusLocked(); err != nil {
		return fmt.Errorf("failed to connect to OpenPnP at %s:%d: %w", host, port, err)
	}

	a.connected = true

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to OpenPnP pick-and-place")

	// Start background status polling.
	go a.statusLoop()

	return nil
}

// Disconnect closes the connection.
func (a *OpenPnPAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false
	a.baseURL = ""

	a.log.Info("Disconnected from OpenPnP")
	return nil
}

// IsConnected returns true if connected to the OpenPnP machine.
func (a *OpenPnPAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current machine status.
func (a *OpenPnPAdapter) GetStatus() OpenPnPStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw G-code command to the OpenPnP machine (implements CommandExecutor).
func (a *OpenPnPAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return a.sendGCode(command)
}

// MapCommand translates high-level command names to OpenPnP operations.
// Supported commands: home, move_to, pick, place, get_position, set_feeder.
func (a *OpenPnPAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		return nil, a.sendGCode("G28")

	case "move_to":
		x, hasX := params["x"]
		y, hasY := params["y"]
		z, hasZ := params["z"]
		if !hasX && !hasY && !hasZ {
			return nil, fmt.Errorf("move_to requires at least one of x, y, z parameters")
		}
		cmd := "G0"
		if hasX {
			xf, err := toFloat64(x)
			if err != nil {
				return nil, fmt.Errorf("invalid x: %w", err)
			}
			cmd += fmt.Sprintf(" X%.3f", xf)
		}
		if hasY {
			yf, err := toFloat64(y)
			if err != nil {
				return nil, fmt.Errorf("invalid y: %w", err)
			}
			cmd += fmt.Sprintf(" Y%.3f", yf)
		}
		if hasZ {
			zf, err := toFloat64(z)
			if err != nil {
				return nil, fmt.Errorf("invalid z: %w", err)
			}
			cmd += fmt.Sprintf(" Z%.3f", zf)
		}
		return nil, a.sendGCode(cmd)

	case "pick":
		// Move to position, wait for completion, enable vacuum.
		if err := a.sendGCode("M400"); err != nil {
			return nil, fmt.Errorf("failed to wait for move completion: %w", err)
		}
		// Enable vacuum (M10 is commonly used for vacuum on in OpenPnP).
		if err := a.sendGCode("M10"); err != nil {
			return nil, fmt.Errorf("failed to enable vacuum: %w", err)
		}
		a.mu.Lock()
		a.status.VacuumOn = true
		a.mu.Unlock()
		return map[string]interface{}{"vacuum": "on"}, nil

	case "place":
		// Wait for move completion, disable vacuum.
		if err := a.sendGCode("M400"); err != nil {
			return nil, fmt.Errorf("failed to wait for move completion: %w", err)
		}
		// Disable vacuum (M11 is commonly used for vacuum off in OpenPnP).
		if err := a.sendGCode("M11"); err != nil {
			return nil, fmt.Errorf("failed to disable vacuum: %w", err)
		}
		a.mu.Lock()
		a.status.VacuumOn = false
		a.mu.Unlock()
		return map[string]interface{}{"vacuum": "off"}, nil

	case "get_position":
		if err := a.sendGCode("M114"); err != nil {
			return nil, fmt.Errorf("failed to query position: %w", err)
		}
		status := a.GetStatus()
		return map[string]interface{}{
			"x":        status.PositionX,
			"y":        status.PositionY,
			"z":        status.PositionZ,
			"rotation": status.Rotation,
		}, nil

	case "set_feeder":
		feederID, ok := params["feeder_id"]
		if !ok {
			return nil, fmt.Errorf("set_feeder requires feeder_id parameter")
		}
		partID, _ := params["part_id"]
		return nil, a.executeScript(fmt.Sprintf(
			"machine.getFeeders().getFeederByName('%v').setPart(configuration.getParts().getPartByName('%v'))",
			feederID, partID,
		))

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendGCode sends a G-code command via the OpenPnP HTTP API.
func (a *OpenPnPAdapter) sendGCode(gcode string) error {
	return a.executeScript(fmt.Sprintf("machine.execute('gcode', '%s')", gcode))
}

// executeScript executes a scripting command through the OpenPnP API.
func (a *OpenPnPAdapter) executeScript(script string) error {
	a.mu.RLock()
	baseURL := a.baseURL
	a.mu.RUnlock()

	payload, err := json.Marshal(openPnPScriptRequest{Script: script})
	if err != nil {
		return fmt.Errorf("failed to marshal script request: %w", err)
	}

	req, err := http.NewRequestWithContext(a.ctx, "POST", baseURL+"/api/scripting/execute", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("script execution failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("script execution error %d: %s", resp.StatusCode, string(body))
	}

	a.log.WithField("script", script).Debug("Executed OpenPnP script")
	return nil
}

// queryStatusLocked queries the OpenPnP status endpoint. Caller must hold at least a read lock
// or call this before setting connected=true.
func (a *OpenPnPAdapter) queryStatusLocked() error {
	resp, err := a.httpClient.Get(a.baseURL + "/api/machine/status")
	if err != nil {
		return fmt.Errorf("status query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status query error: %d", resp.StatusCode)
	}

	var statusResp openPnPStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return fmt.Errorf("failed to parse status response: %w", err)
	}

	a.status = OpenPnPStatus{
		State:      statusResp.State,
		PositionX:  statusResp.Position.X,
		PositionY:  statusResp.Position.Y,
		PositionZ:  statusResp.Position.Z,
		Rotation:   statusResp.Position.Rotation,
		VacuumOn:   statusResp.Nozzle.VacuumOn,
		LastUpdate: time.Now(),
	}

	return nil
}

// statusLoop periodically polls the machine for status.
func (a *OpenPnPAdapter) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
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

// pollStatus queries the machine status and publishes telemetry.
func (a *OpenPnPAdapter) pollStatus() {
	a.mu.Lock()
	err := a.queryStatusLocked()
	status := a.status
	a.mu.Unlock()

	if err != nil {
		a.log.WithError(err).Debug("Failed to poll OpenPnP status")
		return
	}

	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		vacuumVal := 0.0
		if status.VacuumOn {
			vacuumVal = 1.0
		}
		a.OnTelemetry([]TelemetryMetric{
			{Type: "position_x", Value: status.PositionX, Unit: "mm", Timestamp: now},
			{Type: "position_y", Value: status.PositionY, Unit: "mm", Timestamp: now},
			{Type: "position_z", Value: status.PositionZ, Unit: "mm", Timestamp: now},
			{Type: "nozzle_rotation", Value: status.Rotation, Unit: "degrees", Timestamp: now},
			{Type: "vacuum_state", Value: vacuumVal, Unit: "bool", Timestamp: now},
		})
	}
}
