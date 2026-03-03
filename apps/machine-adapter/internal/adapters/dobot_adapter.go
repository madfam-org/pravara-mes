// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.bug.st/serial"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// Dobot binary protocol constants.
const (
	dobotHeaderByte1 byte = 0xAA
	dobotHeaderByte2 byte = 0xAA

	// Command IDs for the Dobot binary protocol.
	dobotCmdGetPose    byte = 10  // Get current position
	dobotCmdHome       byte = 31  // Home all axes
	dobotCmdSetPTPCmd  byte = 84  // Point-to-Point movement
	dobotCmdSetSuction byte = 62  // Set suction cup state
	dobotCmdSetGripper byte = 63  // Set gripper state
	dobotCmdClearAlarm byte = 20  // Clear alarm state
	dobotCmdGetAlarm   byte = 21  // Get alarm state

	// PTP mode constants.
	dobotPTPModeJump    byte = 0 // JUMP mode (lift, move, descend)
	dobotPTPModeMovJ    byte = 1 // MOVJ mode (joint movement)
	dobotPTPMoveL       byte = 2 // MOVL mode (linear movement)
	dobotPTPModeJumpXYZ byte = 3 // JUMP in XYZ
	dobotPTPMovJXYZ     byte = 4 // MOVJ in XYZ
	dobotPTPMovLXYZ     byte = 5 // MOVL in XYZ
)

// DobotStatus represents the current state of a Dobot robot arm.
type DobotStatus struct {
	State      string    // idle, moving, alarm, disconnected
	PositionX  float64   // End effector X in mm
	PositionY  float64   // End effector Y in mm
	PositionZ  float64   // End effector Z in mm
	PositionR  float64   // End effector rotation in degrees
	JointAngles [4]float64 // Joint angles in degrees (J1-J4)
	SuctionOn  bool      // Suction cup state
	GripperOn  bool      // Gripper state
	LastUpdate time.Time
}

// DobotAdapter handles communication with Dobot robotic arms using the
// binary serial protocol. The packet format is:
// Header(0xAA 0xAA) + PayloadLen(1 byte) + CmdID(1 byte) + Ctrl(1 byte) + Params(N bytes) + Checksum(1 byte)
type DobotAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	port       serial.Port
	status     DobotStatus
	connected  bool
	ctx        context.Context
	cancel     context.CancelFunc

	// Sequence counter for command tracking.
	seqCounter uint8

	// Telemetry callback for publishing metrics.
	OnTelemetry TelemetryCallback
}

// NewDobotAdapter creates a new Dobot robot arm adapter.
func NewDobotAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *DobotAdapter {
	ctx, cancel := context.WithCancel(context.Background())

	return &DobotAdapter{
		log:        log.WithField("adapter", "dobot"),
		definition: definition,
		ctx:        ctx,
		cancel:     cancel,
		status: DobotStatus{
			State: "disconnected",
		},
	}
}

// Connect establishes a serial connection to the Dobot arm.
func (a *DobotAdapter) Connect(portName string, baudRate int) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	if baudRate == 0 {
		baudRate = 115200 // Dobot default baud rate
	}

	mode := &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(portName, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port %s: %w", portName, err)
	}

	if err := port.SetReadTimeout(2 * time.Second); err != nil {
		port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	a.port = port
	a.connected = true
	a.status.State = "idle"

	a.log.WithFields(logrus.Fields{
		"port":     portName,
		"baudRate": baudRate,
	}).Info("Connected to Dobot arm via serial")

	// Start background pose polling.
	go a.statusLoop()

	return nil
}

// Disconnect closes the serial connection.
func (a *DobotAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	a.cancel()
	a.connected = false
	a.status.State = "disconnected"

	if a.port != nil {
		if err := a.port.Close(); err != nil {
			return fmt.Errorf("failed to close serial port: %w", err)
		}
		a.port = nil
	}

	a.log.Info("Disconnected from Dobot arm")
	return nil
}

