// Package config handles configuration loading for the telemetry worker.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the telemetry worker.
type Config struct {
	Environment string         `mapstructure:"environment"`
	LogLevel    string         `mapstructure:"log_level"`
	MQTT        MQTTConfig     `mapstructure:"mqtt"`
	Database    DatabaseConfig `mapstructure:"database"`
	Redis       RedisConfig    `mapstructure:"redis"`
	Worker      WorkerConfig   `mapstructure:"worker"`
	Command     CommandConfig  `mapstructure:"command"`
}

// CommandConfig holds command dispatcher configuration.
type CommandConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// MQTTConfig holds MQTT broker configuration.
type MQTTConfig struct {
	Broker     string `mapstructure:"broker"`
	Port       int    `mapstructure:"port"`
	ClientID   string `mapstructure:"client_id"`
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"password"`
	UseTLS     bool   `mapstructure:"use_tls"`
	TopicRoot  string `mapstructure:"topic_root"`
	QoS        int    `mapstructure:"qos"`
	CleanStart bool   `mapstructure:"clean_start"`
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	SSLMode  string `mapstructure:"ssl_mode"`
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// WorkerConfig holds worker-specific configuration.
type WorkerConfig struct {
	BatchSize     int `mapstructure:"batch_size"`
	BatchTimeout  int `mapstructure:"batch_timeout_ms"`
	NumWorkers    int `mapstructure:"num_workers"`
	RetryAttempts int `mapstructure:"retry_attempts"`
	RetryDelay    int `mapstructure:"retry_delay_ms"`
	DLQMaxItems   int `mapstructure:"dlq_max_items"`
}

// DSN returns the database connection string.
func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// BrokerURL returns the full MQTT broker URL.
func (c *MQTTConfig) BrokerURL() string {
	protocol := "tcp"
	if c.UseTLS {
		protocol = "ssl"
	}
	return fmt.Sprintf("%s://%s:%d", protocol, c.Broker, c.Port)
}

// URL returns the Redis connection URL.
func (c *RedisConfig) URL() string {
	if c.Password != "" {
		return fmt.Sprintf("redis://:%s@%s:%d/%d", c.Password, c.Host, c.Port, c.DB)
	}
	return fmt.Sprintf("redis://%s:%d/%d", c.Host, c.Port, c.DB)
}

// Load reads configuration from environment variables and config files.
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	v.SetDefault("environment", "development")
	v.SetDefault("log_level", "info")

	// MQTT defaults
	v.SetDefault("mqtt.broker", "localhost")
	v.SetDefault("mqtt.port", 1883)
	v.SetDefault("mqtt.client_id", "pravara-telemetry-worker")
	v.SetDefault("mqtt.topic_root", "+/+/+/+/+/#")
	v.SetDefault("mqtt.qos", 1)
	v.SetDefault("mqtt.clean_start", false)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.user", "pravara")
	v.SetDefault("database.name", "pravara_mes")
	v.SetDefault("database.ssl_mode", "disable")

	// Redis defaults
	v.SetDefault("redis.host", "localhost")
	v.SetDefault("redis.port", 6379)
	v.SetDefault("redis.db", 0)

	// Worker defaults
	v.SetDefault("worker.batch_size", 100)
	v.SetDefault("worker.batch_timeout_ms", 1000)
	v.SetDefault("worker.num_workers", 4)
	v.SetDefault("worker.retry_attempts", 3)
	v.SetDefault("worker.retry_delay_ms", 100)
	v.SetDefault("worker.dlq_max_items", 1000)

	// Command dispatcher defaults
	v.SetDefault("command.enabled", true)

	// Read from environment variables
	v.SetEnvPrefix("PRAVARA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Read from config file if present
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("/etc/pravara/")
	v.AddConfigPath(".")
	_ = v.ReadInConfig()

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}
