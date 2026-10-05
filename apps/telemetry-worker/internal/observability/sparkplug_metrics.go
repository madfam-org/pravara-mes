package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SparkplugHostEvents counts primary host events by name: nbirth, ndeath,
// dbirth, ddata, ddeath, seq_gap, rebirth_requested, rebirth_suppressed,
// quarantined, unknown_edge_node, stale_ndeath, command_published,
// command_deferred, command_resent, command_topic_ignored, state_online,
// state_reasserted, broker_session_ended, ...
var SparkplugHostEvents = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: namespace,
		Subsystem: "sparkplug",
		Name:      "host_events_total",
		Help:      "Sparkplug primary host events, by event",
	},
	[]string{"event"},
)
