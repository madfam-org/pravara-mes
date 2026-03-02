package db

import (
	"testing"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

func TestNewAckStoreAdapter(t *testing.T) {
	// Test with nil store
	adapter := NewAckStoreAdapter(nil)

	if adapter == nil {
		t.Fatal("NewAckStoreAdapter returned nil")
	}

	if adapter.store != nil {
		t.Error("store should be nil when created with nil")
	}
}

func TestAckStoreAdapter_ImplementsInterface(t *testing.T) {
	// Compile-time check that AckStoreAdapter implements command.AckStore
	var _ command.AckStore = (*AckStoreAdapter)(nil)
}

func TestAckStoreAdapter_InterfaceMethods(t *testing.T) {
	// Verify interface methods exist (compile-time check via type assertion)
	adapter := &AckStoreAdapter{}

	// This test verifies the interface is implemented correctly
	// The actual methods require a real store to work
	var store command.AckStore = adapter

	if store == nil {
		t.Fatal("adapter should implement AckStore interface")
	}
}
