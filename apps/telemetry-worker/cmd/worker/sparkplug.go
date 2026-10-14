package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/db"
	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/observability"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// startSparkplugHost runs the Sparkplug B primary host application when it
// is enabled: STATE, births/deaths/data of registered edge nodes, live
// machine state and DCMD delivery for Sparkplug-registered machines (wired
// into the command dispatcher when there is one). The returned function
// waits for the host to publish its offline STATE after ctx is cancelled.
func startSparkplugHost(ctx context.Context, cfg *config.Config, store *db.Store, dispatcher *command.Dispatcher, log *logrus.Logger) func() {
	sc := cfg.Sparkplug
	if !sc.Enabled {
		log.Info("Sparkplug primary host disabled by configuration")
		return func() {}
	}
	clientCfg, err := sparkplugClientConfig(sc)
	if err != nil {
		log.WithError(err).Error("Sparkplug primary host not started: invalid configuration")
		return func() {}
	}

	spStore := db.NewSparkplugStore(store, db.NewCommandLedger(store), log)
	engine := host.New(host.Options{
		HostID:  sc.HostID,
		Store:   spStore,
		Logger:  slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("component", "sparkplug_host"),
		Observe: func(event string) { observability.SparkplugHostEvents.WithLabelValues(event).Inc() },
	})
	if dispatcher != nil {
		dispatcher.SetSparkplugSender(engine)
	} else {
		log.Warn("Sparkplug primary host running without the command dispatcher: no DCMD delivery")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.Run(ctx, clientCfg)
	}()
	log.WithFields(logrus.Fields{"host_id": engine.HostID(), "broker": sc.BrokerURL}).Info("Sparkplug primary host started")
	return func() { <-done }
}

func sparkplugClientConfig(sc config.SparkplugConfig) (host.ClientConfig, error) {
	cc := host.ClientConfig{BrokerURL: sc.BrokerURL, ClientID: sc.ClientID, Username: sc.Username, Password: sc.Password}
	switch {
	case strings.HasPrefix(sc.BrokerURL, "ssl://"), strings.HasPrefix(sc.BrokerURL, "tls://"):
		tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: sc.TLSServerName}
		if sc.CAFile != "" {
			pem, err := os.ReadFile(sc.CAFile)
			if err != nil {
				return cc, fmt.Errorf("read CA file: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return cc, fmt.Errorf("CA file holds no PEM certificate")
			}
			tc.RootCAs = pool
		}
		cc.TLSConfig = tc
	case strings.HasPrefix(sc.BrokerURL, "tcp://"):
	default:
		return cc, fmt.Errorf("broker_url must start with ssl://, tls:// or tcp://")
	}
	if sc.Username == "" {
		return cc, fmt.Errorf("the host needs its own broker username")
	}
	return cc, nil
}
