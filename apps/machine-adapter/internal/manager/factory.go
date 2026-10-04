package manager

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/adapters"
	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// DefaultExecutorFactory builds the network printer adapters the edge node
// dispatches to: Moonraker (Klipper) and Bambu Lab LAN mode. Other protocol
// adapters in this module are not wired to the edge node yet.
func DefaultExecutorFactory(_ context.Context, spec MachineSpec, def *registry.MachineDefinition, log *logrus.Logger) (CommandExecutor, error) {
	if def == nil {
		def = &registry.MachineDefinition{Protocol: registry.Protocol(spec.Protocol)}
	}
	c := spec.Conn
	switch registry.Protocol(spec.Protocol) {
	case registry.ProtocolMoonraker:
		a := adapters.NewMoonrakerAdapter(def, log)
		if err := a.Connect(c.Host, c.Port, c.Secret); err != nil {
			return nil, err
		}
		return a, nil
	case registry.ProtocolBambuMQTT:
		if c.Serial == "" || c.Secret == "" {
			return nil, fmt.Errorf("bambu LAN connection needs the printer serial and access code")
		}
		a := adapters.NewBambuAdapter(def, log)
		a.TLSPinSHA256 = c.TLSPinSHA256
		a.MQTTPort = c.Port
		a.FTPSPort = c.FTPSPort
		if err := a.Connect(c.Host, c.Secret, c.Serial); err != nil {
			return nil, err
		}
		return a, nil
	default:
		return nil, fmt.Errorf("protocol %q is not supported by the edge node", spec.Protocol)
	}
}
