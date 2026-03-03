package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Duet Generic
// ---------------------------------------------------------------------------

func TestDuetGenericDefinition_BasicFields(t *testing.T) {
	def := DuetGenericDefinition()

	assert.Equal(t, "Duet3D", def.Manufacturer)
	assert.Equal(t, "Duet 3 Generic", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolDuet, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestDuetGenericDefinition_BuildVolume(t *testing.T) {
	def := DuetGenericDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 300.0, workVolume["x_mm"])
	assert.Equal(t, 300.0, workVolume["y_mm"])
	assert.Equal(t, 300.0, workVolume["z_mm"])
}

func TestDuetGenericDefinition_Commands(t *testing.T) {
	def := DuetGenericDefinition()

	expectedCommands := []struct {
		key      string
		template string
	}{
		{"home", "GET /rr_gcode?gcode=G28"},
		{"pause", "GET /rr_gcode?gcode=M25"},
		{"resume", "GET /rr_gcode?gcode=M24"},
		{"stop", "GET /rr_gcode?gcode=M0"},
		{"emergency_stop", "GET /rr_gcode?gcode=M112"},
		{"gcode_line", "GET /rr_gcode?gcode={gcode}"},
		{"get_temperature", "GET /rr_status?type=3"},
		{"set_temp_extruder", "GET /rr_gcode?gcode=M104+S{temp}"},
		{"set_temp_bed", "GET /rr_gcode?gcode=M140+S{temp}"},
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

func TestDuetGenericDefinition_StatusMapping(t *testing.T) {
	def := DuetGenericDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"I", "idle"},
		{"P", "running"},
		{"S", "paused"},
		{"D", "paused"},
		{"H", "error"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestDuetGenericDefinition_TelemetryParse(t *testing.T) {
	def := DuetGenericDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"extruder_temp", "temperature_extruder", "celsius"},
		{"bed_temp", "temperature_bed", "celsius"},
		{"print_progress", "print_progress", "percent"},
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

// ---------------------------------------------------------------------------
// Ultimaker S5
// ---------------------------------------------------------------------------

func TestUltimakerS5Definition_BasicFields(t *testing.T) {
	def := UltimakerS5Definition()

	assert.Equal(t, "Ultimaker", def.Manufacturer)
	assert.Equal(t, "S5", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolCustom, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestUltimakerS5Definition_BuildVolume(t *testing.T) {
	def := UltimakerS5Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 330.0, workVolume["x_mm"])
	assert.Equal(t, 240.0, workVolume["y_mm"])
	assert.Equal(t, 300.0, workVolume["z_mm"])
}

func TestUltimakerS5Definition_DualExtrusion(t *testing.T) {
	def := UltimakerS5Definition()

	dualExtrusion, ok := def.Capabilities["dual_extrusion"].(map[string]interface{})
	assert.True(t, ok, "dual_extrusion capability should exist")
	assert.Equal(t, true, dualExtrusion["supported"])
	assert.Equal(t, 2, dualExtrusion["extruder_count"])
}

func TestUltimakerS5Definition_Commands(t *testing.T) {
	def := UltimakerS5Definition()

	expectedCommands := []struct {
		key string
	}{
		{"home"},
		{"pause"},
		{"resume"},
		{"stop"},
		{"get_status"},
		{"get_temperature"},
		{"set_temp"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.NotEmpty(t, cmd.Name)
			assert.NotEmpty(t, cmd.Template)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestUltimakerS5Definition_StatusMapping(t *testing.T) {
	def := UltimakerS5Definition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"idle", "idle"},
		{"printing", "running"},
		{"paused", "paused"},
		{"error", "error"},
		{"maintenance", "idle"},
		{"booting", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestUltimakerS5Definition_TelemetryParse(t *testing.T) {
	def := UltimakerS5Definition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"extruder_temp", "temperature_extruder", "celsius"},
		{"bed_temp", "temperature_bed", "celsius"},
		{"print_progress", "print_progress", "percent"},
		{"print_state", "print_state", "enum"},
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

// ---------------------------------------------------------------------------
// Formlabs Form 4
// ---------------------------------------------------------------------------

func TestFormlabsForm4Definition_BasicFields(t *testing.T) {
	def := FormlabsForm4Definition()

	assert.Equal(t, "Formlabs", def.Manufacturer)
	assert.Equal(t, "Form 4", def.Model)
	assert.Equal(t, MachineType3DPrinterSLA, def.Type)
	assert.Equal(t, ProtocolFormlabs, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestFormlabsForm4Definition_BuildVolume(t *testing.T) {
	def := FormlabsForm4Definition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 200.0, workVolume["x_mm"])
	assert.Equal(t, 125.0, workVolume["y_mm"])
	assert.Equal(t, 210.0, workVolume["z_mm"])
}

func TestFormlabsForm4Definition_Resolution(t *testing.T) {
	def := FormlabsForm4Definition()

	resolution, ok := def.Capabilities["resolution"].(map[string]interface{})
	assert.True(t, ok, "resolution capability should exist")
	assert.Equal(t, 50.0, resolution["xy_microns"])
	assert.Equal(t, 25.0, resolution["z_microns"])
}

func TestFormlabsForm4Definition_Commands(t *testing.T) {
	def := FormlabsForm4Definition()

	expectedCommands := []struct {
		key string
	}{
		{"get_status"},
		{"pause"},
		{"resume"},
		{"stop"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.NotEmpty(t, cmd.Name)
			assert.NotEmpty(t, cmd.Template)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestFormlabsForm4Definition_StatusMapping(t *testing.T) {
	def := FormlabsForm4Definition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"idle", "idle"},
		{"printing", "running"},
		{"paused", "paused"},
		{"error", "error"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestFormlabsForm4Definition_TelemetryParse(t *testing.T) {
	def := FormlabsForm4Definition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"print_progress", "print_progress", "percent"},
		{"printer_state", "printer_state", "enum"},
		{"resin_temp", "resin_temperature", "celsius"},
		{"tank_level", "tank_level", "percent"},
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
