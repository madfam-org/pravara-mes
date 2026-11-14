package repositories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrConsumptionAlreadySuperseded is returned when a correction targets a
// row another correction already superseded (correct the latest row).
var ErrConsumptionAlreadySuperseded = errors.New("this consumption row is already superseded")

// supersededByOf is the SQL for the correction that superseded a row (NULL
// when none).
func supersededByOf(alias string) string {
	return "(SELECT s.id FROM job_consumption s WHERE s.supersedes_id = " + alias + ".id AND " + tenantMatchOn("s") + ")"
}

// Get returns one consumption row with the correction that superseded it,
// if any (nil when not in the tenant).
func (r *JobConsumptionRepository) Get(ctx context.Context, id uuid.UUID) (*JobConsumption, error) {
	var supersededBy uuid.NullUUID
	c, err := scanJobConsumption(r.db.QueryRowContext(ctx, `SELECT `+jobConsumptionColumns+`, `+supersededByOf("c")+`
		FROM job_consumption c WHERE c.id = $1 AND `+tenantMatchOn("c"), id), &supersededBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job consumption: %w", err)
	}
	c.SupersededBy = ptrUUID(supersededBy)
	return c, nil
}

// InsertCorrection stores c, an operator actual that supersedes
// c.SupersedesID. History is never updated: the correction is a new row.
// ErrConsumptionAlreadySuperseded when another correction got there first.
func (r *JobConsumptionRepository) InsertCorrection(ctx context.Context, c *JobConsumption) error {
	if c.SupersedesID == nil {
		return errors.New("insert correction: supersedes_id is required")
	}
	if c.Disposition == "" {
		c.Disposition = DispositionGood
	}
	if err := c.Validate(); err != nil {
		return fmt.Errorf("invalid job consumption: %w", err)
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.Quantity = Round4(c.Quantity)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO job_consumption (id, tenant_id, job_kind, job_id, dispatch_id, print_job_id, task_id,
			order_id, machine_id, kind, material_code, material_lot, quantity, unit, basis, source, note,
			recorded_by, occurred_at, disposition, supersedes_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		RETURNING created_at`,
		c.ID, c.TenantID, c.JobKind, c.JobID, c.DispatchID, c.PrintJobID, c.TaskID,
		c.OrderID, c.MachineID, c.Kind, nullableString(c.MaterialCode), nullableString(c.MaterialLot),
		c.Quantity, c.Unit, c.Basis, c.Source, nullableString(c.Note), c.RecordedBy, c.OccurredAt, c.Disposition,
		c.SupersedesID).Scan(&c.CreatedAt)
	if uniqueViolation(err) {
		return ErrConsumptionAlreadySuperseded
	}
	if err != nil {
		return fmt.Errorf("insert correction: %w", err)
	}
	return nil
}

// Print job statuses the operator completion path reads (print_jobs, 014).
const (
	PrintJobCompleted = "completed"
	PrintJobCancelled = "cancelled"
	PrintJobFailed    = "failed"
)

// MarkPrintJobCompleted records that an operator saw a print job finish:
// status completed, progress 100, completed_at now unless already set. It
// changes nothing on a completed, cancelled or failed job and reports
// whether it changed the row. It is a record only: no machine is told
// anything.
func (r *JobConsumptionRepository) MarkPrintJobCompleted(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE print_jobs SET status = 'completed', progress_percent = 100,
			completed_at = COALESCE(completed_at, NOW()), updated_at = NOW()
		WHERE id = $1 AND status NOT IN ('completed', 'cancelled', 'failed') AND `+tenantMatch, id)
	if err != nil {
		return false, fmt.Errorf("mark print job completed: %w", err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
