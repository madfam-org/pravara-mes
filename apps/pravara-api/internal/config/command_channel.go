package config

import "github.com/spf13/viper"

// CommandsConfig configures the durable machine command stream consumed by
// the telemetry worker.
type CommandsConfig struct {
	// StreamKey is the Redis stream commands are appended to. It must match
	// the worker's PRAVARA_COMMAND_STREAM_KEY.
	StreamKey string `mapstructure:"stream_key"`
	// StreamMaxLen caps the stream length (approximate trimming). Commands
	// trimmed before delivery are expired by the worker's deadline sweep.
	StreamMaxLen int64 `mapstructure:"stream_max_len"`
}

// LivenessConfig configures the offline sweeper that marks machines offline
// when their heartbeat goes stale.
type LivenessConfig struct {
	Enabled                 bool `mapstructure:"enabled"`
	HeartbeatTimeoutSeconds int  `mapstructure:"heartbeat_timeout_seconds"`
	SweepIntervalSeconds    int  `mapstructure:"sweep_interval_seconds"`
}

func setCommandChannelDefaults(v *viper.Viper) {
	v.SetDefault("commands.stream_key", "pravara:commands")
	v.SetDefault("commands.stream_max_len", 100000)
	v.SetDefault("liveness.enabled", true)
	v.SetDefault("liveness.heartbeat_timeout_seconds", 300)
	v.SetDefault("liveness.sweep_interval_seconds", 60)
}

func bindCommandChannelEnv(v *viper.Viper) {
	_ = v.BindEnv("commands.stream_key", "COMMAND_STREAM_KEY")
	_ = v.BindEnv("commands.stream_max_len", "COMMAND_STREAM_MAX_LEN")
	_ = v.BindEnv("liveness.enabled", "LIVENESS_SWEEP_ENABLED")
	_ = v.BindEnv("liveness.heartbeat_timeout_seconds", "LIVENESS_HEARTBEAT_TIMEOUT_SECONDS")
	_ = v.BindEnv("liveness.sweep_interval_seconds", "LIVENESS_SWEEP_INTERVAL_SECONDS")
}
