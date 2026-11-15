// Package mqtttest holds helpers for tests that run an in-process MQTT broker.
package mqtttest

import (
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

// CloseBroker shuts a mochi-mqtt server down without the deadlock in its own
// Close. The same helper lives in apps/machine-adapter/internal/simulator;
// the two apps are separate Go modules.
//
// mochi-mqtt v2.7.9 (the latest release when this was written) closes a
// listener's clients through Clients.GetByListener, which takes the registry's
// read lock and then calls Clients.Len, which takes it again. sync.RWMutex is
// not re-entrant: when a writer (a client being added or deleted, which is
// what a connect or a disconnect does) asks for the lock between the two
// reads, the second read queues behind the writer and the writer waits for
// the first read. Server.Close then never returns. In CI this hung whole test
// binaries until the 10-minute timeout whenever a client connected or dropped
// while a broker was shutting down.
//
// A listener calls its close-clients function only once (an atomic flag). So
// the listeners are closed here first, with a function that reads the
// registry through GetAll (one read lock), and Server.Close then finds every
// listener already ended and never reaches GetByListener. The rest of
// Close runs as usual: the done channel, the hooks, the wait for client
// goroutines.
func CloseBroker(s *mqtt.Server) error {
	if s == nil {
		return nil
	}
	s.Listeners.CloseAll(func(listenerID string) {
		for _, cl := range s.Clients.GetAll() {
			if cl.Net.Listener == listenerID && !cl.Closed() {
				_ = s.DisconnectClient(cl, packets.ErrServerShuttingDown)
			}
		}
	})
	return s.Close()
}
