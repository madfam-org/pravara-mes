package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// ConsumptionService records what completed jobs consumed (material and
// machine time, as quantities) and keeps stock and genealogy in step with
// it. It records no cost: valuing consumption belongs to the cost ledger.
//
// Rules (docs/operations/job-consumption.md):
//   - A fact is (job, kind, basis, source, material). Recording it again is
//     a no-op, so replays never duplicate.
//   - For each job and material, the effective quantity is the latest
//     actual, else the latest estimate. Stock relieved for the job always
//     equals the effective quantity: the first relief is a 'consumption'
//     movement, a later actual posts the difference as an 'adjustment'.
//     Relief happens only when the material code is the SKU of an inventory
//     item in the same unit.
//   - When the material's lot is known and the job has an unsealed
//     genealogy record, the material is written to that record once.
//
// Every method runs in the tenant scope carried by ctx.
type ConsumptionService struct {
	q         repositories.DBTX
	repo      *repositories.JobConsumptionRepository
	genealogy *repositories.GenealogyRepository
	log       *logrus.Logger
}

// NewConsumptionService creates the service. q is the tenant-scoped handle
// the repositories use (savepoints run on it).
func NewConsumptionService(q repositories.DBTX, repo *repositories.JobConsumptionRepository, genealogy *repositories.GenealogyRepository, log *logrus.Logger) *ConsumptionService {
	return &ConsumptionService{q: q, repo: repo, genealogy: genealogy, log: log}
}

// Errors of the operator and print-job paths.
var (
	ErrConsumptionTaskNotFound     = errors.New("task not found")
	ErrConsumptionPrintJobNotFound = errors.New("print job not found")
	ErrConsumptionPrintJobOpen     = errors.New("print job is not completed")
	ErrConsumptionInvalid          = errors.New("invalid consumption report")
)

// JobRef identifies the job facts belong to.
type JobRef struct {
	TenantID  uuid.UUID
	Kind      string // repositories.JobKind*
	ID        uuid.UUID
	TaskID    *uuid.UUID
	OrderID   *uuid.UUID
	MachineID *uuid.UUID
	// GenealogyID is the job's genealogy record when the caller knows it
	// (dispatch completion creates it); otherwise the task's latest is used.
	GenealogyID *uuid.UUID
}

// ConsumptionResult is what Record stored and moved.
type ConsumptionResult struct {
	Facts     []repositories.JobConsumption
	Created   []bool
	Movements []repositories.InventoryTransaction
}

func (j JobRef) apply(c *repositories.JobConsumption) {
	c.TenantID, c.JobKind, c.JobID = j.TenantID, j.Kind, j.ID
	c.TaskID, c.OrderID, c.MachineID = j.TaskID, j.OrderID, j.MachineID
	c.DispatchID, c.PrintJobID = nil, nil
	id := j.ID
	switch j.Kind {
	case repositories.JobKindDispatch:
		c.DispatchID = &id
	case repositories.JobKindPrintJob:
		c.PrintJobID = &id
	}
}

type materialKey struct{ code, unit string }

// Record stores facts for job and settles stock and genealogy for every
// material a new fact touches.
func (s *ConsumptionService) Record(ctx context.Context, job JobRef, facts []repositories.JobConsumption) (*ConsumptionResult, error) {
	res := &ConsumptionResult{}
	var touched []materialKey
	seen := map[materialKey]bool{}
	for i := range facts {
		c := facts[i]
		job.apply(&c)
		created, err := s.repo.Insert(ctx, &c)
		if err != nil {
			return nil, err
		}
		res.Facts, res.Created = append(res.Facts, c), append(res.Created, created)
		k := materialKey{c.MaterialCode, c.Unit}
		if created && c.Kind == repositories.ConsumptionMaterial && !seen[k] {
			seen[k] = true
			touched = append(touched, k)
		}
	}
	for _, k := range touched {
		mv, err := s.settle(ctx, job, k)
		if err != nil {
			return nil, err
		}
		if mv != nil {
			res.Movements = append(res.Movements, *mv)
		}
	}
	return res, nil
}

