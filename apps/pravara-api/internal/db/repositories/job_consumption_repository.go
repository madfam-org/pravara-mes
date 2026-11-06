package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// Job kinds, consumption kinds, bases, sources and units of job_consumption
// (migration 033).
const (
	JobKindDispatch = "dispatch"
	JobKindPrintJob = "print_job"
	JobKindTask     = "task"

	ConsumptionMaterial    = "material"
	ConsumptionMachineTime = "machine_time"

	BasisActual   = "actual"
	BasisEstimate = "estimate"

	SourcePrinter  = "printer"
	SourceOperator = "operator"
	SourceSlicer   = "slicer"

	UnitGram       = "g"
	UnitMillimetre = "mm"
	UnitMillilitre = "ml"
	UnitSheet      = "sheet"
	UnitMinute     = "min"

	// StockReferenceJobConsumption is the inventory_transactions.reference_type
	// of stock relieved for job consumption; reference_id is the
	// job_consumption row the movement settles.
	StockReferenceJobConsumption = "job_consumption"
)

// MaterialUnits are the units a material quantity may be recorded in.
var MaterialUnits = map[string]bool{UnitGram: true, UnitMillimetre: true, UnitMillilitre: true, UnitSheet: true}

// JobConsumption is one recorded quantity of a completed job: material or
// machine time, actual or estimate. Quantities only; no cost.
type JobConsumption struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"-"`
	JobKind    string     `json:"job_kind"`
	JobID      uuid.UUID  `json:"job_id"`
	DispatchID *uuid.UUID `json:"dispatch_id,omitempty"`
	PrintJobID *uuid.UUID `json:"print_job_id,omitempty"`
	TaskID     *uuid.UUID `json:"task_id,omitempty"`
	OrderID    *uuid.UUID `json:"order_id,omitempty"`
	// EngagementSlug is read from the order at query time (List only).
	EngagementSlug string     `json:"engagement_slug,omitempty"`
	MachineID      *uuid.UUID `json:"machine_id,omitempty"`
	Kind           string     `json:"kind"`
	MaterialCode   string     `json:"material_code,omitempty"`
	MaterialLot    string     `json:"material_lot,omitempty"`
	Quantity       float64    `json:"quantity"`
	Unit           string     `json:"unit"`
	Basis          string     `json:"basis"`
	Source         string     `json:"source"`
	Note           string     `json:"note,omitempty"`
	RecordedBy     *uuid.UUID `json:"recorded_by,omitempty"`
	OccurredAt     time.Time  `json:"occurred_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// Validate checks c against the constraints of migration 033.
func (c *JobConsumption) Validate() error {
	switch c.JobKind {
	case JobKindDispatch, JobKindPrintJob, JobKindTask:
	default:
		return fmt.Errorf("job_kind %q", c.JobKind)
	}
	if c.JobID == uuid.Nil {
		return errors.New("job_id is required")
	}
	if c.Basis != BasisActual && c.Basis != BasisEstimate {
		return fmt.Errorf("basis %q", c.Basis)
	}
	switch c.Source {
	case SourcePrinter, SourceOperator, SourceSlicer:
	default:
		return fmt.Errorf("source %q", c.Source)
	}
	if math.IsNaN(c.Quantity) || math.IsInf(c.Quantity, 0) || c.Quantity < 0 || c.Quantity >= 1e11 {
		return fmt.Errorf("quantity %v out of range", c.Quantity)
	}
	switch c.Kind {
	case ConsumptionMaterial:
		if !MaterialUnits[c.Unit] {
			return fmt.Errorf("material unit %q (want g, mm, ml or sheet)", c.Unit)
		}
		if c.MaterialCode == "" {
			return errors.New("material_code is required for material")
		}
	case ConsumptionMachineTime:
		if c.Unit != UnitMinute {
			return fmt.Errorf("machine time unit %q (want min)", c.Unit)
		}
		if c.MaterialCode != "" || c.MaterialLot != "" {
			return errors.New("machine time carries no material")
		}
	default:
		return fmt.Errorf("kind %q", c.Kind)
	}
	if c.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	return nil
}

