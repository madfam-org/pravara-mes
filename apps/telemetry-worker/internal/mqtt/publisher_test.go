package mqtt

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestTelemetryEvent_Structure(t *testing.T) {
	tenantID := uuid.New()
	machineID := uuid.New()

	event := TelemetryEvent{
		ID:        uuid.New().String(),
		Type:      "machine.telemetry_batch",
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data: TelemetryBatchData{
			MachineID:  machineID,
			ReceivedAt: time.Now().UTC(),
			Metrics: []TelemetryMetric{
				{Type: "temperature", Value: 45.2, Unit: "celsius", Timestamp: time.Now().UTC()},
				{Type: "power", Value: 1500.0, Unit: "watts", Timestamp: time.Now().UTC()},
			},
		},
	}

	if event.Type != "machine.telemetry_batch" {
		t.Errorf("Type: got %q, want %q", event.Type, "machine.telemetry_batch")
	}
	if event.TenantID != tenantID {
		t.Errorf("TenantID: got %v, want %v", event.TenantID, tenantID)
	}
	if event.Data.MachineID != machineID {
		t.Errorf("Data.MachineID: got %v, want %v", event.Data.MachineID, machineID)
	}
	if len(event.Data.Metrics) != 2 {
		t.Errorf("Data.Metrics length: got %d, want %d", len(event.Data.Metrics), 2)
	}
}

func TestMachineStatusEvent_Structure(t *testing.T) {
	tenantID := uuid.New()
	machineID := uuid.New()

	event := MachineStatusEvent{
		ID:        uuid.New().String(),
		Type:      "machine.heartbeat",
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		Data: MachineStatusData{
			MachineID:     machineID,
			MachineName:   "CNC-01",
			NewStatus:     string(types.MachineStatusOnline),
			LastHeartbeat: time.Now().UTC(),
			IsOnline:      true,
		},
	}

	if event.Type != "machine.heartbeat" {
		t.Errorf("Type: got %q, want %q", event.Type, "machine.heartbeat")
	}
	if event.Data.MachineName != "CNC-01" {
		t.Errorf("Data.MachineName: got %q, want %q", event.Data.MachineName, "CNC-01")
	}
	if !event.Data.IsOnline {
		t.Error("Data.IsOnline: expected true")
	}
	if event.Data.NewStatus != string(types.MachineStatusOnline) {
		t.Errorf("Data.NewStatus: got %q, want %q", event.Data.NewStatus, types.MachineStatusOnline)
	}
}

func TestTelemetryMetric_Conversion(t *testing.T) {
	now := time.Now().UTC()

	// Test conversion from types.Telemetry to TelemetryMetric
	telemetry := types.Telemetry{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		MachineID:  uuid.New(),
		Timestamp:  now,
		MetricType: "temperature",
		Value:      45.2,
		Unit:       "celsius",
	}

	metric := TelemetryMetric{
		Type:      telemetry.MetricType,
		Value:     telemetry.Value,
		Unit:      telemetry.Unit,
		Timestamp: telemetry.Timestamp,
	}

	if metric.Type != "temperature" {
		t.Errorf("Type: got %q, want %q", metric.Type, "temperature")
	}
	if metric.Value != 45.2 {
		t.Errorf("Value: got %f, want %f", metric.Value, 45.2)
	}
	if metric.Unit != "celsius" {
		t.Errorf("Unit: got %q, want %q", metric.Unit, "celsius")
	}
	if !metric.Timestamp.Equal(now) {
		t.Errorf("Timestamp: got %v, want %v", metric.Timestamp, now)
	}
}

func TestTelemetryBatchGrouping(t *testing.T) {
	tenant1 := uuid.New()
	tenant2 := uuid.New()
	machine1 := uuid.New()
	machine2 := uuid.New()

	records := []types.Telemetry{
		{ID: uuid.New(), TenantID: tenant1, MachineID: machine1, MetricType: "temp", Value: 40.0, Unit: "c"},
		{ID: uuid.New(), TenantID: tenant1, MachineID: machine1, MetricType: "power", Value: 100.0, Unit: "w"},
		{ID: uuid.New(), TenantID: tenant1, MachineID: machine2, MetricType: "temp", Value: 42.0, Unit: "c"},
		{ID: uuid.New(), TenantID: tenant2, MachineID: machine1, MetricType: "temp", Value: 38.0, Unit: "c"},
	}

	// Group records by tenant and machine
	grouped := make(map[uuid.UUID]map[uuid.UUID][]types.Telemetry)
	for _, r := range records {
		if grouped[r.TenantID] == nil {
			grouped[r.TenantID] = make(map[uuid.UUID][]types.Telemetry)
		}
		grouped[r.TenantID][r.MachineID] = append(grouped[r.TenantID][r.MachineID], r)
	}

	// Verify grouping
	if len(grouped) != 2 {
		t.Errorf("tenant groups: got %d, want %d", len(grouped), 2)
	}
	if len(grouped[tenant1]) != 2 {
		t.Errorf("tenant1 machines: got %d, want %d", len(grouped[tenant1]), 2)
	}
	if len(grouped[tenant1][machine1]) != 2 {
		t.Errorf("tenant1/machine1 records: got %d, want %d", len(grouped[tenant1][machine1]), 2)
	}
	if len(grouped[tenant2]) != 1 {
		t.Errorf("tenant2 machines: got %d, want %d", len(grouped[tenant2]), 1)
	}
}

func TestChannelNameFormat(t *testing.T) {
	tests := []struct {
		namespace string
		tenantID  uuid.UUID
		expected  string
	}{
		{
			namespace: "telemetry",
			tenantID:  uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			expected:  "telemetry:123e4567-e89b-12d3-a456-426614174000",
		},
		{
			namespace: "machines",
			tenantID:  uuid.MustParse("550e8400-e29b-41d4-a716-446655440000"),
			expected:  "machines:550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			channel := tt.namespace + ":" + tt.tenantID.String()
			if channel != tt.expected {
				t.Errorf("channel: got %q, want %q", channel, tt.expected)
			}
		})
	}
}

func TestPublisherConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  PublisherConfig
		valid   bool
	}{
		{
			name:    "valid redis URL",
			config:  PublisherConfig{RedisURL: "redis://localhost:6379"},
			valid:   true,
		},
		{
			name:    "valid redis URL with password",
			config:  PublisherConfig{RedisURL: "redis://:password@localhost:6379"},
			valid:   true,
		},
		{
			name:    "empty redis URL",
			config:  PublisherConfig{RedisURL: ""},
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.config.RedisURL != ""
			if valid != tt.valid {
				t.Errorf("config validity: got %v, want %v", valid, tt.valid)
			}
		})
	}
}
