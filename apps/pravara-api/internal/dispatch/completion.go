package dispatch

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/assetshells"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/matchmaking"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// completionWait is the poll interval while a job prints.
func (s *Service) completionWait() time.Duration {
	if s.cfg.PollInterval < 30*time.Second {
		return 30 * time.Second
	}
	return s.cfg.PollInterval
}

// stageAwaitCompletion watches the start_job ledger row. A failed or timed
// out command fails the dispatch; a completion (ledger completed and a
// completion event for this command and machine) writes the record.
func (s *Service) stageAwaitCompletion(ctx context.Context, d *repositories.DispatchJob) *stepError {
	if d.CommandID == nil || d.MachineID == nil {
		return terminal("state_incomplete", "dispatch lacks its command or machine")
	}
	var (
		status    string
		ledgerMID uuid.UUID
		errMsg    string
		ev        *repositories.CompletionEvent
	)
	err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		var err error
		status, ledgerMID, _, errMsg, err = s.Sources.CommandLedgerState(ctx, *d.CommandID)
		if err != nil || status != "completed" {
			return err
		}
		ev, err = s.Sources.FindCompletionEvent(ctx, CompletionEventTypes, *d.CommandID)
		return err
	})
	if err != nil {
		return retryable("database", "%v", err)
	}
	switch status {
	case "":
		return terminal("command_missing", "start_job %s has no ledger row", d.CommandID)
	case "failed", "timeout":
		s.releaseQuietly(ctx, d, "start_job "+status)
		return terminal("command_"+status, "start_job %s: %s", status, errMsg)
	case "completed":
	default:
		d.NextAttemptAt = s.now().Add(s.completionWait())
		return nil
	}
	if ledgerMID != *d.MachineID {
		return terminal("completion_machine_mismatch", "ledger machine %s differs from dispatched machine %s", ledgerMID, d.MachineID)
	}
	if ev == nil {
		d.NextAttemptAt = s.now().Add(s.completionWait())
		return nil
	}
	if evMachine, _ := ev.Data["machine_id"].(string); evMachine != d.MachineID.String() {
		return terminal("completion_machine_mismatch", "completion event machine %q differs from dispatched machine %s", evMachine, d.MachineID)
	}
	if evTask, _ := ev.Data["task_id"].(string); evTask != "" && evTask != d.TaskID.String() {
		return terminal("completion_task_mismatch", "completion event task %q differs from dispatched task %s", evTask, d.TaskID)
	}
	if serr := s.recordCompletion(ctx, d, ev); serr != nil {
		return serr
	}
	now := s.now()
	d.Status, d.CompletedAt = repositories.DispatchCompleted, &now
	return nil
}

func (s *Service) releaseQuietly(ctx context.Context, d *repositories.DispatchJob, reason string) {
	if err := s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		return s.Dispatch.ReleaseReservations(ctx, d.ID, reason)
	}); err != nil {
		s.log.WithError(err).WithField("dispatch_id", d.ID).Warn("dispatch: failed to release reservation")
	}
}

func timeField(data map[string]any, keys ...string) *time.Time {
	for _, k := range keys {
		if v, ok := data[k].(string); ok && v != "" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				t = t.UTC()
				return &t
			}
		}
	}
	return nil
}

// buildRecord assembles the ManufacturingRecord from the dispatch state and
// the completion event. Missing facts become gaps.
func buildRecord(d *repositories.DispatchJob, code string, ev *repositories.CompletionEvent) (*fabrication.ManufacturingRecord, error) {
	match, err := decode[MatchRecord](d.MatchResult)
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}
	bundle, err := decode[RenderBundle](d.RenderBundle)
	if err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	slice, err := decode[SliceRecord](d.SliceResult)
	if err != nil {
		return nil, fmt.Errorf("slice: %w", err)
	}
	sel := match.Selected.Selection
	rec := &fabrication.ManufacturingRecord{
		Format: fabrication.ManufacturingRecordFormat, FormatVersion: "1.0.0",
		DispatchID: d.ID.String(), CommandID: d.CommandID.String(), TaskID: d.TaskID.String(),
		TypeShellID: d.TypeShellID, Cartridge: match.Product.Cartridge, Mode: match.Product.Mode, Part: bundle.Part,
		GOC1InstanceID: bundle.InstanceID, VariablesSHA256: bundle.VariablesSHA256, TreeSHA256: bundle.TreeSHA256,
		SidecarSHA256: bundle.SidecarSHA256, GeometrySHA256: bundle.SHA256, GeometryMedia: bundle.MediaType,
		SliceJobID: slice.JobID, SlicerProfiles: slice.Profiles, Slicer: slice.Slicer, EffectiveSHA256: slice.EffectiveSHA256,
		SlicerVariablesSHA256: slice.VariablesSHA256, ArtifactSHA256: slice.OutputSHA256, ArtifactMediaType: slice.OutputMediaType,
		GcodeSHA256: slice.GcodeSHA256, MachineID: d.MachineID.String(), MachineCode: code,
		MaterialClass: sel.MaterialClass, MaterialSlot: sel.MaterialSlot, MaterialLot: sel.MaterialLot,
		PrinterReportedAt: timeField(ev.Data, "printer_reported_at", "job_reported_at", "reported_at"),
		BrokerReceivedAt:  timeField(ev.Data, "broker_received_at", "received_at"),
		ServerRecordedAt:  ev.CreatedAt.UTC(),
	}
	if !bundle.Complete {
		rec.Gaps = append(rec.Gaps, matchmaking.GapIncompleteGOC1)
	}
	if rec.MaterialLot == "" {
		rec.Gaps = append(rec.Gaps, matchmaking.GapMaterialLot)
	}
	if rec.GcodeSHA256 == "" {
		rec.Gaps = append(rec.Gaps, matchmaking.GapGcodeDigest)
	}
	if rec.PrinterReportedAt == nil {
		rec.Gaps = append(rec.Gaps, matchmaking.GapPrinterTime)
	}
	if rec.BrokerReceivedAt == nil {
		rec.Gaps = append(rec.Gaps, matchmaking.GapBrokerTime)
	}
	if !match.Product.BoundingBox.Valid() {
		rec.Gaps = append(rec.Gaps, matchmaking.GapBoundingBox)
	}
	return rec, nil
}

