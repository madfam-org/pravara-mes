// Package api provides HTTP handlers and routing for the PravaraMES API.
package api

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/auth"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// RegisterRoutes sets up all API routes.
func RegisterRoutes(router *gin.Engine, database *db.DB, cfg *config.Config, log *logrus.Logger) {
	RegisterRoutesWithRecorder(router, database, cfg, log, nil, nil)
}

// RegisterRoutesWithPublisher sets up all API routes with an optional event publisher.
func RegisterRoutesWithPublisher(router *gin.Engine, database *db.DB, cfg *config.Config, log *logrus.Logger, publisher *pubsub.Publisher) {
	RegisterRoutesWithRecorder(router, database, cfg, log, publisher, nil)
}

// RegisterRoutesWithRecorder sets up all API routes with optional event publisher and usage recorder.
func RegisterRoutesWithRecorder(router *gin.Engine, database *db.DB, cfg *config.Config, log *logrus.Logger, publisher *pubsub.Publisher, usageRecorder billing.UsageRecorder) {
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
	qualityCertRepo := repositories.NewQualityCertificateRepository(database.DB)
	inspectionRepo := repositories.NewInspectionRepository(database.DB)
	batchLotRepo := repositories.NewBatchLotRepository(database.DB)
	taskCmdRepo := repositories.NewTaskCommandRepository(database.DB)

	// Phase 2.6+ repositories
	oeeRepo := repositories.NewOEERepository(database.DB)
	maintRepo := repositories.NewMaintenanceRepository(database.DB)
	productRepo := repositories.NewProductRepository(database.DB)
	genealogyRepo := repositories.NewGenealogyRepository(database.DB)
	wiRepo := repositories.NewWorkInstructionRepository(database.DB)
	spcRepo := repositories.NewSPCRepository(database.DB)
	inventoryRepo := repositories.NewInventoryRepository(database.DB)

	// Initialize handlers
	healthHandler := NewHealthHandler(database, log)
	orderHandler := NewOrderHandler(orderRepo, orderItemRepo, log)
	taskHandler := NewTaskHandler(taskRepo, log)
	machineHandler := NewMachineHandler(machineRepo, telemetryRepo, log)
	telemetryHandler := NewTelemetryHandler(telemetryRepo, log)
	webhookHandler := NewWebhookHandler(orderRepo, orderItemRepo, log, cfg.Cotiza.WebhookSecret)
	tezcaWebhookHandler := NewTezcaWebhookHandler(log, cfg.Tezca.WebhookSecret)

	// Initialize Dhanam webhook handler
	invoiceRepo := billing.NewInvoiceRepository(database.DB)
	dhanamWebhookHandler := billing.NewWebhookHandler(invoiceRepo, cfg.Dhanam.WebhookSecret, log)
	realtimeHandler := NewRealtimeHandler(&cfg.Centrifugo, log)
	qualityHandler := NewQualityHandler(qualityCertRepo, inspectionRepo, batchLotRepo, log)

	// Phase 2.6+ handlers
	analyticsHandler := NewAnalyticsHandler(oeeRepo, log)
	maintenanceHandler := NewMaintenanceHandler(maintRepo, log)
	productHandler := NewProductHandler(productRepo, log)
	genealogyHandler := NewGenealogyHandler(genealogyRepo, log)
	wiHandler := NewWorkInstructionHandler(wiRepo, log)
	spcHandler := NewSPCHandler(spcRepo, log)
	inventoryHandler := NewInventoryHandler(inventoryRepo, log)

	// Initialize Yantra4D handler
	var yantra4dHandler *Yantra4DHandler
	if publisher != nil {
		hyperobjectMapper := services.NewHyperobjectMapper(productRepo, wiRepo, publisher, log)
		yantra4dHandler = NewYantra4DHandler(
			hyperobjectMapper,
			"http://localhost:4502",        // viz-engine URL (override via config)
			"https://yantra4d.madfam.io",   // Yantra4D URL
			log,
		)
	}

	// Set publisher on handlers that support events
	if publisher != nil {
		taskHandler.SetPublisher(publisher)
		orderHandler.SetPublisher(publisher)
		machineHandler.SetPublisher(publisher)

		// Initialize and set automation service for task-machine integration
		automationService := services.NewAutomationService(taskRepo, machineRepo, taskCmdRepo, publisher, log)
		taskHandler.SetAutomation(automationService)

		// Phase 2.6+ services
		oeeService := services.NewOEEService(oeeRepo, publisher, log)
		analyticsHandler.SetPublisher(publisher)
		analyticsHandler.SetOEEService(oeeService)

		maintService := services.NewMaintenanceService(maintRepo, publisher, log)
		maintenanceHandler.SetPublisher(publisher)
		maintenanceHandler.SetMaintenanceService(maintService)

		genealogyService := services.NewGenealogyService(genealogyRepo, productRepo, publisher, log)
		genealogyHandler.SetPublisher(publisher)
		genealogyHandler.SetGenealogyService(genealogyService)

		wiService := services.NewWorkInstructionService(wiRepo, publisher, log)
		wiHandler.SetPublisher(publisher)
		wiHandler.SetWIService(wiService)

		spcService := services.NewSPCService(spcRepo, publisher, log)
		spcHandler.SetPublisher(publisher)
		spcHandler.SetSPCService(spcService)

		inventoryService := services.NewInventoryService(inventoryRepo, publisher, log)
		inventoryHandler.SetPublisher(publisher)
		inventoryHandler.SetInventoryService(inventoryService)
	}

	// Set usage recorder on handlers that track billable events
	if usageRecorder != nil {
		orderHandler.SetUsageRecorder(usageRecorder)
		qualityHandler.SetUsageRecorder(usageRecorder)
	}

	// Initialize billing handler if usage recorder is available
	var billingHandler *BillingHandler
	if usageRecorder != nil {
		billingHandler = NewBillingHandler(usageRecorder, log)
	}

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
			// Work instruction endpoints on tasks
			tasks.POST("/:id/work-instructions", wiHandler.AttachToTask)
			tasks.GET("/:id/work-instructions", wiHandler.GetTaskWorkInstructions)
			tasks.POST("/:id/work-instructions/:wiId/acknowledge", wiHandler.AcknowledgeStep)
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
			machines.POST("/:id/command", machineHandler.SendCommand)
			machines.GET("/:id/maintenance", maintenanceHandler.GetMachineMaintenance)
		}

		// Telemetry endpoints
		telemetry := v1.Group("/telemetry")
		{
			telemetry.GET("", telemetryHandler.List)
			telemetry.GET("/aggregated", telemetryHandler.GetAggregated)
			telemetry.GET("/latest", telemetryHandler.GetLatest)
			telemetry.POST("/batch", telemetryHandler.BatchInsert)
		}

		// Quality Management endpoints
		quality := v1.Group("/quality")
		{
			// Quality Certificates
			certificates := quality.Group("/certificates")
			{
				certificates.GET("", qualityHandler.ListCertificates)
				certificates.POST("", qualityHandler.CreateCertificate)
				certificates.GET("/:id", qualityHandler.GetCertificateByID)
				certificates.PATCH("/:id", qualityHandler.UpdateCertificate)
				certificates.DELETE("/:id", qualityHandler.DeleteCertificate)
			}

			// Inspections
			inspections := quality.Group("/inspections")
			{
				inspections.GET("", qualityHandler.ListInspections)
				inspections.POST("", qualityHandler.CreateInspection)
				inspections.GET("/:id", qualityHandler.GetInspectionByID)
				inspections.PATCH("/:id", qualityHandler.UpdateInspection)
				inspections.DELETE("/:id", qualityHandler.DeleteInspection)
				inspections.POST("/:id/complete", qualityHandler.CompleteInspection)
			}

			// Batch Lots
			batches := quality.Group("/batches")
			{
				batches.GET("", qualityHandler.ListBatchLots)
				batches.POST("", qualityHandler.CreateBatchLot)
				batches.GET("/:id", qualityHandler.GetBatchLotByID)
				batches.PATCH("/:id", qualityHandler.UpdateBatchLot)
				batches.DELETE("/:id", qualityHandler.DeleteBatchLot)
			}
		}

		// Analytics endpoints (OEE + SPC)
		analytics := v1.Group("/analytics")
		{
			analytics.GET("/oee", analyticsHandler.GetOEE)
			analytics.GET("/oee/summary", analyticsHandler.GetOEESummary)
			analytics.POST("/oee/compute", analyticsHandler.ComputeOEE)
			analytics.GET("/spc/limits", spcHandler.GetLimits)
			analytics.POST("/spc/limits/compute", spcHandler.ComputeLimits)
			analytics.GET("/spc/chart", spcHandler.GetChart)
			analytics.GET("/spc/violations", spcHandler.GetViolations)
			analytics.POST("/spc/violations/:id/acknowledge", spcHandler.AcknowledgeViolation)
		}

		// Maintenance endpoints
		maintenance := v1.Group("/maintenance")
		{
			schedules := maintenance.Group("/schedules")
			{
				schedules.GET("", maintenanceHandler.ListSchedules)
				schedules.POST("", maintenanceHandler.CreateSchedule)
				schedules.GET("/:id", maintenanceHandler.GetScheduleByID)
				schedules.PATCH("/:id", maintenanceHandler.UpdateSchedule)
				schedules.DELETE("/:id", maintenanceHandler.DeleteSchedule)
			}
			workOrders := maintenance.Group("/work-orders")
			{
				workOrders.GET("", maintenanceHandler.ListWorkOrders)
				workOrders.POST("", maintenanceHandler.CreateWorkOrder)
				workOrders.GET("/:id", maintenanceHandler.GetWorkOrderByID)
				workOrders.PATCH("/:id", maintenanceHandler.UpdateWorkOrder)
				workOrders.POST("/:id/complete", maintenanceHandler.CompleteWorkOrder)
			}
		}

		// Product endpoints
		products := v1.Group("/products")
		{
			products.GET("", productHandler.ListProducts)
			products.POST("", productHandler.CreateProduct)
			products.GET("/:id", productHandler.GetProductByID)
			products.PATCH("/:id", productHandler.UpdateProduct)
			products.DELETE("/:id", productHandler.DeleteProduct)
			products.GET("/:id/bom", productHandler.GetBOM)
			products.POST("/:id/bom/items", productHandler.AddBOMItem)
			products.DELETE("/:id/bom/items/:itemId", productHandler.DeleteBOMItem)
		}

		// Genealogy endpoints
		genealogy := v1.Group("/genealogy")
		{
			genealogy.GET("", genealogyHandler.ListGenealogy)
			genealogy.POST("", genealogyHandler.CreateGenealogy)
			genealogy.GET("/:id", genealogyHandler.GetGenealogyByID)
			genealogy.PATCH("/:id", genealogyHandler.UpdateGenealogy)
			genealogy.POST("/:id/seal", genealogyHandler.SealGenealogy)
			genealogy.GET("/:id/tree", genealogyHandler.GetGenealogyTree)
		}

		// Work Instructions endpoints
		workInstructions := v1.Group("/work-instructions")
		{
			workInstructions.GET("", wiHandler.ListWorkInstructions)
			workInstructions.POST("", wiHandler.CreateWorkInstruction)
			workInstructions.GET("/:id", wiHandler.GetWorkInstructionByID)
			workInstructions.PATCH("/:id", wiHandler.UpdateWorkInstruction)
			workInstructions.DELETE("/:id", wiHandler.DeleteWorkInstruction)
		}

		// Inventory endpoints
		inventory := v1.Group("/inventory")
		{
			inventory.GET("", inventoryHandler.ListItems)
			inventory.POST("", inventoryHandler.CreateItem)
			inventory.GET("/low-stock", inventoryHandler.GetLowStock)
			inventory.GET("/:id", inventoryHandler.GetItemByID)
			inventory.PATCH("/:id", inventoryHandler.UpdateItem)
			inventory.POST("/:id/adjust", inventoryHandler.AdjustItem)
		}

		// Factory layout endpoints (proxy to viz-engine)
		layouts := v1.Group("/layouts")
		{
			layouts.GET("/active", handleGetActiveLayout(database, log))
			layouts.GET("", handleProxyLayouts(log))
			layouts.GET("/:id", handleProxyLayout(log))
			layouts.PUT("/:id", handleProxyLayoutUpdate(log))
		}

		// 3D model endpoints (proxy to viz-engine)
		models := v1.Group("/models")
		{
			models.GET("", handleProxyModels(log))
			models.POST("/upload", handleProxyModelUpload(log))
		}

		// Yantra4D import endpoints
		if yantra4dHandler != nil {
			yantra4dGroup := v1.Group("/import/yantra4d")
			{
				yantra4dGroup.POST("", yantra4dHandler.ImportHyperobject)
				yantra4dGroup.GET("/preview", yantra4dHandler.PreviewImport)
			}
		}

		// Webhook endpoints (may need different auth)
		webhooks := v1.Group("/webhooks")
		{
			webhooks.POST("/cotiza", webhookHandler.CotizaWebhook)
			webhooks.POST("/dhanam", dhanamWebhookHandler.HandleWebhook)
			webhooks.POST("/forgesight", inventoryHandler.ForgeSightWebhook)
			webhooks.POST("/tezca", tezcaWebhookHandler.HandleWebhook)
		}

		// Real-time connection endpoints
		realtime := v1.Group("/realtime")
		{
			realtime.GET("/token", realtimeHandler.GetToken)
		}

		// Billing endpoints
		if billingHandler != nil {
			billing := v1.Group("/billing")
			{
				billing.GET("/usage", billingHandler.GetUsage)
				billing.GET("/usage/daily", billingHandler.GetDailyUsage)
			}

			// Admin billing endpoints (requires admin role)
			admin := v1.Group("/admin/billing")
			admin.Use(middleware.RequireRole("admin"))
			{
				admin.GET("/tenants/:id/usage", billingHandler.GetTenantUsageAdmin)
			}
		}
	}

	// Centrifugo proxy endpoints (no user auth - called by Centrifugo internally)
	// These should be protected by API key or internal network only
	centrifugoProxy := router.Group("/v1/realtime")
	{
		centrifugoProxy.POST("/auth", realtimeHandler.AuthConnect)
		centrifugoProxy.POST("/subscribe", realtimeHandler.AuthSubscribe)
	}
}
