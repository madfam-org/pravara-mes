// Package main provides the entry point for the telemetry worker.
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/db"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/dlq"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/mqtt"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
)

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{})

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

	log.WithFields(logrus.Fields{
		"environment": cfg.Environment,
		"mqtt_broker": cfg.MQTT.BrokerURL(),
	}).Info("Starting telemetry worker")

	// Initialize database store
	store, err := db.NewStore(&cfg.Database)
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}
	defer store.Close()

	log.Info("Connected to database")

	// Initialize Redis event publisher (optional - for real-time updates)
	var publisher *mqtt.EventPublisher
	redisURL := cfg.Redis.URL()
	if redisURL != "" {
		var err error
		publisher, err = mqtt.NewEventPublisher(mqtt.PublisherConfig{
			RedisURL: redisURL,
		}, log)
		if err != nil {
			log.WithError(err).Warn("Failed to connect to Redis for event publishing, real-time updates disabled")
		} else {
			defer publisher.Close()
			log.Info("Event publisher connected to Redis")
		}
	}

	// Initialize Dead-Letter Queue if Redis is available
	var deadLetterQueue *dlq.DLQ
	if publisher != nil {
		deadLetterQueue = dlq.NewDLQ(
			publisher.GetRedisClient(),
			log,
			"pravara:telemetry:dlq",
			cfg.Worker.DLQMaxItems,
		)
		log.Info("Dead-letter queue initialized")
	}

	// Initialize MQTT handler
	handler := mqtt.NewHandler(cfg, store, log)

	// Set event publisher if available
	if publisher != nil {
		handler.SetPublisher(publisher)
	}

	// Set dead-letter queue if available
	if deadLetterQueue != nil {
		handler.SetDLQ(deadLetterQueue)
	}

	// Connect to MQTT broker
	if err := handler.Connect(); err != nil {
		log.WithError(err).Fatal("Failed to connect to MQTT broker")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start HTTP server for metrics and health checks
	metricsAddr := ":4502"
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	metricsServer := &http.Server{
		Addr:         metricsAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.WithField("addr", metricsAddr).Info("Metrics server listening")
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Error("Metrics server error")
		}
	}()

	// Start background goroutine to collect database stats
	go collectDBStats(ctx, store, log)

	// Start processing messages
	if err := handler.Start(ctx); err != nil {
		log.WithError(err).Fatal("Failed to start handler")
	}

	// Initialize and start command dispatcher if enabled
	var dispatcher *command.Dispatcher
	var ackHandler *command.AckHandler
	if cfg.Command.Enabled && publisher != nil {
		dispatcher = command.NewDispatcher(
			publisher.GetRedisClient(),
			handler.GetMQTTClient(),
			log,
		)

		if err := dispatcher.Start(ctx); err != nil {
			log.WithError(err).Error("Failed to start command dispatcher")
			// Continue without command dispatch - not fatal
		} else {
			log.Info("Command dispatcher started")
		}

		// Initialize and start ack handler for command acknowledgments
		ackHandler = command.NewAckHandler(
			handler.GetMQTTClient(),
			publisher,
			log,
			cfg.MQTT.TopicRoot,
		)

		if err := ackHandler.Start(ctx); err != nil {
			log.WithError(err).Error("Failed to start ack handler")
			// Continue without ack handling - not fatal
		} else {
			log.Info("Command ack handler started")
		}
	} else if !cfg.Command.Enabled {
		log.Info("Command dispatch disabled by configuration")
	} else {
		log.Warn("Command dispatch disabled - Redis publisher not available")
	}

	log.Info("Telemetry worker is running")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Info("Shutdown signal received, stopping worker...")

	// Cancel context to stop workers
	cancel()

	// Stop ack handler first
	if ackHandler != nil {
		ackHandler.Stop()
		log.Info("Command ack handler stopped")
	}

	// Stop command dispatcher (before MQTT handler)
	if dispatcher != nil {
		dispatcher.Stop()
		log.Info("Command dispatcher stopped")
	}

	// Stop the handler gracefully
	handler.Stop()

	// Shutdown metrics server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Metrics server shutdown error")
	}

	log.Info("Telemetry worker stopped")
}

// collectDBStats periodically collects and reports database connection pool statistics.
func collectDBStats(ctx context.Context, store *db.Store, log *logrus.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := store.Stats()
			observability.DBConnectionsOpen.Set(float64(stats.OpenConnections))
			observability.DBConnectionsInUse.Set(float64(stats.InUse))
		}
	}
}