// effective is the latest actual, else the latest estimate.
func effective(facts []repositories.JobConsumption) *repositories.JobConsumption {
	var actual, estimate *repositories.JobConsumption
	for i := range facts {
		switch facts[i].Basis {
		case repositories.BasisActual:
			actual = &facts[i]
		case repositories.BasisEstimate:
			estimate = &facts[i]
		}
	}
	if actual != nil {
		return actual
	}
	return estimate
}

func (s *ConsumptionService) settle(ctx context.Context, job JobRef, k materialKey) (*repositories.InventoryTransaction, error) {
	// Lock the item first: the facts and the relieved quantity read after
	// the lock include whatever a concurrent settle for this job committed.
	item, err := s.repo.LockStockItem(ctx, k.code, k.unit)
	if err != nil {
		return nil, err
	}
	facts, err := s.repo.MaterialFacts(ctx, job.Kind, job.ID, k.code, k.unit)
	if err != nil {
		return nil, err
	}
	eff := effective(facts)
	if eff == nil {
		return nil, nil
	}
	var mv *repositories.InventoryTransaction
	if item != nil {
		relieved, err := s.repo.RelievedQuantity(ctx, item.ID, job.Kind, job.ID)
		if err != nil {
			return nil, err
		}
		if delta := repositories.Round4(eff.Quantity - relieved); delta != 0 {
			typ := "adjustment"
			if relieved == 0 && delta > 0 {
				typ = "consumption"
			}
			note := fmt.Sprintf("%s %s: %s %s %s (%s, %s)", job.Kind, job.ID, k.code,
				formatQuantity(eff.Quantity), k.unit, eff.Basis, eff.Source)
			if mv, err = s.repo.PostStockMovement(ctx, item.ID, eff.ID, typ, -delta, note); err != nil {
				return nil, err
			}
		}
	}
	if err := s.linkGenealogy(ctx, job, facts, eff, item); err != nil {
		return nil, err
	}
	return mv, nil
}

func (s *ConsumptionService) linkGenealogy(ctx context.Context, job JobRef, facts []repositories.JobConsumption, eff *repositories.JobConsumption, item *repositories.StockItem) error {
	if s.genealogy == nil {
		return nil
	}
	lot := eff.MaterialLot
	for i := len(facts) - 1; lot == "" && i >= 0; i-- {
		lot = facts[i].MaterialLot
	}
	if lot == "" {
		return nil
	}
	gid := job.GenealogyID
	if gid == nil && job.TaskID != nil {
		id, sealed, err := s.repo.TaskGenealogy(ctx, *job.TaskID)
		if err != nil || sealed {
			return err
		}
		gid = id
	}
	if gid == nil {
		return nil
	}
	has, err := s.repo.GenealogyHasMaterial(ctx, *gid, eff.MaterialCode)
	if err != nil || has {
		return err
	}
	lotRef, err := s.repo.BatchLotByNumber(ctx, lot)
	if err != nil {
		return err
	}
	mc := &repositories.MaterialConsumption{TenantID: job.TenantID, GenealogyID: *gid,
		MaterialName: eff.MaterialCode, MaterialCode: eff.MaterialCode, QuantityConsumed: eff.Quantity, Unit: eff.Unit}
	if item != nil {
		mc.MaterialName = item.Name
	}
	if lotRef != nil {
		mc.BatchLotID = &lotRef.ID
	}
	return s.genealogy.CreateMaterialConsumption(ctx, mc)
}

func formatQuantity(q float64) string { return fmt.Sprintf("%.4g", q) }

// ResolveMaterialCode names a material for consumption: the product code of
// the batch lot when the lot is known and carries one (the stock SKU of that
// lot), else the material class.
func (s *ConsumptionService) ResolveMaterialCode(ctx context.Context, class, lot string) (string, error) {
	if lot != "" {
		b, err := s.repo.BatchLotByNumber(ctx, lot)
		if err != nil {
			return "", err
		}
		if b != nil && b.ProductCode != "" {
			return b.ProductCode, nil
		}
	}
	return class, nil
}

