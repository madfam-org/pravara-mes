package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// DispatchConfig configures fabrication dispatch (MES-1 §5-§7): matchmaking,
// the dispatcher that renders, slices and enqueues start_job, and the passport
// updater.
type DispatchConfig struct {
	// Enabled turns on POST /v1/dispatches and the background runner. Off by
	// default: dispatch starts real jobs on printers. POST /v1/match (dry run)
	// works either way.
	Enabled bool `mapstructure:"enabled"`
	// RenderFormat is the yantra4d export format sent to slicing: "3mf" or
	// "stl" (the input formats fabrication-prep accepts).
	RenderFormat string `mapstructure:"render_format"`
	// ReservationTTLSeconds is how long a matched machine is held before the
	// command is enqueued. Each completed hop extends it.
	ReservationTTLSeconds int `mapstructure:"reservation_ttl_seconds"`
	// CommandHoldSeconds is how long the reservation is held after start_job is
	// enqueued, until the job completes or the command fails.
	CommandHoldSeconds int `mapstructure:"command_hold_seconds"`
	// MatchWaitSeconds bounds how long a dispatch waits for an eligible
	// machine (all busy, reserved or unloaded) before it fails.
	MatchWaitSeconds int `mapstructure:"match_wait_seconds"`
	// MaxAttempts bounds automatic retries of retryable failures per dispatch.
	MaxAttempts int `mapstructure:"max_attempts"`
	// PollIntervalSeconds is the runner tick and the slice-job poll interval.
	PollIntervalSeconds int `mapstructure:"poll_interval_seconds"`
	// PassportMaxAttempts bounds delivery attempts of one passport outbox row.
	PassportMaxAttempts int `mapstructure:"passport_max_attempts"`
	// RequireBoundingBox refuses a match when no part bounding box is known.
	// Default false: the gap is reported in the explanation and slicing is the
	// backstop (an oversized part fails there).
	RequireBoundingBox bool `mapstructure:"require_bounding_box"`

	YantraAPIURL          string `mapstructure:"yantra4d_api_url"`
	FabricationPrepAPIURL string `mapstructure:"fabrication_prep_api_url"`
	AssetShellsAPIURL     string `mapstructure:"asset_shells_api_url"`
}

// MachineClientsConfig holds the Janua machine clients (client_credentials).
// Secrets arrive as environment variables from the pravara-service-clients
// Secret (infra/k8s/base/external-secrets/pravara-service-clients.yaml).
type MachineClientsConfig struct {
	TokenURL string `mapstructure:"token_url"`

	Yantra4DClientID     string `mapstructure:"yantra4d_client_id"`
	Yantra4DClientSecret string `mapstructure:"yantra4d_client_secret"`

	FabricationPrepClientID     string `mapstructure:"fabrication_prep_client_id"`
	FabricationPrepClientSecret string `mapstructure:"fabrication_prep_client_secret"`

	// AssetShellsPublisherTenants maps a pravara tenant slug to the environment
	// prefix of its asset-shells publisher client, e.g.
	// "madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM". The client id and
	// secret are read from <PREFIX>_CLIENT_ID and <PREFIX>_CLIENT_SECRET.
	// The asset-shells client is bound to one Janua organisation, so each
	// producing tenant needs its own entry.
	AssetShellsPublisherTenants string `mapstructure:"asset_shells_publisher_tenants"`
}

// TenantClientPrefixes parses AssetShellsPublisherTenants.
func (c MachineClientsConfig) TenantClientPrefixes() (map[string]string, error) {
	out := map[string]string{}
	for _, entry := range strings.FieldsFunc(c.AssetShellsPublisherTenants, func(r rune) bool { return r == ',' || r == ';' }) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		slug, prefix, ok := strings.Cut(entry, "=")
		slug, prefix = strings.TrimSpace(slug), strings.TrimSpace(prefix)
		if !ok || slug == "" || prefix == "" {
			return nil, fmt.Errorf("asset_shells_publisher_tenants: entry %q is not slug=PREFIX", entry)
		}
		if strings.ToUpper(prefix) != prefix || strings.ContainsAny(prefix, " =") {
			return nil, fmt.Errorf("asset_shells_publisher_tenants: prefix %q must be an upper-case environment prefix", prefix)
		}
		if _, dup := out[slug]; dup {
			return nil, fmt.Errorf("asset_shells_publisher_tenants: tenant %q listed twice", slug)
		}
		out[slug] = prefix
	}
	return out, nil
}