// IsConnected returns true if connected to the Dobot arm.
func (a *DobotAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

// GetStatus returns the current arm status.
func (a *DobotAdapter) GetStatus() DobotStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// SendCommand sends a raw binary command (hex string) to the Dobot (implements CommandExecutor).
func (a *DobotAdapter) SendCommand(command string, timeout time.Duration) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}
	// For raw commands, interpret as a command ID to send with no params.
	return fmt.Errorf("raw command sending not supported; use MapCommand for structured commands")
}

// MapCommand translates high-level command names to Dobot binary protocol commands.
// Supported commands: home, move_to, set_suction, set_gripper, get_position, clear_alarm.
func (a *DobotAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		// Command ID 31: Home. No parameters needed.
		if err := a.sendDobotCommand(dobotCmdHome, 0x01, nil); err != nil {
			return nil, fmt.Errorf("home command failed: %w", err)
		}
		return nil, nil

	case "move_to":
		x, err := a.extractFloat(params, "x")
		if err != nil {
			return nil, fmt.Errorf("move_to requires x: %w", err)
		}
		y, err := a.extractFloat(params, "y")
		if err != nil {
			return nil, fmt.Errorf("move_to requires y: %w", err)
		}
		z, err := a.extractFloat(params, "z")
		if err != nil {
			return nil, fmt.Errorf("move_to requires z: %w", err)
		}
		r := 0.0
		if rVal, ok := params["r"]; ok {
			if rf, err := toFloat64(rVal); err == nil {
				r = rf
			}
		}

		// Determine PTP mode.
		ptpMode := dobotPTPMovLXYZ // Default to linear XYZ movement
		if modeVal, ok := params["mode"]; ok {
			if modeStr, ok := modeVal.(string); ok {
				switch modeStr {
				case "jump":
					ptpMode = dobotPTPModeJumpXYZ
				case "joint":
					ptpMode = dobotPTPMovJXYZ
				case "linear":
					ptpMode = dobotPTPMovLXYZ
				}
			}
		}

		// Build PTP command payload: mode(1) + x(4) + y(4) + z(4) + r(4) = 17 bytes
		payload := make([]byte, 17)
		payload[0] = ptpMode
		binary.LittleEndian.PutUint32(payload[1:5], math.Float32bits(float32(x)))
		binary.LittleEndian.PutUint32(payload[5:9], math.Float32bits(float32(y)))
		binary.LittleEndian.PutUint32(payload[9:13], math.Float32bits(float32(z)))
		binary.LittleEndian.PutUint32(payload[13:17], math.Float32bits(float32(r)))

		if err := a.sendDobotCommand(dobotCmdSetPTPCmd, 0x03, payload); err != nil {
			return nil, fmt.Errorf("move_to command failed: %w", err)
		}
		return nil, nil

	case "set_suction":
		on := false
		if stateVal, ok := params["on"]; ok {
			switch v := stateVal.(type) {
			case bool:
				on = v
			case float64:
				on = v != 0
			case int:
				on = v != 0
			case string:
				on = v == "true" || v == "1" || v == "on"
			}
		}
		payload := []byte{0x01} // Enable suction cup control
		if on {
			payload = append(payload, 0x01)
		} else {
			payload = append(payload, 0x00)
		}
		if err := a.sendDobotCommand(dobotCmdSetSuction, 0x03, payload); err != nil {
			return nil, fmt.Errorf("set_suction failed: %w", err)
		}
		a.mu.Lock()
		a.status.SuctionOn = on
		a.mu.Unlock()
		return map[string]interface{}{"suction": on}, nil

	case "set_gripper":
		on := false
		if stateVal, ok := params["on"]; ok {
			switch v := stateVal.(type) {
			case bool:
				on = v
			case float64:
				on = v != 0
			case int:
				on = v != 0
			case string:
				on = v == "true" || v == "1" || v == "on"
			}
		}
		payload := []byte{0x01} // Enable gripper control
		if on {
			payload = append(payload, 0x01)
		} else {
			payload = append(payload, 0x00)
		}
		if err := a.sendDobotCommand(dobotCmdSetGripper, 0x03, payload); err != nil {
			return nil, fmt.Errorf("set_gripper failed: %w", err)
		}
		a.mu.Lock()
		a.status.GripperOn = on
		a.mu.Unlock()
		return map[string]interface{}{"gripper": on}, nil

	case "get_position":
		if err := a.queryPose(); err != nil {
			return nil, fmt.Errorf("get_position failed: %w", err)
		}
		status := a.GetStatus()
		return map[string]interface{}{
			"x": status.PositionX,
			"y": status.PositionY,
			"z": status.PositionZ,
			"r": status.PositionR,
			"joint_angles": map[string]float64{
				"j1": status.JointAngles[0],
				"j2": status.JointAngles[1],
				"j3": status.JointAngles[2],
				"j4": status.JointAngles[3],
			},
		}, nil

	case "clear_alarm":
		if err := a.sendDobotCommand(dobotCmdClearAlarm, 0x01, nil); err != nil {
			return nil, fmt.Errorf("clear_alarm failed: %w", err)
		}
		a.mu.Lock()
		a.status.State = "idle"
		a.mu.Unlock()
		return nil, nil

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}

