// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// Universal Robots robot mode constants decoded from the 1116-byte state packet.
const (
	urModeDisconnected  = -1
	urModeConfirmSafety = 1
	urModeBooting       = 2
	urModePowerOff      = 3
	urModePowerOn       = 4
	urModeIdle          = 5
	urModeBackdrive     = 6
	urModeRunning       = 7
)

// Universal Robots safety mode constants.
const (
	urSafetyNormal          = 1
	urSafetyReduced         = 2
	urSafetyProtectiveStop  = 3
	urSafetyRecovery        = 4
	urSafetySafeguardStop   = 5
	urSafetySystemEmergency = 6
	urSafetyRobotEmergency  = 7
	urSafetyViolation       = 8
	urSafetyFault           = 9
)

// URScriptStatus represents the current state of a Universal Robots arm.
type URScriptStatus struct {
	RobotMode      int        // Robot mode code
	SafetyMode     int        // Safety mode code
	JointPositions [6]float64 // Joint angles in radians (6 DOF)
	TCPPosition    [6]float64 // Tool center point: x, y, z (m), rx, ry, rz (rad)
	DigitalOutputs uint64     // Digital output bit mask
	LastUpdate     time.Time
}

// urStatePacketSize is the size of the UR secondary interface state packet.
const urStatePacketSize = 1116

// URScriptAdapter handles communication with Universal Robots arms via the
// secondary interface on TCP port 30002. It sends URScript commands as
// newline-terminated text and receives 1116-byte state packets at 10 Hz.
type URScriptAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	conn       net.Conn
	status     URScriptStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Home joint position in radians (configurable).
	homePosition [6]float64

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewURScriptAdapter creates a new Universal Robots adapter.
func NewURScriptAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *URScriptAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &URScriptAdapter{
		log:        log.WithField("adapter", "urscript"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		// Default home position: all joints at 0 rad.
		homePosition: [6]float64{0, -math.Pi / 2, 0, -math.Pi / 2, 0, 0},
	}
}

// Connect establishes a TCP connection to the UR secondary interface.
func (a *URScriptAdapter) Connect(host string, port int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if port == 0 {
		port = 30002 // UR secondary interface default port
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to UR at %s: %w", addr, err)
	}

	a.conn = conn
	a.connected = true

	a.log.WithFields(logrus.Fields{
		"host": host,
		"port": port,
	}).Info("Connected to Universal Robots arm")

	// Start background state packet reader.
	go a.stateReaderLoop()

	return nil
}

// Disconnect closes the TCP connection.
func (a *URScriptAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false

	if a.conn != nil {
		if err := a.conn.Close(); err != nil {
			return fmt.Errorf("failed to close UR connection: %w", err)
		}
		a.conn = nil
	}

	a.log.Info("Disconnected from Universal Robots arm")
	return nil
}

