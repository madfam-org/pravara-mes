package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const commandSubsystem = "command"

var (
	// CommandDispatchOutcomes counts stream entries by dispatch outcome:
	// published, deferred (Sparkplug device not born yet), retry, failed,
	// duplicate, rejected.
	CommandDispatchOutcomes = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: commandSubsystem,
			Name:      "dispatch_total",
			Help:      "Command stream entries processed, by outcome",
		},
		[]string{"outcome"},
	)

	// CommandStreamReclaimed counts stream entries reclaimed from idle
	// consumers (retries and redelivery after a restart).
	CommandStreamReclaimed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: commandSubsystem,
			Name:      "stream_reclaimed_total",
			Help:      "Command stream entries reclaimed for redelivery",
		},
	)

	// CommandAcks counts machine acks by disposition: applied,
	// unknown_machine, unknown_command, machine_mismatch, already_final,
	// invalid.
	CommandAcks = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: commandSubsystem,
			Name:      "acks_total",
			Help:      "Machine command acks, by disposition",
		},
		[]string{"disposition"},
	)

	// CommandTimeouts counts commands expired by the deadline sweep, by the
	// stage they were stuck in (pending = never sent, sent = never acked).
	CommandTimeouts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: commandSubsystem,
			Name:      "timeouts_total",
			Help:      "Commands moved to timeout, by previous status",
		},
		[]string{"stage"},
	)

	// CommandCompletionHookErrors counts job-completion hook failures.
	CommandCompletionHookErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: commandSubsystem,
			Name:      "completion_hook_errors_total",
			Help:      "Job completion hook errors",
		},
	)

	// MQTTControlTopicsSkipped counts command-channel messages (cmd, ack)
	// seen by the telemetry subscription and not ingested as telemetry.
	MQTTControlTopicsSkipped = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "mqtt_control_topics_skipped_total",
			Help:      "Command-channel MQTT messages excluded from telemetry ingest",
		},
		[]string{"channel"},
	)
)
