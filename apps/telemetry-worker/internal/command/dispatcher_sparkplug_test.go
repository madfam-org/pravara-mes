package command

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

const testSHA = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

type fakeSparkplugSender struct {
	mu       sync.Mutex
	sent     []host.Target
	cmds     []sparkplug.DeviceCommand
	delivery host.Delivery
	err      error
}

func (f *fakeSparkplugSender) SendDeviceCommand(t host.Target, c sparkplug.DeviceCommand) (host.Delivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.sent = append(f.sent, t)
	f.cmds = append(f.cmds, c)
	if f.delivery == 0 {
		return host.Published, nil
	}
	return f.delivery, nil
}

func (f *fakeSparkplugSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cmds)
}

// enqueueSparkplug appends a command for a Sparkplug-registered machine.
func (r *streamRig) enqueueSparkplug(t *testing.T, cmdType string, params map[string]interface{}) (uuid.UUID, uuid.UUID) {
	t.Helper()
	tenantID, commandID, machineID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cmd := MachineCommand{CommandID: commandID, MachineID: machineID, Command: cmdType, Parameters: params,
		TaskID: &taskID, IssuedBy: uuid.New(), IssuedAt: time.Now().UTC()}
	payload, err := json.Marshal(cmd)
	require.NoError(t, err)
	require.NoError(t, r.client.XAdd(context.Background(), &redis.XAddArgs{Stream: testStream, Values: map[string]interface{}{
		StreamFieldTenantID: tenantID.String(), StreamFieldCommandID: commandID.String(), StreamFieldPayload: string(payload),
	}}).Err())
	r.ledger.add(LedgerCommand{TenantID: tenantID, CommandID: commandID, MachineID: machineID, TaskID: &taskID,
		CommandType: cmdType, Status: StatusPending, SparkplugEdgeID: "site-north", MachineCode: "VORON-01", TenantSlug: "acme"})
	return tenantID, commandID
}

func startJobParams() map[string]interface{} {
	return map[string]interface{}{
		ParamArtifactURL: "https://prep.example.test/a.gcode", ParamArtifactSHA256: testSHA, ParamArtifactMediaType: "text/x-gcode",
	}
}

func TestDispatcher_SparkplugMachineGetsDCMD(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueueSparkplug(t, string(CommandStartJob), startJobParams())
	sender := &fakeSparkplugSender{}
	d := r.dispatcher("w1", 3)
	d.SetSparkplugSender(sender)
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()

	require.Eventually(t, func() bool { return r.allDeliveredAndAcked(t) }, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, sender.count())
	require.Equal(t, host.Target{Group: "acme", EdgeNodeID: "site-north", DeviceID: "VORON-01"}, sender.sent[0])
	got := sender.cmds[0]
	require.Equal(t, commandID.String(), got.ID)
	require.Equal(t, sparkplug.CommandStartJob, got.Name)
	require.Equal(t, testSHA, got.ArtifactSHA256)
	require.Equal(t, StatusSent, r.ledger.get(commandID).Status)
	published, attempts := r.pub.snapshot()
	require.Empty(t, published, "the legacy /cmd channel must not be used")
	require.Zero(t, attempts)
}

func TestDispatcher_SparkplugDeferredIsStillSent(t *testing.T) {
	r := newStreamRig(t)
	_, commandID := r.enqueueSparkplug(t, string(CommandPause), nil)
	d := r.dispatcher("w1", 3)
	d.SetSparkplugSender(&fakeSparkplugSender{delivery: host.Deferred})
	require.NoError(t, d.Start(context.Background()))
	defer d.Stop()
	require.Eventually(t, func() bool { return r.allDeliveredAndAcked(t) }, 3*time.Second, 10*time.Millisecond)
	require.Equal(t, StatusSent, r.ledger.get(commandID).Status)
}

func TestDispatcher_SparkplugFailures(t *testing.T) {
	cases := []struct {
		name    string
		cmdType string
		params  map[string]interface{}
		sender  *fakeSparkplugSender
		want    string
		final   string
	}{
		{"host disabled", string(CommandPause), nil, nil, "primary host is not enabled", StatusFailed},
		{"unsupported command", string(CommandHome), nil, &fakeSparkplugSender{}, "not supported by Sparkplug devices", StatusFailed},
		{"start_job without artifact", string(CommandStartJob), nil, &fakeSparkplugSender{}, "Artifact/Url", StatusFailed},
		{"broker down is retried", string(CommandPause), nil, &fakeSparkplugSender{err: host.ErrNotConnected}, "not connected", StatusPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newStreamRig(t)
			_, commandID := r.enqueueSparkplug(t, c.cmdType, c.params)
			d := r.dispatcher("w1", 3)
			if c.sender != nil {
				d.SetSparkplugSender(c.sender)
			}
			require.NoError(t, d.Start(context.Background()))
			defer d.Stop()
			require.Eventually(t, func() bool {
				r.ledger.mu.Lock()
				defer r.ledger.mu.Unlock()
				return strings.Contains(r.ledger.errors[commandID], c.want)
			}, 3*time.Second, 10*time.Millisecond)
			if c.final == StatusFailed {
				require.Equal(t, StatusFailed, r.ledger.get(commandID).Status)
			} else {
				require.NotEqual(t, StatusFailed, r.ledger.get(commandID).Status)
			}
		})
	}
}

func TestBuildSparkplugCommand(t *testing.T) {
	id, task := uuid.New(), uuid.New()
	cmd, err := BuildSparkplugCommand("stop", id, &task, nil)
	require.NoError(t, err)
	require.Equal(t, sparkplug.CommandCancel, cmd.Name)
	require.Equal(t, task.String(), cmd.TaskID)

	cmd, err = BuildSparkplugCommand(string(CommandStartJob), id, nil, startJobParams())
	require.NoError(t, err)
	require.Equal(t, "text/x-gcode", cmd.ArtifactMediaType)
	require.Empty(t, cmd.TaskID)

	bad := startJobParams()
	bad[ParamArtifactURL] = "http://prep.example.test/a.gcode"
	_, err = BuildSparkplugCommand(string(CommandStartJob), id, nil, bad)
	require.Error(t, err)
	_, err = BuildSparkplugCommand(string(CommandCalibrate), id, nil, nil)
	require.Error(t, err)
}
