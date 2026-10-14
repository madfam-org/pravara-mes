package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/assetshells"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/fabprep"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/matchmaking"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// MatchRecord is stored in dispatch_jobs.match_result.
type MatchRecord struct {
	Product   ProductSpec           `json:"product"`
	Selected  matchmaking.Candidate `json:"selected"`
	Result    matchmaking.Result    `json:"result"`
	MatchedAt time.Time             `json:"matched_at"`
	// Rechecks are the dispatch-time re-evaluations of the reserved machine.
	Rechecks []matchmaking.Candidate `json:"rechecks,omitempty"`
}

// RenderBundle is stored in dispatch_jobs.render_bundle.
type RenderBundle struct {
	Part            string `json:"part"`
	URL             string `json:"url"`
	SHA256          string `json:"sha256"`
	MediaType       string `json:"media_type"`
	InstanceID      string `json:"instance_id"`
	VariablesURL    string `json:"variables_url"`
	SidecarSHA256   string `json:"sidecar_sha256"`
	VariablesSHA256 string `json:"variables_sha256"`
	TreeSHA256      string `json:"tree_sha256"`
	Complete        bool   `json:"complete"`
	Engine          string `json:"engine"`
}

// SliceRecord is stored in dispatch_jobs.slice_result. Signed URLs are not
// stored: they expire, and a fresh one is read right before enqueueing.
type SliceRecord struct {
	JobID           string                               `json:"job_id"`
	IdempotencyKey  string                               `json:"idempotency_key"`
	Status          string                               `json:"status"`
	Profiles        map[string]fabrication.ProfileDigest `json:"profiles,omitempty"`
	OutputSHA256    string                               `json:"output_sha256,omitempty"`
	OutputMediaType string                               `json:"output_media_type,omitempty"`
	OutputBytes     int64                                `json:"output_bytes,omitempty"`
	VariablesSHA256 string                               `json:"slicer_variables_sha256,omitempty"`
	GcodeSHA256     string                               `json:"gcode_sha256,omitempty"`
	EffectiveSHA256 string                               `json:"effective_sha256,omitempty"`
	Slicer          string                               `json:"slicer,omitempty"`
	Estimates       map[string]any                       `json:"estimates,omitempty"`
}

func decode[T any](raw json.RawMessage) (*T, error) {
	var v T
	if len(raw) == 0 {
		return nil, errors.New("missing")
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func encode(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// inTenant runs fn in its own tenant transaction.
func (s *Service) inTenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	return db.RunInTenantTx(ctx, s.Pool, tenantID.String(), fn)
}

// advance runs one hop of d and returns it with its new state. The caller
// saves the result.
func (s *Service) advance(ctx context.Context, d *repositories.DispatchJob) *stepError {
	switch d.Status {
	case repositories.DispatchQueued:
		return s.stageMatch(ctx, d)
	case repositories.DispatchReserved:
		return s.stageRender(ctx, d)
	case repositories.DispatchRendered:
		return s.stageSliceSubmit(ctx, d)
	case repositories.DispatchSlicing:
		return s.stageSlicePoll(ctx, d)
	case repositories.DispatchSliced, repositories.DispatchEnqueuing:
		return s.stageEnqueue(ctx, d)
	case repositories.DispatchCommandEnqueued:
		return s.stageAwaitCompletion(ctx, d)
	}
	return terminal("unknown_status", "dispatch status %q", d.Status)
}

func (s *Service) loadTask(ctx context.Context, d *repositories.DispatchJob) (*repositories.TaskContext, *ProductSpec, *stepError) {
	var tc *repositories.TaskContext
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		tc, err = s.Sources.TaskContext(ctx, d.TaskID)
		return err
	})
	if err != nil {
		return nil, nil, retryable("database", "%v", err)
	}
	if tc == nil {
		return nil, nil, terminal("task_not_found", "task %s no longer exists", d.TaskID)
	}
	spec, serr := s.resolveProduct(ctx, tc)
	return tc, spec, serr
}

