// Package adapters provides protocol-specific machine adapters.
package adapters

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// BambuStatus represents the current state of a Bambu Lab printer.
type BambuStatus struct {
	State        string  // idle, running, paused, error
	NozzleTemp   float64 // Current nozzle temperature
	NozzleTarget float64 // Target nozzle temperature
	BedTemp      float64 // Current bed temperature
	BedTarget    float64 // Target bed temperature
	ChamberTemp  float64 // Current chamber temperature
	PrintPercent int     // Print progress percentage (0-100)
	SpeedLevel   int     // Speed level percentage
	FanSpeed     int     // Part cooling fan speed (0-15 mapped to 0-100%)
	AMSHumidity  []int   // Humidity per AMS slot
	LastUpdate   time.Time
}

// BambuAdapter handles communication with Bambu Lab printers via MQTT over TLS.
// It connects to the printer's local MQTT broker on port 8883 using the access
// code displayed on the printer's LCD screen for authentication.
type BambuAdapter struct {
	mu         sync.RWMutex
	log        *logrus.Entry
	definition *registry.MachineDefinition
	client     mqtt.Client
	serial     string
	connected  bool
	status     BambuStatus
	seqID      atomic.Int64

	// Telemetry callback for publishing metrics
	OnTelemetry TelemetryCallback
}

// NewBambuAdapter creates a new Bambu Lab MQTT adapter.
func NewBambuAdapter(definition *registry.MachineDefinition, log *logrus.Logger) *BambuAdapter {
	return &BambuAdapter{
		log:        log.WithField("adapter", "bambu"),
		definition: definition,
	}
}

// Connect establishes a TLS MQTT connection to the Bambu Lab printer.
// The host should be the printer's IP address. The accessCode is the code
// shown on the printer's LCD under Network > LAN Only or LAN Mode.
// The serial is the printer's serial number used in MQTT topic paths.
func (a *BambuAdapter) Connect(host, accessCode, serial string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.connected {
		return fmt.Errorf("already connected")
	}

	a.serial = serial

	// Bambu printers use TLS on port 8883 with self-signed certificates.
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Printer uses self-signed cert
	}

	broker := fmt.Sprintf("tls://%s:8883", host)
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(fmt.Sprintf("pravara-mes-%s", serial)).
		SetUsername("bblp").
		SetPassword(accessCode).
		SetTLSConfig(tlsConfig).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOnConnectHandler(func(_ mqtt.Client) {
			a.log.Info("Connected to Bambu printer MQTT broker")
			// Re-subscribe on reconnect
			a.subscribe()
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			a.log.WithError(err).Warn("MQTT connection lost")
		})

	a.client = mqtt.NewClient(opts)
	token := a.client.Connect()
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("MQTT connection timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("MQTT connection failed: %w", token.Error())
	}

	a.connected = true
	a.subscribe()

	a.log.WithFields(logrus.Fields{
		"host":   host,
		"serial": serial,
	}).Info("Connected to Bambu Lab printer")

	// Request initial status
	a.publishCommand("push_status", nil)

	return nil
}

// subscribe registers the MQTT topic handler for report messages.
func (a *BambuAdapter) subscribe() {
	topic := fmt.Sprintf("device/%s/report", a.serial)
	token := a.client.Subscribe(topic, 1, a.handleReport)
	if !token.WaitTimeout(5 * time.Second) {
		a.log.Warn("MQTT subscribe timeout")
		return
	}
	if token.Error() != nil {
		a.log.WithError(token.Error()).Warn("MQTT subscribe failed")
		return
	}
	a.log.WithField("topic", topic).Debug("Subscribed to report topic")
}

// handleReport processes incoming MQTT report messages from the printer.
func (a *BambuAdapter) handleReport(_ mqtt.Client, msg mqtt.Message) {
	var report struct {
		Print *bambuPrintReport `json:"print"`
	}

	if err := json.Unmarshal(msg.Payload(), &report); err != nil {
		a.log.WithError(err).Debug("Failed to parse report message")
		return
	}

	if report.Print == nil {
		return
	}

	a.processReport(report.Print)
}

