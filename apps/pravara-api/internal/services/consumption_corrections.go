package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// Errors of the correction and print-job completion paths.
var (
	ErrConsumptionNotFound       = errors.New("consumption row not found")
	ErrConsumptionSuperseded     = repositories.ErrConsumptionAlreadySuperseded
	ErrConsumptionPrintJobClosed = errors.New("print job was cancelled or failed")
)

// Correction is an operator's correction of a recorded row.
type Correction struct {
	Quantity float64
	// Unit defaults to the corrected row's unit.
	Unit string
	// Disposition defaults to the corrected row's disposition.
	Disposition string
	Reason      string
	By          *uuid.UUID
}

// CorrectionResult is the stored correction and the stock it moved.
type CorrectionResult struct {
	Correction *repositories.JobConsumption
	Superseded *repositories.JobConsumption
	Movements  []repositories.InventoryTransaction
}

// Correct supersedes a recorded row with an operator actual. History is
// never updated: the correction is a new row (supersedes_id) of the same
// job, kind and material, it becomes the effective row, and the job's stock
// relief moves by the difference. A row is corrected once; correct the
// latest correction to change it again.
func (s *ConsumptionService) Correct(ctx context.Context, targetID uuid.UUID, in Correction) (*CorrectionResult, error) {
	target, err := s.repo.Get(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrConsumptionNotFound
	}
	if target.SupersededBy != nil {
		return nil, fmt.Errorf("%w by %s", ErrConsumptionSuperseded, *target.SupersededBy)
	}
	if math.IsNaN(in.Quantity) || in.Quantity < 0 || in.Quantity >= 1e11 {
		return nil, fmt.Errorf("%w: quantity must be 0 or more", ErrConsumptionInvalid)
	}
	if len(in.Reason) > 500 {
		return nil, fmt.Errorf("%w: reason is at most 500 characters", ErrConsumptionInvalid)
	}
	c := *target
	c.ID, c.SupersedesID, c.SupersededBy = uuid.Nil, &target.ID, nil
	c.Quantity, c.Basis, c.Source, c.RecordedBy = in.Quantity, repositories.BasisActual, repositories.SourceOperator, in.By
	if in.Unit != "" {
		c.Unit = in.Unit
	}
	if in.Disposition != "" {
		c.Disposition = in.Disposition
	}
	c.Note = "correction of " + target.ID.String()
	if r := strings.TrimSpace(in.Reason); r != "" {
		c.Note += ": " + r
	}
	// occurred_at stays the job's: a correction never moves the period.
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumptionInvalid, err)
	}
	if err := s.repo.InsertCorrection(ctx, &c); err != nil {
		return nil, err
	}
	res := &CorrectionResult{Correction: &c, Superseded: target, Movements: []repositories.InventoryTransaction{}}
	if c.Kind != repositories.ConsumptionMaterial {
		return res, nil
	}
	ids, err := s.JobLockSet(ctx, c.JobKind, c.JobID, []string{c.MaterialCode})
	if err != nil {
		return nil, err
	}
	if err := s.stock.LockItems(ctx, ids); err != nil {
		return nil, err
	}
	item, err := s.stock.ResolveStockItem(ctx, c.MaterialCode)
	if err != nil {
		return nil, err
	}
	var moves []repositories.InventoryTransaction
	if item == nil {
		moves, err = s.withdrawRelief(ctx, c.JobKind, c.JobID, c.MaterialCode)
	} else {
		moves, err = s.settleItem(ctx, c.JobKind, c.JobID, item)
	}
	if err != nil {
		return nil, err
	}
	res.Movements = append(res.Movements, moves...)
	return res, nil
}

// PrintJobCompletion is what an operator reports when a print job finished.
type PrintJobCompletion struct {
	// MaterialQuantity, when given, is the operator's actual material in
	// MaterialUnit (g, mm, ml or sheet).
	MaterialQuantity *float64
	MaterialUnit     string
	// MaterialCode overrides the print profile's material type (required
	// with MaterialQuantity when the job has no material profile).
	MaterialCode string
	// Disposition of the material: good (default), scrap or failed_print.
	Disposition string
	By          *uuid.UUID
}

// PrintJobCompletionResult is what CompletePrintJob recorded.
type PrintJobCompletionResult struct {
	PrintJobID       uuid.UUID
	MarkedCompleted  bool // false when the job was already completed
	Facts            []repositories.JobConsumption
	Created          []bool
	Movements        []repositories.InventoryTransaction
	MaterialRecorded bool
}

// CompletePrintJob records that an operator saw a print job finish and
// records its actual consumption (RecordPrintJob: slicer estimates, any
// printer-reported actuals, and the operator's material actual when given).
// It only writes Pravara's own records; it never sends anything to a
// machine. Calling it again on a completed job records nothing new.
func (s *ConsumptionService) CompletePrintJob(ctx context.Context, tenantID, printJobID uuid.UUID, in PrintJobCompletion) (*PrintJobCompletionResult, error) {
	if in.Disposition == "" {
		in.Disposition = repositories.DispositionGood
	}
	if !repositories.Dispositions[in.Disposition] {
		return nil, fmt.Errorf("%w: disposition must be good, scrap or failed_print", ErrConsumptionInvalid)
	}
	p, err := s.repo.PrintJobUsage(ctx, printJobID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrConsumptionPrintJobNotFound
	}
	if p.Status == repositories.PrintJobCancelled || p.Status == repositories.PrintJobFailed {
		return nil, ErrConsumptionPrintJobClosed
	}
	code := in.MaterialCode
	if code == "" {
		code = p.MaterialType
	}
	if in.MaterialQuantity != nil && code == "" {
		return nil, fmt.Errorf("%w: the print job has no material profile; send material_code", ErrConsumptionInvalid)
	}
	res := &PrintJobCompletionResult{PrintJobID: printJobID, Movements: []repositories.InventoryTransaction{}}
	if res.MarkedCompleted, err = s.repo.MarkPrintJobCompleted(ctx, printJobID); err != nil {
		return nil, err
	}
	rec, err := s.recordPrintJob(ctx, tenantID, printJobID, in.Disposition)
	if err != nil {
		return nil, err
	}
	res.Facts, res.Created, res.Movements = rec.Facts, rec.Created, append(res.Movements, rec.Movements...)
	if in.MaterialQuantity == nil {
		return res, nil
	}
	if p, err = s.repo.PrintJobUsage(ctx, printJobID); err != nil {
		return nil, err
	}
	occurred := time.Now().UTC()
	if p.CompletedAt != nil {
		occurred = *p.CompletedAt
	}
	fact := repositories.JobConsumption{Kind: repositories.ConsumptionMaterial, MaterialCode: code,
		Quantity: *in.MaterialQuantity, Unit: in.MaterialUnit, Basis: repositories.BasisActual,
		Source: repositories.SourceOperator, Note: "reported by an operator at print completion",
		RecordedBy: in.By, OccurredAt: occurred.UTC(), Disposition: in.Disposition}
	job := JobRef{TenantID: tenantID, Kind: repositories.JobKindPrintJob, ID: printJobID,
		TaskID: p.TaskID, OrderID: p.OrderID, MachineID: p.MachineID}
	probe := fact
	job.apply(&probe)
	if err := probe.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConsumptionInvalid, err)
	}
	op, err := s.Record(ctx, job, []repositories.JobConsumption{fact})
	if err != nil {
		return nil, err
	}
	res.Facts, res.Created = append(res.Facts, op.Facts...), append(res.Created, op.Created...)
	res.Movements, res.MaterialRecorded = append(res.Movements, op.Movements...), op.Created[0]
	return res, nil
}
