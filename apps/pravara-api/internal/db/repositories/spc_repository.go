// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// SPCRepository handles SPC (Statistical Process Control) database operations.
type SPCRepository struct {
	db *sql.DB
}

// NewSPCRepository creates a new SPC repository.
func NewSPCRepository(db *sql.DB) *SPCRepository {
	return &SPCRepository{db: db}
}

// SPCControlLimit represents a statistical control limit for a machine metric.
type SPCControlLimit struct {
	ID          uuid.UUID      `json:"id"`
	TenantID    uuid.UUID      `json:"tenant_id"`
	MachineID   uuid.UUID      `json:"machine_id"`
	MetricType  string         `json:"metric_type"`
	Mean        float64        `json:"mean"`
	Stddev      float64        `json:"stddev"`
	UCL         float64        `json:"ucl"`
	LCL         float64        `json:"lcl"`
	USL         *float64       `json:"usl,omitempty"`
	LSL         *float64       `json:"lsl,omitempty"`
	SampleCount int            `json:"sample_count"`
	SampleStart time.Time      `json:"sample_start"`
	SampleEnd   time.Time      `json:"sample_end"`
	IsActive    bool           `json:"is_active"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// SPCViolation represents a control limit violation detected for a machine metric.
type SPCViolation struct {
	ID             uuid.UUID      `json:"id"`
	TenantID       uuid.UUID      `json:"tenant_id"`
	ControlLimitID uuid.UUID      `json:"control_limit_id"`
	MachineID      uuid.UUID      `json:"machine_id"`
	ViolationType  string         `json:"violation_type"` // above_ucl, below_lcl, run_of_7, trend
	MetricType     string         `json:"metric_type"`
	Value          float64        `json:"value"`
	LimitValue     float64        `json:"limit_value"`
	DetectedAt     time.Time      `json:"detected_at"`
	Acknowledged   bool           `json:"acknowledged"`
	AcknowledgedBy *uuid.UUID     `json:"acknowledged_by,omitempty"`
	AcknowledgedAt *time.Time     `json:"acknowledged_at,omitempty"`
	Notes          *string        `json:"notes,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// ListLimits retrieves all control limits for a given machine.
func (r *SPCRepository) ListLimits(ctx context.Context, machineID uuid.UUID) ([]SPCControlLimit, error) {
	query := `
		SELECT id, tenant_id, machine_id, metric_type, mean, stddev,
		       ucl, lcl, usl, lsl, sample_count, sample_start, sample_end,
		       is_active, metadata, created_at, updated_at
		FROM spc_control_limits
		WHERE machine_id = $1
		ORDER BY metric_type ASC, created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, machineID)
	if err != nil {
		return nil, fmt.Errorf("failed to query SPC control limits: %w", err)
	}
	defer rows.Close()

	var limits []SPCControlLimit
	for rows.Next() {
		cl, err := r.scanControlLimit(rows)
		if err != nil {
			return nil, err
		}
		limits = append(limits, *cl)
	}

	return limits, nil
}

// GetLimitByID retrieves a control limit by its unique identifier.
// Returns nil, nil if not found.
func (r *SPCRepository) GetLimitByID(ctx context.Context, id uuid.UUID) (*SPCControlLimit, error) {
	query := `
		SELECT id, tenant_id, machine_id, metric_type, mean, stddev,
		       ucl, lcl, usl, lsl, sample_count, sample_start, sample_end,
		       is_active, metadata, created_at, updated_at
		FROM spc_control_limits
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	cl, err := r.scanControlLimitRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get SPC control limit: %w", err)
	}

	return cl, nil
}

