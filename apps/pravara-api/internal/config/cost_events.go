package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// CostEventsConfig configures the emitter that sends job consumption to
// Dhanam's cost ledger as quantity-only cost.event.v1 `consumption` events
// (RFC 0050, owner ruling C12: Pravara never values consumption; Dhanam
// does). See docs/operations/cost-events.md.
type CostEventsConfig struct {
	// Enabled starts the emitter (env PRAVARA_COST_EVENTS_ENABLED). Default
	// false: nothing is built, stored or sent, and nothing below is required.
	// The owner turns it on; agents never do.
	Enabled bool `mapstructure:"enabled"`
	// DryRun builds and stores the events (cost_event_outbox) but never
	// sends them (env PRAVARA_COST_EVENTS_DRY_RUN). No credentials needed.
	DryRun bool `mapstructure:"dry_run"`
	// TenantID is the ONE Pravara tenant whose consumption is sent (env
	// PRAVARA_COST_EVENTS_TENANT_ID). Dhanam's writer grant binds the client
	// to one space and one source, so one tenant maps to that space.
	TenantID string `mapstructure:"tenant_id"`
	// StartDate (YYYY-MM-DD) is the first completion date, in
	// America/Mexico_City, whose consumption is sent (env
	// PRAVARA_COST_EVENTS_START_DATE). Required when enabled.
	StartDate string `mapstructure:"start_date"`
	// DhanamURL is the Dhanam API origin (env PRAVARA_COST_EVENTS_DHANAM_URL);
	// events go to <DhanamURL>/v1/cost-events. A trailing /v1 is accepted.
	DhanamURL string `mapstructure:"dhanam_url"`
	// TokenURL is Janua's token endpoint (env PRAVARA_COST_EVENTS_TOKEN_URL);
	// empty means the machine clients' JANUA_TOKEN_URL.
	TokenURL string `mapstructure:"token_url"`
	// ClientID / ClientSecret are the Janua confidential client Dhanam's
	// CostEventWriterGrant binds to source `pravara` (env
	// PRAVARA_COST_EVENTS_CLIENT_ID / PRAVARA_COST_EVENTS_CLIENT_SECRET).
	// Environment only; never logged.
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	// CostClass is the cost class sent with every event (env
	// PRAVARA_COST_EVENTS_COST_CLASS, default cogs).
	CostClass string `mapstructure:"cost_class"`
	// IntervalSeconds is the sweep period (env
	// PRAVARA_COST_EVENTS_INTERVAL_SECONDS, default 300).
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// LookbackDays bounds which lines are rebuilt each sweep: those with a
	// fact recorded in the last N days (env PRAVARA_COST_EVENTS_LOOKBACK_DAYS,
	// default 62).
	LookbackDays int `mapstructure:"lookback_days"`
	// BatchSize is the events per POST, 1-500 (env
	// PRAVARA_COST_EVENTS_BATCH_SIZE, default 100).
	BatchSize int `mapstructure:"batch_size"`
}

// CostEventClasses are the cost classes of cost.event.v1.
var CostEventClasses = []string{"cogs", "opex", "capex", "financing"}

// CostEventsMaxBatch is Dhanam's batch limit.
const CostEventsMaxBatch = 500

func setCostEventsDefaults(v *viper.Viper) {
	v.SetDefault("cost_events.enabled", false)
	v.SetDefault("cost_events.dry_run", false)
	v.SetDefault("cost_events.cost_class", "cogs")
	v.SetDefault("cost_events.interval_seconds", 300)
	v.SetDefault("cost_events.lookback_days", 62)
	v.SetDefault("cost_events.batch_size", 100)
}

