// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// Ruida binary command bytes.
var (
	ruidaCmdStart    = []byte{0xD7, 0x00}
	ruidaCmdStop     = []byte{0xD7, 0x01}
	ruidaCmdPause    = []byte{0xD7, 0x02}
	ruidaCmdResume   = []byte{0xD7, 0x03}
	ruidaCmdStatus   = []byte{0xDA, 0x00, 0x04}
	ruidaCmdPosition = []byte{0xDA, 0x00, 0x00}
	ruidaCmdEStop    = []byte{0xD7, 0x01} // Same as stop; controller treats rapid stop identically
)

// Ruida machine state codes returned in status response.
const (
	ruidaStateIdle    byte = 0x00
	ruidaStateRunning byte = 0x01
	ruidaStatePaused  byte = 0x02
	ruidaStateFinish  byte = 0x03
	ruidaStateError   byte = 0xFF
)

// ruidaScrambleXOR is the XOR key used for simple packet scrambling.
const ruidaScrambleXOR byte = 0x88

// RuidaStatus represents the current state of a Ruida laser controller.
type RuidaStatus struct {
	State      string  // idle, running, paused, finished, error, unknown
	PositionX  float64 // Laser head X position in mm
	PositionY  float64 // Laser head Y position in mm
	LastUpdate time.Time
}

// RuidaAdapter handles UDP communication with Ruida laser controllers
// (RDC6442G, RDC6445, RDC6445G, etc.).
type RuidaAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	conn       *net.UDPConn
	addr       *net.UDPAddr
	status     RuidaStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewRuidaAdapter creates a new Ruida laser controller adapter.
func NewRuidaAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *RuidaAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &RuidaAdapter{
		log:        log.WithField("adapter", "ruida"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Connect establishes a UDP connection to the Ruida controller.
func (a *RuidaAdapter) Connect(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return fmt.Errorf("failed to resolve address %s:%d: %w", host, port, err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to connect to %s:%d: %w", host, port, err)
	}

	a.conn = conn
	a.addr = addr
	a.connected = true

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to Ruida laser controller")

	// Start background status polling.
	go a.statusLoop()

	return nil
}

// Disconnect closes the UDP connection.
func (a *RuidaAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false

	if a.conn != nil {
		if err := a.conn.Close(); err != nil {
			return fmt.Errorf("failed to close UDP connection: %w", err)
		}
		a.conn = nil
	}

	a.log.Info("Disconnected from Ruida laser controller")
	return nil
}

// IsConnected returns true if connected to the controller.
func (a *RuidaAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current controller status.
func (a *RuidaAdapter) GetStatus() RuidaStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw hex-encoded byte sequence to the controller.
// The command string is expected to be a hex-encoded byte string (e.g. "D700").
func (a *RuidaAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Decode hex string to raw bytes.
	raw, err := hex.DecodeString(strings.ReplaceAll(command, " ", ""))
	if err != nil {
		return fmt.Errorf("invalid hex command %q: %w", command, err)
	}

	return a.sendPacket(raw, timeout)
}

// MapCommand translates high-level command names to Ruida binary protocol packets.
// Supported commands: start, stop, pause, resume, status, get_position, emergency_stop.
func (a *RuidaAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "start":
		return nil, a.sendPacket(ruidaCmdStart, 2*time.Second)

	case "stop":
		return nil, a.sendPacket(ruidaCmdStop, 2*time.Second)

	case "pause":
		return nil, a.sendPacket(ruidaCmdPause, 2*time.Second)

	case "resume":
		return nil, a.sendPacket(ruidaCmdResume, 2*time.Second)

	case "status":
		resp, err := a.sendPacketWithResponse(ruidaCmdStatus, 2*time.Second)
		if err != nil {
			return nil, err
		}
		state := a.parseStatusByte(resp)
		return map[string]interface{}{"state": state}, nil

	case "get_position":
		resp, err := a.sendPacketWithResponse(ruidaCmdPosition, 2*time.Second)
		if err != nil {
			return nil, err
		}
		x, y := a.parsePositionResponse(resp)
		return map[string]interface{}{"x": x, "y": y}, nil

	case "emergency_stop":
		return nil, a.sendPacket(ruidaCmdEStop, 1*time.Second)

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendPacket scrambles and sends a binary packet to the controller.
func (a *RuidaAdapter) sendPacket(payload []byte, timeout time.Duration) error {
	a.mu.RLock()
	conn := a.conn
	a.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	packet := a.buildPacket(payload)

	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send packet: %w", err)
	}

	a.log.WithField("payload", hex.EncodeToString(payload)).Debug("Sent Ruida packet")
	return nil
}

// sendPacketWithResponse sends a packet and waits for a response.
func (a *RuidaAdapter) sendPacketWithResponse(payload []byte, timeout time.Duration) ([]byte, error) {
	a.mu.RLock()
	conn := a.conn
	a.mu.RUnlock()

	if conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	packet := a.buildPacket(payload)

	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("failed to set write deadline: %w", err)
	}

	if _, err := conn.Write(packet); err != nil {
		return nil, fmt.Errorf("failed to send packet: %w", err)
	}

	// Read response.
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	response := a.descramblePacket(buf[:n])
	a.log.WithField("response", hex.EncodeToString(response)).Debug("Received Ruida response")
	return response, nil
}

// buildPacket creates a scrambled Ruida packet from raw payload bytes.
// Format: scrambled(payload) + checksum byte.
func (a *RuidaAdapter) buildPacket(payload []byte) []byte {
	checksum := ruidaChecksum(payload)
	data := append(payload, checksum)
	return scrambleBytes(data)
}

// descramblePacket reverses the scrambling on a received packet.
func (a *RuidaAdapter) descramblePacket(data []byte) []byte {
	return scrambleBytes(data) // XOR is its own inverse.
}

// scrambleBytes applies XOR scrambling to a byte slice.
func scrambleBytes(data []byte) []byte {
	result := make([]byte, len(data))
	for i, b := range data {
		result[i] = b ^ ruidaScrambleXOR
	}
	return result
}

// ruidaChecksum computes an XOR checksum over all bytes.
func ruidaChecksum(data []byte) byte {
	var cs byte
	for _, b := range data {
		cs ^= b
	}
	return cs
}

// parseStatusByte interprets a status response from the controller.
func (a *RuidaAdapter) parseStatusByte(resp []byte) string {
	if len(resp) == 0 {
		return "unknown"
	}

	// The state byte is typically at a known offset in the response.
	// For simplicity, use the first meaningful byte after any header.
	var stateByte byte
	if len(resp) > 3 {
		stateByte = resp[3]
	} else {
		stateByte = resp[0]
	}

	switch stateByte {
	case ruidaStateIdle:
		return "idle"
	case ruidaStateRunning:
		return "running"
	case ruidaStatePaused:
		return "paused"
	case ruidaStateFinish:
		return "finished"
	case ruidaStateError:
		return "error"
	default:
		return "unknown"
	}
}

// parsePositionResponse extracts X and Y position from a position response.
// Ruida encodes positions as 32-bit integers in 1/1000 mm units (big-endian).
func (a *RuidaAdapter) parsePositionResponse(resp []byte) (float64, float64) {
	var x, y float64

	// Position response typically has X at offset 2 and Y at offset 6 (4 bytes each).
	if len(resp) >= 10 {
		xRaw := int32(resp[2])<<24 | int32(resp[3])<<16 | int32(resp[4])<<8 | int32(resp[5])
		yRaw := int32(resp[6])<<24 | int32(resp[7])<<16 | int32(resp[8])<<8 | int32(resp[9])
		x = float64(xRaw) / 1000.0
		y = float64(yRaw) / 1000.0
	}

	return x, y
}

// statusLoop periodically polls the controller for status and position.
func (a *RuidaAdapter) statusLoop() {
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

// pollStatus queries the controller for current state and position.
func (a *RuidaAdapter) pollStatus() {
	now := time.Now()

	// Query machine status.
	statusResp, err := a.sendPacketWithResponse(ruidaCmdStatus, 2*time.Second)
	if err != nil {
		a.log.WithError(err).Debug("Failed to poll status")
		return
	}

	state := a.parseStatusByte(statusResp)

	// Query position.
	posResp, err := a.sendPacketWithResponse(ruidaCmdPosition, 2*time.Second)
	if err != nil {
		a.log.WithError(err).Debug("Failed to poll position")
		// Still update state even if position query fails.
		a.mu.Lock()
		a.status.State = state
		a.status.LastUpdate = now
		a.mu.Unlock()
		return
	}

	x, y := a.parsePositionResponse(posResp)

	a.mu.Lock()
	a.status.State = state
	a.status.PositionX = x
	a.status.PositionY = y
	a.status.LastUpdate = now
	a.mu.Unlock()

	// Publish telemetry.
	if a.OnTelemetry != nil {
		ts := now.UTC().Format(time.RFC3339Nano)
		stateVal := 0.0
		switch state {
		case "idle":
			stateVal = 0
		case "running":
			stateVal = 1
		case "paused":
			stateVal = 2
		case "finished":
			stateVal = 3
		case "error":
			stateVal = -1
		}

		a.OnTelemetry([]TelemetryMetric{
			{Type: "machine_state", Value: stateVal, Unit: "enum", Timestamp: ts},
			{Type: "laser_position_x", Value: x, Unit: "mm", Timestamp: ts},
			{Type: "laser_position_y", Value: y, Unit: "mm", Timestamp: ts},
		})
	}
}
