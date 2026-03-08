// Package config provides configuration management for the machine adapter service.
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config holds the machine adapter service configuration.
type Config struct {
	Environment string
	LogLevel    string
	Server      ServerConfig
	MQTT        MQTTConfig
	Database    DatabaseConfig
	Discovery   DiscoveryConfig
	Adapters    AdapterConfig
}

// ServerConfig holds HTTP server configuration.
type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// MQTTConfig holds MQTT broker configuration.
type MQTTConfig struct {
	BrokerURL       string
	Username        string
	Password        string
	ClientID        string
	CommandTopic    string
	TelemetryTopic  string
	StatusTopic     string
	QoS             byte
	CleanSession    bool
	AutoReconnect   bool
	ReconnectDelay  time.Duration
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	URL             string
	MaxConnections  int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DiscoveryConfig holds machine discovery configuration.
type DiscoveryConfig struct {
	EnableMDNS       bool
	EnableUSB        bool
	EnableNetScan    bool
	ScanInterval     time.Duration
	NetworkRanges    []string
	KnownMachines    []string
}

// AdapterConfig holds protocol adapter configuration.
type AdapterConfig struct {
	SerialPorts      []string
	BaudRates        []int
	TCPPorts         []int
	CommandTimeout   time.Duration
	RetryAttempts    int
	RetryDelay       time.Duration
	BufferSize       int
}

// Load reads configuration from environment and config files.
func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.AddConfigPath("/etc/machine-adapter/")

	// Set defaults
	setDefaults()

	// Bind environment variables
	viper.SetEnvPrefix("MACHINE_ADAPTER")
	viper.AutomaticEnv()

	// Explicit env var bindings for K8s deployment
	viper.BindEnv("mqtt.brokerurl", "MQTT_BROKER_URL")
	viper.BindEnv("database.url", "DATABASE_URL")

	// Read config file if it exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default configuration values.
func setDefaults() {
	// Environment
	viper.SetDefault("environment", "development")
	viper.SetDefault("loglevel", "info")

	// Server
	viper.SetDefault("server.port", 4503)
	viper.SetDefault("server.readtimeout", "30s")
	viper.SetDefault("server.writetimeout", "30s")

	// MQTT
	viper.SetDefault("mqtt.brokerurl", "tcp://emqx-pravara:1883")
	viper.SetDefault("mqtt.clientid", "machine-adapter")
	viper.SetDefault("mqtt.commandtopic", "pravara/+/machines/+/command")
	viper.SetDefault("mqtt.telemetrytopic", "pravara/+/machines/+/telemetry")
	viper.SetDefault("mqtt.statustopic", "pravara/+/machines/+/status")
	viper.SetDefault("mqtt.qos", 1)
	viper.SetDefault("mqtt.cleansession", false)
	viper.SetDefault("mqtt.autoreconnect", true)
	viper.SetDefault("mqtt.reconnectdelay", "5s")

	// Database
	viper.SetDefault("database.maxconnections", 10)
	viper.SetDefault("database.maxidleconns", 5)
	viper.SetDefault("database.connmaxlifetime", "30m")

	// Discovery
	viper.SetDefault("discovery.enablemdns", true)
	viper.SetDefault("discovery.enableusb", true)
	viper.SetDefault("discovery.enablenetscan", false)
	viper.SetDefault("discovery.scaninterval", "30s")

	// Adapters
	viper.SetDefault("adapters.commandtimeout", "30s")
	viper.SetDefault("adapters.retryattempts", 3)
	viper.SetDefault("adapters.retrydelay", "1s")
	viper.SetDefault("adapters.buffersize", 4096)
	viper.SetDefault("adapters.baudrates", []int{9600, 19200, 38400, 57600, 115200, 250000})
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.MQTT.BrokerURL == "" {
		return fmt.Errorf("MQTT broker URL is required")
	}

	if c.Database.URL == "" {
		return fmt.Errorf("database URL is required")
	}

	if c.Adapters.CommandTimeout < 1*time.Second {
		return fmt.Errorf("command timeout must be at least 1 second")
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