// IsConnected returns true if connected to the robot.
func (a *URScriptAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current robot status.
func (a *URScriptAdapter) GetStatus() URScriptStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SetHomePosition sets the joint positions used for the home command.
func (a *URScriptAdapter) SetHomePosition(joints [6]float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.homePosition = joints
}

// SendCommand sends a raw URScript command to the robot (implements CommandExecutor).
func (a *URScriptAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return a.sendURScript(command)
}

// MapCommand translates high-level command names to URScript commands.
// Supported commands: home, move_linear, move_joint, set_digital_out, get_position,
// stop, pause, resume.
func (a *URScriptAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		home := a.getHomePosition()
		script := fmt.Sprintf("movej([%.4f, %.4f, %.4f, %.4f, %.4f, %.4f], a=1.4, v=1.05)",
			home[0], home[1], home[2], home[3], home[4], home[5])
		return nil, a.sendURScript(script)

	case "move_linear":
		pose, err := a.extractPose(params)
		if err != nil {
			return nil, fmt.Errorf("move_linear: %w", err)
		}
		accel := 1.2
		vel := 0.25
		if a, ok := params["acceleration"]; ok {
			if af, err := toFloat64(a); err == nil {
				accel = af
			}
		}
		if v, ok := params["velocity"]; ok {
			if vf, err := toFloat64(v); err == nil {
				vel = vf
			}
		}
		script := fmt.Sprintf("movel(p[%.4f, %.4f, %.4f, %.4f, %.4f, %.4f], a=%.2f, v=%.2f)",
			pose[0], pose[1], pose[2], pose[3], pose[4], pose[5], accel, vel)
		return nil, a.sendURScript(script)

	case "move_joint":
		joints, err := a.extractJoints(params)
		if err != nil {
			return nil, fmt.Errorf("move_joint: %w", err)
		}
		accel := 1.4
		vel := 1.05
		if a, ok := params["acceleration"]; ok {
			if af, err := toFloat64(a); err == nil {
				accel = af
			}
		}
		if v, ok := params["velocity"]; ok {
			if vf, err := toFloat64(v); err == nil {
				vel = vf
			}
		}
		script := fmt.Sprintf("movej([%.4f, %.4f, %.4f, %.4f, %.4f, %.4f], a=%.2f, v=%.2f)",
			joints[0], joints[1], joints[2], joints[3], joints[4], joints[5], accel, vel)
		return nil, a.sendURScript(script)

	case "set_digital_out":
		pinVal, ok := params["pin"]
		if !ok {
			return nil, fmt.Errorf("set_digital_out requires pin parameter")
		}
		pin, err := toInt(pinVal)
		if err != nil {
			return nil, fmt.Errorf("invalid pin: %w", err)
		}
		stateVal, ok := params["state"]
		if !ok {
			return nil, fmt.Errorf("set_digital_out requires state parameter")
		}
		state := false
		switch v := stateVal.(type) {
		case bool:
			state = v
		case float64:
			state = v != 0
		case int:
			state = v != 0
		case string:
			state = v == "true" || v == "1" || v == "on"
		}
		stateStr := "False"
		if state {
			stateStr = "True"
		}
		script := fmt.Sprintf("set_digital_out(%d, %s)", pin, stateStr)
		return nil, a.sendURScript(script)

	case "get_position":
		status := a.GetStatus()
		return map[string]interface{}{
			"joint_positions": status.JointPositions,
			"tcp_position": map[string]float64{
				"x":  status.TCPPosition[0],
				"y":  status.TCPPosition[1],
				"z":  status.TCPPosition[2],
				"rx": status.TCPPosition[3],
				"ry": status.TCPPosition[4],
				"rz": status.TCPPosition[5],
			},
			"robot_mode":  status.RobotMode,
			"safety_mode": status.SafetyMode,
		}, nil

	case "stop":
		return nil, a.sendURScript("stopj(2.0)")

	case "pause":
		return nil, a.sendURScript("pause program")

	case "resume":
		return nil, a.sendURScript("play")

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendURScript sends a URScript command as a newline-terminated string.
func (a *URScriptAdapter) sendURScript(script string) error {
	a.mu.RLock()
	conn := a.conn
	a.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	data := []byte(script + "\n")
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("URScript send failed: %w", err)
	}

	a.log.WithField("script", script).Debug("Sent URScript command")
	return nil
}

// stateReaderLoop continuously reads 1116-byte state packets from the UR secondary interface.
func (a *URScriptAdapter) stateReaderLoop() {
	buf := make([]byte, 4096)

	for {
		select {
		case <-a.ctx.Done():
			return
		default:
		}

		a.mu.RLock()
		conn := a.conn
		a.mu.RUnlock()

		if conn == nil {
			return
		}

		if err := conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			a.log.WithError(err).Debug("Failed to set read deadline")
			continue
		}

		n, err := conn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // Read timeout is expected, retry.
			}
			a.log.WithError(err).Warn("UR state reader error")
			return
		}

		if n >= urStatePacketSize {
			a.parseStatePacket(buf[:urStatePacketSize])
		}
	}
}

// parseStatePacket parses the 1116-byte UR state packet and updates status.
// The packet layout follows the UR secondary interface specification.
func (a *URScriptAdapter) parseStatePacket(data []byte) {
	if len(data) < urStatePacketSize {
		return
	}

	now := time.Now()

	// Robot mode is at byte offset 756 (double, 8 bytes, big-endian).
	robotMode := int(math.Round(readBEFloat64(data, 756)))

	// Safety mode is at byte offset 764 (double, 8 bytes, big-endian).
	safetyMode := int(math.Round(readBEFloat64(data, 764)))

	// Joint positions start at offset 252 (6 doubles, each 8 bytes).
	var joints [6]float64
	for i := 0; i < 6; i++ {
		joints[i] = readBEFloat64(data, 252+i*8)
	}

	// TCP position starts at offset 444 (6 doubles: x, y, z, rx, ry, rz).
	var tcp [6]float64
	for i := 0; i < 6; i++ {
		tcp[i] = readBEFloat64(data, 444+i*8)
	}

	// Digital outputs at offset 1044 (uint64, big-endian).
	digitalOutputs := binary.BigEndian.Uint64(data[1044:1052])

	a.mu.Lock()
	a.status = URScriptStatus{
		RobotMode:      robotMode,
		SafetyMode:     safetyMode,
		JointPositions: joints,
		TCPPosition:    tcp,
		DigitalOutputs: digitalOutputs,
		LastUpdate:     now,
	}
	a.mu.Unlock()

	// Publish telemetry.
	if a.OnTelemetry != nil {
		ts := now.UTC().Format(time.RFC3339Nano)
		metrics := []TelemetryMetric{
			{Type: "robot_mode", Value: float64(robotMode), Unit: "enum", Timestamp: ts},
			{Type: "safety_mode", Value: float64(safetyMode), Unit: "enum", Timestamp: ts},
			{Type: "tcp_x", Value: tcp[0], Unit: "m", Timestamp: ts},
			{Type: "tcp_y", Value: tcp[1], Unit: "m", Timestamp: ts},
			{Type: "tcp_z", Value: tcp[2], Unit: "m", Timestamp: ts},
			{Type: "tcp_rx", Value: tcp[3], Unit: "rad", Timestamp: ts},
			{Type: "tcp_ry", Value: tcp[4], Unit: "rad", Timestamp: ts},
			{Type: "tcp_rz", Value: tcp[5], Unit: "rad", Timestamp: ts},
		}
		for i := 0; i < 6; i++ {
			metrics = append(metrics, TelemetryMetric{
				Type:      fmt.Sprintf("joint_%d_position", i),
				Value:     joints[i],
				Unit:      "rad",
				Timestamp: ts,
			})
		}
		a.OnTelemetry(metrics)
	}
}

