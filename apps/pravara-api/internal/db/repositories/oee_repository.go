// Package repositories provides database access layer implementations.
package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OEESnapshot represents an OEE (Overall Equipment Effectiveness) snapshot for a machine on a given date.
type OEESnapshot struct {
	ID              uuid.UUID      `json:"id"`
	TenantID        uuid.UUID      `json:"tenant_id"`
	MachineID       uuid.UUID      `json:"machine_id"`
	SnapshotDate    time.Time      `json:"snapshot_date"`
	PlannedMinutes  float64        `json:"planned_minutes"`
	DowntimeMinutes float64        `json:"downtime_minutes"`
	RunMinutes      float64        `json:"run_minutes"`
	TasksCompleted  int            `json:"tasks_completed"`
	TasksFailed     int            `json:"tasks_failed"`
	TasksTotal      int            `json:"tasks_total"`
	Availability    float64        `json:"availability"`
	Performance     float64        `json:"performance"`
	Quality         float64        `json:"quality"`
	OEE             float64        `json:"oee"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// OEEFilter defines filtering options for listing OEE snapshots.
type OEEFilter struct {
	MachineID *uuid.UUID
	From      *time.Time
	To        *time.Time
	Interval  string
	Limit     int
	Offset    int
}

// OEERepository handles OEE snapshot database operations.
type OEERepository struct {
	db *sql.DB
}

// NewOEERepository creates a new OEE repository.
func NewOEERepository(db *sql.DB) *OEERepository {
	return &OEERepository{db: db}
}

// DB returns the underlying database connection for use by services
// that need to perform cross-table queries.
func (r *OEERepository) DB() *sql.DB {
	return r.db
}

// List retrieves OEE snapshots matching the given filter with pagination.
// Results are ordered by snapshot_date descending.
// Returns the list of snapshots, total count (for pagination), and any error encountered.
func (r *OEERepository) List(ctx context.Context, filter OEEFilter) ([]OEESnapshot, int, error) {
	query := `
		SELECT id, tenant_id, machine_id, snapshot_date,
		       planned_minutes, downtime_minutes, run_minutes,
		       tasks_completed, tasks_failed, tasks_total,
		       availability, performance, quality, oee,
		       metadata, created_at, updated_at
		FROM oee_snapshots
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM oee_snapshots WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.From != nil {
		query += fmt.Sprintf(" AND snapshot_date >= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND snapshot_date >= $%d", argIndex)
		args = append(args, *filter.From)
		argIndex++
	}

	if filter.To != nil {
		query += fmt.Sprintf(" AND snapshot_date <= $%d", argIndex)
		countQuery += fmt.Sprintf(" AND snapshot_date <= $%d", argIndex)
		args = append(args, *filter.To)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count oee snapshots: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY snapshot_date DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
		argIndex++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query oee snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []OEESnapshot
	for rows.Next() {
		s, err := r.scanOEESnapshot(rows)
		if err != nil {
			return nil, 0, err
		}
		snapshots = append(snapshots, *s)
	}

	return snapshots, total, nil
}

// GetByID retrieves an OEE snapshot by its unique identifier.
// Returns nil, nil if the snapshot is not found (not an error condition).
// Returns nil, error if a database error occurs.
func (r *OEERepository) GetByID(ctx context.Context, id uuid.UUID) (*OEESnapshot, error) {
	query := `
		SELECT id, tenant_id, machine_id, snapshot_date,
		       planned_minutes, downtime_minutes, run_minutes,
		       tasks_completed, tasks_failed, tasks_total,
		       availability, performance, quality, oee,
		       metadata, created_at, updated_at
		FROM oee_snapshots
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	s, err := r.scanOEESnapshotRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get oee snapshot: %w", err)
	}

	return s, nil
}

// Upsert inserts a new OEE snapshot or updates an existing one on conflict
// of (tenant_id, machine_id, snapshot_date).
func (r *OEERepository) Upsert(ctx context.Context, snapshot *OEESnapshot) error {
	query := `
		INSERT INTO oee_snapshots (
			id, tenant_id, machine_id, snapshot_date,
			planned_minutes, downtime_minutes, run_minutes,
			tasks_completed, tasks_failed, tasks_total,
			availability, performance, quality, oee,
			metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, machine_id, snapshot_date) DO UPDATE SET
			planned_minutes = EXCLUDED.planned_minutes,
			downtime_minutes = EXCLUDED.downtime_minutes,
			run_minutes = EXCLUDED.run_minutes,
			tasks_completed = EXCLUDED.tasks_completed,
			tasks_failed = EXCLUDED.tasks_failed,
			tasks_total = EXCLUDED.tasks_total,
			availability = EXCLUDED.availability,
			performance = EXCLUDED.performance,
			quality = EXCLUDED.quality,
			oee = EXCLUDED.oee,
			metadata = EXCLUDED.metadata
		RETURNING id, created_at, updated_at
	`

	if snapshot.ID == uuid.Nil {
		snapshot.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(snapshot.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		snapshot.ID, snapshot.TenantID, snapshot.MachineID, snapshot.SnapshotDate,
		snapshot.PlannedMinutes, snapshot.DowntimeMinutes, snapshot.RunMinutes,
		snapshot.TasksCompleted, snapshot.TasksFailed, snapshot.TasksTotal,
		snapshot.Availability, snapshot.Performance, snapshot.Quality, snapshot.OEE,
		metadataJSON,
	).Scan(&snapshot.ID, &snapshot.CreatedAt, &snapshot.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to upsert oee snapshot: %w", err)
	}

	return nil
}

// GetFleetSummary retrieves aggregated OEE data across all machines for a date range.
// Returns one aggregated row per machine within the specified time range.
func (r *OEERepository) GetFleetSummary(ctx context.Context, from, to time.Time) ([]OEESnapshot, error) {
	query := `
		SELECT
			uuid_generate_v4() as id,
			tenant_id,
			machine_id,
			MAX(snapshot_date) as snapshot_date,
			AVG(planned_minutes) as planned_minutes,
			AVG(downtime_minutes) as downtime_minutes,
			AVG(run_minutes) as run_minutes,
			SUM(tasks_completed) as tasks_completed,
			SUM(tasks_failed) as tasks_failed,
			SUM(tasks_total) as tasks_total,
			AVG(availability) as availability,
			AVG(performance) as performance,
			AVG(quality) as quality,
			AVG(oee) as oee,
			MIN(created_at) as created_at,
			MAX(updated_at) as updated_at
		FROM oee_snapshots
		WHERE snapshot_date >= $1 AND snapshot_date <= $2
		GROUP BY tenant_id, machine_id
		ORDER BY AVG(oee) DESC
	`

	rows, err := r.db.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to query fleet oee summary: %w", err)
	}
	defer rows.Close()

	var snapshots []OEESnapshot
	for rows.Next() {
		var s OEESnapshot
		err := rows.Scan(
			&s.ID, &s.TenantID, &s.MachineID, &s.SnapshotDate,
			&s.PlannedMinutes, &s.DowntimeMinutes, &s.RunMinutes,
			&s.TasksCompleted, &s.TasksFailed, &s.TasksTotal,
			&s.Availability, &s.Performance, &s.Quality, &s.OEE,
			&s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan fleet oee summary row: %w", err)
		}
		snapshots = append(snapshots, s)
	}

	return snapshots, nil
}

// ComputeForMachine computes the OEE metrics for a given machine on a given date
// by querying telemetry and tasks tables, then upserts the resulting snapshot.
//
// Availability = (PlannedMinutes - DowntimeMinutes) / PlannedMinutes
// Performance  = sum(actual_minutes) / sum(estimated_minutes), capped at 1.0
// Quality      = tasks_completed / tasks_total (where total = completed + failed)
// OEE          = Availability * Performance * Quality
func (r *OEERepository) ComputeForMachine(ctx context.Context, tenantID, machineID uuid.UUID, date time.Time) (*OEESnapshot, error) {
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	// Default planned minutes for a standard 8-hour shift
	plannedMinutes := 480.0

	// --- Availability ---
	// Query telemetry for machine_status metric on the given date.
	// Count entries with value indicating offline/error as downtime.
	// We count distinct minutes where the machine was in a downtime state.
	downtimeQuery := `
		SELECT COALESCE(COUNT(*), 0)
		FROM telemetry
		WHERE machine_id = $1
		  AND metric_type = 'machine_status'
		  AND timestamp >= $2
		  AND timestamp < $3
		  AND (value = 0 OR value = -1)
	`
	var downtimeEntries int
	if err := r.db.QueryRowContext(ctx, downtimeQuery, machineID, dayStart, dayEnd).Scan(&downtimeEntries); err != nil {
		return nil, fmt.Errorf("failed to query downtime telemetry: %w", err)
	}

	// Query total telemetry entries for machine_status to compute ratio
	totalStatusQuery := `
		SELECT COALESCE(COUNT(*), 0)
		FROM telemetry
		WHERE machine_id = $1
		  AND metric_type = 'machine_status'
		  AND timestamp >= $2
		  AND timestamp < $3
	`
	var totalStatusEntries int
	if err := r.db.QueryRowContext(ctx, totalStatusQuery, machineID, dayStart, dayEnd).Scan(&totalStatusEntries); err != nil {
		return nil, fmt.Errorf("failed to query total status telemetry: %w", err)
	}

	var downtimeMinutes float64
	if totalStatusEntries > 0 {
		downtimeMinutes = (float64(downtimeEntries) / float64(totalStatusEntries)) * plannedMinutes
	}
	runMinutes := plannedMinutes - downtimeMinutes

	availability := 0.0
	if plannedMinutes > 0 {
		availability = runMinutes / plannedMinutes
	}

	// --- Performance ---
	// Query tasks completed on that machine on that date.
	// Performance = sum(actual_minutes) / sum(estimated_minutes), capped at 1.0
	perfQuery := `
		SELECT COALESCE(SUM(estimated_minutes), 0),
		       COALESCE(SUM(actual_minutes), 0)
		FROM tasks
		WHERE machine_id = $1
		  AND status = 'completed'
		  AND completed_at >= $2
		  AND completed_at < $3
	`
	var estimatedSum, actualSum int
	if err := r.db.QueryRowContext(ctx, perfQuery, machineID, dayStart, dayEnd).Scan(&estimatedSum, &actualSum); err != nil {
		return nil, fmt.Errorf("failed to query task performance: %w", err)
	}

	performance := 0.0
	if estimatedSum > 0 && actualSum > 0 {
		performance = float64(estimatedSum) / float64(actualSum)
		if performance > 1.0 {
			performance = 1.0
		}
	}

	// --- Quality ---
	// Count tasks completed vs tasks that failed quality checks.
	completedQuery := `
		SELECT COALESCE(COUNT(*), 0)
		FROM tasks
		WHERE machine_id = $1
		  AND status = 'completed'
		  AND completed_at >= $2
		  AND completed_at < $3
	`
	var tasksCompleted int
	if err := r.db.QueryRowContext(ctx, completedQuery, machineID, dayStart, dayEnd).Scan(&tasksCompleted); err != nil {
		return nil, fmt.Errorf("failed to count completed tasks: %w", err)
	}

	failedQuery := `
		SELECT COALESCE(COUNT(*), 0)
		FROM tasks
		WHERE machine_id = $1
		  AND status IN ('blocked', 'quality_check')
		  AND updated_at >= $2
		  AND updated_at < $3
	`
	var tasksFailed int
	if err := r.db.QueryRowContext(ctx, failedQuery, machineID, dayStart, dayEnd).Scan(&tasksFailed); err != nil {
		return nil, fmt.Errorf("failed to count failed tasks: %w", err)
	}

	tasksTotal := tasksCompleted + tasksFailed

	quality := 0.0
	if tasksTotal > 0 {
		quality = float64(tasksCompleted) / float64(tasksTotal)
	}

	// --- OEE ---
	oee := availability * performance * quality

	snapshot := &OEESnapshot{
		TenantID:        tenantID,
		MachineID:       machineID,
		SnapshotDate:    dayStart,
		PlannedMinutes:  plannedMinutes,
		DowntimeMinutes: downtimeMinutes,
		RunMinutes:      runMinutes,
		TasksCompleted:  tasksCompleted,
		TasksFailed:     tasksFailed,
		TasksTotal:      tasksTotal,
		Availability:    availability,
		Performance:     performance,
		Quality:         quality,
		OEE:             oee,
	}

	// Upsert the computed snapshot
	if err := r.Upsert(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("failed to store computed oee snapshot: %w", err)
	}

	return snapshot, nil
}

// Helper functions

func (r *OEERepository) scanOEESnapshot(rows *sql.Rows) (*OEESnapshot, error) {
	var s OEESnapshot
	var metadataJSON []byte

	err := rows.Scan(
		&s.ID, &s.TenantID, &s.MachineID, &s.SnapshotDate,
		&s.PlannedMinutes, &s.DowntimeMinutes, &s.RunMinutes,
		&s.TasksCompleted, &s.TasksFailed, &s.TasksTotal,
		&s.Availability, &s.Performance, &s.Quality, &s.OEE,
		&metadataJSON, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan oee snapshot: %w", err)
	}

	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &s.Metadata)
	}

	return &s, nil
}

func (r *OEERepository) scanOEESnapshotRow(row *sql.Row) (*OEESnapshot, error) {
	var s OEESnapshot
	var metadataJSON []byte

	err := row.Scan(
		&s.ID, &s.TenantID, &s.MachineID, &s.SnapshotDate,
		&s.PlannedMinutes, &s.DowntimeMinutes, &s.RunMinutes,
		&s.TasksCompleted, &s.TasksFailed, &s.TasksTotal,
		&s.Availability, &s.Performance, &s.Quality, &s.OEE,
		&metadataJSON, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &s.Metadata)
	}

	return &s, nil
}