// Round4 rounds q to the 4 decimals NUMERIC(15,4) stores.
func Round4(q float64) float64 { return math.Round(q*1e4) / 1e4 }

// JobConsumptionRepository stores job consumption and the stock relief and
// lookups it needs. Every statement runs in the tenant scope carried by ctx.
type JobConsumptionRepository struct {
	db DBTX
}

// NewJobConsumptionRepository creates the repository.
func NewJobConsumptionRepository(db DBTX) *JobConsumptionRepository {
	return &JobConsumptionRepository{db: db}
}

const jobConsumptionColumns = `c.id, c.tenant_id, c.job_kind, c.job_id, c.dispatch_id, c.print_job_id, c.task_id,
	c.order_id, c.machine_id, c.kind, COALESCE(c.material_code, ''), COALESCE(c.material_lot, ''),
	c.quantity, c.unit, c.basis, c.source, COALESCE(c.note, ''), c.recorded_by, c.occurred_at, c.created_at`

func scanJobConsumption(row interface{ Scan(...any) error }, extra ...any) (*JobConsumption, error) {
	var c JobConsumption
	var dispatchID, printJobID, taskID, orderID, machineID, recordedBy uuid.NullUUID
	dest := []any{&c.ID, &c.TenantID, &c.JobKind, &c.JobID, &dispatchID, &printJobID, &taskID,
		&orderID, &machineID, &c.Kind, &c.MaterialCode, &c.MaterialLot,
		&c.Quantity, &c.Unit, &c.Basis, &c.Source, &c.Note, &recordedBy, &c.OccurredAt, &c.CreatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	c.DispatchID, c.PrintJobID, c.TaskID = ptrUUID(dispatchID), ptrUUID(printJobID), ptrUUID(taskID)
	c.OrderID, c.MachineID, c.RecordedBy = ptrUUID(orderID), ptrUUID(machineID), ptrUUID(recordedBy)
	return &c, nil
}

// Insert stores c unless the same fact (job, kind, basis, source, material)
// is already recorded. It reports whether c was created; when it was not, c
// is overwritten with the stored fact.
func (r *JobConsumptionRepository) Insert(ctx context.Context, c *JobConsumption) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, fmt.Errorf("invalid job consumption: %w", err)
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.Quantity = Round4(c.Quantity)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO job_consumption (id, tenant_id, job_kind, job_id, dispatch_id, print_job_id, task_id,
			order_id, machine_id, kind, material_code, material_lot, quantity, unit, basis, source, note,
			recorded_by, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (tenant_id, job_kind, job_id, kind, basis, source, (COALESCE(material_code, ''))) DO NOTHING
		RETURNING created_at`,
		c.ID, c.TenantID, c.JobKind, c.JobID, c.DispatchID, c.PrintJobID, c.TaskID,
		c.OrderID, c.MachineID, c.Kind, nullableString(c.MaterialCode), nullableString(c.MaterialLot),
		c.Quantity, c.Unit, c.Basis, c.Source, nullableString(c.Note), c.RecordedBy, c.OccurredAt,
	).Scan(&c.CreatedAt)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("insert job consumption: %w", err)
	}
	existing, err := scanJobConsumption(r.db.QueryRowContext(ctx, `
		SELECT `+jobConsumptionColumns+` FROM job_consumption c
		WHERE c.job_kind = $1 AND c.job_id = $2 AND c.kind = $3 AND c.basis = $4 AND c.source = $5
		  AND COALESCE(c.material_code, '') = $6 AND `+tenantMatchOn("c"),
		c.JobKind, c.JobID, c.Kind, c.Basis, c.Source, c.MaterialCode))
	if err != nil {
		return false, fmt.Errorf("load recorded job consumption: %w", err)
	}
	*c = *existing
	return false, nil
}

// MaterialFacts lists a job's material facts for materialCode, oldest
// first. unit "" matches every unit.
func (r *JobConsumptionRepository) MaterialFacts(ctx context.Context, jobKind string, jobID uuid.UUID, materialCode, unit string) ([]JobConsumption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+jobConsumptionColumns+` FROM job_consumption c
		WHERE c.job_kind = $1 AND c.job_id = $2 AND c.kind = 'material' AND c.material_code = $3
		  AND ($4 = '' OR c.unit = $4) AND `+tenantMatchOn("c")+`
		ORDER BY c.created_at, c.id`, jobKind, jobID, materialCode, unit)
	if err != nil {
		return nil, fmt.Errorf("list material facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JobConsumption
	for rows.Next() {
		c, err := scanJobConsumption(rows)
		if err != nil {
			return nil, fmt.Errorf("scan material fact: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// JobConsumptionFilter selects rows for GET /v1/jobs/consumption.
type JobConsumptionFilter struct {
	From           *time.Time // occurred_at >= From
	To             *time.Time // occurred_at < To
	TaskID         *uuid.UUID
	OrderID        *uuid.UUID
	EngagementSlug *string
	Limit          int
	Offset         int
}

// List returns the tenant's consumption rows with each order's current
// engagement slug, ordered by occurred_at, plus the total count.
func (r *JobConsumptionRepository) List(ctx context.Context, f JobConsumptionFilter) ([]JobConsumption, int, error) {
	where := " WHERE " + tenantMatchOn("c")
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND "+cond, len(args))
	}
	if f.From != nil {
		add("c.occurred_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("c.occurred_at < $%d", *f.To)
	}
	if f.TaskID != nil {
		add("c.task_id = $%d", *f.TaskID)
	}
	if f.OrderID != nil {
		add("c.order_id = $%d", *f.OrderID)
	}
	if f.EngagementSlug != nil {
		add("o.engagement_slug = $%d", *f.EngagementSlug)
	}
	from := " FROM job_consumption c LEFT JOIN orders o ON o.id = c.order_id"

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT count(*)"+from+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count job consumption: %w", err)
	}
	query := "SELECT " + jobConsumptionColumns + ", COALESCE(o.engagement_slug, '')" + from + where +
		" ORDER BY c.occurred_at, c.id"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list job consumption: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []JobConsumption{}
	for rows.Next() {
		var slug string
		c, err := scanJobConsumption(rows, &slug)
		if err != nil {
			return nil, 0, fmt.Errorf("scan job consumption: %w", err)
		}
		c.EngagementSlug = slug
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// StockItem is the inventory item a material maps to.
type StockItem struct {
	ID   uuid.UUID
	Name string
}

// LockStockItem returns the tenant's inventory item whose SKU is sku and
// whose unit is unit, locked for update (nil when the material maps to no
// item in that unit).
func (r *JobConsumptionRepository) LockStockItem(ctx context.Context, sku, unit string) (*StockItem, error) {
	var it StockItem
	err := r.db.QueryRowContext(ctx, `
		SELECT id, name FROM inventory_items
		WHERE sku = $1 AND unit = $2 AND `+tenantMatch+` FOR UPDATE`, sku, unit).Scan(&it.ID, &it.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock inventory item: %w", err)
	}
	return &it, nil
}

// RelievedQuantity is the stock already relieved from itemID for one job
// (positive when stock left), summed over its job_consumption movements.
func (r *JobConsumptionRepository) RelievedQuantity(ctx context.Context, itemID uuid.UUID, jobKind string, jobID uuid.UUID) (float64, error) {
	var q float64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(-SUM(t.quantity), 0) FROM inventory_transactions t
		WHERE t.inventory_item_id = $1 AND t.reference_type = '`+StockReferenceJobConsumption+`'
		  AND t.reference_id IN (SELECT c.id FROM job_consumption c
		                         WHERE c.job_kind = $2 AND c.job_id = $3 AND `+tenantMatchOn("c")+`)
		  AND `+tenantMatchOn("t"), itemID, jobKind, jobID).Scan(&q)
	if err != nil {
		return 0, fmt.Errorf("relieved quantity: %w", err)
	}
	return q, nil
}

