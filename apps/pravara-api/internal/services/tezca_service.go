package services

import (
	"context"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations"
)

// TezcaService wraps the Tezca client with feature-gate and caching logic.
type TezcaService struct {
	client *integrations.TezcaClient
	cfg    config.TezcaConfig
	log    *logrus.Logger
	mu     sync.RWMutex
}

// NewTezcaService creates a new Tezca service.
// If cfg.Enabled is false, the client is not initialized and all methods
// gracefully return nil/empty results.
func NewTezcaService(cfg config.TezcaConfig, log *logrus.Logger) *TezcaService {
	var client *integrations.TezcaClient
	if cfg.Enabled {
		client = integrations.NewTezcaClient(cfg.APIURL, cfg.APIKey)
		log.Info("Tezca integration enabled")
	} else {
		log.Info("Tezca integration disabled")
	}
	return &TezcaService{client: client, cfg: cfg, log: log}
}

// IsEnabled returns true if Tezca is configured and active.
func (s *TezcaService) IsEnabled() bool {
	return s.cfg.Enabled && s.client != nil
}

// SearchManufacturingLaws searches for laws relevant to manufacturing.
func (s *TezcaService) SearchManufacturingLaws(ctx context.Context, query string) (map[string]interface{}, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	return s.client.SearchArticles(ctx, query, integrations.DefaultDomain)
}

// SearchSafetyNorms searches for NOM/STPS safety standards.
func (s *TezcaService) SearchSafetyNorms(ctx context.Context, query string) (map[string]interface{}, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	return s.client.SearchArticles(ctx, query, "safety")
}

// GetLawDetail fetches a law by its official ID.
func (s *TezcaService) GetLawDetail(ctx context.Context, lawID string) (map[string]interface{}, error) {
	if !s.IsEnabled() {
		return nil, nil
	}
	return s.client.GetLawDetail(ctx, lawID)
}

// InvalidateNOMCache clears any cached NOM data.
// Currently a placeholder for future cache implementation.
func (s *TezcaService) InvalidateNOMCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.Info("NOM cache invalidated")
}