// stageMatch: requirements → match → reservation.
func (s *Service) stageMatch(ctx context.Context, d *repositories.DispatchJob) *stepError {
	_, spec, serr := s.loadTask(ctx, d)
	if serr != nil {
		return serr
	}
	profile, serr := s.requirementProfile(ctx, spec.TypeShellID)
	if serr != nil {
		return serr
	}
	catalog, source := s.catalog(ctx)
	if catalog == nil {
		return retryable("slicing_catalog_unavailable", "%s", source)
	}
	var res *matchmaking.Result
	var reserved *matchmaking.Candidate
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		res, err = s.rank(ctx, d.TenantID, profile, spec, catalog, &d.ID)
		if err != nil {
			return err
		}
		for i := range res.Candidates {
			c := &res.Candidates[i]
			if !c.Eligible {
				continue
			}
			_, _, err := s.Dispatch.AcquireReservation(ctx, d.TenantID, c.MachineID, d.ID, s.cfg.ReservationTTL)
			if errors.Is(err, repositories.ErrMachineReserved) {
				c.Eligible = false
				c.Checks = append(c.Checks, matchmaking.Check{Name: "reservation", Result: matchmaking.Fail,
					Detail: "reserved by another dispatch while matching"})
				continue
			}
			if err != nil {
				return err
			}
			reserved = c
			return nil
		}
		return nil
	})
	if err != nil {
		return retryable("database", "%v", err)
	}
	d.Requirements = encode(profile)
	d.TypeShellID, d.Part = spec.TypeShellID, spec.Part
	rec := MatchRecord{Product: *spec, Result: *res, MatchedAt: s.now()}
	if reserved == nil {
		d.MatchResult = encode(rec)
		serr := retryable("no_eligible_machine", "%s", summarize(res))
		serr.wait = true
		return serr
	}
	rec.Selected = *reserved
	d.MatchResult = encode(rec)
	id := reserved.MachineID
	d.MachineID = &id
	d.Status = repositories.DispatchReserved
	return nil
}

// summarize names why no machine qualified (first failing check per machine).
func summarize(res *matchmaking.Result) string {
	if len(res.Candidates) == 0 {
		return "the tenant has no registered machines"
	}
	var parts []string
	for _, c := range res.Candidates {
		for _, ch := range c.Checks {
			if ch.Result == matchmaking.Fail {
				parts = append(parts, fmt.Sprintf("%s: %s (%s)", c.MachineCode, ch.Name, ch.Detail))
				break
			}
		}
	}
	return strings.Join(parts, "; ")
}

// holdReservation extends the dispatch's reservation; when it was lost, the
// dispatch goes back to matching.
func (s *Service) holdReservation(ctx context.Context, d *repositories.DispatchJob, ttl time.Duration) *stepError {
	var held bool
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		held, err = s.Dispatch.ExtendReservation(ctx, d.ID, ttl)
		return err
	})
	if err != nil {
		return retryable("database", "%v", err)
	}
	if !held {
		s.rematch(d)
		return retryable("reservation_lost", "the machine reservation expired; matching again")
	}
	return nil
}

// rematch sends a dispatch back to matching, keeping the render (it does not
// depend on the machine) and dropping the slice (it does).
func (s *Service) rematch(d *repositories.DispatchJob) {
	d.MachineID = nil
	d.SliceResult = nil
	d.CommandID = nil
	d.Status = repositories.DispatchQueued
}

