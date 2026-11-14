package config

import (
	"strings"
	"testing"
)

func TestCostEventsAreOffByDefault(t *testing.T) {
	t.Setenv("PRAVARA_COST_EVENTS_ENABLED", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ce := cfg.CostEvents
	if ce.Enabled || ce.DryRun {
		t.Fatal("cost events must be off unless PRAVARA_COST_EVENTS_ENABLED is set")
	}
	if ce.CostClass != "cogs" || ce.IntervalSeconds != 300 || ce.LookbackDays != 62 || ce.BatchSize != 100 {
		t.Fatalf("defaults: %+v", ce)
	}
	if p := ce.Problems("https://janua.example/api/v1/oauth/token"); len(p) != 0 {
		t.Fatalf("switch off requires nothing, got %v", p)
	}
}

func complete() CostEventsConfig {
	return CostEventsConfig{Enabled: true, TenantID: "4f9d7a52-2b1c-4e0a-9c3d-8e7f6a5b4c3d", StartDate: "2026-11-01",
		DhanamURL: "https://dhanam.example", ClientID: "jnc_test", ClientSecret: "jns_secret_value",
		CostClass: "cogs", IntervalSeconds: 300, LookbackDays: 62, BatchSize: 100}
}

func TestCostEventsProblems(t *testing.T) {
	if p := complete().Problems("https://janua.example/token"); len(p) != 0 {
		t.Fatalf("complete config has problems: %v", p)
	}
	c := complete()
	c.TenantID, c.StartDate, c.CostClass, c.BatchSize, c.ClientSecret, c.DhanamURL = "madfam", "1-11-2026", "cost", 501, "", ""
	p := strings.Join(c.Problems(""), "\n")
	for _, want := range []string{"TENANT_ID", "START_DATE is not a YYYY-MM-DD", "COST_CLASS", "BATCH_SIZE",
		"DHANAM_URL", "TOKEN_URL", "CLIENT_SECRET"} {
		if !strings.Contains(p, want) {
			t.Errorf("problems lack %q:\n%s", want, p)
		}
	}
	c = complete()
	if p := strings.Join(c.Problems(""), "\n"); strings.Contains(p, "jns_secret_value") {
		t.Fatal("a problem carries the secret")
	}
	// A dry run needs no Dhanam URL, token URL or client.
	c = complete()
	c.DryRun, c.DhanamURL, c.ClientID, c.ClientSecret = true, "", "", ""
	if p := c.Problems(""); len(p) != 0 {
		t.Fatalf("dry run should need no credentials: %v", p)
	}
}

func TestCostEventsEndpointAndTokenURL(t *testing.T) {
	for _, base := range []string{"https://dhanam.example", "https://dhanam.example/", "https://dhanam.example/v1", "https://dhanam.example/v1/"} {
		c := CostEventsConfig{DhanamURL: base}
		if got := c.Endpoint(); got != "https://dhanam.example/v1/cost-events" {
			t.Errorf("%s → %s", base, got)
		}
	}
	c := CostEventsConfig{}
	if c.EffectiveTokenURL("https://janua.example/t") != "https://janua.example/t" {
		t.Fatal("falls back to JANUA_TOKEN_URL")
	}
	c.TokenURL = "https://other.example/t"
	if c.EffectiveTokenURL("https://janua.example/t") != "https://other.example/t" {
		t.Fatal("own token URL wins")
	}
}

func TestCostEventsFromEnv(t *testing.T) {
	t.Setenv("PRAVARA_COST_EVENTS_ENABLED", "true")
	t.Setenv("PRAVARA_COST_EVENTS_DRY_RUN", "true")
	t.Setenv("PRAVARA_COST_EVENTS_TENANT_ID", "4f9d7a52-2b1c-4e0a-9c3d-8e7f6a5b4c3d")
	t.Setenv("PRAVARA_COST_EVENTS_START_DATE", "2026-11-01")
	t.Setenv("PRAVARA_COST_EVENTS_BATCH_SIZE", "250")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ce := cfg.CostEvents
	if !ce.Enabled || !ce.DryRun || ce.BatchSize != 250 || ce.StartDate != "2026-11-01" {
		t.Fatalf("got %+v", ce)
	}
	if p := ce.Problems(cfg.MachineClients.TokenURL); len(p) != 0 {
		t.Fatalf("problems: %v", p)
	}
}
