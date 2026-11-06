package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// Slicer estimate keys in a fabrication-prep job view (SliceRecord.Estimates).
const (
	estimateFilamentG  = "filament_g"
	estimatePrintTimeS = "print_time_s"
	estimateSource     = "source"
)

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

// completedAt is when the job finished: the printer's report, else the
// ledger's completion, else when pravara recorded it.
func completedAt(rec *fabrication.ManufacturingRecord, ledgerDone *time.Time) time.Time {
	switch {
	case rec.PrinterReportedAt != nil:
		return rec.PrinterReportedAt.UTC()
	case ledgerDone != nil:
		return ledgerDone.UTC()
	}
	return rec.ServerRecordedAt.UTC()
}

// consumptionFacts derives what a completed dispatch consumed, from its
// stored state:
//   - the slicer's estimates (fabrication-prep: filament_g, print_time_s) as
//     basis estimate, source slicer;
//   - machine time from the start_job acknowledgement to the completion
//     report as basis actual, source printer.
//
// The Sparkplug completion does not report filament used, so a dispatch has
// no actual material until an operator reports it
// (POST /v1/tasks/:id/consumption). materialCode "" records no material.
func consumptionFacts(slice *SliceRecord, rec *fabrication.ManufacturingRecord, materialCode string, ackedAt, ledgerDone *time.Time) []repositories.JobConsumption {
	end := completedAt(rec, ledgerDone)
	var out []repositories.JobConsumption
	note := "fabrication-prep estimate"
	if slice != nil {
		if src, _ := slice.Estimates[estimateSource].(string); src != "" {
			note += " (" + src + ")"
		}
		if g, ok := number(slice.Estimates[estimateFilamentG]); ok && g >= 0 && materialCode != "" {
			out = append(out, repositories.JobConsumption{Kind: repositories.ConsumptionMaterial,
				MaterialCode: materialCode, MaterialLot: rec.MaterialLot, Quantity: g, Unit: repositories.UnitGram,
				Basis: repositories.BasisEstimate, Source: repositories.SourceSlicer, Note: note, OccurredAt: end})
		}
		if sec, ok := number(slice.Estimates[estimatePrintTimeS]); ok && sec >= 0 {
			out = append(out, repositories.JobConsumption{Kind: repositories.ConsumptionMachineTime,
				Quantity: sec / 60, Unit: repositories.UnitMinute, Basis: repositories.BasisEstimate,
				Source: repositories.SourceSlicer, Note: note, OccurredAt: end})
		}
	}
	if ackedAt != nil && end.After(*ackedAt) {
		out = append(out, repositories.JobConsumption{Kind: repositories.ConsumptionMachineTime,
			Quantity: end.Sub(*ackedAt).Minutes(), Unit: repositories.UnitMinute, Basis: repositories.BasisActual,
			Source: repositories.SourcePrinter, Note: "start_job acknowledged to completion reported", OccurredAt: end})
	}
	return out
}

// recordConsumption writes the dispatch's consumption inside a savepoint of
// the completion transaction: a failure is logged and never blocks the
// manufacturing record. ctx carries the tenant scope.
func (s *Service) recordConsumption(ctx context.Context, d *repositories.DispatchJob, rec *fabrication.ManufacturingRecord, orderID *uuid.UUID) {
	if s.Consumption == nil {
		return
	}
	taskID, machineID := d.TaskID, *d.MachineID
	job := services.JobRef{TenantID: d.TenantID, Kind: repositories.JobKindDispatch, ID: d.ID,
		TaskID: &taskID, OrderID: orderID, MachineID: &machineID}
	if rec.GenealogyID != "" {
		if gid, err := uuid.Parse(rec.GenealogyID); err == nil {
			job.GenealogyID = &gid
		}
	}
	_, err := s.Consumption.RecordInSavepoint(ctx, job, func(ctx context.Context) ([]repositories.JobConsumption, error) {
		acked, done, err := s.Sources.CommandTimes(ctx, *d.CommandID)
		if err != nil {
			return nil, err
		}
		code, err := s.Consumption.ResolveMaterialCode(ctx, rec.MaterialClass, rec.MaterialLot)
		if err != nil {
			return nil, err
		}
		slice, err := decode[SliceRecord](d.SliceResult)
		if err != nil {
			return nil, fmt.Errorf("slice: %w", err)
		}
		return consumptionFacts(slice, rec, code, acked, done), nil
	})
	if err != nil {
		s.log.WithError(err).WithField("dispatch_id", d.ID).Error("dispatch: job consumption not recorded")
	}
}