// readBEFloat64 reads a big-endian IEEE 754 float64 from the given offset.
func readBEFloat64(data []byte, offset int) float64 {
	bits := binary.BigEndian.Uint64(data[offset : offset+8])
	return math.Float64frombits(bits)
}

// getHomePosition returns the configured home joint positions.
func (a *URScriptAdapter) getHomePosition() [6]float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.homePosition
}

// extractPose extracts a 6-element TCP pose (x, y, z, rx, ry, rz) from parameters.
func (a *URScriptAdapter) extractPose(params map[string]interface{}) ([6]float64, error) {
	var pose [6]float64
	keys := [6]string{"x", "y", "z", "rx", "ry", "rz"}
	for i, key := range keys {
		val, ok := params[key]
		if !ok {
			return pose, fmt.Errorf("missing parameter: %s", key)
		}
		f, err := toFloat64(val)
		if err != nil {
			return pose, fmt.Errorf("invalid %s: %w", key, err)
		}
		pose[i] = f
	}
	return pose, nil
}

// extractJoints extracts 6 joint angles from parameters.
func (a *URScriptAdapter) extractJoints(params map[string]interface{}) ([6]float64, error) {
	// Accept either a "joints" array or individual j0-j5 parameters.
	if jointsVal, ok := params["joints"]; ok {
		if jointsSlice, ok := jointsVal.([]interface{}); ok && len(jointsSlice) == 6 {
			var joints [6]float64
			for i, v := range jointsSlice {
				f, err := toFloat64(v)
				if err != nil {
					return joints, fmt.Errorf("invalid joint[%d]: %w", i, err)
				}
				joints[i] = f
			}
			return joints, nil
		}
		return [6]float64{}, fmt.Errorf("joints must be an array of 6 floats")
	}

	var joints [6]float64
	keys := [6]string{"j0", "j1", "j2", "j3", "j4", "j5"}
	for i, key := range keys {
		val, ok := params[key]
		if !ok {
			return joints, fmt.Errorf("missing parameter: %s", key)
		}
		f, err := toFloat64(val)
		if err != nil {
			return joints, fmt.Errorf("invalid %s: %w", key, err)
		}
		joints[i] = f
	}
	return joints, nil
}

// robotModeString returns a human-readable string for the robot mode code.
func robotModeString(mode int) string {
	switch mode {
	case urModeDisconnected:
		return "disconnected"
	case urModeConfirmSafety:
		return "confirm_safety"
	case urModeBooting:
		return "booting"
	case urModePowerOff:
		return "power_off"
	case urModePowerOn:
		return "power_on"
	case urModeIdle:
		return "idle"
	case urModeBackdrive:
		return "backdrive"
	case urModeRunning:
		return "running"
	default:
		return "unknown"
	}
}

// safetyModeString returns a human-readable string for the safety mode code.
func safetyModeString(mode int) string {
	switch mode {
	case urSafetyNormal:
		return "normal"
	case urSafetyReduced:
		return "reduced"
	case urSafetyProtectiveStop:
		return "protective_stop"
	case urSafetyRecovery:
		return "recovery"
	case urSafetySafeguardStop:
		return "safeguard_stop"
	case urSafetySystemEmergency:
		return "system_emergency"
	case urSafetyRobotEmergency:
		return "robot_emergency"
	case urSafetyViolation:
		return "violation"
	case urSafetyFault:
		return "fault"
	default:
		return "unknown"
	}
}