// sendDobotCommand builds and sends a binary Dobot protocol packet.
// Packet format: 0xAA 0xAA <len> <id> <ctrl> <params...> <checksum>
func (a *DobotAdapter) sendDobotCommand(cmdID byte, ctrl byte, params []byte) error {
	a.mu.Lock()
	port := a.port
	a.seqCounter++
	a.mu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not available")
	}

	// Payload: cmdID + ctrl + params
	payloadLen := 2 + len(params) // cmdID(1) + ctrl(1) + params(N)
	packet := make([]byte, 0, 3+payloadLen+1)

	// Header
	packet = append(packet, dobotHeaderByte1, dobotHeaderByte2)

	// Payload length
	packet = append(packet, byte(payloadLen))

	// Command ID
	packet = append(packet, cmdID)

	// Control byte (isQueued flag, rw flag)
	packet = append(packet, ctrl)

	// Parameters
	if len(params) > 0 {
		packet = append(packet, params...)
	}

	// Checksum: sum of payload bytes (from cmdID onward), truncated to 8 bits, then negated.
	var checksum byte
	for _, b := range packet[3:] { // Skip header and length byte.
		checksum += b
	}
	checksum = byte(0 - checksum)
	packet = append(packet, checksum)

	if _, err := port.Write(packet); err != nil {
		return fmt.Errorf("serial write failed: %w", err)
	}

	a.log.WithFields(logrus.Fields{
		"cmdID":      cmdID,
		"paramsLen":  len(params),
		"packetSize": len(packet),
	}).Debug("Sent Dobot binary command")

	// Read response packet.
	return a.readDobotResponse(cmdID)
}

