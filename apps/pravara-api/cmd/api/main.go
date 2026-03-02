// Package main is the entry point for the PravaraMES API server.
//
// @title PravaraMES API
// @version 1.0.0
// @description Cloud-native Manufacturing Execution System API for phygital fabrication.
// @description Provides endpoints for order management, task/Kanban board operations,
// @description machine control, telemetry, quality management, and real-time updates.
//
// @contact.name PravaraMES Team
// @contact.url https://github.com/madfam-org/pravara-mes
// @contact.email support@pravara.io
//
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
//
// @host localhost:4500
// @BasePath /v1
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description JWT Bearer token from OIDC provider (format: "Bearer <token>")
//
// @tag.name health
// @tag.description Health check and readiness endpoints
// @tag.name orders
// @tag.description Order management endpoints
// @tag.name tasks
// @tag.description Task and Kanban board operations
// @tag.name machines
// @tag.description Machine management and control
// @tag.name telemetry
// @tag.description Machine telemetry data endpoints
// @tag.name quality
// @tag.description Quality certificates, inspections, and batch lots
// @tag.name billing
// @tag.description Usage tracking and billing endpoints
// @tag.name realtime
// @tag.description WebSocket connection authentication
// @tag.name webhooks
// @tag.description External webhook endpoints
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/api"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	// Set log level
	level, err := logrus.ParseLevel(cfg.App.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	log.WithFields(logrus.Fields{
		"env":  cfg.App.Env,
		"port": cfg.Server.Port,
	}).Info("Starting PravaraMES API")

	// Initialize database connection
	database, err := db.NewConnection(cfg.Database)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	defer database.Close()

	log.Info("Database connection established")

	// Initialize Redis publisher for real-time events (optional)
	var publisher *pubsub.Publisher
	if cfg.Redis.URL != "" {
		var err error
		publisher, err = pubsub.NewPublisher(pubsub.PublisherConfig{
			RedisURL: cfg.Redis.URL,
		}, log)
		if err != nil {
			log.WithError(err).Warn("Failed to connect to Redis for real-time events, continuing without publisher")
		} else {
			defer publisher.Close()
			log.Info("Redis publisher connected for real-time events")
		}
	}

	// Initialize Redis usage recorder for billing (optional)
	var usageRecorder billing.UsageRecorder
	if cfg.Redis.URL != "" {
		var err error
		usageRecorder, err = billing.NewRedisUsageRecorder(billing.RecorderConfig{
			RedisURL:      cfg.Redis.URL,
			BufferSize:    1000,
			FlushInterval: 5 * time.Minute,
		}, log)
		if err != nil {
			log.WithError(err).Warn("Failed to connect to Redis for usage tracking, continuing without recorder")
		} else {
			defer usageRecorder.Close()
			log.Info("Redis usage recorder connected for billing tracking")
		}
	}

	// Set Gin mode
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(requestLogger(log))
	router.Use(middleware.RateLimiter(log))
	router.Use(middleware.Metrics())
	if usageRecorder != nil {
		router.Use(middleware.UsageTracking(usageRecorder, log))
	}

	// Add Prometheus metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Register routes with optional publisher and usage recorder
	api.RegisterRoutesWithRecorder(router, database, cfg, log, publisher, usageRecorder)

	// Start background goroutine to collect database stats
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collectDBStats(ctx, database, log)

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.WithField("addr", srv.Addr).Info("HTTP server listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server error")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Cancel background tasks
	cancel()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout)*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Server forced to shutdown")
	}

	log.Info("Server exited")
}

// collectDBStats periodically collects and reports database connection pool statistics.
func collectDBStats(ctx context.Context, database *db.DB, log *logrus.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := database.Stats()
			observability.DBConnectionsOpen.Set(float64(stats.OpenConnections))
			observability.DBConnectionsInUse.Set(float64(stats.InUse))
			observability.DBConnectionsIdle.Set(float64(stats.Idle))
			observability.DBConnectionsWaitCount.Add(float64(stats.WaitCount))
			observability.DBConnectionsWaitDuration.Add(stats.WaitDuration.Seconds())
			observability.DBConnectionsMaxIdleClosed.Add(float64(stats.MaxIdleClosed))
			observability.DBConnectionsMaxLifetimeClosed.Add(float64(stats.MaxLifetimeClosed))
		}
	}
}

// requestLogger returns a Gin middleware for structured request logging.
func requestLogger(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)

		log.WithFields(logrus.Fields{
			"status":     c.Writer.Status(),
			"method":     c.Request.Method,
			"path":       path,
			"query":      query,
			"ip":         c.ClientIP(),
			"user_agent": c.Request.UserAgent(),
			"latency_ms": latency.Milliseconds(),
		}).Info("Request completed")
	}
}
