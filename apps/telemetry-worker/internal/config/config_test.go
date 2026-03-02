package config

import (
	"os"
	"testing"
)

func TestConfig_Load_Defaults(t *testing.T) {
	// Clear environment variables
	clearEnvVars()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Check defaults
	if cfg.Environment != "development" {
		t.Errorf("Environment: got %q, want %q", cfg.Environment, "development")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.MQTT.Broker != "localhost" {
		t.Errorf("MQTT.Broker: got %q, want %q", cfg.MQTT.Broker, "localhost")
	}
	if cfg.MQTT.Port != 1883 {
		t.Errorf("MQTT.Port: got %d, want %d", cfg.MQTT.Port, 1883)
	}
	if cfg.Worker.BatchSize != 100 {
		t.Errorf("Worker.BatchSize: got %d, want %d", cfg.Worker.BatchSize, 100)
	}
	if cfg.Worker.NumWorkers != 4 {
		t.Errorf("Worker.NumWorkers: got %d, want %d", cfg.Worker.NumWorkers, 4)
	}
}

func TestConfig_Load_FromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("PRAVARA_ENVIRONMENT", "production")
	os.Setenv("PRAVARA_LOG_LEVEL", "debug")
	os.Setenv("PRAVARA_MQTT_BROKER", "mqtt.example.com")
	os.Setenv("PRAVARA_MQTT_PORT", "8883")
	os.Setenv("PRAVARA_WORKER_BATCH_SIZE", "200")
	defer clearEnvVars()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Environment != "production" {
		t.Errorf("Environment: got %q, want %q", cfg.Environment, "production")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: got %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.MQTT.Broker != "mqtt.example.com" {
		t.Errorf("MQTT.Broker: got %q, want %q", cfg.MQTT.Broker, "mqtt.example.com")
	}
	if cfg.MQTT.Port != 8883 {
		t.Errorf("MQTT.Port: got %d, want %d", cfg.MQTT.Port, 8883)
	}
	if cfg.Worker.BatchSize != 200 {
		t.Errorf("Worker.BatchSize: got %d, want %d", cfg.Worker.BatchSize, 200)
	}
}

func TestMQTTConfig_BrokerURL(t *testing.T) {
	tests := []struct {
		name     string
		config   MQTTConfig
		expected string
	}{
		{
			name: "tcp without TLS",
			config: MQTTConfig{
				Broker: "localhost",
				Port:   1883,
				UseTLS: false,
			},
			expected: "tcp://localhost:1883",
		},
		{
			name: "ssl with TLS",
			config: MQTTConfig{
				Broker: "mqtt.example.com",
				Port:   8883,
				UseTLS: true,
			},
			expected: "ssl://mqtt.example.com:8883",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.config.BrokerURL()
			if url != tt.expected {
				t.Errorf("BrokerURL: got %q, want %q", url, tt.expected)
			}
		})
	}
}

func TestDatabaseConfig_DSN(t *testing.T) {
	config := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "pravara",
		Password: "secret",
		Name:     "pravara_mes",
		SSLMode:  "disable",
	}

	expected := "host=localhost port=5432 user=pravara password=secret dbname=pravara_mes sslmode=disable"
	dsn := config.DSN()

	if dsn != expected {
		t.Errorf("DSN: got %q, want %q", dsn, expected)
	}
}

func TestWorkerConfig_Defaults(t *testing.T) {
	clearEnvVars()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify worker defaults
	if cfg.Worker.BatchSize != 100 {
		t.Errorf("BatchSize: got %d, want %d", cfg.Worker.BatchSize, 100)
	}
	if cfg.Worker.BatchTimeout != 1000 {
		t.Errorf("BatchTimeout: got %d, want %d", cfg.Worker.BatchTimeout, 1000)
	}
	if cfg.Worker.NumWorkers != 4 {
		t.Errorf("NumWorkers: got %d, want %d", cfg.Worker.NumWorkers, 4)
	}
	if cfg.Worker.RetryAttempts != 3 {
		t.Errorf("RetryAttempts: got %d, want %d", cfg.Worker.RetryAttempts, 3)
	}
	if cfg.Worker.RetryDelay != 100 {
		t.Errorf("RetryDelay: got %d, want %d", cfg.Worker.RetryDelay, 100)
	}
}

func clearEnvVars() {
	envVars := []string{
		"PRAVARA_ENVIRONMENT",
		"PRAVARA_LOG_LEVEL",
		"PRAVARA_MQTT_BROKER",
		"PRAVARA_MQTT_PORT",
		"PRAVARA_MQTT_USERNAME",
		"PRAVARA_MQTT_PASSWORD",
		"PRAVARA_DATABASE_HOST",
		"PRAVARA_DATABASE_PORT",
		"PRAVARA_DATABASE_USER",
		"PRAVARA_DATABASE_PASSWORD",
		"PRAVARA_DATABASE_NAME",
		"PRAVARA_WORKER_BATCH_SIZE",
		"PRAVARA_WORKER_NUM_WORKERS",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}
}
