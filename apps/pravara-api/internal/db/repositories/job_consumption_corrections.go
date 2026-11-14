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
	return "(SELECT s.id FROM job_consumption_corrections s WHERE s.supersedes_id = " + alias + ".id AND " +
		tenantMatchOn("s") + ")"
}

// Get returns one consumption row (a recorded fact or a correction) with
// the correction that superseded it, if any (nil when not in the tenant).
func (r *JobConsumptionRepository) Get(ctx context.Context, id uuid.UUID) (*JobConsumption, error) {
	var supersededBy uuid.NullUUID
	c, err := scanJobConsumption(r.db.QueryRowContext(ctx, `SELECT `+jobConsumptionColumns+`, `+supersededByOf("c")+`
		FROM job_consumption_facts c WHERE c.id = $1 AND `+tenantMatchOn("c"), id), &supersededBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job consumption: %w", err)
	}
	c.SupersededBy = ptrUUID(supersededBy)
	return c, nil
}

// LineFacts lists a job's rows of one kind and line: for material, every
// row whose code resolves to the same registry code as code (or, when code
// is not registered, carries code itself); for machine time, every
// machine-time row. Oldest first, corrections included.
func (r *JobConsumptionRepository) LineFacts(ctx context.Context, jobKind string, jobID uuid.UUID, kind, code string) ([]JobConsumption, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+jobConsumptionColumns+` FROM job_consumption_facts c
		WHERE c.job_kind = $1 AND c.job_id = $2 AND c.kind = $3
		  AND ($3 = 'machine_time'
		       OR COALESCE(material_resolve(c.material_code)::text, 'code:' || c.material_code)
		        = COALESCE(material_resolve($4)::text, 'code:' || $4))
		  AND `+tenantMatchOn("c")+`
		ORDER BY c.created_at, c.id`, jobKind, jobID, kind, code)
	if err != nil {
		return nil, fmt.Errorf("list line facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []JobConsumption
	for rows.Next() {
		c, err := scanJobConsumption(rows)
		if err != nil {
			return nil, fmt.Errorf("scan line fact: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// InsertCorrection stores c, an operator actual that supersedes
// c.SupersedesID, in job_consumption_corrections (migration 036): history
// is never updated, and the 033 fact key is left alone. c carries the
// corrected row's job, kind and material; RootID is the recorded fact the
// chain corrects. ErrConsumptionAlreadySuperseded when another correction
// got there first.
func (r *JobConsumptionRepository) InsertCorrection(ctx context.Context, c *JobConsumption) error {
	if c.SupersedesID == nil || c.RootID == uuid.Nil {
		return errors.New("insert correction: supersedes_id and the corrected fact are required")
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
		INSERT INTO job_consumption_corrections (id, tenant_id, consumption_id, supersedes_id, quantity, unit,
			disposition, note, recorded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at`,
		c.ID, c.TenantID, c.RootID, c.SupersedesID, c.Quantity, c.Unit, c.Disposition, nullableString(c.Note),
		c.RecordedBy).Scan(&c.CreatedAt)
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
	PrintJobPending   = "pending"
	PrintJobPreparing = "preparing"
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