// stageRender: yantra4d render + GOC-1 sidecar, checked against the type shell.
func (s *Service) stageRender(ctx context.Context, d *repositories.DispatchJob) *stepError {
	if serr := s.holdReservation(ctx, d, s.cfg.ReservationTTL); serr != nil {
		return serr
	}
	if len(d.RenderBundle) > 0 { // rendered before a re-match
		d.Status = repositories.DispatchRendered
		return nil
	}
	_, spec, serr := s.loadTask(ctx, d)
	if serr != nil {
		return serr
	}
	resp, err := s.Renderer.Render(ctx, yantra4d.RenderRequest{Project: spec.Cartridge, Mode: spec.Mode,
		Parameters: spec.Parameters, ExportFormat: s.cfg.RenderFormat})
	if err != nil {
		return fromRemote("render_failed", err)
	}
	part, serr := pickPart(resp.Parts, spec.Part)
	if serr != nil {
		return serr
	}
	sidecar, sidecarSHA, err := s.Renderer.FetchSidecar(ctx, *part)
	if err != nil {
		return fromRemote("render_sidecar_invalid", err)
	}
	tree16 := assetshells.TypeShellTree16(d.TypeShellID)
	if tree16 == "" || !strings.HasPrefix(sidecar.Generator.Source.TreeSHA256, tree16) {
		return terminal("design_revision_mismatch",
			"rendered design tree %s does not match type shell %s; the requirements belong to another revision",
			sidecar.Generator.Source.TreeSHA256, d.TypeShellID)
	}
	if sidecar.Generator.Cartridge != spec.Cartridge {
		return terminal("render_cartridge_mismatch", "sidecar cartridge %q, expected %q", sidecar.Generator.Cartridge, spec.Cartridge)
	}
	switch part.MediaType {
	case "model/3mf", "model/stl":
	default:
		return terminal("render_format_unsliceable", "fabrication-prep slices model/stl or model/3mf, got %q", part.MediaType)
	}
	d.RenderBundle = encode(RenderBundle{Part: part.Type, URL: s.Renderer.AbsoluteURL(part.URL), SHA256: part.SHA256,
		MediaType: part.MediaType, InstanceID: sidecar.InstanceID, VariablesURL: s.Renderer.AbsoluteURL(part.VariablesURL),
		SidecarSHA256: sidecarSHA, VariablesSHA256: sidecar.VariablesSHA256, TreeSHA256: sidecar.Generator.Source.TreeSHA256,
		Complete: sidecar.Complete, Engine: sidecar.Generator.Engine})
	d.Status = repositories.DispatchRendered
	return nil
}

func pickPart(parts []yantra4d.Part, want string) (*yantra4d.Part, *stepError) {
	if want != "" {
		for i := range parts {
			if parts[i].Type == want {
				return &parts[i], nil
			}
		}
		return nil, terminal("render_part_missing", "render returned no part %q", want)
	}
	if len(parts) != 1 {
		return nil, terminal("render_part_ambiguous", "the mode renders %d parts; set the order item's specifications.part", len(parts))
	}
	return &parts[0], nil
}

// stageSliceSubmit: create the slice job with the matched machine's profiles.
func (s *Service) stageSliceSubmit(ctx context.Context, d *repositories.DispatchJob) *stepError {
	if serr := s.holdReservation(ctx, d, s.cfg.ReservationTTL); serr != nil {
		return serr
	}
	match, err1 := decode[MatchRecord](d.MatchResult)
	bundle, err2 := decode[RenderBundle](d.RenderBundle)
	profile, err3 := decode[fabrication.RequirementProfile](d.Requirements)
	if err1 != nil || err2 != nil || err3 != nil || d.MachineID == nil {
		return terminal("state_incomplete", "dispatch lacks its match, render or requirements")
	}
	sel := match.Selected.Selection
	// The key changes with the machine (its profiles change the request), so
	// a re-match never collides with the earlier job.
	key := fmt.Sprintf("pravara-dispatch:%s:%s", d.ID, d.MachineID.String()[:8])
	job, err := s.Slicer.CreateSliceJob(ctx, fabprep.SliceJobRequest{
		Input: fabprep.Input{URL: bundle.URL, SHA256: bundle.SHA256, MediaType: bundle.MediaType,
			Variables: &fabprep.Ref{URL: bundle.VariablesURL, SHA256: bundle.SidecarSHA256}},
		PrinterProfile: sel.PrinterProfile, FilamentProfile: sel.FilamentProfile, ProcessProfile: sel.ProcessProfile,
		Overrides: match.Product.Overrides, Requirements: profile, Part: bundle.Part, Target: sel.Target,
	}, key)
	if err != nil {
		return fromRemote("slice_rejected", err)
	}
	d.SliceResult = encode(SliceRecord{JobID: job.ID, IdempotencyKey: key, Status: job.Status})
	d.Status = repositories.DispatchSlicing
	return nil
}

