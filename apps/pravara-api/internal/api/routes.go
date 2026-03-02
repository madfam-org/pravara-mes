// Package api provides HTTP handlers and routing for the PravaraMES API.
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// RegisterRoutes sets up all API routes.
func RegisterRoutes(router *gin.Engine, database *db.DB, cfg *config.Config, log *logrus.Logger) {
	// Initialize OIDC verifier
	oidcConfig := auth.OIDCConfig{
		Issuer:   cfg.OIDC.Issuer,
		JWKSURL:  cfg.OIDC.JWKSURL,
		Audience: cfg.OIDC.Audience,
	}
	verifier := auth.NewOIDCVerifier(oidcConfig, log)

	// Initialize repositories (database.DB is the embedded *sql.DB)
	orderRepo := repositories.NewOrderRepository(database.DB)
	orderItemRepo := repositories.NewOrderItemRepository(database.DB)
	taskRepo := repositories.NewTaskRepository(database.DB)
	machineRepo := repositories.NewMachineRepository(database.DB)
	telemetryRepo := repositories.NewTelemetryRepository(database.DB)

	// Initialize handlers
	healthHandler := NewHealthHandler(database, log)
	orderHandler := NewOrderHandler(orderRepo, orderItemRepo, log)
	taskHandler := NewTaskHandler(taskRepo, log)
	machineHandler := NewMachineHandler(machineRepo, telemetryRepo, log)
	telemetryHandler := NewTelemetryHandler(telemetryRepo, log)
	webhookHandler := NewWebhookHandler(orderRepo, orderItemRepo, log, "") // TODO: Add cotiza secret from config

	// Health check endpoints (no auth required)
	router.GET("/health", healthHandler.Health)
	router.GET("/health/live", healthHandler.Liveness)
	router.GET("/health/ready", healthHandler.Readiness)

	// API v1 routes (protected)
	v1 := router.Group("/v1")
	v1.Use(middleware.AuthMiddleware(verifier, database, log))
	{
		// Orders endpoints
		orders := v1.Group("/orders")
		{
			orders.GET("", orderHandler.List)
			orders.POST("", orderHandler.Create)
			orders.GET("/:id", orderHandler.GetByID)
			orders.PATCH("/:id", orderHandler.Update)
			orders.DELETE("/:id", orderHandler.Delete)
			orders.GET("/:id/items", orderHandler.ListItems)
			orders.POST("/:id/items", orderHandler.AddItem)
		}

		// Tasks (Kanban) endpoints
		tasks := v1.Group("/tasks")
		{
			tasks.GET("", taskHandler.List)
			tasks.POST("", taskHandler.Create)
			tasks.GET("/board", taskHandler.GetKanbanBoard)
			tasks.GET("/:id", taskHandler.GetByID)
			tasks.PATCH("/:id", taskHandler.Update)
			tasks.DELETE("/:id", taskHandler.Delete)
			tasks.POST("/:id/move", taskHandler.Move)
			tasks.POST("/:id/assign", taskHandler.Assign)
		}

		// Machines endpoints
		machines := v1.Group("/machines")
		{
			machines.GET("", machineHandler.List)
			machines.POST("", machineHandler.Create)
			machines.GET("/:id", machineHandler.GetByID)
			machines.PATCH("/:id", machineHandler.Update)
			machines.DELETE("/:id", machineHandler.Delete)
			machines.GET("/:id/telemetry", machineHandler.GetTelemetry)
			machines.POST("/:id/heartbeat", machineHandler.Heartbeat)
		}

		// Telemetry endpoints
		telemetry := v1.Group("/telemetry")
		{
			telemetry.GET("", telemetryHandler.List)
			telemetry.GET("/aggregated", telemetryHandler.GetAggregated)
			telemetry.GET("/latest", telemetryHandler.GetLatest)
			telemetry.POST("/batch", telemetryHandler.BatchInsert)
		}

		// Webhook endpoints (may need different auth)
		webhooks := v1.Group("/webhooks")
		{
			webhooks.POST("/cotiza", webhookHandler.CotizaWebhook)
			webhooks.POST("/forgesight", placeholderHandler("forgesight webhook"))
		}
	}
}

// placeholderHandler returns a handler that indicates the endpoint is not yet implemented.
func placeholderHandler(description string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(501, gin.H{
			"error":       "not_implemented",
			"message":     "This endpoint is not yet implemented",
			"description": description,
		})
	}
}