// readDobotResponse reads and validates a response packet from the Dobot.
func (a *DobotAdapter) readDobotResponse(expectedCmdID byte) error {
	a.mu.RLock()
	port := a.port
	a.mu.RUnlock()

	if port == nil {
		return fmt.Errorf("serial port not available")
	}

	// Read header (2 bytes).
	header := make([]byte, 2)
	if _, err := port.Read(header); err != nil {
		return fmt.Errorf("failed to read response header: %w", err)
	}

	if header[0] != dobotHeaderByte1 || header[1] != dobotHeaderByte2 {
		return fmt.Errorf("invalid response header: 0x%02X 0x%02X", header[0], header[1])
	}

	// Read payload length.
	lenBuf := make([]byte, 1)
	if _, err := port.Read(lenBuf); err != nil {
		return fmt.Errorf("failed to read payload length: %w", err)
	}
	payloadLen := int(lenBuf[0])

	// Read payload + checksum.
	payload := make([]byte, payloadLen+1) // +1 for checksum
	totalRead := 0
	for totalRead < len(payload) {
		n, err := port.Read(payload[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read payload: %w", err)
		}
		totalRead += n
	}

	// Validate command ID in response.
	if payloadLen > 0 && payload[0] != expectedCmdID {
		a.log.WithFields(logrus.Fields{
			"expected": expectedCmdID,
			"got":      payload[0],
		}).Warn("Unexpected response command ID")
	}

	return nil
}

// queryPose sends a GetPose command and parses the response to update position.
func (a *DobotAdapter) queryPose() error {
	a.mu.Lock()
	port := a.port
	a.seqCounter++
	a.mu.Unlock()

	if port == nil {
		return fmt.Errorf("serial port not available")
	}

	// Build GetPose packet: cmdID=10, ctrl=0x00 (read), no params.
	payloadLen := 2
	packet := []byte{
		dobotHeaderByte1, dobotHeaderByte2,
		byte(payloadLen),
		dobotCmdGetPose,
		0x00, // ctrl: read
	}
	var checksum byte
	for _, b := range packet[3:] {
		checksum += b
	}
	checksum = byte(0 - checksum)
	packet = append(packet, checksum)

	if _, err := port.Write(packet); err != nil {
		return fmt.Errorf("failed to send GetPose: %w", err)
	}

	// Read response header.
	header := make([]byte, 3)
	if _, err := port.Read(header); err != nil {
		return fmt.Errorf("failed to read pose response header: %w", err)
	}
	if header[0] != dobotHeaderByte1 || header[1] != dobotHeaderByte2 {
		return fmt.Errorf("invalid pose response header")
	}
	respLen := int(header[2])

	// Read response payload + checksum.
	respData := make([]byte, respLen+1)
	totalRead := 0
	for totalRead < len(respData) {
		n, err := port.Read(respData[totalRead:])
		if err != nil {
			return fmt.Errorf("failed to read pose response: %w", err)
		}
		totalRead += n
	}

	// Parse pose response: cmdID(1) + ctrl(1) + x(4) + y(4) + z(4) + r(4) + j1(4) + j2(4) + j3(4) + j4(4)
	// Total params: 32 bytes + 2 header bytes = 34
	if respLen < 34 {
		return fmt.Errorf("pose response too short: %d bytes", respLen)
	}

	// Skip cmdID(1) and ctrl(1), then read floats.
	offset := 2
	x := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	y := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	z := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	r := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	j1 := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	j2 := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	j3 := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))
	offset += 4
	j4 := math.Float32frombits(binary.LittleEndian.Uint32(respData[offset : offset+4]))

	a.mu.Lock()
	a.status.PositionX = float64(x)
	a.status.PositionY = float64(y)
	a.status.PositionZ = float64(z)
	a.status.PositionR = float64(r)
	a.status.JointAngles = [4]float64{float64(j1), float64(j2), float64(j3), float64(j4)}
	a.status.LastUpdate = time.Now()
	a.mu.Unlock()

	return nil
}

// extractFloat extracts a float64 parameter by name from the parameter map.
func (a *DobotAdapter) extractFloat(params map[string]interface{}, key string) (float64, error) {
	val, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("missing parameter: %s", key)
	}
	return toFloat64(val)
}

// statusLoop periodically polls the Dobot for position and alarm state.
func (a *DobotAdapter) statusLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
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

// pollStatus queries the Dobot for current pose and publishes telemetry.
func (a *DobotAdapter) pollStatus() {
	if err := a.queryPose(); err != nil {
		a.log.WithError(err).Debug("Failed to poll Dobot pose")
		return
	}

	status := a.GetStatus()

	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		suctionVal := 0.0
		if status.SuctionOn {
			suctionVal = 1.0
		}
		gripperVal := 0.0
		if status.GripperOn {
			gripperVal = 1.0
		}
		a.OnTelemetry([]TelemetryMetric{
			{Type: "position_x", Value: status.PositionX, Unit: "mm", Timestamp: now},
			{Type: "position_y", Value: status.PositionY, Unit: "mm", Timestamp: now},
			{Type: "position_z", Value: status.PositionZ, Unit: "mm", Timestamp: now},
			{Type: "position_r", Value: status.PositionR, Unit: "degrees", Timestamp: now},
			{Type: "joint_1", Value: status.JointAngles[0], Unit: "degrees", Timestamp: now},
			{Type: "joint_2", Value: status.JointAngles[1], Unit: "degrees", Timestamp: now},
			{Type: "joint_3", Value: status.JointAngles[2], Unit: "degrees", Timestamp: now},
			{Type: "joint_4", Value: status.JointAngles[3], Unit: "degrees", Timestamp: now},
			{Type: "suction_on", Value: suctionVal, Unit: "bool", Timestamp: now},
			{Type: "gripper_on", Value: gripperVal, Unit: "bool", Timestamp: now},
		})
	}
}
