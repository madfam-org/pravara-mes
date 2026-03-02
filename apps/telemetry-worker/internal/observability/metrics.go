// Package observability provides Prometheus metrics for the telemetry worker.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "pravara"
	subsystem = "telemetry"
)

var (
	// MQTTMessagesReceived counts MQTT messages received by topic root.
	MQTTMessagesReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "mqtt_messages_received_total",
			Help:      "Total number of MQTT messages received",
		},
		[]string{"topic_root"},
	)

	// MQTTMessagesProcessed counts successfully processed MQTT messages by metric type.
	MQTTMessagesProcessed = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "mqtt_messages_processed_total",
			Help:      "Total number of MQTT messages successfully processed",
		},
		[]string{"metric_type"},
	)

	// MQTTMessagesDropped counts dropped MQTT messages by reason.
	MQTTMessagesDropped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "mqtt_messages_dropped_total",
			Help:      "Total number of MQTT messages dropped",
		},
		[]string{"reason"},
	)

	// MQTTConnectionStatus indicates MQTT connection status (1=connected, 0=disconnected).
	MQTTConnectionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "mqtt_connection_status",
			Help:      "MQTT connection status (1=connected, 0=disconnected)",
		},
	)

	// BatchSize tracks the size of telemetry batches written to database.
	BatchSize = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "batch_size",
			Help:      "Size of telemetry batches written to database",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		},
	)

	// BatchWriteDuration tracks batch write operation duration in seconds.
	BatchWriteDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "batch_write_duration_seconds",
			Help:      "Duration of batch write operations to database",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		},
	)

	// BatchWriteRetries counts batch write retry attempts.
	BatchWriteRetries = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "batch_write_retries_total",
			Help:      "Total number of batch write retry attempts",
		},
	)

	// BatchWriteFailures counts failed batch write operations.
	BatchWriteFailures = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "batch_write_failures_total",
			Help:      "Total number of failed batch write operations",
		},
	)

	// WorkerQueueLength tracks the current processing queue length.
	WorkerQueueLength = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "worker_queue_length",
			Help:      "Current number of telemetry messages in processing queue",
		},
	)

	// TelemetryPointsIngested counts total telemetry data points ingested.
	TelemetryPointsIngested = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "telemetry_points_ingested_total",
			Help:      "Total number of telemetry data points ingested",
		},
	)

	// DBConnectionsOpen tracks the number of established database connections.
	DBConnectionsOpen = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_open",
			Help:      "Number of established database connections",
		},
	)

	// DBConnectionsInUse tracks the number of database connections currently in use.
	DBConnectionsInUse = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_in_use",
			Help:      "Number of database connections currently in use",
		},
	)
)
