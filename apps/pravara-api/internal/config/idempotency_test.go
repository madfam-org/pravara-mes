package config

import (
	"testing"
	"time"
)

func TestIdempotencyRetentionDefaultsTo35Days(t *testing.T) {
	t.Setenv("PRAVARA_IDEMPOTENCY_RETENTION_DAYS", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Idempotency.Retention(); got != 35*24*time.Hour {
		t.Fatalf("default retention: got %s, want 35 days", got)
	}
}

func TestIdempotencyRetentionFromEnv(t *testing.T) {
	t.Setenv("PRAVARA_IDEMPOTENCY_RETENTION_DAYS", "60")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Idempotency.Retention(); got != 60*24*time.Hour {
		t.Fatalf("retention: got %s, want 60 days", got)
	}
}

func TestIdempotencyRetentionNeverZero(t *testing.T) {
	if got := (IdempotencyConfig{RetentionDays: 0}).Retention(); got != 35*24*time.Hour {
		t.Fatalf("zero days: got %s, want the default", got)
	}
	if got := (IdempotencyConfig{RetentionDays: -3}).Retention(); got != 35*24*time.Hour {
		t.Fatalf("negative days: got %s, want the default", got)
	}
}