// PostStockMovement changes the item's quantity on hand by quantity
// (negative relieves stock) and records the movement with its running
// balance, referencing the job_consumption row it settles. The caller holds
// the item lock (LockStockItem) in the same transaction.
func (r *JobConsumptionRepository) PostStockMovement(ctx context.Context, itemID, consumptionID uuid.UUID, txnType string, quantity float64, note string) (*InventoryTransaction, error) {
	t := &InventoryTransaction{ID: uuid.New(), InventoryItemID: itemID, TransactionType: txnType, Quantity: Round4(quantity)}
	err := r.db.QueryRowContext(ctx, `
		UPDATE inventory_items SET quantity_on_hand = quantity_on_hand + $2, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch+`
		RETURNING tenant_id, quantity_on_hand`, itemID, t.Quantity).Scan(&t.TenantID, &t.RunningBalance)
	if err != nil {
		return nil, fmt.Errorf("relieve inventory item: %w", err)
	}
	ref := StockReferenceJobConsumption
	t.ReferenceType, t.ReferenceID, t.Notes = &ref, &consumptionID, &note
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO inventory_transactions (id, tenant_id, inventory_item_id, transaction_type, quantity,
			running_balance, reference_type, reference_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at`, t.ID, t.TenantID, itemID, txnType, t.Quantity, t.RunningBalance,
		ref, consumptionID, note).Scan(&t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("record inventory transaction: %w", err)
	}
	return t, nil
}

