package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/edge"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/manager"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/simulator"
)

// simulators holds the running printer simulators of simulate mode.
type simulators struct {
	closers []func()
}

func (s *simulators) Close() {
	for _, c := range s.closers {
		c()
	}
}

// startSimulators starts one local protocol simulator per configured device
// and points the device at it. No printer on the network is contacted.
func startSimulators(ctx context.Context, cfg edge.Config, log *logrus.Logger) (*simulators, edge.Options, error) {
	reg := registry.NewRegistry()
	sims := &simulators{}
	opts := edge.Options{Connections: map[string]manager.ConnectionParams{}, Secrets: map[string]string{}}
	for _, d := range cfg.Devices {
		protocol := d.Protocol
		if protocol == "" {
			def, ok := reg.GetDefinition(d.Definition)
			if !ok {
				sims.Close()
				return nil, opts, fmt.Errorf("device %s: unknown definition %q", d.DeviceID, d.Definition)
			}
			protocol = string(def.Protocol)
		}
		var tick func()
		switch registry.Protocol(protocol) {
		case registry.ProtocolMoonraker:
			m := simulator.NewMoonraker("")
			m.Start()
			m.SetSpoolMaterial("PLA")
			host, port := m.HostPort()
			opts.Connections[d.DeviceID] = manager.ConnectionParams{Host: host, Port: port}
			opts.Secrets[d.DeviceID] = ""
			sims.closers = append(sims.closers, m.Close)
			tick = m.Tick
		case registry.ProtocolBambuMQTT:
			serial := "SIM" + d.DeviceID
			b := simulator.NewBambu(serial, "simulator")
			if err := b.Start(); err != nil {
				sims.Close()
				return nil, opts, fmt.Errorf("device %s: %w", d.DeviceID, err)
			}
			b.SetTrays("PLA", "PETG", "", "")
			mqttPort, ftpsPort := b.Ports()
			opts.Connections[d.DeviceID] = manager.ConnectionParams{
				Host: b.Host(), Port: mqttPort, FTPSPort: ftpsPort, Secret: "simulator", Serial: serial, TLSPinSHA256: b.CertSHA256(),
			}
			opts.Secrets[d.DeviceID] = "simulator"
			sims.closers = append(sims.closers, b.Close)
			tick = b.Tick
		default:
			sims.Close()
			return nil, opts, fmt.Errorf("device %s: no simulator for protocol %q", d.DeviceID, protocol)
		}
		go func(tick func()) {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					tick()
				}
			}
		}(tick)
		log.WithFields(logrus.Fields{"device_id": d.DeviceID, "protocol": protocol}).Warn("simulate mode: device replaced by a local simulator")
	}
	return sims, opts, nil
}
