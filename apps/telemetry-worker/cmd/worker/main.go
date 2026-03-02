// Package main provides the entry point for the telemetry worker.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/db"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/mqtt"
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

	// Initialize MQTT handler
	handler := mqtt.NewHandler(cfg, store, log)

	// Connect to MQTT broker
	if err := handler.Connect(); err != nil {
		log.WithError(err).Fatal("Failed to connect to MQTT broker")
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start processing messages
	if err := handler.Start(ctx); err != nil {
		log.WithError(err).Fatal("Failed to start handler")
	}

	log.Info("Telemetry worker is running")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Info("Shutdown signal received, stopping worker...")

	// Cancel context to stop workers
	cancel()

	// Stop the handler gracefully
	handler.Stop()

	log.Info("Telemetry worker stopped")
}