// stageSlicePoll: wait for the job; on success record digests.
func (s *Service) stageSlicePoll(ctx context.Context, d *repositories.DispatchJob) *stepError {
	if serr := s.holdReservation(ctx, d, s.cfg.ReservationTTL); serr != nil {
		return serr
	}
	rec, err := decode[SliceRecord](d.SliceResult)
	if err != nil {
		return terminal("state_incomplete", "dispatch lacks its slice job")
	}
	job, err := s.Slicer.GetSliceJob(ctx, rec.JobID)
	if err != nil {
		return fromRemote("slice_poll_failed", err)
	}
	rec.Status = job.Status
	switch job.Status {
	case fabprep.StatusFailed:
		d.SliceResult = encode(rec)
		code, msg := "slice_failed", "fabrication-prep failed the job"
		if job.Error != nil {
			code, msg = "slice_failed:"+job.Error.Code, job.Error.Message
		}
		return terminal(code, "%s", msg)
	case fabprep.StatusDeadLettered:
		d.SliceResult = encode(rec)
		return terminal("slice_dead_lettered", "fabrication-prep exhausted its retries (dispatch again to retry)")
	case fabprep.StatusSucceeded:
	default:
		d.SliceResult = encode(rec)
		d.NextAttemptAt = s.now().Add(s.cfg.PollInterval)
		return nil
	}
	if job.Output == nil || job.SlicerVariables == nil {
		return retryable("slice_output_missing", "succeeded job carries no output")
	}
	vars, err := s.Slicer.FetchSlicerVariables(ctx, job.SlicerVariables)
	if err != nil {
		return fromRemote("slicer_variables_invalid", err)
	}
	if vars.Output.SHA256 != job.Output.SHA256 {
		return terminal("slice_digest_mismatch", "slicer-variables output digest differs from the job output")
	}
	rec.Profiles = map[string]fabrication.ProfileDigest{}
	for kind, p := range vars.Profiles {
		rec.Profiles[kind] = fabrication.ProfileDigest{ID: p.ID, Version: p.Version, SHA256: p.SHA256}
	}
	rec.OutputSHA256, rec.OutputMediaType, rec.OutputBytes = job.Output.SHA256, job.Output.MediaType, job.Output.Bytes
	rec.VariablesSHA256, rec.GcodeSHA256, rec.EffectiveSHA256 = job.SlicerVariables.SHA256, vars.Output.GcodeSHA256, vars.EffectiveSHA256
	rec.Slicer = strings.TrimSpace(vars.Slicer.Name + " " + vars.Slicer.Version)
	rec.Estimates = job.Estimates
	d.SliceResult = encode(rec)
	d.Status = repositories.DispatchSliced
	return nil
}