// bambuPrintReport represents the "print" section of a Bambu Lab report message.
type bambuPrintReport struct {
	GcodeState       string  `json:"gcode_state"`
	NozzleTemper     float64 `json:"nozzle_temper"`
	NozzleTargetTemp float64 `json:"nozzle_target_temper"`
	BedTemper        float64 `json:"bed_temper"`
	BedTargetTemp    float64 `json:"bed_target_temper"`
	ChamberTemper    float64 `json:"chamber_temper"`
	MCPercent        int     `json:"mc_percent"`
	SpdLvl           int     `json:"spd_lvl"`
	FanSpeed         string  `json:"big_fan1_speed"`
	AMS              *struct {
		AMS []struct {
			Humidity string `json:"humidity"`
		} `json:"ams"`
	} `json:"ams"`
}

// processReport updates internal status and emits telemetry from a report.
func (a *BambuAdapter) processReport(r *bambuPrintReport) {
	a.mu.Lock()

	if r.GcodeState != "" {
		a.status.State = a.mapState(r.GcodeState)
	}
	a.status.NozzleTemp = r.NozzleTemper
	a.status.NozzleTarget = r.NozzleTargetTemp
	a.status.BedTemp = r.BedTemper
	a.status.BedTarget = r.BedTargetTemp
	a.status.ChamberTemp = r.ChamberTemper
	a.status.PrintPercent = r.MCPercent
	a.status.SpeedLevel = r.SpdLvl

	if fanVal, err := strconv.Atoi(r.FanSpeed); err == nil {
		a.status.FanSpeed = fanVal
	}

	// Parse AMS humidity
	if r.AMS != nil && r.AMS.AMS != nil {
		a.status.AMSHumidity = make([]int, len(r.AMS.AMS))
		for i, slot := range r.AMS.AMS {
			if h, err := strconv.Atoi(slot.Humidity); err == nil {
				a.status.AMSHumidity[i] = h
			}
		}
	}

	a.status.LastUpdate = time.Now()

	// Copy values for telemetry emission outside the lock.
	nozzle := a.status.NozzleTemp
	bed := a.status.BedTemp
	chamber := a.status.ChamberTemp
	percent := a.status.PrintPercent
	speed := a.status.SpeedLevel
	fan := a.status.FanSpeed
	humidity := make([]int, len(a.status.AMSHumidity))
	copy(humidity, a.status.AMSHumidity)

	a.mu.Unlock()

	// Emit telemetry
	if a.OnTelemetry != nil {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		metrics := []TelemetryMetric{
			{Type: "nozzle_temp", Value: nozzle, Unit: "celsius", Timestamp: now},
			{Type: "bed_temp", Value: bed, Unit: "celsius", Timestamp: now},
			{Type: "chamber_temp", Value: chamber, Unit: "celsius", Timestamp: now},
			{Type: "print_percent", Value: float64(percent), Unit: "percent", Timestamp: now},
			{Type: "print_speed", Value: float64(speed), Unit: "percent", Timestamp: now},
			{Type: "fan_speed", Value: float64(fan), Unit: "raw", Timestamp: now},
		}

		for i, h := range humidity {
			metrics = append(metrics, TelemetryMetric{
				Type:      fmt.Sprintf("ams_humidity_%d", i),
				Value:     float64(h),
				Unit:      "percent",
				Timestamp: now,
			})
		}

		a.OnTelemetry(metrics)
	}
}

// mapState converts Bambu gcode_state strings to normalized status values.
func (a *BambuAdapter) mapState(state string) string {
	switch state {
	case "IDLE":
		return "idle"
	case "RUNNING", "PREPARE":
		return "running"
	case "PAUSE":
		return "paused"
	case "FAILED":
		return "error"
	case "FINISH":
		return "idle"
	default:
		return "unknown"
	}
}

// Disconnect closes the MQTT connection to the printer.
func (a *BambuAdapter) Disconnect() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.connected {
		return nil
	}

	if a.client != nil && a.client.IsConnected() {
		topic := fmt.Sprintf("device/%s/report", a.serial)
		a.client.Unsubscribe(topic)
		a.client.Disconnect(1000) // Wait up to 1s for clean disconnect
	}

	a.connected = false
	a.log.Info("Disconnected from Bambu Lab printer")
	return nil
}

