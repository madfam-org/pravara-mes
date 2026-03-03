// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// MoonrakerStatus represents the current state of a Klipper printer via Moonraker.
type MoonrakerStatus struct {
	State          string    // standby, printing, paused, error, complete
	ExtruderTemp   float64   // Current extruder temperature
	ExtruderTarget float64   // Target extruder temperature
	BedTemp        float64   // Current bed temperature
	BedTarget      float64   // Target bed temperature
	Progress       float64   // Print progress 0.0-1.0
	PrintState     string    // Klipper print_stats state
	FanSpeed       float64   // Part cooling fan speed 0.0-1.0
	Filename       string    // Currently loaded file
	PrintDuration  float64   // Seconds elapsed
	LastUpdate     time.Time
}

// moonrakerWSMessage represents a JSON-RPC 2.0 message for the WebSocket API.
type moonrakerWSMessage struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method,omitempty"`
	Params  interface{} `json:"params,omitempty"`
	ID      int64       `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
}

// moonrakerQueryResult represents the response from /printer/objects/query.
type moonrakerQueryResult struct {
	Result struct {
		Status map[string]map[string]interface{} `json:"status"`
	} `json:"result"`
}

// MoonrakerAdapter handles communication with Klipper-based printers via Moonraker API.
// It uses HTTP REST for commands and WebSocket (JSON-RPC 2.0) for real-time telemetry.
type MoonrakerAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	status     MoonrakerStatus

	// Connection state
	host      string
	port      int
	apiKey    string
	connected bool
	baseURL   string

	// HTTP client
	httpClient *http.Client

	// WebSocket
	wsConn       *websocket.Conn
	wsCtx        context.Context
	wsCancel     context.CancelFunc
	wsReconnect  atomic.Bool
	wsNextID     atomic.Int64

	// Telemetry callback
	OnTelemetry TelemetryCallback
}

// NewMoonrakerAdapter creates a new Moonraker adapter.
func NewMoonrakerAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *MoonrakerAdapter {
	return &MoonrakerAdapter{
		log:        log.WithField("adapter", "moonraker"),
		definition: definition,
		port:       7125,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Connect establishes HTTP and WebSocket connections to Moonraker.
func (a *MoonrakerAdapter) Connect(host string, port int, apiKey string) error {
	a.mu.Lock()
	a.host = host
	if port > 0 {
		a.port = port
	}
	a.apiKey = apiKey
	a.baseURL = fmt.Sprintf("http://%s:%d", a.host, a.port)
	a.mu.Unlock()

	// Verify connectivity with a server info request
	if err := a.verifyConnection(); err != nil {
		return fmt.Errorf("moonraker connection verification failed: %w", err)
	}

	a.mu.Lock()
	a.connected = true
	a.mu.Unlock()

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": a.port,
	}).Info("Connected to Moonraker")

	// Start WebSocket subscription for real-time telemetry
	a.wsReconnect.Store(true)
	a.startWebSocket()

	return nil
}

// Disconnect closes all connections to Moonraker.
func (a *MoonrakerAdapter) Disconnect() error {
	a.wsReconnect.Store(false)

	if a.wsCancel != nil {
		a.wsCancel()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.wsConn != nil {
		a.wsConn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		a.wsConn.Close()
		a.wsConn = nil
	}

	a.connected = false
	a.log.Info("Disconnected from Moonraker")
	return nil
}

// IsConnected returns whether the adapter has an active connection.
func (a *MoonrakerAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current printer status.
func (a *MoonrakerAdapter) GetStatus() MoonrakerStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends raw G-code to the printer via Moonraker's gcode/script endpoint.
func (a *MoonrakerAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	payload := map[string]string{"script": command}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gcode payload: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/printer/gcode/script", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create gcode request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gcode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gcode request error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// MapCommand translates a high-level command into Moonraker API calls.
func (a *MoonrakerAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	if !a.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}

	switch command {
	case "home":
		return nil, a.SendCommand("G28", 60*time.Second)

	case "pause":
		return nil, a.postAPI("/printer/print/pause", nil, 10*time.Second)

	case "resume":
		return nil, a.postAPI("/printer/print/resume", nil, 10*time.Second)

	case "stop":
		return nil, a.postAPI("/printer/print/cancel", nil, 10*time.Second)

	case "emergency_stop":
		return nil, a.postAPI("/printer/emergency_stop", nil, 5*time.Second)

	case "gcode_line":
		line, ok := params["line"].(string)
		if !ok || line == "" {
			return nil, fmt.Errorf("gcode_line requires a 'line' string parameter")
		}
		return nil, a.SendCommand(line, 30*time.Second)

	case "get_temperature":
		return nil, a.SendCommand("M105", 5*time.Second)

	case "set_temp":
		return a.handleSetTemp(params)

	case "get_status":
		return a.queryPrinterObjects()

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// handleSetTemp sets extruder or bed temperature via G-code.
func (a *MoonrakerAdapter) handleSetTemp(params map[string]interface{}) (interface{}, error) {
	target, ok := params["target"].(string)
	if !ok {
		target = "extruder"
	}

	tempVal, ok := params["temp"]
	if !ok {
		return nil, fmt.Errorf("set_temp requires a 'temp' parameter")
	}
	temp, err := toFloat64(tempVal)
	if err != nil {
		return nil, fmt.Errorf("invalid temperature value: %w", err)
	}

	var gcode string
	switch strings.ToLower(target) {
	case "extruder", "nozzle":
		gcode = fmt.Sprintf("M104 S%.0f", temp)
	case "bed":
		gcode = fmt.Sprintf("M140 S%.0f", temp)
	default:
		return nil, fmt.Errorf("unknown temperature target: %s", target)
	}

	return nil, a.SendCommand(gcode, 5*time.Second)
}

// queryPrinterObjects fetches current printer state from the query endpoint.
func (a *MoonrakerAdapter) queryPrinterObjects() (interface{}, error) {
	url := a.baseURL + "/printer/objects/query?heater_bed&extruder&print_stats&display_status"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create query request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("query error %d: %s", resp.StatusCode, string(respBody))
	}

	var result moonrakerQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse query response: %w", err)
	}

	// Update local status from query result
	a.updateStatusFromQuery(result.Result.Status)

	return result.Result.Status, nil
}

// postAPI performs a POST request to a Moonraker API endpoint.
func (a *MoonrakerAdapter) postAPI(path string, payload interface{}, timeout time.Duration) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", path, err)
	}
	a.setHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request to %s error %d: %s", path, resp.StatusCode, string(respBody))
	}

	return nil
}

// setHeaders applies common headers including optional API key.
func (a *MoonrakerAdapter) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	a.mu.RLock()
	apiKey := a.apiKey
	a.mu.RUnlock()
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
}

// verifyConnection tests connectivity by requesting server info.
func (a *MoonrakerAdapter) verifyConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/server/info", nil)
	if err != nil {
		return err
	}
	a.setHeaders(req)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}
	return nil
}

// startWebSocket connects to Moonraker's WebSocket and subscribes to printer objects.
func (a *MoonrakerAdapter) startWebSocket() {
	a.wsCtx, a.wsCancel = context.WithCancel(context.Background())
	go a.wsLoop()
}

// wsLoop manages the WebSocket connection lifecycle with auto-reconnect.
func (a *MoonrakerAdapter) wsLoop() {
	for {
		if !a.wsReconnect.Load() {
			return
		}

		if err := a.wsConnect(); err != nil {
			a.log.WithError(err).Warn("WebSocket connection failed, retrying in 5s")
			select {
			case <-a.wsCtx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Subscribe to printer objects
		if err := a.wsSubscribe(); err != nil {
			a.log.WithError(err).Warn("WebSocket subscription failed")
		}

		// Read messages until disconnect
		a.wsReadLoop()

		a.log.Info("WebSocket disconnected")

		if !a.wsReconnect.Load() {
			return
		}

		a.log.Info("Attempting WebSocket reconnection in 3s")
		select {
		case <-a.wsCtx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// wsConnect establishes the WebSocket connection.
func (a *MoonrakerAdapter) wsConnect() error {
	a.mu.RLock()
	wsURL := fmt.Sprintf("ws://%s:%d/websocket", a.host, a.port)
	a.mu.RUnlock()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	headers := http.Header{}
	a.mu.RLock()
	if a.apiKey != "" {
		headers.Set("X-Api-Key", a.apiKey)
	}
	a.mu.RUnlock()

	conn, _, err := dialer.DialContext(a.wsCtx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	a.mu.Lock()
	a.wsConn = conn
	a.mu.Unlock()

	a.log.Info("WebSocket connected")
	return nil
}

// wsSubscribe sends a JSON-RPC subscription for printer objects.
func (a *MoonrakerAdapter) wsSubscribe() error {
	a.mu.RLock()
	conn := a.wsConn
	a.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	id := a.wsNextID.Add(1)
	msg := moonrakerWSMessage{
		JSONRPC: "2.0",
		Method:  "printer.objects.subscribe",
		Params: map[string]interface{}{
			"objects": map[string]interface{}{
				"extruder":       nil,
				"heater_bed":     nil,
				"print_stats":    nil,
				"display_status": nil,
				"fan":            nil,
			},
		},
		ID: id,
	}

	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("websocket subscribe failed: %w", err)
	}

	a.log.Debug("Subscribed to printer objects via WebSocket")
	return nil
}

// wsReadLoop reads messages from the WebSocket and processes telemetry updates.
func (a *MoonrakerAdapter) wsReadLoop() {
	for {
		select {
		case <-a.wsCtx.Done():
			return
		default:
		}

		a.mu.RLock()
		conn := a.wsConn
		a.mu.RUnlock()

		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				return
			}
			a.log.WithError(err).Debug("WebSocket read error")
			return
		}

		a.processWSMessage(message)
	}
}

// processWSMessage parses and processes a WebSocket JSON-RPC message.
func (a *MoonrakerAdapter) processWSMessage(data []byte) {
	var msg struct {
		Method string `json:"method"`
		Params []struct {
			Status map[string]map[string]interface{} `json:"status"`
		} `json:"params"`
		Result *struct {
			Status map[string]map[string]interface{} `json:"status"`
		} `json:"result"`
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		a.log.WithError(err).Debug("Failed to parse WebSocket message")
		return
	}

	// Handle subscription initial result
	if msg.Result != nil && msg.Result.Status != nil {
		a.updateStatusFromQuery(msg.Result.Status)
		a.emitTelemetry()
		return
	}

	// Handle notify_status_update events
	if msg.Method == "notify_status_update" && len(msg.Params) > 0 {
		a.updateStatusFromQuery(msg.Params[0].Status)
		a.emitTelemetry()
	}
}

// updateStatusFromQuery updates the local status from a Moonraker object query result.
func (a *MoonrakerAdapter) updateStatusFromQuery(status map[string]map[string]interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if extruder, ok := status["extruder"]; ok {
		if temp, ok := extruder["temperature"]; ok {
			a.status.ExtruderTemp, _ = toFloat64(temp)
		}
		if target, ok := extruder["target"]; ok {
			a.status.ExtruderTarget, _ = toFloat64(target)
		}
	}

	if bed, ok := status["heater_bed"]; ok {
		if temp, ok := bed["temperature"]; ok {
			a.status.BedTemp, _ = toFloat64(temp)
		}
		if target, ok := bed["target"]; ok {
			a.status.BedTarget, _ = toFloat64(target)
		}
	}

	if stats, ok := status["print_stats"]; ok {
		if state, ok := stats["state"].(string); ok {
			a.status.PrintState = state
			a.status.State = a.mapKlipperState(state)
		}
		if filename, ok := stats["filename"].(string); ok {
			a.status.Filename = filename
		}
		if duration, ok := stats["print_duration"]; ok {
			a.status.PrintDuration, _ = toFloat64(duration)
		}
	}

	if display, ok := status["display_status"]; ok {
		if progress, ok := display["progress"]; ok {
			a.status.Progress, _ = toFloat64(progress)
		}
	}

	if fan, ok := status["fan"]; ok {
		if speed, ok := fan["speed"]; ok {
			a.status.FanSpeed, _ = toFloat64(speed)
		}
	}

	a.status.LastUpdate = time.Now()
}

// mapKlipperState converts Klipper print_stats state to a normalized status.
func (a *MoonrakerAdapter) mapKlipperState(state string) string {
	switch strings.ToLower(state) {
	case "standby":
		return "idle"
	case "printing":
		return "running"
	case "paused":
		return "paused"
	case "error":
		return "error"
	case "complete":
		return "idle"
	default:
		return state
	}
}

// emitTelemetry publishes the current status as telemetry metrics.
func (a *MoonrakerAdapter) emitTelemetry() {
	if a.OnTelemetry == nil {
		return
	}

	a.mu.RLock()
	s := a.status
	a.mu.RUnlock()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	metrics := []TelemetryMetric{
		{Type: "extruder_temp", Value: s.ExtruderTemp, Unit: "celsius", Timestamp: now},
		{Type: "bed_temp", Value: s.BedTemp, Unit: "celsius", Timestamp: now},
		{Type: "print_progress", Value: s.Progress, Unit: "ratio", Timestamp: now},
		{Type: "fan_speed", Value: s.FanSpeed, Unit: "ratio", Timestamp: now},
	}

	// Encode print state as a numeric value for telemetry
	stateValue := 0.0
	switch s.State {
	case "idle":
		stateValue = 0
	case "running":
		stateValue = 1
	case "paused":
		stateValue = 2
	case "error":
		stateValue = 3
	}
	metrics = append(metrics, TelemetryMetric{
		Type: "print_state", Value: stateValue, Unit: "enum", Timestamp: now,
	})

	a.OnTelemetry(metrics)
}

// toFloat64 is defined in graphtec_adapter.go and shared across adapters.
