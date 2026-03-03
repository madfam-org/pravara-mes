package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnapmakerA350Definition_BasicFields(t *testing.T) {
	def := SnapmakerA350Definition()

	assert.Equal(t, "Snapmaker", def.Manufacturer)
	assert.Equal(t, "2.0 A350", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolMarlin, def.Protocol)
	assert.Equal(t, ConnectionSerial, def.Connection)
}

func TestSnapmakerA350Definition_BuildVolume(t *testing.T) {
	def := SnapmakerA350Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 320.0, workVolume["x_mm"])
	assert.Equal(t, 350.0, workVolume["y_mm"])
	assert.Equal(t, 330.0, workVolume["z_mm"])
}

func TestSnapmakerA350Definition_MultiToolCapabilities(t *testing.T) {
	def := SnapmakerA350Definition()

	multiTool, ok := def.Capabilities["multi_tool"].(map[string]interface{})
	assert.True(t, ok, "multi_tool capability should exist")

	modes, ok := multiTool["modes"].([]string)
	assert.True(t, ok, "modes should be a string slice")
	assert.ElementsMatch(t, []string{"3dp", "laser", "cnc"}, modes)
	assert.Equal(t, "M1005", multiTool["detect"])
	assert.Equal(t, "M605", multiTool["switch"])
}

func TestSnapmakerA350Definition_Commands(t *testing.T) {
	def := SnapmakerA350Definition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"home", "G28"},
		{"pause", "M25"},
		{"resume", "M24"},
		{"stop", "M524"},
		{"emergency_stop", "M112"},
		{"detect_toolhead", "M1005"},
		{"switch_mode", "M605 S{mode}"},
		{"auto_level", "G29"},
		{"get_position", "M114"},
		{"get_temperature", "M105"},
		{"temp_extruder", "M104 S{temp}"},
		{"temp_bed", "M140 S{temp}"},
		{"enclosure_led", "M2000 L{brightness}"},
		{"enclosure_fan", "M2000 F{speed}"},
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

func TestSnapmakerA350Definition_TelemetryParse(t *testing.T) {
	def := SnapmakerA350Definition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"temp_extruder", "temperature_extruder", "celsius"},
		{"temp_bed", "temperature_bed", "celsius"},
		{"position_x", "position_x", "mm"},
		{"position_y", "position_y", "mm"},
		{"position_z", "position_z", "mm"},
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

func TestSnapmakerA350Definition_StatusMapping(t *testing.T) {
	def := SnapmakerA350Definition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"Idle", "idle"},
		{"Printing", "running"},
		{"Paused", "paused"},
		{"Error", "error"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}
