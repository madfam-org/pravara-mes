// Package billing - Example integration for telemetry worker
// This file demonstrates how to integrate usage tracking into the telemetry worker.
package billing

/*
Example usage in telemetry worker main.go or processor:

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/billing"
)

func main() {
	log := logrus.New()

	// Initialize usage recorder
	recorder, err := billing.NewRedisUsageRecorder(billing.RecorderConfig{
		RedisURL:   "redis://localhost:6379",
		BufferSize: 1000,
	}, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize usage recorder")
	}
	defer recorder.Close()

	// In telemetry batch processor
	processTelemetryBatch(recorder, log)
}

func processTelemetryBatch(recorder billing.UsageRecorder, log *logrus.Logger) {
	ctx := context.Background()

	// Example: Process telemetry batch from queue
	batch := []TelemetryPoint{
		{TenantID: "tenant-123", MachineID: "machine-1", Value: 42.5},
		{TenantID: "tenant-123", MachineID: "machine-1", Value: 43.0},
		{TenantID: "tenant-456", MachineID: "machine-2", Value: 100.0},
	}

	// Store telemetry points in database...
	// ...

	// After successful processing, record usage events
	// Group by tenant for efficient batch recording
	usageByTenant := make(map[string]int64)
	for _, point := range batch {
		usageByTenant[point.TenantID]++
	}

	// Create usage events
	var events []billing.UsageEvent
	for tenantID, count := range usageByTenant {
		events = append(events, billing.UsageEvent{
			TenantID:  tenantID,
			EventType: billing.UsageEventTelemetry,
			Quantity:  count,
			Metadata: map[string]string{
				"batch_size": fmt.Sprintf("%d", count),
			},
			Timestamp: time.Now(),
		})
	}

	// Record all usage events in a batch (more efficient)
	if err := recorder.RecordBatch(ctx, events); err != nil {
		log.WithError(err).Error("Failed to record telemetry usage")
	} else {
		log.WithField("tenant_count", len(events)).Info("Recorded telemetry usage")
	}
}

// Alternative: Record individual events
func recordSingleEvent(recorder billing.UsageRecorder, tenantID string, count int64, log *logrus.Logger) {
	event := billing.UsageEvent{
		TenantID:  tenantID,
		EventType: billing.UsageEventTelemetry,
		Quantity:  count,
		Timestamp: time.Now(),
	}

	ctx := context.Background()
	if err := recorder.RecordEvent(ctx, event); err != nil {
		log.WithError(err).Warn("Failed to record telemetry event")
	}
}

type TelemetryPoint struct {
	TenantID  string
	MachineID string
	Value     float64
}
*/
