package edge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/adapters"
	"github.com/madfam-org/pravara-mes/packages/sparkplug"
)

// commandOutputs are the metrics published for every command transition.
var commandOutputs = []sparkplug.MetricName{
	sparkplug.MetricCommandLastID, sparkplug.MetricCommandStatus, sparkplug.MetricCommandError,
	sparkplug.MetricJobID, sparkplug.MetricJobStatus,
}

// adapterCommands maps MES-1 commands to the adapters' command names.
var adapterCommands = map[sparkplug.CommandName]string{
	sparkplug.CommandPause:  "pause",
	sparkplug.CommandResume: "resume",
	sparkplug.CommandCancel: "stop",
}

// handleCommand validates a DCMD and acknowledges it with DDATA Command/*.
// Long-running work happens in a goroutine so MQTT delivery is not blocked.
func (n *Node) handleCommand(d *device, payload []byte) {
	log := n.log.WithField("device_id", d.id)
	p, err := sparkplug.Decode(payload)
	if err != nil {
		log.WithError(err).Warn("ignoring undecodable DCMD")
		return
	}
	cmd, err := sparkplug.ParseDeviceCommand(p, n.aliases.ResolverFor(d.id))
	if err != nil {
		log.WithError(err).Warn("rejecting DCMD")
		if cmd.ID != "" {
			n.setCommand(d, cmd.ID, sparkplug.CommandFailed, err.Error(), nil)
		}
		return
	}
	log = log.WithFields(logrus.Fields{"command_id": cmd.ID, "command": cmd.Name})

	d.mu.Lock()
	if cmd.ID == d.lastCommandID {
		d.mu.Unlock()
		log.Info("duplicate DCMD; republishing its status")
		n.publishDevice(d, commandOutputs)
		return
	}
	reason := ""
	switch {
	case !d.online:
		reason = "device offline"
	case cmd.Name == sparkplug.CommandStartJob &&
		(d.startInFlight || d.job.active() || d.values[sparkplug.MetricStateStatus] != string(sparkplug.StatusIdle)):
		reason = "device busy"
	}
	d.mu.Unlock()
	if reason != "" {
		log.WithField("reason", reason).Warn("rejecting DCMD")
		n.setCommand(d, cmd.ID, sparkplug.CommandFailed, reason, nil)
		return
	}

	n.setCommand(d, cmd.ID, sparkplug.CommandAccepted, "", func() {
		if cmd.Name == sparkplug.CommandStartJob {
			d.startInFlight = true
			d.job = jobState{id: jobIDFor(cmd), status: sparkplug.JobQueued}
		}
		if cmd.Name == sparkplug.CommandCancel && d.job.active() {
			d.job.cancelRequested = true
		}
	})
	log.Info("DCMD accepted")
	n.running.Add(1)
	go func() {
		defer n.running.Done()
		var err error
		if cmd.Name == sparkplug.CommandStartJob {
			err = n.startJob(d, cmd)
		} else {
			err = n.mgr.Execute(d.id, adapterCommands[cmd.Name], nil)
		}
		if err != nil {
			log.WithError(err).Warn("DCMD failed")
			n.setCommand(d, cmd.ID, sparkplug.CommandFailed, err.Error(), func() {
				if cmd.Name == sparkplug.CommandStartJob {
					d.startInFlight = false
					d.job.status = sparkplug.JobFailed
				}
			})
			return
		}
		log.Info("DCMD done")
		n.setCommand(d, cmd.ID, sparkplug.CommandDone, "", func() {
			if cmd.Name == sparkplug.CommandStartJob {
				d.startInFlight = false
			}
		})
		d.poke()
	}()
}

// setCommand records a command transition (plus any job change made by
// mutate under the device lock) and publishes it.
func (n *Node) setCommand(d *device, id string, status sparkplug.CommandStatus, errText string, mutate func()) {
	d.mu.Lock()
	d.lastCommandID = id
	if mutate != nil {
		mutate()
	}
	var errValue any
	if errText != "" {
		errValue = errText
	}
	d.values[sparkplug.MetricCommandLastID] = id
	d.values[sparkplug.MetricCommandStatus] = string(status)
	d.values[sparkplug.MetricCommandError] = errValue
	if d.job.id != "" {
		d.values[sparkplug.MetricJobID] = d.job.id
		d.values[sparkplug.MetricJobStatus] = string(d.job.status)
	}
	d.mu.Unlock()
	n.publishDevice(d, commandOutputs)
}

// startJob downloads the artifact, verifies its digest, uploads it and starts
// the print. The digest is checked before anything reaches the printer.
func (n *Node) startJob(d *device, cmd sparkplug.DeviceCommand) error {
	exec, ok := n.mgr.Executor(d.id)
	if !ok {
		return errors.New("device offline")
	}
	runner, ok := exec.(adapters.JobRunner)
	if !ok {
		return errors.New("printer adapter cannot receive print files")
	}
	if !runner.AcceptsMediaType(cmd.ArtifactMediaType) {
		return fmt.Errorf("media type %q is not accepted by this printer", cmd.ArtifactMediaType)
	}
	n.setCommand(d, cmd.ID, sparkplug.CommandRunning, "", nil)

	ctx, cancel := context.WithTimeout(context.Background(), n.cfg.ArtifactTimeout)
	defer cancel()
	path, size, err := n.fetcher.Fetch(ctx, cmd.ArtifactURL, cmd.ArtifactSHA256)
	if err != nil {
		if errors.Is(err, ErrDigestMismatch) {
			return ErrDigestMismatch
		}
		return err
	}
	defer os.Remove(path)

	name := artifactFileName(jobIDFor(cmd), cmd.ArtifactSHA256, cmd.ArtifactMediaType)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	err = runner.UploadFile(ctx, name, f, size)
	f.Close()
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	d.mu.Lock()
	d.job.file = name
	d.job.startedAt = n.now()
	d.mu.Unlock()
	if err := runner.StartPrint(ctx, name); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// jobIDFor is the Job/Id reported for a start_job: the task id when given.
func jobIDFor(cmd sparkplug.DeviceCommand) string {
	if cmd.TaskID != "" {
		return cmd.TaskID
	}
	return cmd.ID
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// artifactFileName is the printer-side file name: job id + digest prefix + extension.
func artifactFileName(jobID, sha, mediaType string) string {
	base := unsafeName.ReplaceAllString(jobID, "_")
	if len(base) > 40 {
		base = base[:40]
	}
	ext := ".gcode"
	if mediaType == adapters.MediaType3MF || mediaType == adapters.MediaType3MFMS {
		ext = ".3mf"
	}
	return "pravara-" + strings.Trim(base, "._-") + "-" + strings.ToLower(sha[:12]) + ext
}