// BatchLotRef is the batch lot a material lot number names.
type BatchLotRef struct {
	ID          uuid.UUID
	ProductCode string
}

// BatchLotByNumber returns the tenant's most recent batch lot with
// lot_number (nil when none).
func (r *JobConsumptionRepository) BatchLotByNumber(ctx context.Context, lotNumber string) (*BatchLotRef, error) {
	var b BatchLotRef
	var code sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, product_code FROM batch_lots WHERE lot_number = $1 AND `+tenantMatch+`
		ORDER BY created_at DESC LIMIT 1`, lotNumber).Scan(&b.ID, &code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find batch lot: %w", err)
	}
	b.ProductCode = code.String
	return &b, nil
}

// TaskGenealogy returns the latest genealogy record of a task and whether it
// is sealed (nil when the task has none).
func (r *JobConsumptionRepository) TaskGenealogy(ctx context.Context, taskID uuid.UUID) (*uuid.UUID, bool, error) {
	var id uuid.UUID
	var status string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, status FROM product_genealogy WHERE task_id = $1 AND `+tenantMatch+`
		ORDER BY created_at DESC LIMIT 1`, taskID).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("find task genealogy: %w", err)
	}
	return &id, status == string(GenealogyStatusSealed), nil
}

// GenealogyHasMaterial reports whether a genealogy record already lists
// materialCode.
func (r *JobConsumptionRepository) GenealogyHasMaterial(ctx context.Context, genealogyID uuid.UUID, materialCode string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM genealogy_material_consumption
		               WHERE genealogy_id = $1 AND material_code = $2 AND `+tenantMatch+`)`,
		genealogyID, materialCode).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check genealogy material: %w", err)
	}
	return ok, nil
}

// TaskJob is a task with its order and its latest completed dispatch.
type TaskJob struct {
	TaskID            uuid.UUID
	OrderID           *uuid.UUID
	MachineID         *uuid.UUID
	DispatchID        *uuid.UUID
	DispatchMachineID *uuid.UUID
}

// TaskJob loads a task in the tenant (nil when not found).
func (r *JobConsumptionRepository) TaskJob(ctx context.Context, taskID uuid.UUID) (*TaskJob, error) {
	tj := &TaskJob{TaskID: taskID}
	var orderID, machineID, dispatchID, dispatchMachine uuid.NullUUID
	err := r.db.QueryRowContext(ctx, `
		SELECT t.order_id, t.machine_id, d.id, d.machine_id
		FROM tasks t
		LEFT JOIN LATERAL (
			SELECT id, machine_id FROM dispatch_jobs
			WHERE task_id = t.id AND status = 'completed' AND `+tenantMatch+`
			ORDER BY completed_at DESC NULLS LAST, created_at DESC LIMIT 1
		) d ON true
		WHERE t.id = $1 AND `+tenantMatchOn("t"), taskID).Scan(&orderID, &machineID, &dispatchID, &dispatchMachine)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	tj.OrderID, tj.MachineID, tj.DispatchID, tj.DispatchMachineID = ptrUUID(orderID), ptrUUID(machineID), ptrUUID(dispatchID), ptrUUID(dispatchMachine)
	return tj, nil
}

// PrintJobUsage is what a print job recorded about its consumption, with
// its material profile.
type PrintJobUsage struct {
	ID                   uuid.UUID
	Status               string
	TaskID               *uuid.UUID
	OrderID              *uuid.UUID
	MachineID            *uuid.UUID
	EstimatedSeconds     *int
	EstimatedFilamentMM  *float64
	EstimatedFilamentG   *float64
	ActualSeconds        *int
	ActualFilamentMM     *float64
	StartedAt            *time.Time
	CompletedAt          *time.Time
	MaterialType         string
	MaterialDiameterMM   *float64
	MaterialDensityGcm3  *float64
	MaterialProfileFound bool
}

// PrintJobUsage loads a print job (nil when not in the tenant). The order is
// the print job's, else its task's.
func (r *JobConsumptionRepository) PrintJobUsage(ctx context.Context, id uuid.UUID) (*PrintJobUsage, error) {
	p := &PrintJobUsage{ID: id}
	var taskID, orderID, machineID, profileID uuid.NullUUID
	var estS, actS sql.NullInt64
	var estMM, estG, actMM, diameter, density sql.NullFloat64
	var started, completed sql.NullTime
	var materialType sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT pj.status, pj.task_id, COALESCE(pj.order_id, t.order_id), COALESCE(pc.machine_id, t.machine_id),
		       pj.estimated_time_seconds, pj.estimated_filament_mm, pj.estimated_filament_g,
		       pj.actual_time_seconds, pj.actual_filament_mm, pj.started_at, pj.completed_at,
		       mp.id, mp.material_type, mp.diameter, mp.density
		FROM print_jobs pj
		LEFT JOIN tasks t ON t.id = pj.task_id
		LEFT JOIN printer_connections pc ON pc.id = pj.connection_id
		LEFT JOIN material_profiles mp ON mp.id = pj.material_id
		WHERE pj.id = $1 AND `+tenantMatchOn("pj"), id).Scan(&p.Status, &taskID, &orderID, &machineID,
		&estS, &estMM, &estG, &actS, &actMM, &started, &completed, &profileID, &materialType, &diameter, &density)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load print job: %w", err)
	}
	p.TaskID, p.OrderID, p.MachineID = ptrUUID(taskID), ptrUUID(orderID), ptrUUID(machineID)
	if estS.Valid {
		v := int(estS.Int64)
		p.EstimatedSeconds = &v
	}
	if actS.Valid {
		v := int(actS.Int64)
		p.ActualSeconds = &v
	}
	p.EstimatedFilamentMM, p.EstimatedFilamentG, p.ActualFilamentMM = ptrFloat(estMM), ptrFloat(estG), ptrFloat(actMM)
	p.MaterialDiameterMM, p.MaterialDensityGcm3 = ptrFloat(diameter), ptrFloat(density)
	if started.Valid {
		p.StartedAt = &started.Time
	}
	if completed.Valid {
		p.CompletedAt = &completed.Time
	}
	p.MaterialProfileFound, p.MaterialType = profileID.Valid, materialType.String
	return p, nil
}

func ptrFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}
