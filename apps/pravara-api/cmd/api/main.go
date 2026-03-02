// Package main is the entry point for the PravaraMES API server.
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

	// Set Gin mode
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// Initialize router
	router := gin.New()

	// Add middleware
	router.Use(gin.Recovery())
	router.Use(requestLogger(log))
	router.Use(middleware.Metrics())

	// Add Prometheus metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// Register routes with optional publisher
	api.RegisterRoutesWithPublisher(router, database, cfg, log, publisher)

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