// recordCompletion writes, in one tenant transaction: the genealogy record,
// the ManufacturingRecord, the instance publish and the first passport
// event (outbox), and releases the reservation. Delivery happens later.
func (s *Service) recordCompletion(ctx context.Context, d *repositories.DispatchJob, ev *repositories.CompletionEvent) *stepError {
	code, err := s.machineCode(ctx, d)
	if err != nil {
		return retryable("database", "%v", err)
	}
	rec, err := buildRecord(d, code, ev)
	if err != nil {
		return terminal("state_incomplete", "%v", err)
	}
	instance := uuid.New()
	err = s.inTenant(ctx, d.TenantID, func(ctx context.Context) error {
		existing, err := s.Passports.RecordByDispatch(ctx, d.ID)
		if err != nil || existing != nil {
			return err // already recorded by an earlier attempt
		}
		taskID, orderItem, machine := d.TaskID, d.OrderItemID, *d.MachineID
		var orderID *uuid.UUID
		var sku string
		if tc, err := s.Sources.TaskContext(ctx, d.TaskID); err == nil && tc != nil {
			orderID, sku = tc.OrderID, tc.ProductSKU
		}
		if s.Genealogy != nil {
			g, err := s.Genealogy.AutoCreateFromTask(ctx, services.TaskInfo{ID: taskID, TenantID: d.TenantID,
				OrderID: orderID, OrderItemID: orderItem, MachineID: &machine, ProductSKU: sku})
			if err != nil {
				return err
			}
			rec.GenealogyID = g.ID.String()
		}
		sha, body, err := rec.Digest()
		if err != nil {
			return err
		}
		row := &repositories.ManufacturingRecordRow{TenantID: d.TenantID, DispatchID: d.ID, CommandID: *d.CommandID,
			TaskID: &taskID, MachineID: &machine, InstanceUUID: instance, CompletionEventID: &ev.EventID,
			Record: body, RecordSHA256: sha}
		if rec.GenealogyID != "" {
			gid := uuid.MustParse(rec.GenealogyID)
			row.GenealogyID = &gid
		}
		if err := s.Passports.InsertRecord(ctx, row); err != nil {
			return err
		}
		env, err := assetshells.BuildInstanceEnvironment(instance.String(), rec, sha)
		if err != nil {
			return err
		}
		if err := s.Passports.Enqueue(ctx, &repositories.PassportOutboxRow{TenantID: d.TenantID,
			ManufacturingRecordID: row.ID, Kind: repositories.PassportKindInstance, Payload: env}); err != nil {
			return err
		}
		eventID := uuid.New()
		evBody, err := assetshells.BuildPassportEventBody(assetshells.PassportEvent{EventID: eventID.String(),
			Type: "manufactured", OccurredAt: rec.ServerRecordedAt,
			Facts: map[string]string{"ManufacturingRecordSha256": sha, "MachineCode": code}})
		if err != nil {
			return err
		}
		if err := s.Passports.Enqueue(ctx, &repositories.PassportOutboxRow{TenantID: d.TenantID,
			ManufacturingRecordID: row.ID, Kind: repositories.PassportKindEvent, EventID: &eventID, Payload: evBody}); err != nil {
			return err
		}
		return s.Dispatch.ReleaseReservations(ctx, d.ID, "job completed")
	})
	if err != nil {
		return retryable("record_write_failed", "%v", err)
	}
	return nil
}

// AppendPassportEvent queues a later fact about a completed dispatch's
// instance (append-only; delivered after the instance itself). ctx carries
// the tenant scope.
func (s *Service) AppendPassportEvent(ctx context.Context, tenantID, dispatchID uuid.UUID, eventType string, occurredAt time.Time, facts map[string]string) (*repositories.PassportOutboxRow, error) {
	rec, err := s.Passports.RecordByDispatch(ctx, dispatchID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, terminal("no_manufacturing_record", "dispatch %s has no manufacturing record", dispatchID)
	}
	eventID := uuid.New()
	body, err := assetshells.BuildPassportEventBody(assetshells.PassportEvent{EventID: eventID.String(), Type: eventType,
		OccurredAt: occurredAt, Facts: facts})
	if err != nil {
		return nil, err
	}
	row := &repositories.PassportOutboxRow{TenantID: tenantID, ManufacturingRecordID: rec.ID,
		Kind: repositories.PassportKindEvent, EventID: &eventID, Payload: body}
	if err := s.Passports.Enqueue(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}
