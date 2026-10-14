package config

import "testing"

func TestSparkplugDefaultsAndEnv(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sparkplug.Enabled || cfg.Sparkplug.HostID != "pravara-mes" || cfg.Sparkplug.ClientID != "pravara-mes-host" {
		t.Fatalf("defaults %+v", cfg.Sparkplug)
	}
	t.Setenv("PRAVARA_SPARKPLUG_ENABLED", "true")
	t.Setenv("PRAVARA_SPARKPLUG_BROKER_URL", "ssl://broker.example.test:8883")
	t.Setenv("PRAVARA_SPARKPLUG_USERNAME", "pravara-mes-host")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sparkplug.Enabled || cfg.Sparkplug.BrokerURL != "ssl://broker.example.test:8883" || cfg.Sparkplug.Username != "pravara-mes-host" {
		t.Fatalf("env %+v", cfg.Sparkplug)
	}
}
