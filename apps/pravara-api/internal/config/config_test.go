package config

import (
	"os"
	"testing"
)

func TestConfig_Load_Defaults(t *testing.T) {
	// Clear any environment variables that might interfere
	envVars := []string{
		"APP_ENV",
		"LOG_LEVEL",
		"PRAVARA_API_PORT",
		"PORT",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Check defaults
	if cfg.App.Env != "development" {
		t.Errorf("App.Env: got %q, want %q", cfg.App.Env, "development")
	}
	if cfg.App.LogLevel != "info" {
		t.Errorf("App.LogLevel: got %q, want %q", cfg.App.LogLevel, "info")
	}
	if cfg.Server.Port != 4500 {
		t.Errorf("Server.Port: got %d, want %d", cfg.Server.Port, 4500)
	}
}

func TestConfig_Load_FromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("APP_ENV", "production")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("PRAVARA_API_PORT", "8080")
	defer func() {
		os.Unsetenv("APP_ENV")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("PRAVARA_API_PORT")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.App.Env != "production" {
		t.Errorf("App.Env: got %q, want %q", cfg.App.Env, "production")
	}
	if cfg.App.LogLevel != "debug" {
		t.Errorf("App.LogLevel: got %q, want %q", cfg.App.LogLevel, "debug")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port: got %d, want %d", cfg.Server.Port, 8080)
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{"development", "development", true},
		{"production", "production", false},
		{"staging", "staging", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				App: AppConfig{Env: tt.env},
			}
			if got := cfg.IsDevelopment(); got != tt.expected {
				t.Errorf("IsDevelopment: got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_IsProduction(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected bool
	}{
		{"development", "development", false},
		{"production", "production", true},
		{"staging", "staging", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				App: AppConfig{Env: tt.env},
			}
			if got := cfg.IsProduction(); got != tt.expected {
				t.Errorf("IsProduction: got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOIDCConfig_Defaults(t *testing.T) {
	// Clear any OIDC environment variables
	envVars := []string{
		"OIDC_ISSUER",
		"OIDC_JWKS_URL",
		"OIDC_AUDIENCE",
		"OIDC_CLIENT_ID",
	}
	for _, env := range envVars {
		os.Unsetenv(env)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.OIDC.Issuer != "https://auth.madfam.io" {
		t.Errorf("OIDC.Issuer: got %q, want %q", cfg.OIDC.Issuer, "https://auth.madfam.io")
	}
	if cfg.OIDC.JWKSURL != "https://auth.madfam.io/.well-known/jwks.json" {
		t.Errorf("OIDC.JWKSURL: got %q, want %q", cfg.OIDC.JWKSURL, "https://auth.madfam.io/.well-known/jwks.json")
	}
	if cfg.OIDC.Audience != "pravara-api" {
		t.Errorf("OIDC.Audience: got %q, want %q", cfg.OIDC.Audience, "pravara-api")
	}
}

func TestServerConfig_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host: got %q, want %q", cfg.Server.Host, "0.0.0.0")
	}
	if cfg.Server.ReadTimeout != 30 {
		t.Errorf("Server.ReadTimeout: got %d, want %d", cfg.Server.ReadTimeout, 30)
	}
	if cfg.Server.WriteTimeout != 30 {
		t.Errorf("Server.WriteTimeout: got %d, want %d", cfg.Server.WriteTimeout, 30)
	}
	if cfg.Server.ShutdownTimeout != 10 {
		t.Errorf("Server.ShutdownTimeout: got %d, want %d", cfg.Server.ShutdownTimeout, 10)
	}
}

func TestDatabaseConfig_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Database.MaxConnections != 25 {
		t.Errorf("Database.MaxConnections: got %d, want %d", cfg.Database.MaxConnections, 25)
	}
	if cfg.Database.MaxIdleConns != 5 {
		t.Errorf("Database.MaxIdleConns: got %d, want %d", cfg.Database.MaxIdleConns, 5)
	}
	if cfg.Database.ConnMaxLifetime != 300 {
		t.Errorf("Database.ConnMaxLifetime: got %d, want %d", cfg.Database.ConnMaxLifetime, 300)
	}
}

func TestDhanamConfig_WebhookSecret(t *testing.T) {
	// Set webhook secret via environment variable
	os.Setenv("DHANAM_WEBHOOK_SECRET", "test-dhanam-secret")
	defer os.Unsetenv("DHANAM_WEBHOOK_SECRET")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Dhanam.WebhookSecret != "test-dhanam-secret" {
		t.Errorf("Dhanam.WebhookSecret: got %q, want %q", cfg.Dhanam.WebhookSecret, "test-dhanam-secret")
	}
}
