package manager

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestMapCommandToGCode(t *testing.T) {
	tests := []struct {
		name            string
		command         string
		params          map[string]interface{}
		expectedGCode   string
		expectedTimeout time.Duration
	}{
		{
			name:            "home command",
			command:         "home",
			params:          nil,
			expectedGCode:   "G28",
			expectedTimeout: 60 * time.Second,
		},
		{
			name:            "pause command",
			command:         "pause",
			params:          nil,
			expectedGCode:   "M25",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "resume command",
			command:         "resume",
			params:          nil,
			expectedGCode:   "M24",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "stop command",
			command:         "stop",
			params:          nil,
			expectedGCode:   "M524",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "emergency_stop command",
			command:         "emergency_stop",
			params:          nil,
			expectedGCode:   "M112",
			expectedTimeout: 1 * time.Second,
		},
		{
			name:            "preheat with default temperature",
			command:         "preheat",
			params:          nil,
			expectedGCode:   "M104 S200",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "preheat with custom temperature",
			command:         "preheat",
			params:          map[string]interface{}{"temperature": 220.0},
			expectedGCode:   "M104 S220",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "preheat_bed with default temperature",
			command:         "preheat_bed",
			params:          nil,
			expectedGCode:   "M140 S60",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "preheat_bed with custom temperature",
			command:         "preheat_bed",
			params:          map[string]interface{}{"temperature": 80.0},
			expectedGCode:   "M140 S80",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "cooldown command",
			command:         "cooldown",
			params:          nil,
			expectedGCode:   "M104 S0\nM140 S0",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "get_position command",
			command:         "get_position",
			params:          nil,
			expectedGCode:   "M114",
			expectedTimeout: 2 * time.Second,
		},
		{
			name:            "get_temperature command",
			command:         "get_temperature",
			params:          nil,
			expectedGCode:   "M105",
			expectedTimeout: 2 * time.Second,
		},
		{
			name:            "auto_level command",
			command:         "auto_level",
			params:          nil,
			expectedGCode:   "G29",
			expectedTimeout: 120 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gcode, timeout := mapCommandToGCode(tc.command, tc.params)
			assert.Equal(t, tc.expectedGCode, gcode)
			assert.Equal(t, tc.expectedTimeout, timeout)
		})
	}
}

func TestMapCommandToGCode_Unknown(t *testing.T) {
	gcode, timeout := mapCommandToGCode("nonexistent_command", nil)
	assert.Empty(t, gcode)
	assert.Equal(t, time.Duration(0), timeout)
}

func TestTelemetryMetric_Fields(t *testing.T) {
	m := TelemetryMetric{
		Type:      "temperature_extruder",
		Value:     210.5,
		Unit:      "celsius",
		Timestamp: "2026-03-03T12:00:00Z",
	}

	assert.Equal(t, "temperature_extruder", m.Type)
	assert.Equal(t, 210.5, m.Value)
	assert.Equal(t, "celsius", m.Unit)
	assert.Equal(t, "2026-03-03T12:00:00Z", m.Timestamp)
}

func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.DebugLevel)
	return l
}