func bindCostEventsEnv(v *viper.Viper) {
	_ = v.BindEnv("cost_events.enabled", "PRAVARA_COST_EVENTS_ENABLED")
	_ = v.BindEnv("cost_events.dry_run", "PRAVARA_COST_EVENTS_DRY_RUN")
	_ = v.BindEnv("cost_events.tenant_id", "PRAVARA_COST_EVENTS_TENANT_ID")
	_ = v.BindEnv("cost_events.start_date", "PRAVARA_COST_EVENTS_START_DATE")
	_ = v.BindEnv("cost_events.dhanam_url", "PRAVARA_COST_EVENTS_DHANAM_URL")
	_ = v.BindEnv("cost_events.token_url", "PRAVARA_COST_EVENTS_TOKEN_URL")
	_ = v.BindEnv("cost_events.client_id", "PRAVARA_COST_EVENTS_CLIENT_ID")
	_ = v.BindEnv("cost_events.client_secret", "PRAVARA_COST_EVENTS_CLIENT_SECRET")
	_ = v.BindEnv("cost_events.cost_class", "PRAVARA_COST_EVENTS_COST_CLASS")
	_ = v.BindEnv("cost_events.interval_seconds", "PRAVARA_COST_EVENTS_INTERVAL_SECONDS")
	_ = v.BindEnv("cost_events.lookback_days", "PRAVARA_COST_EVENTS_LOOKBACK_DAYS")
	_ = v.BindEnv("cost_events.batch_size", "PRAVARA_COST_EVENTS_BATCH_SIZE")
}

// Tenant parses TenantID.
func (c CostEventsConfig) Tenant() (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(c.TenantID))
}

// Start parses StartDate as midnight in loc.
func (c CostEventsConfig) Start(loc *time.Location) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(c.StartDate), loc)
}

// Endpoint is the cost-events URL derived from DhanamURL.
func (c CostEventsConfig) Endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(c.DhanamURL), "/")
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1/cost-events"
}

// Problems lists what keeps an enabled emitter from running, without
// values (secrets never appear). Empty when disabled or complete. Sending
// needs the Dhanam URL, the token URL and the client pair; a dry run does
// not.
func (c CostEventsConfig) Problems(machineTokenURL string) []string {
	if !c.Enabled {
		return nil
	}
	var p []string
	if id, err := c.Tenant(); err != nil || id == uuid.Nil {
		p = append(p, "PRAVARA_COST_EVENTS_TENANT_ID is not a tenant UUID")
	}
	if strings.TrimSpace(c.StartDate) == "" {
		p = append(p, "PRAVARA_COST_EVENTS_START_DATE is not set")
	} else if _, err := c.Start(time.UTC); err != nil {
		p = append(p, "PRAVARA_COST_EVENTS_START_DATE is not a YYYY-MM-DD date")
	}
	valid := false
	for _, cl := range CostEventClasses {
		valid = valid || c.CostClass == cl
	}
	if !valid {
		p = append(p, fmt.Sprintf("PRAVARA_COST_EVENTS_COST_CLASS must be one of %s", strings.Join(CostEventClasses, ", ")))
	}
	if c.IntervalSeconds < 10 {
		p = append(p, "PRAVARA_COST_EVENTS_INTERVAL_SECONDS must be at least 10")
	}
	if c.LookbackDays < 1 {
		p = append(p, "PRAVARA_COST_EVENTS_LOOKBACK_DAYS must be at least 1")
	}
	if c.BatchSize < 1 || c.BatchSize > CostEventsMaxBatch {
		p = append(p, fmt.Sprintf("PRAVARA_COST_EVENTS_BATCH_SIZE must be 1-%d", CostEventsMaxBatch))
	}
	if c.DryRun {
		return p
	}
	if !strings.HasPrefix(strings.TrimSpace(c.DhanamURL), "https://") && !strings.HasPrefix(strings.TrimSpace(c.DhanamURL), "http://") {
		p = append(p, "PRAVARA_COST_EVENTS_DHANAM_URL is not an http(s) URL")
	}
	if strings.TrimSpace(c.EffectiveTokenURL(machineTokenURL)) == "" {
		p = append(p, "PRAVARA_COST_EVENTS_TOKEN_URL (or JANUA_TOKEN_URL) is not set")
	}
	if strings.TrimSpace(c.ClientID) == "" || c.ClientSecret == "" {
		p = append(p, "PRAVARA_COST_EVENTS_CLIENT_ID / PRAVARA_COST_EVENTS_CLIENT_SECRET are not set")
	}
	return p
}

// EffectiveTokenURL is TokenURL, else the machine clients' token URL.
func (c CostEventsConfig) EffectiveTokenURL(machineTokenURL string) string {
	if u := strings.TrimSpace(c.TokenURL); u != "" {
		return u
	}
	return strings.TrimSpace(machineTokenURL)
}
