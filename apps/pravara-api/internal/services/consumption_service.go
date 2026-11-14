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
//   - A material code names a stock item through the registry (the code,
//     an alias, or a linked item's SKU), else an unlinked item whose SKU is
//     the code (docs/operations/inventory.md).
//   - For each job and stock item, the effective quantity is the latest
//     actual, else the latest estimate, across every code and unit naming
//     that item. Stock relieved for the job always equals the effective
//     quantity in the item's unit: the first relief is a 'consumption'
//     movement, a later actual posts the difference as an 'adjustment'.
//   - A quantity whose unit has no conversion to the item's unit relieves
//     nothing: it is recorded as a conversion gap and settled once a
//     conversion exists (MaterialService.AddConversion).
//   - When the material's lot is known and the job has an unsealed
//     genealogy record, the material is written to that record once.
//
// Every method runs in the tenant scope carried by ctx.
type ConsumptionService struct {
	q         repositories.DBTX
	repo      *repositories.JobConsumptionRepository
	stock     *repositories.StockRepository
	genealogy *repositories.GenealogyRepository
	log       *logrus.Logger
}

// NewConsumptionService creates the service. q is the tenant-scoped handle
// the repositories use (savepoints run on it).
func NewConsumptionService(q repositories.DBTX, repo *repositories.JobConsumptionRepository, genealogy *repositories.GenealogyRepository, log *logrus.Logger) *ConsumptionService {
	return &ConsumptionService{q: q, repo: repo, stock: repositories.NewStockRepository(q), genealogy: genealogy, log: log}
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

// Record stores facts for job and settles stock and genealogy for every
// material a new fact touches.
func (s *ConsumptionService) Record(ctx context.Context, job JobRef, facts []repositories.JobConsumption) (*ConsumptionResult, error) {
	res := &ConsumptionResult{}
	var touched []string
	seen := map[string]bool{}
	for i := range facts {
		c := facts[i]
		job.apply(&c)
		created, err := s.repo.Insert(ctx, &c)
		if err != nil {
			return nil, err
		}
		res.Facts, res.Created = append(res.Facts, c), append(res.Created, created)
		if created && c.Kind == repositories.ConsumptionMaterial && !seen[c.MaterialCode] {
			seen[c.MaterialCode] = true
			touched = append(touched, c.MaterialCode)
		}
	}
	if len(touched) > 0 {
		ids, err := s.JobLockSet(ctx, job.Kind, job.ID, touched)
		if err != nil {
			return nil, err
		}
		if err := s.stock.LockItems(ctx, ids); err != nil {
			return nil, err
		}
	}
	settled := map[uuid.UUID]bool{}
	for _, code := range touched {
		item, err := s.stock.ResolveStockItem(ctx, code)
		if err != nil {
			return nil, err
		}
		var moves []repositories.InventoryTransaction
		switch {
		case item == nil:
			moves, err = s.withdrawRelief(ctx, job.Kind, job.ID, code)
		case !settled[item.ID]:
			settled[item.ID] = true
			moves, err = s.settleItem(ctx, job.Kind, job.ID, item)
		}
		if err != nil {
			return nil, err
		}
		res.Movements = append(res.Movements, moves...)
		if err := s.linkGenealogy(ctx, job, code, item); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// JobLockSet is every stock item settling a job may move: the items the
// given codes name now, and every item the job's movements already touched
// (relief that may have to move to where a code points now). The caller
// locks them with StockRepository.LockItems before settling.
func (s *ConsumptionService) JobLockSet(ctx context.Context, jobKind string, jobID uuid.UUID, codes []string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for _, code := range codes {
		id, err := s.stock.ResolveStockItemID(ctx, code)
		if err != nil {
			return nil, err
		}
		if id != nil {
			ids = append(ids, *id)
		}
	}
	relief, err := s.repo.JobRelief(ctx, jobKind, jobID)
	if err != nil {
		return nil, err
	}
	for _, r := range relief {
		ids = append(ids, r.ItemID)
	}
	return ids, nil
}

// effective is the latest actual, else the latest estimate, among the rows
// no correction superseded. facts are ordered by created_at.
func effective(facts []repositories.JobConsumption) *repositories.JobConsumption {
	superseded := map[uuid.UUID]bool{}
	for _, f := range facts {
		if f.SupersedesID != nil {
			superseded[*f.SupersedesID] = true
		}
	}
	var actual, estimate *repositories.JobConsumption
	for i := range facts {
		if superseded[facts[i].ID] {
			continue
		}
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

// settleItem brings a job's stock relief for the codes that name item to
// the job's effective quantity, in the item's unit, taken from item alone.
// Relief those codes took from another item (the code named another item
// when it was settled) is reversed there first, so a job is never relieved
// twice. Without a conversion it records a gap and moves nothing on item.
// The caller holds the locks (JobLockSet).
func (s *ConsumptionService) settleItem(ctx context.Context, jobKind string, jobID uuid.UUID, item *repositories.StockItem) ([]repositories.InventoryTransaction, error) {
	facts, err := s.repo.ItemFacts(ctx, jobKind, jobID, item)
	if err != nil {
		return nil, err
	}
	eff := effective(facts)
	if eff == nil {
		return nil, nil
	}
	codes := map[string]bool{}
	for _, f := range facts {
		codes[f.MaterialCode] = true
	}
	moves, relieved, err := s.moveStrayRelief(ctx, jobKind, jobID, codes, &item.ID, eff)
	if err != nil {
		return nil, err
	}
	factor, ok, err := stockFactor(ctx, s.stock, item, eff.Unit)
	if err != nil {
		return nil, err
	}
	if !ok {
		job := jobID
		return moves, s.stock.OpenGap(ctx, repositories.ConversionGap{InventoryItemID: item.ID, MaterialID: item.MaterialID,
			SourceType: repositories.GapSourceJobConsumption, SourceID: eff.ID, JobKind: jobKind, JobID: &job,
			Quantity: eff.Quantity, FromUnit: eff.Unit, ToUnit: item.Unit})
	}
	if err := s.stock.CloseJobGaps(ctx, item.ID, jobKind, jobID); err != nil {
		return nil, err
	}
	target := repositories.Round4(eff.Quantity * factor)
	delta := repositories.Round4(target - relieved)
	if delta == 0 {
		return moves, nil
	}
	typ := "adjustment"
	if relieved == 0 && delta > 0 {
		typ = "consumption"
	}
	note := fmt.Sprintf("%s %s: %s %s %s (%s, %s)", jobKind, jobID, eff.MaterialCode,
		formatQuantity(eff.Quantity), eff.Unit, eff.Basis, eff.Source)
	if factor != 1 {
		note += fmt.Sprintf(" = %s %s", formatQuantity(target), item.Unit)
	}
	mv, err := s.stock.PostMovement(ctx, repositories.Movement{ItemID: item.ID, Type: typ, Quantity: -delta,
		ReferenceType: repositories.StockReferenceJobConsumption, ReferenceID: eff.ID, Note: note})
	if err != nil {
		return nil, err
	}
	return append(moves, *mv), nil
}

// moveStrayRelief reverses, on every item other than keep (nil: every
// item), the relief the job took under codes, and returns what stays
// relieved on keep. Reversals reference ref, a fact under one of the
// codes, so the per-code sums net to zero.
func (s *ConsumptionService) moveStrayRelief(ctx context.Context, jobKind string, jobID uuid.UUID, codes map[string]bool,
	keep *uuid.UUID, ref *repositories.JobConsumption) ([]repositories.InventoryTransaction, float64, error) {
	relief, err := s.repo.JobRelief(ctx, jobKind, jobID)
	if err != nil {
		return nil, 0, err
	}
	var kept float64
	stray := map[uuid.UUID]float64{}
	var order []uuid.UUID
	for _, r := range relief {
		if !codes[r.MaterialCode] {
			continue
		}
		if keep != nil && r.ItemID == *keep {
			kept += r.Quantity
			continue
		}
		if _, ok := stray[r.ItemID]; !ok {
			order = append(order, r.ItemID)
		}
		stray[r.ItemID] += r.Quantity
	}
	var moves []repositories.InventoryTransaction
	for _, itemID := range order {
		q := repositories.Round4(stray[itemID])
		if q == 0 {
			continue
		}
		note := fmt.Sprintf("%s %s: %s now names another stock item; relief returned here", jobKind, jobID, ref.MaterialCode)
		mv, err := s.stock.PostMovement(ctx, repositories.Movement{ItemID: itemID, Type: "adjustment", Quantity: q,
			ReferenceType: repositories.StockReferenceJobConsumption, ReferenceID: ref.ID, Note: note})
		if err != nil {
			return nil, 0, err
		}
		moves = append(moves, *mv)
	}
	return moves, repositories.Round4(kept), nil
}

// withdrawRelief returns every item's relief a job took under code when
// the code names no stock item any more (its material lost its item).
func (s *ConsumptionService) withdrawRelief(ctx context.Context, jobKind string, jobID uuid.UUID, code string) ([]repositories.InventoryTransaction, error) {
	facts, err := s.repo.MaterialFacts(ctx, jobKind, jobID, code, "")
	if err != nil || len(facts) == 0 {
		return nil, err
	}
	moves, _, err := s.moveStrayRelief(ctx, jobKind, jobID, map[string]bool{code: true}, nil, &facts[len(facts)-1])
	return moves, err
}

// ResettleJob settles one job's stock relief for item again (a conversion
// gap waiting for a conversion that now exists). The caller holds the
// locks of the job's lock set (JobLockSet).
func (s *ConsumptionService) ResettleJob(ctx context.Context, jobKind string, jobID uuid.UUID, item *repositories.StockItem) ([]repositories.InventoryTransaction, error) {
	return s.settleItem(ctx, jobKind, jobID, item)
}

func (s *ConsumptionService) linkGenealogy(ctx context.Context, job JobRef, code string, item *repositories.StockItem) error {
	if s.genealogy == nil {
		return nil
	}
	facts, err := s.repo.MaterialFacts(ctx, job.Kind, job.ID, code, "")
	if err != nil {
		return err
	}
	eff := effective(facts)
	if eff == nil {
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
	// Disposition is good (default), scrap or failed_print.
	Disposition string
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
		OccurredAt: time.Now().UTC(), Disposition: r.Disposition}
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
	return s.recordPrintJob(ctx, tenantID, printJobID, repositories.DispositionGood)
}

func (s *ConsumptionService) recordPrintJob(ctx context.Context, tenantID, printJobID uuid.UUID, disposition string) (*ConsumptionResult, error) {
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
	facts := PrintJobFacts(p, occurred)
	for i := range facts {
		if facts[i].Kind == repositories.ConsumptionMaterial {
			facts[i].Disposition = disposition
		}
	}
	return s.Record(ctx, job, facts)
}
