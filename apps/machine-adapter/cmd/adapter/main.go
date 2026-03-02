// Package main provides the entry point for the machine adapter service.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/config"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

var (
	version = "dev"
	commit  = "unknown"
)

// Metrics
var (
	machinesConnected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "machine_adapter_machines_connected",
			Help: "Number of machines currently connected",
		},
		[]string{"type", "protocol"},
	)

	commandsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "machine_adapter_commands_processed_total",
			Help: "Total number of commands processed",
		},
		[]string{"machine_type", "command", "status"},
	)

	telemetryReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "machine_adapter_telemetry_received_total",
			Help: "Total telemetry messages received",
		},
		[]string{"machine_type", "metric_type"},
	)
)

func init() {
	// Register Prometheus metrics
	prometheus.MustRegister(machinesConnected)
	prometheus.MustRegister(commandsProcessed)
	prometheus.MustRegister(telemetryReceived)
}

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339,
	})

	log.WithFields(logrus.Fields{
		"version": version,
		"commit":  commit,
	}).Info("Starting machine adapter service")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	// Set log level
	level, err := logrus.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	log.SetLevel(level)

	// Connect to database
	db, err := connectDatabase(cfg.Database, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	defer db.Close()

	// Initialize machine registry
	machineRegistry := registry.NewRegistry()
	log.WithField("definitions", len(machineRegistry.ListDefinitions())).Info("Machine registry initialized")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize HTTP server
	router := setupRouter(cfg, log, db, machineRegistry)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Start HTTP server
	go func() {
		log.WithField("port", cfg.Server.Port).Info("HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// TODO: Initialize MQTT client and protocol adapters
	// This will be implemented in subsequent steps

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		log.WithField("signal", sig).Info("Shutdown signal received")
	case <-ctx.Done():
		log.Info("Context cancelled")
	}

	// Graceful shutdown
	log.Info("Starting graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("HTTP server shutdown error")
	}

	log.Info("Machine adapter service stopped")
}

// connectDatabase establishes a database connection.
func connectDatabase(cfg config.DatabaseConfig, log *logrus.Logger) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("Database connection established")
	return db, nil
}

// setupRouter configures the HTTP router.
func setupRouter(cfg *config.Config, log *logrus.Logger, db *sql.DB, reg *registry.Registry) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// Middleware
	router.Use(gin.Recovery())
	router.Use(loggingMiddleware(log))

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		// Check database connection
		if err := db.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "database unavailable",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":      "healthy",
			"version":     version,
			"commit":      commit,
			"environment": cfg.Environment,
		})
	})

	// Metrics endpoint
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API routes
	api := router.Group("/api/v1")
	{
		// Machine registry endpoints
		api.GET("/definitions", func(c *gin.Context) {
			c.JSON(http.StatusOK, reg.ListDefinitions())
		})

		api.GET("/definitions/:id", func(c *gin.Context) {
			id := c.Param("id")
			def, ok := reg.GetDefinition(id)
			if !ok {
				c.JSON(http.StatusNotFound, gin.H{"error": "definition not found"})
				return
			}
			c.JSON(http.StatusOK, def)
		})

		// Machine discovery endpoints (to be implemented)
		api.GET("/discover", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status": "discovery in progress",
				"found":  0,
			})
		})

		// Machine control endpoints (to be implemented)
		api.POST("/machines/:id/connect", func(c *gin.Context) {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": "not yet implemented",
			})
		})

		api.POST("/machines/:id/command", func(c *gin.Context) {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": "not yet implemented",
			})
		})

		api.GET("/machines/:id/status", func(c *gin.Context) {
			c.JSON(http.StatusNotImplemented, gin.H{
				"error": "not yet implemented",
			})
		})
	}

	return router
}

// loggingMiddleware provides request logging.
func loggingMiddleware(log *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Log after request
		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()

		entry := log.WithFields(logrus.Fields{
			"status":     statusCode,
			"method":     method,
			"path":       path,
			"query":      raw,
			"ip":         clientIP,
			"latency_ms": latency.Milliseconds(),
			"user_agent": c.Request.UserAgent(),
		})

		if statusCode >= 500 {
			entry.Error("Server error")
		} else if statusCode >= 400 {
			entry.Warn("Client error")
		} else {
			entry.Info("Request processed")
		}
	}
}