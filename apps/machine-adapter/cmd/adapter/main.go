// Package main is the machine adapter service. On a site box it runs as the
// Sparkplug B Edge Node for the site's printers (see deploy/edge/README.md).
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/config"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/edge"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/manager"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	configPath := flag.String("config", "", "path to the config file (default: search config.yaml)")
	enroll := flag.Bool("enroll", false, "register this box's self-generated broker credential with pravara and wait for approval, then exit")
	rotate := flag.Bool("rotate-credential", false, "with -enroll: generate a new credential even if one exists")
	flag.Parse()

	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{TimestampFormat: time.RFC3339})
	log.WithFields(logrus.Fields{"version": version, "commit": commit}).Info("Starting machine adapter service")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}
	if level, err := logrus.ParseLevel(cfg.LogLevel); err == nil {
		log.SetLevel(level)
	}

	if *enroll {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		err := edge.Enroll(ctx, cfg.Edge, edge.EnrollOptions{Out: os.Stdout, Rotate: *rotate})
		stop()
		if err != nil {
			log.WithError(err).Fatal("Edge enrollment failed")
		}
		return
	}

	reg := registry.NewRegistry()
	mgr := manager.NewManager(reg, nil, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var node *edge.Node
	if cfg.Edge.Enabled {
		opts := edge.Options{}
		if cfg.Edge.Simulate {
			sims, simOpts, err := startSimulators(ctx, cfg.Edge, log)
			if err != nil {
				log.WithError(err).Fatal("Failed to start printer simulators")
			}
			defer sims.Close()
			opts = simOpts
		}
		node, err = edge.NewNode(cfg.Edge, reg, mgr, log, opts)
		if err != nil {
			log.WithError(err).Fatal("Invalid edge node configuration")
		}
	} else {
		log.Warn("edge.enabled is false: no printers are connected")
	}

	srv := &http.Server{
		Addr:         net.JoinHostPort(cfg.Server.Host, strconv.Itoa(cfg.Server.Port)),
		Handler:      setupRouter(cfg, reg, node),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}
	go func() {
		log.WithField("addr", srv.Addr).Info("HTTP server starting")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	nodeDone := make(chan struct{})
	go func() {
		defer close(nodeDone)
		if node != nil {
			if err := node.Run(ctx); err != nil {
				log.WithError(err).Error("edge node stopped")
			}
		}
	}()

	<-ctx.Done()
	log.Info("Shutdown signal received")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("HTTP server shutdown error")
	}
	select {
	case <-nodeDone:
	case <-shutdownCtx.Done():
		log.Warn("edge node did not stop in time")
	}
	mgr.Stop()
	log.Info("Machine adapter service stopped")
}

// setupRouter serves the local, read-only HTTP surface: health, metrics,
// machine definitions and device status. Machines are declared in the config
// file and commanded only through Sparkplug DCMD over the authenticated broker
// session, so there are no connect or command endpoints.
func setupRouter(cfg *config.Config, reg *registry.Registry, node *edge.Node) *gin.Engine {
	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		body := gin.H{"status": "healthy", "version": version, "commit": commit, "environment": cfg.Environment}
		if node != nil {
			st := node.Status()
			body["edge"] = st
			if !st.Born {
				body["status"] = "degraded"
			}
		}
		c.JSON(http.StatusOK, body)
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := router.Group("/api/v1")
	api.GET("/definitions", func(c *gin.Context) {
		c.JSON(http.StatusOK, reg.ListDefinitions())
	})
	api.GET("/definitions/:id", func(c *gin.Context) {
		def, ok := reg.GetDefinition(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "definition not found"})
			return
		}
		c.JSON(http.StatusOK, def)
	})
	api.GET("/devices", func(c *gin.Context) {
		if node == nil {
			c.JSON(http.StatusOK, gin.H{"devices": []edge.DeviceStatus{}})
			return
		}
		c.JSON(http.StatusOK, node.Status())
	})
	return router
}
