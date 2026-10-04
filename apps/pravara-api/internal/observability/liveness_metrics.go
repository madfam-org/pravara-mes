package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MachinesMarkedOffline counts machines the liveness sweep marked
	// offline after their heartbeat went stale.
	MachinesMarkedOffline = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "liveness",
			Name:      "machines_marked_offline_total",
			Help:      "Machines marked offline because their heartbeat went stale",
		},
	)

	// LivenessSweepErrors counts liveness sweep failures per tenant pass.
	LivenessSweepErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "liveness",
			Name:      "sweep_errors_total",
			Help:      "Liveness sweep errors",
		},
	)
)
