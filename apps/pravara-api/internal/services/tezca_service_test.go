package services

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

func TestTezcaService_DisabledReturnsNil(t *testing.T) {
	log := logrus.New()
	cfg := config.TezcaConfig{Enabled: false}
	svc := NewTezcaService(cfg, log)

	if svc.IsEnabled() {
		t.Error("expected IsEnabled() to be false when disabled")
	}

	result, err := svc.SearchManufacturingLaws(context.Background(), "NOM")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestTezcaService_EnabledCreatesClient(t *testing.T) {
	log := logrus.New()
	cfg := config.TezcaConfig{
		Enabled: true,
		APIURL:  "https://tezca.mx/api/v1",
		APIKey:  "tzk_test",
	}
	svc := NewTezcaService(cfg, log)

	if !svc.IsEnabled() {
		t.Error("expected IsEnabled() to be true when enabled")
	}
}

func TestTezcaService_InvalidateNOMCache(t *testing.T) {
	log := logrus.New()
	cfg := config.TezcaConfig{Enabled: true, APIURL: "https://tezca.mx/api/v1"}
	svc := NewTezcaService(cfg, log)

	// Should not panic
	svc.InvalidateNOMCache()
}
