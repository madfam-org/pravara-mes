package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The operator template in deploy/edge must stay loadable and valid.
func TestEdgeConfigTemplateLoads(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "deploy", "edge", "config.example.yaml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Edge
	if !e.Enabled || e.GroupID != "example-tenant" || e.EdgeNodeID != "site-example" || len(e.Devices) != 2 {
		t.Fatalf("edge section = %+v", e)
	}
	if e.PollInterval != 2*time.Second || e.ArtifactTimeout != 10*time.Minute {
		t.Fatalf("durations = %v %v", e.PollInterval, e.ArtifactTimeout)
	}
	if d := e.Devices[0].Capabilities["nozzle_diameters_mm"]; d == nil {
		t.Fatal("device capabilities not decoded")
	}
	e.ApplyDefaults()
	if err := e.Validate(); err != nil {
		t.Fatalf("template does not validate: %v", err)
	}
}

func TestLoadDefaultsAndMissingExplicitFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("explicit missing config file accepted")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("server:\n  port: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("port 0 accepted")
	}
	if err := os.WriteFile(p, []byte("environment: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 4503 || cfg.Edge.Enabled {
		t.Fatalf("defaults = %+v", cfg)
	}
}
