package adapters

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// MockSerialPort mocks a serial port for testing
type MockSerialPort struct {
	mock.Mock
	readData  chan []byte
	writeData chan []byte
}

func (m *MockSerialPort) Read(b []byte) (n int, err error) {
	args := m.Called(b)
	return args.Int(0), args.Error(1)
}

func (m *MockSerialPort) Write(b []byte) (n int, err error) {
	args := m.Called(b)
	if m.writeData != nil {
		m.writeData <- b
	}
	return args.Int(0), args.Error(1)
}

func (m *MockSerialPort) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockSerialPort) SetReadTimeout(timeout time.Duration) error {
	args := m.Called(timeout)
	return args.Error(0)
}

func TestGRBLAdapter_ParseStatus(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "GRBL Test",
	}

	adapter := NewGRBLAdapter(def, logger)

	tests := []struct {
		name     string
		input    string
		expected GRBLStatus
	}{
		{
			name:  "idle status with positions",
			input: "<Idle|MPos:10.000,20.000,5.000|WPos:10.000,20.000,5.000|F:1000|S:12000>",
			expected: GRBLStatus{
				State:      "Idle",
				MachinePos: [3]float64{10.0, 20.0, 5.0},
				WorkPos:    [3]float64{10.0, 20.0, 5.0},
				FeedRate:   1000,
				Spindle:    12000,
			},
		},
		{
			name:  "running status",
			input: "<Run|MPos:50.500,75.250,10.125|F:2500>",
			expected: GRBLStatus{
				State:      "Run",
				MachinePos: [3]float64{50.5, 75.25, 10.125},
				FeedRate:   2500,
			},
		},
		{
			name:  "alarm status",
			input: "<Alarm|MPos:0.000,0.000,0.000>",
			expected: GRBLStatus{
				State:      "Alarm",
				MachinePos: [3]float64{0, 0, 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter.parseStatus(tt.input)
			status := adapter.GetStatus()

			assert.Equal(t, tt.expected.State, status.State)
			assert.Equal(t, tt.expected.MachinePos, status.MachinePos)
			if tt.expected.FeedRate > 0 {
				assert.Equal(t, tt.expected.FeedRate, status.FeedRate)
			}
			if tt.expected.Spindle > 0 {
				assert.Equal(t, tt.expected.Spindle, status.Spindle)
			}
		})
	}
}

func TestGRBLAdapter_ProcessResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "GRBL Test",
	}

	adapter := NewGRBLAdapter(def, logger)

	// Test OK response
	go adapter.processResponse("ok")

	select {
	case resp := <-adapter.responseQueue:
		assert.True(t, resp.Success)
		assert.Equal(t, "ok", resp.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No response received for 'ok'")
	}

	// Test error response
	go adapter.processResponse("error:Bad command")

	select {
	case resp := <-adapter.responseQueue:
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error.Error(), "Bad command")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No response received for error")
	}

	// Test alarm response
	go adapter.processResponse("ALARM:Hard limit")

	select {
	case resp := <-adapter.responseQueue:
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error.Error(), "Hard limit")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No response received for alarm")
	}
}

func TestGRBLAdapter_CommandExecution(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "GRBL Test",
	}

	adapter := NewGRBLAdapter(def, logger)
	adapter.connected = true

	// Start command loop in background
	go adapter.commandLoop()

	// Test sending a command
	go func() {
		time.Sleep(50 * time.Millisecond)
		adapter.responseQueue <- CommandResponse{Success: true, Message: "ok"}
	}()

	err := adapter.SendCommand("G0 X10 Y10", 1*time.Second)
	assert.NoError(t, err)

	// Test command timeout
	err = adapter.SendCommand("G0 X20 Y20", 100*time.Millisecond)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestGRBLAdapter_ParseCoordinates(t *testing.T) {
	tests := []struct {
		input    string
		expected []float64
	}{
		{"10.5,20.3,5.1", []float64{10.5, 20.3, 5.1}},
		{"0,0,0", []float64{0, 0, 0}},
		{"-5.5,10.0,0.25", []float64{-5.5, 10.0, 0.25}},
		{"invalid,data", []float64{}},
	}

	for _, tt := range tests {
		result := parseCoordinates(tt.input)
		if len(tt.expected) > 0 {
			assert.Equal(t, tt.expected, result)
		}
	}
}
