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
	verifier, err := auth.NewOIDCVerifier(cfg.Auth)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize OIDC verifier")
	}

	// Initialize repositories
	orderRepo := repositories.NewOrderRepository(database.Conn)
	taskRepo := repositories.NewTaskRepository(database.Conn)
	machineRepo := repositories.NewMachineRepository(database.Conn)
	telemetryRepo := repositories.NewTelemetryRepository(database.Conn)

	// Initialize handlers
	healthHandler := NewHealthHandler(database, log)
	orderHandler := NewOrderHandler(orderRepo, log)
	taskHandler := NewTaskHandler(taskRepo, log)
	machineHandler := NewMachineHandler(machineRepo, telemetryRepo, log)

	// Health check endpoints (no auth required)
	router.GET("/health", healthHandler.Health)
	router.GET("/health/live", healthHandler.Liveness)
	router.GET("/health/ready", healthHandler.Readiness)

	// API v1 routes (protected)
	v1 := router.Group("/v1")
	v1.Use(middleware.AuthMiddleware(verifier, database.Conn))
	{
		// Orders endpoints
		orders := v1.Group("/orders")
		{
			orders.GET("", orderHandler.List)
			orders.POST("", orderHandler.Create)
			orders.GET("/:id", orderHandler.GetByID)
			orders.PATCH("/:id", orderHandler.Update)
			orders.DELETE("/:id", orderHandler.Delete)
			orders.GET("/:id/items", placeholderHandler("list order items"))
			orders.POST("/:id/items", placeholderHandler("add order item"))
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
			telemetry.GET("", placeholderHandler("query telemetry"))
			telemetry.POST("/batch", placeholderHandler("batch insert telemetry"))
		}

		// Webhook endpoints (may need different auth)
		webhooks := v1.Group("/webhooks")
		{
			webhooks.POST("/cotiza", placeholderHandler("cotiza webhook"))
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
