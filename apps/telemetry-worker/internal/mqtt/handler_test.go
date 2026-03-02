package mqtt

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTelemetryPayload_Parsing(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		valid   bool
		value   float64
		unit    string
		metric  string
	}{
		{
			name:    "valid temperature payload",
			json:    `{"timestamp": "2026-03-01T15:30:00Z", "machine_id": "cnc-01", "metric_type": "temperature", "value": 45.2, "unit": "celsius"}`,
			valid:   true,
			value:   45.2,
			unit:    "celsius",
			metric:  "temperature",
		},
		{
			name:    "valid power payload",
			json:    `{"machine_id": "cnc-01", "metric_type": "power", "value": 1500.0, "unit": "watts"}`,
			valid:   true,
			value:   1500.0,
			unit:    "watts",
			metric:  "power",
		},
		{
			name:    "valid spindle speed",
			json:    `{"machine_id": "cnc-01", "metric_type": "spindle_speed", "value": 12000, "unit": "rpm"}`,
			valid:   true,
			value:   12000,
			unit:    "rpm",
			metric:  "spindle_speed",
		},
		{
			name:    "payload with metadata",
			json:    `{"machine_id": "cnc-01", "metric_type": "temperature", "value": 45.2, "unit": "celsius", "metadata": {"sensor_id": "S001", "location": "spindle"}}`,
			valid:   true,
			value:   45.2,
			unit:    "celsius",
			metric:  "temperature",
		},
		{
			name:  "invalid json",
			json:  `{invalid json}`,
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload TelemetryPayload
			err := json.Unmarshal([]byte(tt.json), &payload)

			if tt.valid {
				if err != nil {
					t.Fatalf("expected valid payload to parse, got error: %v", err)
				}
				if payload.Value != tt.value {
					t.Errorf("Value: got %f, want %f", payload.Value, tt.value)
				}
				if payload.Unit != tt.unit {
					t.Errorf("Unit: got %q, want %q", payload.Unit, tt.unit)
				}
				if payload.MetricType != tt.metric {
					t.Errorf("MetricType: got %q, want %q", payload.MetricType, tt.metric)
				}
			} else {
				if err == nil {
					t.Error("expected invalid payload to fail parsing")
				}
			}
		})
	}
}

func TestTelemetryPayload_Timestamp(t *testing.T) {
	tests := []struct {
		name          string
		json          string
		hasTimestamp  bool
		expectedYear  int
	}{
		{
			name:          "with timestamp",
			json:          `{"timestamp": "2026-03-01T15:30:00Z", "machine_id": "cnc-01", "metric_type": "temp", "value": 25, "unit": "c"}`,
			hasTimestamp:  true,
			expectedYear:  2026,
		},
		{
			name:          "without timestamp",
			json:          `{"machine_id": "cnc-01", "metric_type": "temp", "value": 25, "unit": "c"}`,
			hasTimestamp:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload TelemetryPayload
			if err := json.Unmarshal([]byte(tt.json), &payload); err != nil {
				t.Fatalf("failed to parse: %v", err)
			}

			if tt.hasTimestamp {
				if payload.Timestamp == nil {
					t.Error("expected timestamp to be present")
				} else if payload.Timestamp.Year() != tt.expectedYear {
					t.Errorf("Year: got %d, want %d", payload.Timestamp.Year(), tt.expectedYear)
				}
			} else {
				if payload.Timestamp != nil {
					t.Error("expected timestamp to be nil")
				}
			}
		})
	}
}

func TestTelemetryMessage_TopicParsing(t *testing.T) {
	tests := []struct {
		name         string
		topic        string
		expectedLen  int
		tenant       string
		machine      string
	}{
		{
			name:        "valid UNS topic",
			topic:       "madfam/hel/production/line-1/cnc-01/temperature",
			expectedLen: 6,
			tenant:      "madfam",
			machine:     "cnc-01",
		},
		{
			name:        "topic with event",
			topic:       "madfam/hel/production/line-1/cnc-01/event/job_completed",
			expectedLen: 7,
			tenant:      "madfam",
			machine:     "cnc-01",
		},
		{
			name:        "short topic",
			topic:       "tenant/site/area",
			expectedLen: 3,
			tenant:      "tenant",
			machine:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := splitTopic(tt.topic)

			if len(parts) != tt.expectedLen {
				t.Errorf("parts length: got %d, want %d", len(parts), tt.expectedLen)
			}

			if len(parts) > 0 && parts[0] != tt.tenant {
				t.Errorf("tenant: got %q, want %q", parts[0], tt.tenant)
			}

			if len(parts) > 4 && parts[4] != tt.machine {
				t.Errorf("machine: got %q, want %q", parts[4], tt.machine)
			}
		})
	}
}

func splitTopic(topic string) []string {
	var parts []string
	var current string
	for _, c := range topic {
		if c == '/' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func TestTelemetryMetrics_CommonTypes(t *testing.T) {
	// Common metric types that should be supported
	commonMetrics := []struct {
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

	for _, m := range commonMetrics {
		t.Run(m.metricType+"_"+m.unit, func(t *testing.T) {
			payload := TelemetryPayload{
				MachineID:  "test-machine",
				MetricType: m.metricType,
				Value:      m.sampleVal,
				Unit:       m.unit,
			}

			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			var decoded TelemetryPayload
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			if decoded.MetricType != m.metricType {
				t.Errorf("MetricType: got %q, want %q", decoded.MetricType, m.metricType)
			}
			if decoded.Unit != m.unit {
				t.Errorf("Unit: got %q, want %q", decoded.Unit, m.unit)
			}
			if decoded.Value != m.sampleVal {
				t.Errorf("Value: got %f, want %f", decoded.Value, m.sampleVal)
			}
		})
	}
}

func TestBatchProcessing_Timing(t *testing.T) {
	// Test batch timeout calculation
	batchTimeoutMs := 1000
	timeout := time.Duration(batchTimeoutMs) * time.Millisecond

	if timeout != time.Second {
		t.Errorf("batch timeout: got %v, want %v", timeout, time.Second)
	}

	// Test batch size boundaries
	batchSizes := []struct {
		size      int
		shouldFlush bool
		maxSize   int
	}{
		{size: 50, shouldFlush: false, maxSize: 100},
		{size: 99, shouldFlush: false, maxSize: 100},
		{size: 100, shouldFlush: true, maxSize: 100},
		{size: 101, shouldFlush: true, maxSize: 100},
	}

	for _, tc := range batchSizes {
		shouldFlush := tc.size >= tc.maxSize
		if shouldFlush != tc.shouldFlush {
			t.Errorf("batch size %d: shouldFlush got %v, want %v", tc.size, shouldFlush, tc.shouldFlush)
		}
	}
}
