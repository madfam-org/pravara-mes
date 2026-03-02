package adapters

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

func TestMarlinAdapter_ParseTemperature(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "Marlin Test",
	}

	adapter := NewMarlinAdapter(def, logger)

	tests := []struct {
		name           string
		input          string
		expectedExtruder float64
		expectedExtTarget float64
		expectedBed    float64
		expectedBedTarget float64
	}{
		{
			name:              "full temperature report",
			input:             "ok T:200.5 /200.0 B:60.2 /60.0",
			expectedExtruder:  200.5,
			expectedExtTarget: 200.0,
			expectedBed:       60.2,
			expectedBedTarget: 60.0,
		},
		{
			name:              "extruder only",
			input:             "T:185.3 /190.0",
			expectedExtruder:  185.3,
			expectedExtTarget: 190.0,
		},
		{
			name:              "bed only",
			input:             "B:55.8 /60.0",
			expectedBed:       55.8,
			expectedBedTarget: 60.0,
		},
		{
			name:              "with additional data",
			input:             "T:205.1 /205.0 B:61.5 /60.0 @:127 B@:64",
			expectedExtruder:  205.1,
			expectedExtTarget: 205.0,
			expectedBed:       61.5,
			expectedBedTarget: 60.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter.parseTemperature(tt.input)
			status := adapter.GetStatus()

			if tt.expectedExtruder > 0 {
				assert.Equal(t, tt.expectedExtruder, status.ExtruderTemp)
			}
			if tt.expectedExtTarget > 0 {
				assert.Equal(t, tt.expectedExtTarget, status.ExtruderTarget)
			}
			if tt.expectedBed > 0 {
				assert.Equal(t, tt.expectedBed, status.BedTemp)
			}
			if tt.expectedBedTarget > 0 {
				assert.Equal(t, tt.expectedBedTarget, status.BedTarget)
			}
		})
	}
}

func TestMarlinAdapter_ParsePosition(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "Marlin Test",
	}

	adapter := NewMarlinAdapter(def, logger)

	tests := []struct {
		name     string
		input    string
		expected [3]float64
	}{
		{
			name:     "standard position report",
			input:    "X:100.00 Y:150.00 Z:10.50 E:0.00 Count X:8000 Y:12000 Z:840",
			expected: [3]float64{100.0, 150.0, 10.5},
		},
		{
			name:     "negative positions",
			input:    "X:-5.50 Y:-10.25 Z:0.00",
			expected: [3]float64{-5.5, -10.25, 0},
		},
		{
			name:     "with M114 response",
			input:    "ok X:50.00 Y:75.00 Z:5.00 E:0.00",
			expected: [3]float64{50.0, 75.0, 5.0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter.parsePosition(tt.input)
			status := adapter.GetStatus()
			assert.Equal(t, tt.expected, status.Position)
		})
	}
}

func TestMarlinAdapter_ParseSDProgress(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "Marlin Test",
	}

	adapter := NewMarlinAdapter(def, logger)

	tests := []struct {
		name             string
		input            string
		expectedProgress int
	}{
		{
			name:             "0% progress",
			input:            "SD printing byte 0/100000",
			expectedProgress: 0,
		},
		{
			name:             "50% progress",
			input:            "SD printing byte 50000/100000",
			expectedProgress: 50,
		},
		{
			name:             "100% progress",
			input:            "SD printing byte 100000/100000",
			expectedProgress: 100,
		},
		{
			name:             "partial progress",
			input:            "SD printing byte 33333/100000",
			expectedProgress: 33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter.parseSDProgress(tt.input)
			status := adapter.GetStatus()
			assert.Equal(t, tt.expectedProgress, status.Progress)
		})
	}
}

func TestMarlinAdapter_ProcessResponse(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "Marlin Test",
	}

	adapter := NewMarlinAdapter(def, logger)

	// Test OK response
	go adapter.processResponse("ok")

	select {
	case resp := <-adapter.responseQueue:
		assert.True(t, resp.Success)
		assert.Equal(t, "ok", resp.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No response received for 'ok'")
	}

	// Test OK with temperature
	adapter.processResponse("ok T:200.0 /200.0")
	status := adapter.GetStatus()
	assert.Equal(t, 200.0, status.ExtruderTemp)

	// Test error response
	go adapter.processResponse("Error:Thermal Runaway")

	select {
	case resp := <-adapter.responseQueue:
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error.Error(), "Thermal Runaway")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("No response received for error")
	}
}

func TestMarlinAdapter_Checksum(t *testing.T) {
	logger := logrus.New()
	def := &registry.MachineDefinition{
		Manufacturer: "Test",
		Model:        "Marlin Test",
	}

	adapter := NewMarlinAdapter(def, logger)
	adapter.checksumMode = true
	adapter.lineNumber = 0

	// Test checksum calculation
	cmd := adapter.addChecksum("G0 X10")
	assert.Contains(t, cmd, "N1 ")
	assert.Contains(t, cmd, "*")

	// Verify line number increments
	cmd2 := adapter.addChecksum("G0 Y10")
	assert.Contains(t, cmd2, "N2 ")
}