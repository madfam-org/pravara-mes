// Package config provides configuration management for the machine adapter service.
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/edge"
)

// Config holds the machine adapter service configuration.
type Config struct {
	Environment string
	LogLevel    string
	Server      ServerConfig
	Edge        edge.Config
}

// ServerConfig holds the local HTTP server configuration (health, metrics,
// read-only status). It binds to loopback by default.
type ServerConfig struct {
	Host         string
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Load reads configuration from a config file and the environment. When path
// is empty the file "config.yaml" is searched in ., ./config and
// /etc/machine-adapter/.
func Load(path string) (*Config, error) {
	v := viper.New()
	if path != "" {
		v.SetConfigFile(path)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/machine-adapter/")
	}
	setDefaults(v)
	v.SetEnvPrefix("MACHINE_ADAPTER")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok || path != "" {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("environment", "development")
	v.SetDefault("loglevel", "info")
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", 4503)
	v.SetDefault("server.readtimeout", "30s")
	v.SetDefault("server.writetimeout", "30s")
}

// Validate checks the service-level settings; the edge section is validated
// when the edge node is created.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}
	return nil
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Environment == "development" || c.Environment == "dev"
}

// IsProduction returns true if running in production mode.
func (c *Config) IsProduction() bool {
	return c.Environment == "production" || c.Environment == "prod"
}
