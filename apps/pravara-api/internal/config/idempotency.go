package config

import (
	"time"

	"github.com/spf13/viper"
)

// IdempotencyConfig configures the Idempotency-Key header on the writes a
// shop-floor terminal delivers (RFC 0064 §5, ruling PV-4).
type IdempotencyConfig struct {
	// RetentionDays is how long a used key is remembered (env
	// PRAVARA_IDEMPOTENCY_RETENTION_DAYS, default 35). A key older than this
	// counts as unused. 35 days is a placeholder spanning a month end, to be
	// replaced when the terminal's outbox limits are measured: keep it at
	// least as long as a record can wait on a terminal.
	RetentionDays int `mapstructure:"retention_days"`
}

// DefaultIdempotencyRetentionDays is the retention when none is configured.
const DefaultIdempotencyRetentionDays = 35

// Retention is RetentionDays as a duration; zero or less means the default.
func (c IdempotencyConfig) Retention() time.Duration {
	days := c.RetentionDays
	if days <= 0 {
		days = DefaultIdempotencyRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

func setIdempotencyDefaults(v *viper.Viper) {
	v.SetDefault("idempotency.retention_days", DefaultIdempotencyRetentionDays)
}

func bindIdempotencyEnv(v *viper.Viper) {
	_ = v.BindEnv("idempotency.retention_days", "PRAVARA_IDEMPOTENCY_RETENTION_DAYS")
}
