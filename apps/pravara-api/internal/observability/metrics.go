// Package observability provides Prometheus metrics for the PravaraMES API.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "pravara"
	subsystem = "api"
)

var (
	// HTTPRequestsTotal counts total HTTP requests by method, path, and status code.
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests processed",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration tracks HTTP request duration in seconds.
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request duration in seconds",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path"},
	)

	// HTTPRequestsInFlight tracks the number of HTTP requests currently being processed.
	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "http_requests_in_flight",
			Help:      "Current number of HTTP requests being processed",
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

	// DBConnectionsIdle tracks the number of idle database connections.
	DBConnectionsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_idle",
			Help:      "Number of idle database connections",
		},
	)

	// DBConnectionsWaitCount tracks the total number of connections waited for.
	DBConnectionsWaitCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_wait_count_total",
			Help:      "Total number of connections waited for",
		},
	)

	// DBConnectionsWaitDuration tracks the total time blocked waiting for connections.
	DBConnectionsWaitDuration = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_wait_duration_seconds_total",
			Help:      "Total time blocked waiting for database connections",
		},
	)

	// DBConnectionsMaxIdleClosed tracks connections closed due to max idle.
	DBConnectionsMaxIdleClosed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_max_idle_closed_total",
			Help:      "Total number of connections closed due to SetMaxIdleConns",
		},
	)

	// DBConnectionsMaxLifetimeClosed tracks connections closed due to max lifetime.
	DBConnectionsMaxLifetimeClosed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "db_connections_max_lifetime_closed_total",
			Help:      "Total number of connections closed due to SetConnMaxLifetime",
		},
	)

	// PubSubEventsPublished counts events published to Redis PubSub.
	PubSubEventsPublished = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "pubsub_events_published_total",
			Help:      "Total number of events published to Redis PubSub",
		},
		[]string{"event_type", "channel"},
	)

	// PubSubPublishErrors counts PubSub publish failures.
	PubSubPublishErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "pubsub_publish_errors_total",
			Help:      "Total number of PubSub publish errors",
		},
		[]string{"event_type", "channel"},
	)
)
