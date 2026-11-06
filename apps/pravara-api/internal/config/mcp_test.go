package config

import "testing"

func TestMCPConfigIsOffByDefault(t *testing.T) {
	t.Setenv("PRAVARA_MCP_ENABLED", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.MCP.Enabled {
		t.Fatal("the native MCP must be off unless PRAVARA_MCP_ENABLED is set")
	}
	if cfg.MCP.RatePerMinute != 60 || cfg.MCP.Burst != 10 {
		t.Fatalf("rate defaults: got %d/min burst %d, want 60/min burst 10", cfg.MCP.RatePerMinute, cfg.MCP.Burst)
	}
}

func TestMCPConfigFromEnv(t *testing.T) {
	t.Setenv("PRAVARA_MCP_ENABLED", "true")
	t.Setenv("PRAVARA_MCP_RATE_PER_MINUTE", "120")
	t.Setenv("PRAVARA_MCP_BURST", "5")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.MCP.Enabled || cfg.MCP.RatePerMinute != 120 || cfg.MCP.Burst != 5 {
		t.Fatalf("got %+v", cfg.MCP)
	}
}