func setDispatchDefaults(v *viper.Viper) {
	v.SetDefault("dispatch.enabled", false)
	v.SetDefault("dispatch.render_format", "3mf")
	v.SetDefault("dispatch.reservation_ttl_seconds", 900)
	v.SetDefault("dispatch.command_hold_seconds", 6*3600)
	v.SetDefault("dispatch.max_attempts", 5)
	v.SetDefault("dispatch.match_wait_seconds", 86400)
	v.SetDefault("dispatch.poll_interval_seconds", 10)
	v.SetDefault("dispatch.passport_max_attempts", 20)
	v.SetDefault("dispatch.require_bounding_box", false)
	v.SetDefault("dispatch.yantra4d_api_url", "https://yantra4d.madfam.io")
	v.SetDefault("dispatch.fabrication_prep_api_url", "https://fabrication-prep-api.madfam.io")
	v.SetDefault("dispatch.asset_shells_api_url", "https://asset-shells-api.madfam.io")

	v.SetDefault("machine_clients.token_url", "https://auth.madfam.io/api/v1/oauth/token")
	v.SetDefault("machine_clients.asset_shells_publisher_tenants", "madfam=ASSET_SHELLS_PUBLISHER_MADFAM_ECOSYSTEM")
}

func bindDispatchEnv(v *viper.Viper) {
	_ = v.BindEnv("dispatch.enabled", "DISPATCH_ENABLED")
	_ = v.BindEnv("dispatch.render_format", "DISPATCH_RENDER_FORMAT")
	_ = v.BindEnv("dispatch.reservation_ttl_seconds", "DISPATCH_RESERVATION_TTL_SECONDS")
	_ = v.BindEnv("dispatch.command_hold_seconds", "DISPATCH_COMMAND_HOLD_SECONDS")
	_ = v.BindEnv("dispatch.max_attempts", "DISPATCH_MAX_ATTEMPTS")
	_ = v.BindEnv("dispatch.match_wait_seconds", "DISPATCH_MATCH_WAIT_SECONDS")
	_ = v.BindEnv("dispatch.poll_interval_seconds", "DISPATCH_POLL_INTERVAL_SECONDS")
	_ = v.BindEnv("dispatch.passport_max_attempts", "DISPATCH_PASSPORT_MAX_ATTEMPTS")
	_ = v.BindEnv("dispatch.require_bounding_box", "DISPATCH_REQUIRE_BOUNDING_BOX")
	_ = v.BindEnv("dispatch.yantra4d_api_url", "YANTRA4D_API_URL")
	_ = v.BindEnv("dispatch.fabrication_prep_api_url", "FABRICATION_PREP_API_URL")
	_ = v.BindEnv("dispatch.asset_shells_api_url", "ASSET_SHELLS_API_URL")

	_ = v.BindEnv("machine_clients.token_url", "JANUA_TOKEN_URL")
	_ = v.BindEnv("machine_clients.yantra4d_client_id", "YANTRA4D_STEP_READER_CLIENT_ID")
	_ = v.BindEnv("machine_clients.yantra4d_client_secret", "YANTRA4D_STEP_READER_CLIENT_SECRET")
	_ = v.BindEnv("machine_clients.fabrication_prep_client_id", "FABRICATION_PREP_CLIENT_ID")
	_ = v.BindEnv("machine_clients.fabrication_prep_client_secret", "FABRICATION_PREP_CLIENT_SECRET")
	_ = v.BindEnv("machine_clients.asset_shells_publisher_tenants", "ASSET_SHELLS_PUBLISHER_TENANTS")
}
