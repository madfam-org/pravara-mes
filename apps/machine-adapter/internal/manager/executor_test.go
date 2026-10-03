package manager

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// recordingExecutor is a G-code executor that records what it was asked to send.
type recordingExecutor struct {
	mu           sync.Mutex
	sent         []string
	disconnected bool
}

func (r *recordingExecutor) SendCommand(cmd string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, cmd)
	return nil
}

func (r *recordingExecutor) Disconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disconnected = true
	return nil
}

// mappingExecutor is a protocol-aware executor.
type mappingExecutor struct {
	recordingExecutor
	mapped []string
}

func (m *mappingExecutor) MapCommand(cmd string, _ map[string]interface{}) (interface{}, error) {
	if cmd == "explode" {
		return nil, errors.New("unsupported")
	}
	m.mapped = append(m.mapped, cmd)
	return nil, nil
}

func TestConnectMachineWiresExecutor(t *testing.T) {
	exec := &mappingExecutor{}
	var gotSpec MachineSpec
	var gotDef *registry.MachineDefinition
	factory := func(_ context.Context, spec MachineSpec, def *registry.MachineDefinition, _ *logrus.Logger) (CommandExecutor, error) {
		gotSpec, gotDef = spec, def
		return exec, nil
	}
	m := NewManager(registry.NewRegistry(), factory, newTestLogger())

	a, err := m.ConnectMachine(context.Background(), MachineSpec{MachineID: "VORON-01", DefinitionID: "voron_2_4", TenantID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "moonraker", a.Protocol, "protocol comes from the definition")
	assert.Equal(t, "moonraker", gotSpec.Protocol)
	require.NotNil(t, gotDef)
	assert.Equal(t, "Voron Design", gotDef.Manufacturer)

	got, ok := m.Executor("VORON-01")
	require.True(t, ok, "executor must be wired")
	assert.Same(t, exec, got)

	require.NoError(t, m.Execute("VORON-01", "pause", nil))
	assert.Equal(t, []string{"pause"}, exec.mapped)
	assert.Error(t, m.Execute("VORON-01", "explode", nil))

	_, err = m.ConnectMachine(context.Background(), MachineSpec{MachineID: "VORON-01", DefinitionID: "voron_2_4"})
	assert.Error(t, err, "double connect")

	require.NoError(t, m.DisconnectMachine("VORON-01"))
	assert.True(t, exec.disconnected)
	assert.Error(t, m.Execute("VORON-01", "pause", nil))
	assert.Error(t, m.DisconnectMachine("VORON-01"))
}

func TestExecuteFallsBackToGCode(t *testing.T) {
	exec := &recordingExecutor{}
	factory := func(context.Context, MachineSpec, *registry.MachineDefinition, *logrus.Logger) (CommandExecutor, error) {
		return exec, nil
	}
	m := NewManager(registry.NewRegistry(), factory, newTestLogger())
	_, err := m.ConnectMachine(context.Background(), MachineSpec{MachineID: "M1", Protocol: "marlin"})
	require.NoError(t, err)
	require.NoError(t, m.Execute("M1", "resume", nil))
	assert.Equal(t, []string{"M24"}, exec.sent)
	assert.Error(t, m.Execute("M1", "no_such_command", nil))
	m.Stop()
	assert.True(t, exec.disconnected)
	assert.Empty(t, m.ListConnected())
}

func TestConnectMachineErrors(t *testing.T) {
	m := NewManager(registry.NewRegistry(), nil, newTestLogger())
	ctx := context.Background()
	_, err := m.ConnectMachine(ctx, MachineSpec{})
	assert.Error(t, err)
	_, err = m.ConnectMachine(ctx, MachineSpec{MachineID: "x", DefinitionID: "no_such_definition"})
	assert.Error(t, err)
	_, err = m.ConnectMachine(ctx, MachineSpec{MachineID: "x"})
	assert.Error(t, err, "no protocol")
	_, err = m.ConnectMachine(ctx, MachineSpec{MachineID: "x", Protocol: "grbl"})
	assert.ErrorContains(t, err, "not supported by the edge node")
	_, err = m.ConnectMachine(ctx, MachineSpec{MachineID: "x", Protocol: "bambu_mqtt"})
	assert.ErrorContains(t, err, "serial and access code")
}