// UpsertLimit inserts or updates a control limit.
// On conflict for (machine_id, metric_type) where is_active=true, the existing
// active limit is updated.
func (r *SPCRepository) UpsertLimit(ctx context.Context, limit *SPCControlLimit) error {
	query := `
		INSERT INTO spc_control_limits (
			id, tenant_id, machine_id, metric_type, mean, stddev,
			ucl, lcl, usl, lsl, sample_count, sample_start, sample_end,
			is_active, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (machine_id, metric_type) WHERE is_active = true
		DO UPDATE SET
			mean = EXCLUDED.mean,
			stddev = EXCLUDED.stddev,
			ucl = EXCLUDED.ucl,
			lcl = EXCLUDED.lcl,
			usl = EXCLUDED.usl,
			lsl = EXCLUDED.lsl,
			sample_count = EXCLUDED.sample_count,
			sample_start = EXCLUDED.sample_start,
			sample_end = EXCLUDED.sample_end,
			metadata = EXCLUDED.metadata,
			updated_at = NOW()
		RETURNING id, created_at, updated_at
	`

	if limit.ID == uuid.Nil {
		limit.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(limit.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		limit.ID, limit.TenantID, limit.MachineID, limit.MetricType,
		limit.Mean, limit.Stddev, limit.UCL, limit.LCL,
		limit.USL, limit.LSL, limit.SampleCount,
		limit.SampleStart, limit.SampleEnd,
		limit.IsActive, metadataJSON,
	).Scan(&limit.ID, &limit.CreatedAt, &limit.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert SPC control limit: %w", err)
	}

	return nil
}

// ComputeLimits calculates statistical control limits from telemetry data.
// Uses SQL aggregate functions (AVG, STDDEV) on telemetry records for the
// specified machine, metric type, and sample window.
// Returns UCL = mean + 3*stddev, LCL = mean - 3*stddev.
func (r *SPCRepository) ComputeLimits(ctx context.Context, machineID uuid.UUID, metricType string, sampleDays int) (*SPCControlLimit, error) {
	sampleStart := time.Now().AddDate(0, 0, -sampleDays)

	query := `
		SELECT
			AVG(value) as mean,
			STDDEV(value) as stddev,
			COUNT(*) as sample_count,
			MIN(timestamp) as sample_start,
			MAX(timestamp) as sample_end
		FROM telemetry
		WHERE machine_id = $1
		  AND metric_type = $2
		  AND timestamp >= $3
	`

	var mean, stddev sql.NullFloat64
	var sampleCount int
	var dbSampleStart, dbSampleEnd sql.NullTime

	err := r.db.QueryRowContext(ctx, query, machineID, metricType, sampleStart).Scan(
		&mean, &stddev, &sampleCount, &dbSampleStart, &dbSampleEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute SPC limits: %w", err)
	}

	if sampleCount < 2 || !mean.Valid || !stddev.Valid {
		return nil, fmt.Errorf("insufficient telemetry data: need at least 2 samples, got %d", sampleCount)
	}

	computedMean := mean.Float64
	computedStddev := stddev.Float64

	// Prevent zero stddev from creating UCL=LCL=mean
	if computedStddev == 0 {
		computedStddev = math.Abs(computedMean) * 0.001
		if computedStddev == 0 {
			computedStddev = 0.001
		}
	}

	limit := &SPCControlLimit{
		MachineID:   machineID,
		MetricType:  metricType,
		Mean:        computedMean,
		Stddev:      computedStddev,
		UCL:         computedMean + 3*computedStddev,
		LCL:         computedMean - 3*computedStddev,
		SampleCount: sampleCount,
		IsActive:    true,
	}

	if dbSampleStart.Valid {
		limit.SampleStart = dbSampleStart.Time
	}
	if dbSampleEnd.Valid {
		limit.SampleEnd = dbSampleEnd.Time
	}

	return limit, nil
}

// ListViolations retrieves SPC violations for a machine, optionally filtering
// to only unacknowledged violations.
func (r *SPCRepository) ListViolations(ctx context.Context, machineID uuid.UUID, unackedOnly bool) ([]SPCViolation, error) {
	query := `
		SELECT id, tenant_id, control_limit_id, machine_id, violation_type,
		       metric_type, value, limit_value, detected_at,
		       acknowledged, acknowledged_by, acknowledged_at, notes,
		       metadata, created_at
		FROM spc_violations
		WHERE machine_id = $1
	`

	var args []interface{}
	args = append(args, machineID)

	if unackedOnly {
		query += " AND acknowledged = false"
	}

	query += " ORDER BY detected_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query SPC violations: %w", err)
	}
	defer rows.Close()

	var violations []SPCViolation
	for rows.Next() {
		v, err := r.scanViolation(rows)
		if err != nil {
			return nil, err
		}
		violations = append(violations, *v)
	}

	return violations, nil
}