// stageEnqueue: re-check the machine, then write the ledger row and append
// start_job to the durable command stream.
func (s *Service) stageEnqueue(ctx context.Context, d *repositories.DispatchJob) *stepError {
	rec, err := decode[SliceRecord](d.SliceResult)
	if err != nil || d.MachineID == nil {
		return terminal("state_incomplete", "dispatch lacks its slice result or machine")
	}
	if d.Status == repositories.DispatchSliced {
		if serr := s.recheck(ctx, d); serr != nil {
			return serr
		}
		id := uuid.New()
		d.CommandID = &id
		d.Status = repositories.DispatchEnqueuing
		// Saved (with the command id) before anything is appended, so a crash
		// here replays the same command id.
		d.NextAttemptAt = s.now()
		return nil
	}

	var cmd *repositories.TaskCommand
	err = s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		cmd, err = s.Ledger.GetByCommandID(ctx, *d.CommandID)
		return err
	})
	if err != nil {
		return retryable("database", "%v", err)
	}
	if cmd != nil {
		switch cmd.Status {
		case "sent", "acknowledged", "completed":
			d.Status = repositories.DispatchCommandEnqueued
			return nil
		case "failed", "timeout":
			msg := ""
			if cmd.ErrorMessage != nil {
				msg = *cmd.ErrorMessage
			}
			return terminal("command_"+cmd.Status, "start_job %s: %s", cmd.Status, msg)
		}
	}
	// Fresh signed URL: the one from slicing may have expired.
	job, err := s.Slicer.GetSliceJob(ctx, rec.JobID)
	if err != nil {
		return fromRemote("slice_read_failed", err)
	}
	if job.Output == nil || job.Output.SHA256 != rec.OutputSHA256 {
		return terminal("slice_output_changed", "the slice job output no longer matches the recorded digest")
	}
	code, err := s.machineCode(ctx, d)
	if err != nil {
		return retryable("database", "%v", err)
	}
	taskID := d.TaskID
	params := map[string]interface{}{
		"artifact_url":        job.Output.URL,
		"artifact_sha256":     job.Output.SHA256,
		"artifact_media_type": job.Output.MediaType,
		"job_id":              d.TaskID.String(),
		"dispatch_id":         d.ID.String(),
		"slice_job_id":        rec.JobID,
		"device_id":           code,
	}
	now := s.now()
	if cmd == nil {
		cmd = &repositories.TaskCommand{TenantID: d.TenantID, TaskID: d.TaskID, MachineID: *d.MachineID,
			CommandID: *d.CommandID, CommandType: string(pubsub.CommandStartJob), Status: "pending",
			Parameters: params, IssuedAt: now} // the actor is on the dispatch record
		if err := s.Ledger.CreateDurable(ctx, cmd); err != nil {
			return retryable("ledger_write_failed", "%v", err)
		}
	}
	data := pubsub.MachineCommandData{CommandID: *d.CommandID, MachineID: *d.MachineID, MachineName: code,
		Command: pubsub.CommandStartJob, Parameters: params, TaskID: &taskID, IssuedAt: now}
	if d.RequestedByActor != nil {
		data.IssuedBy = *d.RequestedByActor
	}
	if err := s.Enqueuer.PublishCommandForDispatch(ctx, d.TenantID, data); err != nil {
		if uerr := s.Ledger.UpdateStatusDurable(ctx, d.TenantID, *d.CommandID, "failed", "Failed to enqueue: "+err.Error()); uerr != nil {
			s.log.WithError(uerr).Error("dispatch: failed to record command enqueue failure")
		}
		d.CommandID = nil
		d.Status = repositories.DispatchSliced
		return retryable("enqueue_failed", "%v", err)
	}
	// The command is out: hold the machine until the job ends. A lapsed
	// reservation is logged; the dispatch stays bound to its command.
	var held bool
	if err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		held, err = s.Dispatch.ExtendReservation(ctx, d.ID, s.cfg.CommandHold)
		return err
	}); err != nil || !held {
		s.log.WithError(err).WithField("dispatch_id", d.ID).Warn("dispatch: could not extend the reservation after enqueueing start_job")
	}
	d.Status = repositories.DispatchCommandEnqueued
	return nil
}

func (s *Service) machineCode(ctx context.Context, d *repositories.DispatchJob) (string, error) {
	var code string
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		code, err = s.Sources.MachineCode(ctx, *d.MachineID)
		return err
	})
	return code, err
}

// recheck evaluates the reserved machine again right before start_job: it
// must still be born, idle, loaded with an allowed class, and reserved by
// this dispatch.
func (s *Service) recheck(ctx context.Context, d *repositories.DispatchJob) *stepError {
	if serr := s.holdReservation(ctx, d, s.cfg.ReservationTTL); serr != nil {
		return serr
	}
	match, err := decode[MatchRecord](d.MatchResult)
	profile, err2 := decode[fabrication.RequirementProfile](d.Requirements)
	if err != nil || err2 != nil {
		return terminal("state_incomplete", "dispatch lacks its match or requirements")
	}
	var res *matchmaking.Result
	err = s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		res, err = s.rank(ctx, d.TenantID, profile, &match.Product, nil, &d.ID)
		return err
	})
	if err != nil {
		return retryable("database", "%v", err)
	}
	var again *matchmaking.Candidate
	for i := range res.Candidates {
		if res.Candidates[i].MachineID == *d.MachineID {
			again = &res.Candidates[i]
		}
	}
	if again != nil {
		match.Rechecks = append(match.Rechecks, *again)
		d.MatchResult = encode(match)
	}
	if again == nil || !again.Eligible || again.Selection.MaterialClass != match.Selected.Selection.MaterialClass {
		reason := "machine removed"
		if again != nil {
			reason = summarize(&matchmaking.Result{Candidates: []matchmaking.Candidate{*again}})
			if reason == "" {
				reason = "loaded material changed"
			}
		}
		if err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
			return s.Dispatch.ReleaseReservations(ctx, d.ID, "dispatch-time re-check failed")
		}); err != nil {
			return retryable("database", "%v", err)
		}
		s.rematch(d)
		return retryable("machine_no_longer_eligible", "%s", reason)
	}
	return nil
}
