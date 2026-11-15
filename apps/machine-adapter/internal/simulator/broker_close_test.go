package simulator

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
)

func newQuietBroker(t *testing.T) *mqtt.Server {
	t.Helper()
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	s := mqtt.New(&mqtt.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	l := listeners.NewTCP(listeners.Config{ID: "tcp", Address: fmt.Sprintf("127.0.0.1:%d", port)})
	if err := s.AddListener(l); err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(); err != nil {
		t.Fatal(err)
	}
	return s
}

// churnRegistry asks for the client registry's write lock in a tight loop,
// which is what clients connecting and dropping do. It returns a stop
// function.
func churnRegistry(s *mqtt.Server) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					s.Clients.Delete("no-such-client")
				}
			}
		}()
	}
	return func() { close(done); wg.Wait() }
}

// closesWithin runs shut down on fresh brokers under registry churn and
// reports how many of rounds did not return within limit.
func closesWithin(t *testing.T, rounds int, limit time.Duration, shutDown func(*mqtt.Server) error) (hung int) {
	t.Helper()
	for i := 0; i < rounds; i++ {
		s := newQuietBroker(t)
		stop := churnRegistry(s)
		finished := make(chan struct{})
		go func() { _ = shutDown(s); close(finished) }()
		select {
		case <-finished:
			stop()
		case <-time.After(limit):
			// Deadlocked: the churn goroutines are stuck on the same lock,
			// so they are left behind with this broker.
			hung++
		}
	}
	return hung
}

// CloseBroker must return while clients are connecting and dropping. With
// the library's own Close this hangs (see the next test).
func TestCloseBrokerDoesNotDeadlockUnderRegistryChurn(t *testing.T) {
	if hung := closesWithin(t, 300, 5*time.Second, CloseBroker); hung > 0 {
		t.Fatalf("CloseBroker did not return in %d of 300 rounds", hung)
	}
}

func TestCloseBrokerNilAndRepeat(t *testing.T) {
	if err := CloseBroker(nil); err != nil {
		t.Fatalf("nil server: %v", err)
	}
	s := newQuietBroker(t)
	if err := CloseBroker(s); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Demonstrates the library defect CloseBroker works around. It leaves
// deadlocked goroutines behind by design, so it runs only on request:
//
//	MOCHI_CLOSE_DEADLOCK_DEMO=1 go test ./internal/simulator/ -run LibraryCloseDeadlocks -v
//
// If it ever reports zero hangs, the library has been fixed and CloseBroker
// can go back to calling Close alone.
func TestLibraryCloseDeadlocksUnderRegistryChurn(t *testing.T) {
	if os.Getenv("MOCHI_CLOSE_DEADLOCK_DEMO") == "" {
		t.Skip("set MOCHI_CLOSE_DEADLOCK_DEMO=1 to reproduce the mochi-mqtt Close deadlock")
	}
	hung := closesWithin(t, 300, 2*time.Second, func(s *mqtt.Server) error { return s.Close() })
	t.Logf("Server.Close hung in %d of 300 rounds", hung)
	if hung == 0 {
		t.Fatal("Server.Close no longer deadlocks: mochi-mqtt may have fixed GetByListener; CloseBroker can be simplified")
	}
}