// CreateViolation inserts a new SPC violation record.
func (r *SPCRepository) CreateViolation(ctx context.Context, v *SPCViolation) error {
	query := `
		INSERT INTO spc_violations (
			id, tenant_id, control_limit_id, machine_id, violation_type,
			metric_type, value, limit_value, detected_at,
			acknowledged, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at
	`

	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(v.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		v.ID, v.TenantID, v.ControlLimitID, v.MachineID, v.ViolationType,
		v.MetricType, v.Value, v.LimitValue, v.DetectedAt,
		v.Acknowledged, metadataJSON,
	).Scan(&v.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create SPC violation: %w", err)
	}

	return nil
}

// AcknowledgeViolation marks a violation as acknowledged by a user.
func (r *SPCRepository) AcknowledgeViolation(ctx context.Context, id, userID uuid.UUID, notes *string) error {
	query := `
		UPDATE spc_violations SET
			acknowledged = true,
			acknowledged_by = $2,
			acknowledged_at = NOW(),
			notes = $3
		WHERE id = $1
		RETURNING id
	`

	var returnedID uuid.UUID
	err := r.db.QueryRowContext(ctx, query, id, userID, notes).Scan(&returnedID)

	if err == sql.ErrNoRows {
		return fmt.Errorf("SPC violation not found")
	}
	if err != nil {
		return fmt.Errorf("failed to acknowledge SPC violation: %w", err)
	}

	return nil
}

// Helper functions

func (r *SPCRepository) scanControlLimit(rows *sql.Rows) (*SPCControlLimit, error) {
	var cl SPCControlLimit
	var usl, lsl sql.NullFloat64
	var metadataJSON []byte

	err := rows.Scan(
		&cl.ID, &cl.TenantID, &cl.MachineID, &cl.MetricType,
		&cl.Mean, &cl.Stddev, &cl.UCL, &cl.LCL,
		&usl, &lsl, &cl.SampleCount, &cl.SampleStart, &cl.SampleEnd,
		&cl.IsActive, &metadataJSON, &cl.CreatedAt, &cl.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan SPC control limit: %w", err)
	}

	if usl.Valid {
		cl.USL = &usl.Float64
	}
	if lsl.Valid {
		cl.LSL = &lsl.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &cl.Metadata)
	}

	return &cl, nil
}

func (r *SPCRepository) scanControlLimitRow(row *sql.Row) (*SPCControlLimit, error) {
	var cl SPCControlLimit
	var usl, lsl sql.NullFloat64
	var metadataJSON []byte

	err := row.Scan(
		&cl.ID, &cl.TenantID, &cl.MachineID, &cl.MetricType,
		&cl.Mean, &cl.Stddev, &cl.UCL, &cl.LCL,
		&usl, &lsl, &cl.SampleCount, &cl.SampleStart, &cl.SampleEnd,
		&cl.IsActive, &metadataJSON, &cl.CreatedAt, &cl.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if usl.Valid {
		cl.USL = &usl.Float64
	}
	if lsl.Valid {
		cl.LSL = &lsl.Float64
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &cl.Metadata)
	}

	return &cl, nil
}

func (r *SPCRepository) scanViolation(rows *sql.Rows) (*SPCViolation, error) {
	var v SPCViolation
	var acknowledgedBy *uuid.UUID
	var acknowledgedAt sql.NullTime
	var notes sql.NullString
	var metadataJSON []byte

	err := rows.Scan(
		&v.ID, &v.TenantID, &v.ControlLimitID, &v.MachineID, &v.ViolationType,
		&v.MetricType, &v.Value, &v.LimitValue, &v.DetectedAt,
		&v.Acknowledged, &acknowledgedBy, &acknowledgedAt, &notes,
		&metadataJSON, &v.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan SPC violation: %w", err)
	}

	v.AcknowledgedBy = acknowledgedBy
	if acknowledgedAt.Valid {
		v.AcknowledgedAt = &acknowledgedAt.Time
	}
	if notes.Valid {
		v.Notes = &notes.String
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &v.Metadata)
	}

	return &v, nil
}
