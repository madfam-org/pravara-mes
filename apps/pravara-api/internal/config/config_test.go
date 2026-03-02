package config

import (
	"os"
	"testing"
)

func TestConfig_Load_Defaults(t *testing.T) {
	// Clear any environment variables that might interfere
	envVars := []string{
		"PRAVARA_ENVIRONMENT",
		"PRAVARA_LOG_LEVEL",
		"PRAVARA_SERVER_PORT",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}

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
	if cfg.Server.Port != 4500 {
		t.Errorf("Server.Port: got %d, want %d", cfg.Server.Port, 4500)
	}
}

func TestConfig_Load_FromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("PRAVARA_ENVIRONMENT", "production")
	os.Setenv("PRAVARA_LOG_LEVEL", "debug")
	os.Setenv("PRAVARA_SERVER_PORT", "8080")
	defer func() {
		os.Unsetenv("PRAVARA_ENVIRONMENT")
		os.Unsetenv("PRAVARA_LOG_LEVEL")
		os.Unsetenv("PRAVARA_SERVER_PORT")
	}()

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
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port: got %d, want %d", cfg.Server.Port, 8080)
	}
}

func TestDatabaseConfig_DSN(t *testing.T) {
	tests := []struct {
		name     string
		config   DatabaseConfig
		expected string
	}{
		{
			name: "standard config",
			config: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				User:     "pravara",
				Password: "secret",
				Name:     "pravara_mes",
				SSLMode:  "disable",
			},
			expected: "host=localhost port=5432 user=pravara password=secret dbname=pravara_mes sslmode=disable",
		},
		{
			name: "production config",
			config: DatabaseConfig{
				Host:     "db.example.com",
				Port:     5432,
				User:     "app_user",
				Password: "prod_password",
				Name:     "production_db",
				SSLMode:  "require",
			},
			expected: "host=db.example.com port=5432 user=app_user password=prod_password dbname=production_db sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn := tt.config.DSN()
			if dsn != tt.expected {
				t.Errorf("DSN: got %q, want %q", dsn, tt.expected)
			}
		})
	}
}

func TestAuthConfig_JWKSEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		config   AuthConfig
		expected string
	}{
		{
			name: "standard issuer",
			config: AuthConfig{
				Issuer: "https://auth.example.com",
			},
			expected: "https://auth.example.com/.well-known/jwks.json",
		},
		{
			name: "issuer with trailing slash",
			config: AuthConfig{
				Issuer: "https://auth.example.com/",
			},
			expected: "https://auth.example.com/.well-known/jwks.json",
		},
		{
			name: "realm issuer",
			config: AuthConfig{
				Issuer: "https://auth.janua.io/realms/madfam",
			},
			expected: "https://auth.janua.io/realms/madfam/.well-known/jwks.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoint := tt.config.JWKSEndpoint()
			if endpoint != tt.expected {
				t.Errorf("JWKSEndpoint: got %q, want %q", endpoint, tt.expected)
			}
		})
	}
}