// RecordInSavepoint runs Record inside a savepoint of the caller's
// transaction, so a failure leaves that transaction usable. Dispatch
// completion uses it: consumption must never block the manufacturing record.
func (s *ConsumptionService) RecordInSavepoint(ctx context.Context, job JobRef, facts func(ctx context.Context) ([]repositories.JobConsumption, error)) (*ConsumptionResult, error) {
	var res *ConsumptionResult
	err := db.Savepoint(ctx, s.q, func(db.Querier) error {
		f, err := facts(ctx)
		if err != nil {
			return err
		}
		res, err = s.Record(ctx, job, f)
		return err
	})
	return res, err
}

// OperatorReport is an operator-reported actual for a task: material (unit
// g, mm, ml or sheet, with material_code) or machine time (unit min).
type OperatorReport struct {
	MaterialCode string
	MaterialLot  string
	Quantity     float64
	Unit         string
	ReportedBy   *uuid.UUID
}

// RecordOperatorReport stores an operator's actual for a task. It belongs to
// the task's latest completed dispatch when there is one (so it settles that
// dispatch's estimate), else to the task. created is false when the same
// actual was already reported; the stored fact is returned.
func (s *ConsumptionService) RecordOperatorReport(ctx context.Context, tenantID, taskID uuid.UUID, r OperatorReport) (*repositories.JobConsumption, bool, []repositories.InventoryTransaction, error) {
	tj, err := s.repo.TaskJob(ctx, taskID)
	if err != nil {
		return nil, false, nil, err
	}
	if tj == nil {
		return nil, false, nil, ErrConsumptionTaskNotFound
	}
	task := taskID
	job := JobRef{TenantID: tenantID, Kind: repositories.JobKindTask, ID: taskID, TaskID: &task,
		OrderID: tj.OrderID, MachineID: tj.MachineID}
	if tj.DispatchID != nil {
		job.Kind, job.ID, job.MachineID = repositories.JobKindDispatch, *tj.DispatchID, tj.DispatchMachineID
	}
	fact := repositories.JobConsumption{Kind: repositories.ConsumptionMaterial, MaterialCode: r.MaterialCode,
		MaterialLot: r.MaterialLot, Quantity: r.Quantity, Unit: r.Unit, Basis: repositories.BasisActual,
		Source: repositories.SourceOperator, Note: "reported by an operator", RecordedBy: r.ReportedBy,
		OccurredAt: time.Now().UTC()}
	if r.Unit == repositories.UnitMinute {
		fact.Kind = repositories.ConsumptionMachineTime
	}
	if fact.Kind == repositories.ConsumptionMaterial && fact.MaterialLot == "" && fact.MaterialCode != "" {
		earlier, err := s.repo.MaterialFacts(ctx, job.Kind, job.ID, fact.MaterialCode, "")
		if err != nil {
			return nil, false, nil, err
		}
		for i := len(earlier) - 1; fact.MaterialLot == "" && i >= 0; i-- {
			fact.MaterialLot = earlier[i].MaterialLot
		}
	}
	probe := fact
	job.apply(&probe)
	if err := probe.Validate(); err != nil {
		return nil, false, nil, fmt.Errorf("%w: %v", ErrConsumptionInvalid, err)
	}
	res, err := s.Record(ctx, job, []repositories.JobConsumption{fact})
	if err != nil {
		return nil, false, nil, err
	}
	return &res.Facts[0], res.Created[0], res.Movements, nil
}

// FilamentGrams converts a filament length to grams for a filament of
// diameterMM and densityGcm3. ok is false when either is missing or not
// positive.
func FilamentGrams(lengthMM float64, diameterMM, densityGcm3 *float64) (float64, bool) {
	if diameterMM == nil || densityGcm3 == nil || *diameterMM <= 0 || *densityGcm3 <= 0 {
		return 0, false
	}
	r := *diameterMM / 2
	cubicCM := lengthMM * math.Pi * r * r / 1000
	return cubicCM * *densityGcm3, true
}