// IsConnected returns true if the MQTT client is connected.
func (a *BambuAdapter) IsConnected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected && a.client != nil && a.client.IsConnected()
}

// GetStatus returns a snapshot of the current printer status.
func (a *BambuAdapter) GetStatus() BambuStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// nextSeqID returns the next sequence ID for MQTT commands.
func (a *BambuAdapter) nextSeqID() string {
	return strconv.FormatInt(a.seqID.Add(1), 10)
}

// publishCommand sends a JSON command to the printer's request topic.
func (a *BambuAdapter) publishCommand(command string, extra map[string]interface{}) error {
	if !a.IsConnected() {
		return fmt.Errorf("not connected")
	}

	payload := map[string]interface{}{
		"print": map[string]interface{}{
			"command":     command,
			"sequence_id": a.nextSeqID(),
		},
	}

	// Merge extra fields into the "print" map.
	if extra != nil {
		printMap := payload["print"].(map[string]interface{})
		for k, v := range extra {
			printMap[k] = v
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	topic := fmt.Sprintf("device/%s/request", a.serial)
	token := a.client.Publish(topic, 1, false, data)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("MQTT publish timeout")
	}
	if token.Error() != nil {
		return fmt.Errorf("MQTT publish failed: %w", token.Error())
	}

	a.log.WithFields(logrus.Fields{
		"command": command,
		"topic":   topic,
	}).Debug("Published command")

	return nil
}

// SendCommand wraps a raw G-code string in the Bambu JSON gcode_line format
// and publishes it to the printer. This implements the CommandExecutor interface.
func (a *BambuAdapter) SendCommand(command string, timeout time.Duration) error {
	_ = timeout // MQTT is fire-and-forget; timeout is unused but kept for interface compatibility.
	return a.publishCommand("gcode_line", map[string]interface{}{
		"param": command + "\n",
	})
}

// MapCommand translates high-level command names to Bambu MQTT JSON payloads.
// Supported commands: home, pause, resume, stop, push_status, gcode_line,
// set_speed, set_temp, emergency_stop.
func (a *BambuAdapter) MapCommand(command string, params map[string]interface{}) (interface{}, error) {
	switch command {
	case "home":
		return nil, a.publishCommand("gcode_line", map[string]interface{}{
			"param": "G28\n",
		})

	case "pause":
		return nil, a.publishCommand("pause", nil)

	case "resume":
		return nil, a.publishCommand("resume", nil)

	case "stop":
		return nil, a.publishCommand("stop", nil)

	case "push_status":
		return nil, a.publishCommand("push_status", nil)

	case "gcode_line":
		gcode, ok := params["gcode"].(string)
		if !ok {
			return nil, fmt.Errorf("gcode_line requires 'gcode' string parameter")
		}
		return nil, a.publishCommand("gcode_line", map[string]interface{}{
			"param": gcode + "\n",
		})

	case "set_speed":
		level, ok := params["level"]
		if !ok {
			return nil, fmt.Errorf("set_speed requires 'level' parameter")
		}
		// Speed levels: 1=silent, 2=standard, 3=sport, 4=ludicrous
		return nil, a.publishCommand("gcode_line", map[string]interface{}{
			"param": fmt.Sprintf("M220 S%v\n", level),
		})

	case "set_temp":
		target, ok := params["target"].(string)
		if !ok {
			return nil, fmt.Errorf("set_temp requires 'target' string parameter (nozzle|bed)")
		}
		temp, ok := params["temp"]
		if !ok {
			return nil, fmt.Errorf("set_temp requires 'temp' parameter")
		}
		var gcode string
		switch target {
		case "nozzle":
			gcode = fmt.Sprintf("M104 S%v\n", temp)
		case "bed":
			gcode = fmt.Sprintf("M140 S%v\n", temp)
		default:
			return nil, fmt.Errorf("set_temp target must be 'nozzle' or 'bed', got %q", target)
		}
		return nil, a.publishCommand("gcode_line", map[string]interface{}{
			"param": gcode,
		})

	case "emergency_stop":
		return nil, a.publishCommand("gcode_line", map[string]interface{}{
			"param": "M112\n",
		})

	default:
		return nil, fmt.Errorf("unknown command: %s", command)
	}
}
