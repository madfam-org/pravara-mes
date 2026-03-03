package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuidaGenericDefinition_BasicFields(t *testing.T) {
	def := RuidaGenericDefinition()

	assert.Equal(t, "Ruida", def.Manufacturer)
	assert.Equal(t, "RDC6445 Generic", def.Model)
	assert.Equal(t, MachineTypeLaserCutter, def.Type)
	assert.Equal(t, ProtocolRuida, def.Protocol)
	assert.Equal(t, ConnectionUDP, def.Connection)
}

func TestRuidaGenericDefinition_Capabilities(t *testing.T) {
	def := RuidaGenericDefinition()

	t.Run("work_volume", func(t *testing.T) {
		wv, ok := def.Capabilities["work_volume"].(map[string]interface{})
		assert.True(t, ok, "work_volume capability should exist")
		assert.Equal(t, 600.0, wv["x_mm"])
		assert.Equal(t, 400.0, wv["y_mm"])
		assert.Equal(t, 0.0, wv["z_mm"])
	})

	t.Run("laser_power", func(t *testing.T) {
		lp, ok := def.Capabilities["laser_power"].(map[string]interface{})
		assert.True(t, ok, "laser_power capability should exist")
		assert.Equal(t, 100.0, lp["watts"])
	})
}

func TestRuidaGenericDefinition_Commands(t *testing.T) {
	def := RuidaGenericDefinition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"start", "D700"},
		{"stop", "D701"},
		{"pause", "D702"},
		{"resume", "D703"},
		{"status", "DA0004"},
		{"get_position", "DA0000"},
		{"emergency_stop", "D701"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.Equal(t, tc.template, cmd.Template)
			assert.NotEmpty(t, cmd.Name)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestRuidaGenericDefinition_StatusMapping(t *testing.T) {
	def := RuidaGenericDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"idle", "idle"},
		{"running", "running"},
		{"paused", "paused"},
		{"finished", "finished"},
		{"error", "error"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok, "status mapping for %q should exist", tc.raw)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestRuidaGenericDefinition_TelemetryParse(t *testing.T) {
	def := RuidaGenericDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"machine_state", "machine_state", "enum"},
		{"position_x", "laser_position_x", "mm"},
		{"position_y", "laser_position_y", "mm"},
	}

	for _, tc := range expectedTelemetry {
		t.Run(tc.key, func(t *testing.T) {
			td, ok := def.TelemetryParse[tc.key]
			assert.True(t, ok, "telemetry %q should exist", tc.key)
			assert.Equal(t, tc.metricType, td.MetricType)
			assert.Equal(t, tc.unit, td.Unit)
			assert.NotEmpty(t, td.Pattern)
			assert.Greater(t, td.ValueIndex, 0)
		})
	}
}