const keptInMM = "kept in mm: the material profile has no diameter or density"

// PrintJobFacts derives consumption facts from a completed print job:
// slicer estimates (G-code analysis) and printer-reported actuals. Filament
// is converted from mm to g only when the material profile gives diameter
// and density; otherwise it stays in mm and the note says so.
func PrintJobFacts(p *repositories.PrintJobUsage, occurredAt time.Time) []repositories.JobConsumption {
	var out []repositories.JobConsumption
	code := p.MaterialType
	material := func(q float64, unit, basis, source, note string) {
		if code == "" || q < 0 {
			return
		}
		out = append(out, repositories.JobConsumption{Kind: repositories.ConsumptionMaterial, MaterialCode: code,
			Quantity: q, Unit: unit, Basis: basis, Source: source, Note: note, OccurredAt: occurredAt})
	}
	filament := func(mm float64, basis, source, note string) {
		if g, ok := FilamentGrams(mm, p.MaterialDiameterMM, p.MaterialDensityGcm3); ok {
			material(g, repositories.UnitGram, basis, source, note+"; converted from "+formatQuantity(mm)+" mm")
			return
		}
		material(mm, repositories.UnitMillimetre, basis, source, note+"; "+keptInMM)
	}
	minutes := func(seconds float64, basis, source, note string) {
		if seconds < 0 {
			return
		}
		out = append(out, repositories.JobConsumption{Kind: repositories.ConsumptionMachineTime,
			Quantity: seconds / 60, Unit: repositories.UnitMinute, Basis: basis, Source: source, Note: note,
			OccurredAt: occurredAt})
	}

	switch {
	case p.EstimatedFilamentG != nil:
		material(*p.EstimatedFilamentG, repositories.UnitGram, repositories.BasisEstimate, repositories.SourceSlicer, "G-code analysis")
	case p.EstimatedFilamentMM != nil:
		filament(*p.EstimatedFilamentMM, repositories.BasisEstimate, repositories.SourceSlicer, "G-code analysis")
	}
	if p.ActualFilamentMM != nil {
		filament(*p.ActualFilamentMM, repositories.BasisActual, repositories.SourcePrinter, "reported by the printer")
	}
	if p.EstimatedSeconds != nil {
		minutes(float64(*p.EstimatedSeconds), repositories.BasisEstimate, repositories.SourceSlicer, "G-code analysis")
	}
	switch {
	case p.ActualSeconds != nil:
		minutes(float64(*p.ActualSeconds), repositories.BasisActual, repositories.SourcePrinter, "print time reported by the printer")
	case p.StartedAt != nil && p.CompletedAt != nil && !p.CompletedAt.Before(*p.StartedAt):
		minutes(p.CompletedAt.Sub(*p.StartedAt).Seconds(), repositories.BasisActual, repositories.SourcePrinter, "started_at to completed_at")
	}
	return out
}

// RecordPrintJob records the consumption of a completed print job. It is
// idempotent: calling it again records nothing new.
func (s *ConsumptionService) RecordPrintJob(ctx context.Context, tenantID, printJobID uuid.UUID) (*ConsumptionResult, error) {
	p, err := s.repo.PrintJobUsage(ctx, printJobID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrConsumptionPrintJobNotFound
	}
	if p.Status != "completed" {
		return nil, ErrConsumptionPrintJobOpen
	}
	occurred := time.Now().UTC()
	if p.CompletedAt != nil {
		occurred = p.CompletedAt.UTC()
	}
	if p.MaterialType == "" && s.log != nil {
		s.log.WithField("print_job_id", printJobID).Warn("print job has no material profile: material consumption not recorded")
	}
	job := JobRef{TenantID: tenantID, Kind: repositories.JobKindPrintJob, ID: printJobID,
		TaskID: p.TaskID, OrderID: p.OrderID, MachineID: p.MachineID}
	return s.Record(ctx, job, PrintJobFacts(p, occurred))
}
