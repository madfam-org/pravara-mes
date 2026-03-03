package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Creality K1 Max ---

func TestCrealityK1MaxDefinition_BasicFields(t *testing.T) {
	def := CrealityK1MaxDefinition()

	assert.Equal(t, "Creality", def.Manufacturer)
	assert.Equal(t, "K1 Max", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolMoonraker, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestCrealityK1MaxDefinition_BuildVolume(t *testing.T) {
	def := CrealityK1MaxDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 300.0, workVolume["x_mm"])
	assert.Equal(t, 300.0, workVolume["y_mm"])
	assert.Equal(t, 300.0, workVolume["z_mm"])
}

func TestCrealityK1MaxDefinition_Commands(t *testing.T) {
	def := CrealityK1MaxDefinition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"home", "G28"},
		{"pause", "PAUSE"},
		{"resume", "RESUME"},
		{"stop", "CANCEL_PRINT"},
		{"emergency_stop", "M112"},
		{"gcode_line", "{line}"},
		{"get_temperature", "M105"},
		{"set_temp_extruder", "M104 S{temp}"},
		{"set_temp_bed", "M140 S{temp}"},
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

func TestCrealityK1MaxDefinition_TelemetryParse(t *testing.T) {
	def := CrealityK1MaxDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"extruder_temp", "extruder_temp", "celsius"},
		{"bed_temp", "bed_temp", "celsius"},
		{"print_progress", "print_progress", "ratio"},
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

func TestCrealityK1MaxDefinition_StatusMapping(t *testing.T) {
	def := CrealityK1MaxDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"standby", "idle"},
		{"printing", "running"},
		{"paused", "paused"},
		{"error", "error"},
		{"complete", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestCrealityK1MaxDefinition_Camera(t *testing.T) {
	def := CrealityK1MaxDefinition()

	camera, ok := def.Capabilities["camera"].(map[string]interface{})
	assert.True(t, ok, "camera capability should exist")
	assert.Equal(t, true, camera["supported"])
	assert.Equal(t, "1080p", camera["resolution"])
	assert.Equal(t, true, camera["ai"])
}

// --- Voron 2.4 ---

func TestVoron24Definition_BasicFields(t *testing.T) {
	def := Voron24Definition()

	assert.Equal(t, "Voron Design", def.Manufacturer)
	assert.Equal(t, "2.4", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolMoonraker, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestVoron24Definition_BuildVolume(t *testing.T) {
	def := Voron24Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 350.0, workVolume["x_mm"])
	assert.Equal(t, 350.0, workVolume["y_mm"])
	assert.Equal(t, 340.0, workVolume["z_mm"])
}

func TestVoron24Definition_Commands(t *testing.T) {
	def := Voron24Definition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"home", "G28"},
		{"pause", "PAUSE"},
		{"resume", "RESUME"},
		{"stop", "CANCEL_PRINT"},
		{"emergency_stop", "M112"},
		{"gcode_line", "{line}"},
		{"get_temperature", "M105"},
		{"set_temp_extruder", "M104 S{temp}"},
		{"set_temp_bed", "M140 S{temp}"},
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

func TestVoron24Definition_TelemetryParse(t *testing.T) {
	def := Voron24Definition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"extruder_temp", "extruder_temp", "celsius"},
		{"bed_temp", "bed_temp", "celsius"},
		{"print_progress", "print_progress", "ratio"},
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

func TestVoron24Definition_StatusMapping(t *testing.T) {
	def := Voron24Definition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"standby", "idle"},
		{"printing", "running"},
		{"paused", "paused"},
		{"error", "error"},
		{"complete", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestVoron24Definition_ChamberTemp(t *testing.T) {
	def := Voron24Definition()

	chamber, ok := def.Capabilities["chamber_temp"].(map[string]interface{})
	assert.True(t, ok, "chamber_temp capability should exist")
	assert.Equal(t, true, chamber["passive"])
}

// --- Rat Rig V-Core 4 ---

func TestRatRigVCore4Definition_BasicFields(t *testing.T) {
	def := RatRigVCore4Definition()

	assert.Equal(t, "Rat Rig", def.Manufacturer)
	assert.Equal(t, "V-Core 4", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolMoonraker, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestRatRigVCore4Definition_BuildVolume(t *testing.T) {
	def := RatRigVCore4Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 300.0, workVolume["x_mm"])
	assert.Equal(t, 300.0, workVolume["y_mm"])
	assert.Equal(t, 300.0, workVolume["z_mm"])
}

func TestRatRigVCore4Definition_Commands(t *testing.T) {
	def := RatRigVCore4Definition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"home", "G28"},
		{"pause", "PAUSE"},
		{"resume", "RESUME"},
		{"stop", "CANCEL_PRINT"},
		{"emergency_stop", "M112"},
		{"gcode_line", "{line}"},
		{"get_temperature", "M105"},
		{"set_temp_extruder", "M104 S{temp}"},
		{"set_temp_bed", "M140 S{temp}"},
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

func TestRatRigVCore4Definition_TelemetryParse(t *testing.T) {
	def := RatRigVCore4Definition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"extruder_temp", "extruder_temp", "celsius"},
		{"bed_temp", "bed_temp", "celsius"},
		{"print_progress", "print_progress", "ratio"},
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

func TestRatRigVCore4Definition_StatusMapping(t *testing.T) {
	def := RatRigVCore4Definition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"standby", "idle"},
		{"printing", "running"},
		{"paused", "paused"},
		{"error", "error"},
		{"complete", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}
