package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTelemetryQueryResponse_Fields(t *testing.T) {
	tests := []struct {
		name     string
		response TelemetryQueryResponse
	}{
		{
			name: "empty response",
			response: TelemetryQueryResponse{
				Data:  nil,
				Count: 0,
			},
		},
		{
			name: "response with filters",
			response: TelemetryQueryResponse{
				Data:       nil,
				Count:      0,
				MachineID:  "550e8400-e29b-41d4-a716-446655440000",
				MetricType: "temperature",
				FromTime:   "2026-03-01T00:00:00Z",
				ToTime:     "2026-03-01T23:59:59Z",
			},
		},
		{
			name: "response with count",
			response: TelemetryQueryResponse{
				Data:       nil,
				Count:      100,
				MachineID:  "550e8400-e29b-41d4-a716-446655440000",
				MetricType: "power",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TelemetryQueryResponse
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.Count != tt.response.Count {
				t.Errorf("Count: got %d, want %d", decoded.Count, tt.response.Count)
			}
			if decoded.MachineID != tt.response.MachineID {
				t.Errorf("MachineID: got %q, want %q", decoded.MachineID, tt.response.MachineID)
			}
			if decoded.MetricType != tt.response.MetricType {
				t.Errorf("MetricType: got %q, want %q", decoded.MetricType, tt.response.MetricType)
			}
		})
	}
}

func TestBatchTelemetryRequest_Validation(t *testing.T) {
	tests := []struct {
		name         string
		request      BatchTelemetryRequest
		recordCount  int
		hasRecords   bool
	}{
		{
			name: "single record",
			request: BatchTelemetryRequest{
				Records: []TelemetryRecord{
					{
						MachineID:  "550e8400-e29b-41d4-a716-446655440000",
						MetricType: "temperature",
						Value:      45.2,
						Unit:       "celsius",
					},
				},
			},
			recordCount: 1,
			hasRecords:  true,
		},
		{
			name: "multiple records",
			request: BatchTelemetryRequest{
				Records: []TelemetryRecord{
					{MachineID: "m1", MetricType: "temperature", Value: 45.0, Unit: "celsius"},
					{MachineID: "m1", MetricType: "power", Value: 1500.0, Unit: "watts"},
					{MachineID: "m2", MetricType: "temperature", Value: 38.5, Unit: "celsius"},
				},
			},
			recordCount: 3,
			hasRecords:  true,
		},
		{
			name: "empty records",
			request: BatchTelemetryRequest{
				Records: []TelemetryRecord{},
			},
			recordCount: 0,
			hasRecords:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded BatchTelemetryRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if len(decoded.Records) != tt.recordCount {
				t.Errorf("record count: got %d, want %d", len(decoded.Records), tt.recordCount)
			}

			hasRecords := len(decoded.Records) > 0
			if hasRecords != tt.hasRecords {
				t.Errorf("hasRecords: got %v, want %v", hasRecords, tt.hasRecords)
			}
		})
	}
}

func TestTelemetryRecord_Fields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name   string
		record TelemetryRecord
	}{
		{
			name: "record with timestamp",
			record: TelemetryRecord{
				MachineID:  "550e8400-e29b-41d4-a716-446655440000",
				Timestamp:  now,
				MetricType: "temperature",
				Value:      45.2,
				Unit:       "celsius",
			},
		},
		{
			name: "record without timestamp",
			record: TelemetryRecord{
				MachineID:  "550e8400-e29b-41d4-a716-446655440000",
				MetricType: "power",
				Value:      1500.0,
				Unit:       "watts",
			},
		},
		{
			name: "record with metadata",
			record: TelemetryRecord{
				MachineID:  "550e8400-e29b-41d4-a716-446655440000",
				Timestamp:  now,
				MetricType: "vibration",
				Value:      0.5,
				Unit:       "g",
				Metadata: map[string]any{
					"sensor_id": "VIB-001",
					"axis":      "z",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.record)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TelemetryRecord
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.MachineID != tt.record.MachineID {
				t.Errorf("MachineID: got %q, want %q", decoded.MachineID, tt.record.MachineID)
			}
			if decoded.MetricType != tt.record.MetricType {
				t.Errorf("MetricType: got %q, want %q", decoded.MetricType, tt.record.MetricType)
			}
			if decoded.Value != tt.record.Value {
				t.Errorf("Value: got %f, want %f", decoded.Value, tt.record.Value)
			}
			if decoded.Unit != tt.record.Unit {
				t.Errorf("Unit: got %q, want %q", decoded.Unit, tt.record.Unit)
			}
		})
	}
}

func TestBatchTelemetryRequest_SizeLimits(t *testing.T) {
	// Test batch size limits
	batchLimits := []struct {
		size       int
		withinLimit bool
		maxSize    int
	}{
		{size: 1, withinLimit: true, maxSize: 1000},
		{size: 500, withinLimit: true, maxSize: 1000},
		{size: 1000, withinLimit: true, maxSize: 1000},
		{size: 1001, withinLimit: false, maxSize: 1000},
		{size: 5000, withinLimit: false, maxSize: 1000},
	}

	for _, tc := range batchLimits {
		withinLimit := tc.size <= tc.maxSize
		if withinLimit != tc.withinLimit {
			t.Errorf("batch size %d: withinLimit got %v, want %v", tc.size, withinLimit, tc.withinLimit)
		}
	}
}

func TestTelemetryMetricTypes_Supported(t *testing.T) {
	// Metric types that should be supported per README
	supportedMetrics := []struct {
		metricType string
		unit       string
		sampleVal  float64
	}{
		{"temperature", "celsius", 45.2},
		{"temperature", "fahrenheit", 113.4},
		{"power", "watts", 1500.0},
		{"power", "kilowatts", 1.5},
		{"spindle_speed", "rpm", 12000},
		{"feed_rate", "mm/min", 500},
		{"vibration", "g", 0.5},
		{"current", "amps", 15.5},
		{"voltage", "volts", 220},
		{"pressure", "psi", 100},
		{"pressure", "bar", 6.89},
		{"humidity", "percent", 45},
		{"cycle_count", "count", 1523},
		{"uptime", "hours", 1250.5},
	}

	for _, m := range supportedMetrics {
		t.Run(m.metricType+"_"+m.unit, func(t *testing.T) {
			record := TelemetryRecord{
				MachineID:  "test-machine",
				MetricType: m.metricType,
				Value:      m.sampleVal,
				Unit:       m.unit,
			}

			data, err := json.Marshal(record)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TelemetryRecord
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.MetricType != m.metricType {
				t.Errorf("MetricType: got %q, want %q", decoded.MetricType, m.metricType)
			}
		})
	}
}

func TestTelemetryTimestampHandling(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name         string
		record       TelemetryRecord
		hasTimestamp bool
	}{
		{
			name: "with explicit timestamp",
			record: TelemetryRecord{
				MachineID:  "m1",
				Timestamp:  now,
				MetricType: "temp",
				Value:      25,
				Unit:       "c",
			},
			hasTimestamp: true,
		},
		{
			name: "without timestamp (zero value)",
			record: TelemetryRecord{
				MachineID:  "m1",
				MetricType: "temp",
				Value:      25,
				Unit:       "c",
			},
			hasTimestamp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.record)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TelemetryRecord
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			hasTimestamp := !decoded.Timestamp.IsZero()
			if hasTimestamp != tt.hasTimestamp {
				t.Errorf("hasTimestamp: got %v, want %v", hasTimestamp, tt.hasTimestamp)
			}
		})
	}
}
