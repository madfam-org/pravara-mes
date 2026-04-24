package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Prusa Core One ---

func TestPrusaCoreOneDefinition_BasicFields(t *testing.T) {
	def := PrusaCoreOneDefinition()

	assert.Equal(t, "Prusa", def.Manufacturer)
	assert.Equal(t, "Core One", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolPrusaLink, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestPrusaCoreOneDefinition_BuildVolume(t *testing.T) {
	def := PrusaCoreOneDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 250.0, workVolume["x_mm"])
	assert.Equal(t, 220.0, workVolume["y_mm"])
	assert.Equal(t, 270.0, workVolume["z_mm"])
}

func TestPrusaCoreOneDefinition_Commands(t *testing.T) {
	def := PrusaCoreOneDefinition()

	expectedCommands := []struct {
		key  string
		name string
	}{
		{"home", "Home All Axes"},
		{"pause", "Pause Print"},
		{"resume", "Resume Print"},
		{"stop", "Stop Print"},
		{"get_status", "Get Printer Status"},
		{"get_temperature", "Get Temperature"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.Equal(t, tc.name, cmd.Name)
			assert.NotEmpty(t, cmd.Template)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestPrusaCoreOneDefinition_TelemetryParse(t *testing.T) {
	def := PrusaCoreOneDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"nozzle_temp", "temperature_nozzle", "celsius"},
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

func TestPrusaCoreOneDefinition_StatusMapping(t *testing.T) {
	def := PrusaCoreOneDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"IDLE", "idle"},
		{"PRINTING", "running"},
		{"PAUSED", "paused"},
		{"ERROR", "error"},
		{"FINISHED", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestPrusaCoreOneDefinition_InputShaping(t *testing.T) {
	def := PrusaCoreOneDefinition()

	inputShaping, ok := def.Capabilities["input_shaping"].(map[string]interface{})
	assert.True(t, ok, "input_shaping capability should exist")
	assert.Equal(t, true, inputShaping["supported"])
}

// --- Prusa MINI+ ---

func TestPrusaMiniPlusDefinition_BasicFields(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	assert.Equal(t, "Prusa", def.Manufacturer)
	assert.Equal(t, "MINI+", def.Model)
	assert.Equal(t, MachineType3DPrinterFDM, def.Type)
	assert.Equal(t, ProtocolPrusaLink, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestPrusaMiniPlusDefinition_BuildVolume(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 180.0, workVolume["x_mm"])
	assert.Equal(t, 180.0, workVolume["y_mm"])
	assert.Equal(t, 180.0, workVolume["z_mm"])
}

func TestPrusaMiniPlusDefinition_Commands(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	expectedCommands := []struct {
		key  string
		name string
	}{
		{"home", "Home All Axes"},
		{"pause", "Pause Print"},
		{"resume", "Resume Print"},
		{"stop", "Stop Print"},
		{"get_status", "Get Printer Status"},
		{"get_temperature", "Get Temperature"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.Equal(t, tc.name, cmd.Name)
			assert.NotEmpty(t, cmd.Template)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestPrusaMiniPlusDefinition_TelemetryParse(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"nozzle_temp", "temperature_nozzle", "celsius"},
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

func TestPrusaMiniPlusDefinition_StatusMapping(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"IDLE", "idle"},
		{"PRINTING", "running"},
		{"PAUSED", "paused"},
		{"ERROR", "error"},
		{"FINISHED", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestPrusaMiniPlusDefinition_NozzleTempRange(t *testing.T) {
	def := PrusaMiniPlusDefinition()

	nozzle, ok := def.Capabilities["nozzle_temp"].(map[string]interface{})
	assert.True(t, ok, "nozzle_temp capability should exist")
	assert.Equal(t, 0.0, nozzle["min_celsius"])
	assert.Equal(t, 280.0, nozzle["max_celsius"])
}

// --- Prusa SL1S Speed ---

func TestPrusaSL1SDefinition_BasicFields(t *testing.T) {
	def := PrusaSL1SDefinition()

	assert.Equal(t, "Prusa", def.Manufacturer)
	assert.Equal(t, "SL1S Speed", def.Model)
	assert.Equal(t, MachineType3DPrinterSLA, def.Type)
	assert.Equal(t, ProtocolPrusaLink, def.Protocol)
	assert.Equal(t, ConnectionHTTP, def.Connection)
}

func TestPrusaSL1SDefinition_BuildVolume(t *testing.T) {
	def := PrusaSL1SDefinition()

	workVolume, ok := def.Capabilities["work_volume"].(map[string]interface{})
	assert.True(t, ok, "work_volume capability should exist")
	assert.Equal(t, 127.0, workVolume["x_mm"])
	assert.Equal(t, 80.0, workVolume["y_mm"])
	assert.Equal(t, 150.0, workVolume["z_mm"])
}

func TestPrusaSL1SDefinition_Resolution(t *testing.T) {
	def := PrusaSL1SDefinition()

	resolution, ok := def.Capabilities["resolution"].(map[string]interface{})
	assert.True(t, ok, "resolution capability should exist")
	assert.Equal(t, 47.0, resolution["xy_microns"])
}

func TestPrusaSL1SDefinition_Commands(t *testing.T) {
	def := PrusaSL1SDefinition()

	expectedCommands := []struct {
		key  string
		name string
	}{
		{"start_print", "Start Print"},
		{"pause", "Pause Print"},
		{"resume", "Resume Print"},
		{"stop", "Stop Print"},
		{"get_status", "Get Printer Status"},
	}

	for _, tc := range expectedCommands {
		t.Run(tc.key, func(t *testing.T) {
			cmd, ok := def.Commands[tc.key]
			assert.True(t, ok, "command %q should exist", tc.key)
			assert.Equal(t, tc.name, cmd.Name)
			assert.NotEmpty(t, cmd.Template)
			assert.Greater(t, cmd.Timeout.Seconds(), 0.0)
		})
	}
}

func TestPrusaSL1SDefinition_TelemetryParse(t *testing.T) {
	def := PrusaSL1SDefinition()

	expectedTelemetry := []struct {
		key        string
		metricType string
		unit       string
	}{
		{"print_progress", "print_progress", "percent"},
		{"uv_led_temp", "temperature_uv_led", "celsius"},
		{"ambient_temp", "temperature_ambient", "celsius"},
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

func TestPrusaSL1SDefinition_StatusMapping(t *testing.T) {
	def := PrusaSL1SDefinition()

	tests := []struct {
		raw      string
		expected string
	}{
		{"IDLE", "idle"},
		{"PRINTING", "running"},
		{"PAUSED", "paused"},
		{"ERROR", "error"},
		{"FINISHED", "idle"},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			mapped, ok := def.StatusMapping[tc.raw]
			assert.True(t, ok)
			assert.Equal(t, tc.expected, mapped)
		})
	}
}

func TestPrusaSL1SDefinition_NoNozzleOrBedTemp(t *testing.T) {
	def := PrusaSL1SDefinition()

	_, hasNozzle := def.Capabilities["nozzle_temp"]
	assert.False(t, hasNozzle, "SL1S should not have nozzle_temp capability")

	_, hasBed := def.Capabilities["bed_temp"]
	assert.False(t, hasBed, "SL1S should not have bed_temp capability")
}
