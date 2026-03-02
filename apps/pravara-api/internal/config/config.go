// Package config provides configuration loading for the PravaraMES API.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application.
type Config struct {
	App        AppConfig        `mapstructure:"app"`
	Server     ServerConfig     `mapstructure:"server"`
	Database   DatabaseConfig   `mapstructure:"database"`
	Redis      RedisConfig      `mapstructure:"redis"`
	OIDC       OIDCConfig       `mapstructure:"oidc"`
	R2         R2Config         `mapstructure:"r2"`
	Centrifugo CentrifugoConfig `mapstructure:"centrifugo"`
	Dhanam     DhanamConfig     `mapstructure:"dhanam"`
	Cotiza     CotizaConfig     `mapstructure:"cotiza"`
}

// AppConfig holds application-level settings.
type AppConfig struct {
	Env      string `mapstructure:"env"`
	LogLevel string `mapstructure:"log_level"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port            int    `mapstructure:"port"`
	Host            string `mapstructure:"host"`
	ReadTimeout     int    `mapstructure:"read_timeout"`
	WriteTimeout    int    `mapstructure:"write_timeout"`
	ShutdownTimeout int    `mapstructure:"shutdown_timeout"`
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	URL             string `mapstructure:"url"`
	MaxConnections  int    `mapstructure:"max_connections"`
	MaxIdleConns    int    `mapstructure:"max_idle_connections"`
	ConnMaxLifetime int    `mapstructure:"conn_max_lifetime"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL string `mapstructure:"url"`
}

// OIDCConfig holds Janua SSO settings.
type OIDCConfig struct {
	Issuer   string `mapstructure:"issuer"`
	JWKSURL  string `mapstructure:"jwks_url"`
	Audience string `mapstructure:"audience"`
	ClientID string `mapstructure:"client_id"`
}

// R2Config holds Cloudflare R2 storage settings.
type R2Config struct {
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
	Bucket          string `mapstructure:"bucket"`
}

// CentrifugoConfig holds Centrifugo WebSocket gateway settings.
type CentrifugoConfig struct {
	TokenSecret   string `mapstructure:"token_secret"`
	TokenTTL      int    `mapstructure:"token_ttl"` // Token TTL in seconds
	APIKey        string `mapstructure:"api_key"`
	APIURL        string `mapstructure:"api_url"`
	PublicURL     string `mapstructure:"public_url"`
}

// DhanamConfig holds Dhanam billing integration settings.
type DhanamConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	APIURL       string `mapstructure:"api_url"`
	APIKey       string `mapstructure:"api_key"`
	SyncInterval int    `mapstructure:"sync_interval"` // Sync interval in minutes
	RetryCount   int    `mapstructure:"retry_count"`
	RetryDelay   int    `mapstructure:"retry_delay"` // Retry delay in seconds
}

// CotizaConfig holds Cotiza webhook integration settings.
type CotizaConfig struct {
	WebhookSecret string `mapstructure:"webhook_secret"`
	Enabled       bool   `mapstructure:"enabled"`
}

// Load reads configuration from environment variables and config files.
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read from environment variables
	v.SetEnvPrefix("PRAVARA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Map environment variables to config keys
	bindEnvVars(v)

	// Optionally read from config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	v.AddConfigPath("/etc/pravara")

	if err := v.ReadInConfig(); err != nil {
		// Config file not found is okay, we use env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// App defaults
	v.SetDefault("app.env", "development")
	v.SetDefault("app.log_level", "info")

	// Server defaults
	v.SetDefault("server.port", 4500)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.read_timeout", 30)
	v.SetDefault("server.write_timeout", 30)
	v.SetDefault("server.shutdown_timeout", 10)

	// Database defaults
	v.SetDefault("database.max_connections", 25)
	v.SetDefault("database.max_idle_connections", 5)
	v.SetDefault("database.conn_max_lifetime", 300)

	// OIDC defaults
	v.SetDefault("oidc.issuer", "https://auth.madfam.io")
	v.SetDefault("oidc.jwks_url", "https://auth.madfam.io/.well-known/jwks.json")
	v.SetDefault("oidc.audience", "pravara-api")

	// Centrifugo defaults
	v.SetDefault("centrifugo.token_ttl", 3600) // 1 hour
	v.SetDefault("centrifugo.api_url", "http://pravara-gateway:9000")
	v.SetDefault("centrifugo.public_url", "wss://gateway.pravara.madfam.io")

	// Dhanam defaults
	v.SetDefault("dhanam.enabled", false)
	v.SetDefault("dhanam.api_url", "https://api.dhanam.io/v1")
	v.SetDefault("dhanam.sync_interval", 60)  // 60 minutes
	v.SetDefault("dhanam.retry_count", 3)
	v.SetDefault("dhanam.retry_delay", 30) // 30 seconds

	// Cotiza defaults
	v.SetDefault("cotiza.enabled", false)
}

func bindEnvVars(v *viper.Viper) {
	// Direct environment variable mappings
	v.BindEnv("app.env", "APP_ENV")
	v.BindEnv("app.log_level", "LOG_LEVEL")

	v.BindEnv("server.port", "PRAVARA_API_PORT", "PORT")

	v.BindEnv("database.url", "DATABASE_URL")
	v.BindEnv("database.max_connections", "DATABASE_MAX_CONNECTIONS")
	v.BindEnv("database.max_idle_connections", "DATABASE_MAX_IDLE_CONNECTIONS")

	v.BindEnv("redis.url", "REDIS_URL")

	v.BindEnv("oidc.issuer", "OIDC_ISSUER")
	v.BindEnv("oidc.jwks_url", "OIDC_JWKS_URL")
	v.BindEnv("oidc.audience", "OIDC_AUDIENCE")
	v.BindEnv("oidc.client_id", "OIDC_CLIENT_ID")

	v.BindEnv("r2.endpoint", "R2_ENDPOINT")
	v.BindEnv("r2.access_key_id", "R2_ACCESS_KEY_ID")
	v.BindEnv("r2.secret_access_key", "R2_SECRET_ACCESS_KEY")
	v.BindEnv("r2.bucket", "R2_BUCKET")

	v.BindEnv("centrifugo.token_secret", "CENTRIFUGO_TOKEN_SECRET")
	v.BindEnv("centrifugo.token_ttl", "CENTRIFUGO_TOKEN_TTL")
	v.BindEnv("centrifugo.api_key", "CENTRIFUGO_API_KEY")
	v.BindEnv("centrifugo.api_url", "CENTRIFUGO_API_URL")
	v.BindEnv("centrifugo.public_url", "CENTRIFUGO_PUBLIC_URL")

	v.BindEnv("dhanam.enabled", "DHANAM_ENABLED")
	v.BindEnv("dhanam.api_url", "DHANAM_API_URL")
	v.BindEnv("dhanam.api_key", "DHANAM_API_KEY")
	v.BindEnv("dhanam.sync_interval", "DHANAM_SYNC_INTERVAL")
	v.BindEnv("dhanam.retry_count", "DHANAM_RETRY_COUNT")
	v.BindEnv("dhanam.retry_delay", "DHANAM_RETRY_DELAY")

	v.BindEnv("cotiza.enabled", "COTIZA_ENABLED")
	v.BindEnv("cotiza.webhook_secret", "COTIZA_WEBHOOK_SECRET")
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.App.Env == "development"
}

// IsProduction returns true if running in production mode.
func (c *Config) IsProduction() bool {
	return c.App.Env == "production"
}
