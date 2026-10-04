package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatch"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/assetshells"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/fabprep"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// BuildDispatchService wires fabrication dispatch from configuration: the
// three Janua machine clients, the yantra4d / fabrication-prep / asset-shells
// clients, the repositories and the durable command stream. A service built
// with dispatch disabled still answers POST /v1/match.
func BuildDispatchService(cfg *config.Config, database *db.DB, publisher *pubsub.Publisher, log *logrus.Logger) (*dispatch.Service, error) {
	prefixes, err := cfg.MachineClients.TenantClientPrefixes()
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	clients := machineclients.NewSet(machineclients.SetConfig{
		TokenURL:                    cfg.MachineClients.TokenURL,
		Yantra4DClientID:            cfg.MachineClients.Yantra4DClientID,
		Yantra4DClientSecret:        cfg.MachineClients.Yantra4DClientSecret,
		FabricationPrepClientID:     cfg.MachineClients.FabricationPrepClientID,
		FabricationPrepClientSecret: cfg.MachineClients.FabricationPrepClientSecret,
		AssetShellsTenantPrefixes:   prefixes,
	}, &http.Client{Timeout: 15 * time.Second})

	deps := dispatch.Deps{Clients: clients}
	if cfg.Dispatch.YantraAPIURL != "" {
		c, err := yantra4d.NewClient(cfg.Dispatch.YantraAPIURL, clients.Yantra4D, httpClient)
		if err != nil {
			return nil, err
		}
		deps.Renderer = c
	}
	if cfg.Dispatch.FabricationPrepAPIURL != "" {
		c, err := fabprep.NewClient(cfg.Dispatch.FabricationPrepAPIURL, clients.FabricationPrep, &http.Client{Timeout: 60 * time.Second})
		if err != nil {
			return nil, err
		}
		deps.Slicer = c
	}
	if cfg.Dispatch.AssetShellsAPIURL != "" {
		c, err := assetshells.NewClient(cfg.Dispatch.AssetShellsAPIURL, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			return nil, err
		}
		deps.Shells = c
	}
	if database != nil {
		tdb := database.Tenant(log)
		sources := repositories.NewDispatchSources(tdb)
		deps.Pool = database.DB
		deps.Dispatch = repositories.NewDispatchRepository(tdb)
		deps.Sources = sources
		deps.Live = sources
		deps.Passports = repositories.NewPassportRepository(tdb)
		deps.Ledger = repositories.NewTaskCommandRepository(tdb)
		deps.Genealogy = services.NewGenealogyService(repositories.NewGenealogyRepository(tdb),
			repositories.NewProductRepository(tdb), publisher, log)
	}
	if publisher != nil {
		deps.Enqueuer = publisher
	}
	svc := dispatch.NewService(deps, dispatch.Settings{
		Enabled:            cfg.Dispatch.Enabled,
		RenderFormat:       cfg.Dispatch.RenderFormat,
		ReservationTTL:     time.Duration(cfg.Dispatch.ReservationTTLSeconds) * time.Second,
		CommandHold:        time.Duration(cfg.Dispatch.CommandHoldSeconds) * time.Second,
		MaxAttempts:        cfg.Dispatch.MaxAttempts,
		PollInterval:       time.Duration(cfg.Dispatch.PollIntervalSeconds) * time.Second,
		PassportAttempts:   cfg.Dispatch.PassportMaxAttempts,
		RequireBoundingBox: cfg.Dispatch.RequireBoundingBox,
		MatchWait:          time.Duration(cfg.Dispatch.MatchWaitSeconds) * time.Second,
	}, log)
	switch cfg.Dispatch.RenderFormat {
	case "", "3mf", "stl":
	default:
		return nil, fmt.Errorf("dispatch.render_format %q: fabrication-prep slices 3mf or stl", cfg.Dispatch.RenderFormat)
	}
	if cfg.Dispatch.Enabled {
		log.WithFields(logrus.Fields{"missing_machine_clients": clients.Missing(), "unavailable": svc.Unavailable()}).
			Info("Fabrication dispatch configured")
	}
	return svc, nil
}

// registerDispatchRoutes adds POST /v1/match and /v1/dispatches.
func registerDispatchRoutes(v1 *gin.RouterGroup, database *db.DB, cfg *config.Config, log *logrus.Logger, publisher *pubsub.Publisher, svc *dispatch.Service) {
	if svc == nil {
		var err error
		if svc, err = BuildDispatchService(cfg, database, publisher, log); err != nil {
			log.WithError(err).Error("Fabrication dispatch is misconfigured; its routes answer 503")
			v1.POST("/match", dispatchMisconfigured(err))
			v1.POST("/dispatches", dispatchMisconfigured(err))
			v1.GET("/dispatches", dispatchMisconfigured(err))
			v1.GET("/dispatches/:id", dispatchMisconfigured(err))
			return
		}
	}
	tdb := database.Tenant(log)
	h := NewDispatchHandler(svc, repositories.NewDispatchRepository(tdb), repositories.NewPassportRepository(tdb), log)
	v1.POST("/match", h.Match)
	v1.POST("/dispatches", h.Create)
	v1.GET("/dispatches", h.List)
	v1.GET("/dispatches/:id", h.Get)
}

func dispatchMisconfigured(err error) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dispatch_misconfigured", "message": err.Error()})
	}
}
