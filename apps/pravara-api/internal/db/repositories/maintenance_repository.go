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

// MaintenanceSchedule represents a recurring or condition-based maintenance schedule.
type MaintenanceSchedule struct {
	ID                 uuid.UUID      `json:"id"`
	TenantID           uuid.UUID      `json:"tenant_id"`
	MachineID          uuid.UUID      `json:"machine_id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	TriggerType        string         `json:"trigger_type"`
	Priority           int            `json:"priority"`
	IntervalDays       *int           `json:"interval_days,omitempty"`
	IntervalHours      *float64       `json:"interval_hours,omitempty"`
	LastDoneHours      *float64       `json:"last_done_hours,omitempty"`
	NextDueHours       *float64       `json:"next_due_hours,omitempty"`
	IntervalCycles     *int           `json:"interval_cycles,omitempty"`
	LastDoneCycles     *int           `json:"last_done_cycles,omitempty"`
	NextDueCycles      *int           `json:"next_due_cycles,omitempty"`
	ConditionMetric    *string        `json:"condition_metric,omitempty"`
	ConditionThreshold *float64       `json:"condition_threshold,omitempty"`
	LastDoneAt         *time.Time     `json:"last_done_at,omitempty"`
	NextDueAt          *time.Time     `json:"next_due_at,omitempty"`
	AssignedTo         *uuid.UUID     `json:"assigned_to,omitempty"`
	IsActive           bool           `json:"is_active"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

// MaintenanceWorkOrder represents a specific maintenance work order instance.
type MaintenanceWorkOrder struct {
	ID              uuid.UUID        `json:"id"`
	TenantID        uuid.UUID        `json:"tenant_id"`
	ScheduleID      *uuid.UUID       `json:"schedule_id,omitempty"`
	MachineID       uuid.UUID        `json:"machine_id"`
	WorkOrderNumber string           `json:"work_order_number"`
	Title           string           `json:"title"`
	Description     string           `json:"description"`
	Status          string           `json:"status"`
	Priority        int              `json:"priority"`
	AssignedTo      *uuid.UUID       `json:"assigned_to,omitempty"`
	Checklist       json.RawMessage  `json:"checklist,omitempty"`
	ScheduledAt     *time.Time       `json:"scheduled_at,omitempty"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	DueAt           *time.Time       `json:"due_at,omitempty"`
	Notes           string           `json:"notes"`
	PartsUsed       json.RawMessage  `json:"parts_used,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// ScheduleFilter defines filtering options for listing maintenance schedules.
type ScheduleFilter struct {
	MachineID   *uuid.UUID
	TriggerType *string
	IsActive    *bool
	Limit       int
	Offset      int
}

// WorkOrderFilter defines filtering options for listing maintenance work orders.
type WorkOrderFilter struct {
	MachineID  *uuid.UUID
	ScheduleID *uuid.UUID
	Status     *string
	AssignedTo *uuid.UUID
	Limit      int
	Offset     int
}

// MaintenanceRepository handles maintenance schedule and work order database operations.
type MaintenanceRepository struct {
	db *sql.DB
}

// NewMaintenanceRepository creates a new maintenance repository.
func NewMaintenanceRepository(db *sql.DB) *MaintenanceRepository {
	return &MaintenanceRepository{db: db}
}

// DB returns the underlying database connection.
func (r *MaintenanceRepository) DB() *sql.DB {
	return r.db
}

// =============== Schedules ===============

// ListSchedules retrieves maintenance schedules matching the given filter with pagination.
// Results are ordered by priority ascending, then created_at descending.
func (r *MaintenanceRepository) ListSchedules(ctx context.Context, filter ScheduleFilter) ([]MaintenanceSchedule, int, error) {
	query := `
		SELECT id, tenant_id, machine_id, name, description, trigger_type, priority,
		       interval_days, interval_hours, last_done_hours, next_due_hours,
		       interval_cycles, last_done_cycles, next_due_cycles,
		       condition_metric, condition_threshold,
		       last_done_at, next_due_at, assigned_to, is_active,
		       metadata, created_at, updated_at
		FROM maintenance_schedules
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM maintenance_schedules WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.TriggerType != nil {
		query += fmt.Sprintf(" AND trigger_type = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND trigger_type = $%d", argIndex)
		args = append(args, *filter.TriggerType)
		argIndex++
	}

	if filter.IsActive != nil {
		query += fmt.Sprintf(" AND is_active = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND is_active = $%d", argIndex)
		args = append(args, *filter.IsActive)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count maintenance schedules: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY priority ASC, created_at DESC"

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
		return nil, 0, fmt.Errorf("failed to query maintenance schedules: %w", err)
	}
	defer rows.Close()

	var schedules []MaintenanceSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan maintenance schedule: %w", err)
		}
		schedules = append(schedules, s)
	}

	return schedules, total, nil
}

// GetScheduleByID retrieves a maintenance schedule by its unique identifier.
// Returns nil, nil if the schedule is not found (not an error condition).
func (r *MaintenanceRepository) GetScheduleByID(ctx context.Context, id uuid.UUID) (*MaintenanceSchedule, error) {
	query := `
		SELECT id, tenant_id, machine_id, name, description, trigger_type, priority,
		       interval_days, interval_hours, last_done_hours, next_due_hours,
		       interval_cycles, last_done_cycles, next_due_cycles,
		       condition_metric, condition_threshold,
		       last_done_at, next_due_at, assigned_to, is_active,
		       metadata, created_at, updated_at
		FROM maintenance_schedules
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	s, err := scanSchedule(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get maintenance schedule: %w", err)
	}

	return &s, nil
}

// CreateSchedule inserts a new maintenance schedule into the database.
func (r *MaintenanceRepository) CreateSchedule(ctx context.Context, schedule *MaintenanceSchedule) error {
	query := `
		INSERT INTO maintenance_schedules (
			id, tenant_id, machine_id, name, description, trigger_type, priority,
			interval_days, interval_hours, last_done_hours, next_due_hours,
			interval_cycles, last_done_cycles, next_due_cycles,
			condition_metric, condition_threshold,
			last_done_at, next_due_at, assigned_to, is_active, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		RETURNING created_at, updated_at
	`

	if schedule.ID == uuid.Nil {
		schedule.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(schedule.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		schedule.ID, schedule.TenantID, schedule.MachineID,
		schedule.Name, schedule.Description, schedule.TriggerType, schedule.Priority,
		schedule.IntervalDays, schedule.IntervalHours, schedule.LastDoneHours, schedule.NextDueHours,
		schedule.IntervalCycles, schedule.LastDoneCycles, schedule.NextDueCycles,
		schedule.ConditionMetric, schedule.ConditionThreshold,
		schedule.LastDoneAt, schedule.NextDueAt, nullUUID(schedule.AssignedTo),
		schedule.IsActive, metadataJSON,
	).Scan(&schedule.CreatedAt, &schedule.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create maintenance schedule: %w", err)
	}

	return nil
}

// UpdateSchedule modifies an existing maintenance schedule.
func (r *MaintenanceRepository) UpdateSchedule(ctx context.Context, schedule *MaintenanceSchedule) error {
	query := `
		UPDATE maintenance_schedules SET
			name = $2,
			description = $3,
			trigger_type = $4,
			priority = $5,
			interval_days = $6,
			interval_hours = $7,
			last_done_hours = $8,
			next_due_hours = $9,
			interval_cycles = $10,
			last_done_cycles = $11,
			next_due_cycles = $12,
			condition_metric = $13,
			condition_threshold = $14,
			last_done_at = $15,
			next_due_at = $16,
			assigned_to = $17,
			is_active = $18,
			metadata = $19
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(schedule.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		schedule.ID, schedule.Name, schedule.Description, schedule.TriggerType, schedule.Priority,
		schedule.IntervalDays, schedule.IntervalHours, schedule.LastDoneHours, schedule.NextDueHours,
		schedule.IntervalCycles, schedule.LastDoneCycles, schedule.NextDueCycles,
		schedule.ConditionMetric, schedule.ConditionThreshold,
		schedule.LastDoneAt, schedule.NextDueAt, nullUUID(schedule.AssignedTo),
		schedule.IsActive, metadataJSON,
	).Scan(&schedule.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("maintenance schedule not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update maintenance schedule: %w", err)
	}

	return nil
}

// DeleteSchedule permanently removes a maintenance schedule.
func (r *MaintenanceRepository) DeleteSchedule(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM maintenance_schedules WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete maintenance schedule: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("maintenance schedule not found")
	}

	return nil
}

// =============== Work Orders ===============

// ListWorkOrders retrieves maintenance work orders matching the given filter with pagination.
// Results are ordered by priority ascending, then created_at descending.
func (r *MaintenanceRepository) ListWorkOrders(ctx context.Context, filter WorkOrderFilter) ([]MaintenanceWorkOrder, int, error) {
	query := `
		SELECT id, tenant_id, schedule_id, machine_id, work_order_number,
		       title, description, status, priority, assigned_to,
		       checklist, scheduled_at, started_at, completed_at, due_at,
		       notes, parts_used, metadata, created_at, updated_at
		FROM maintenance_work_orders
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM maintenance_work_orders WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.ScheduleID != nil {
		query += fmt.Sprintf(" AND schedule_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND schedule_id = $%d", argIndex)
		args = append(args, *filter.ScheduleID)
		argIndex++
	}

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.AssignedTo != nil {
		query += fmt.Sprintf(" AND assigned_to = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND assigned_to = $%d", argIndex)
		args = append(args, *filter.AssignedTo)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count maintenance work orders: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY priority ASC, created_at DESC"

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
		return nil, 0, fmt.Errorf("failed to query maintenance work orders: %w", err)
	}
	defer rows.Close()

	var workOrders []MaintenanceWorkOrder
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan maintenance work order: %w", err)
		}
		workOrders = append(workOrders, wo)
	}

	return workOrders, total, nil
}

// GetWorkOrderByID retrieves a maintenance work order by its unique identifier.
// Returns nil, nil if the work order is not found (not an error condition).
func (r *MaintenanceRepository) GetWorkOrderByID(ctx context.Context, id uuid.UUID) (*MaintenanceWorkOrder, error) {
	query := `
		SELECT id, tenant_id, schedule_id, machine_id, work_order_number,
		       title, description, status, priority, assigned_to,
		       checklist, scheduled_at, started_at, completed_at, due_at,
		       notes, parts_used, metadata, created_at, updated_at
		FROM maintenance_work_orders
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	wo, err := scanWorkOrder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get maintenance work order: %w", err)
	}

	return &wo, nil
}

// CreateWorkOrder inserts a new maintenance work order into the database.
func (r *MaintenanceRepository) CreateWorkOrder(ctx context.Context, wo *MaintenanceWorkOrder) error {
	query := `
		INSERT INTO maintenance_work_orders (
			id, tenant_id, schedule_id, machine_id, work_order_number,
			title, description, status, priority, assigned_to,
			checklist, scheduled_at, started_at, completed_at, due_at,
			notes, parts_used, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING created_at, updated_at
	`

	if wo.ID == uuid.Nil {
		wo.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(wo.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		wo.ID, wo.TenantID, nullUUID(wo.ScheduleID), wo.MachineID, wo.WorkOrderNumber,
		wo.Title, wo.Description, wo.Status, wo.Priority, nullUUID(wo.AssignedTo),
		wo.Checklist, wo.ScheduledAt, wo.StartedAt, wo.CompletedAt, wo.DueAt,
		wo.Notes, wo.PartsUsed, metadataJSON,
	).Scan(&wo.CreatedAt, &wo.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create maintenance work order: %w", err)
	}

	return nil
}

// UpdateWorkOrder modifies an existing maintenance work order.
func (r *MaintenanceRepository) UpdateWorkOrder(ctx context.Context, wo *MaintenanceWorkOrder) error {
	query := `
		UPDATE maintenance_work_orders SET
			title = $2,
			description = $3,
			status = $4,
			priority = $5,
			assigned_to = $6,
			checklist = $7,
			scheduled_at = $8,
			started_at = $9,
			completed_at = $10,
			due_at = $11,
			notes = $12,
			parts_used = $13,
			metadata = $14
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(wo.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		wo.ID, wo.Title, wo.Description, wo.Status, wo.Priority,
		nullUUID(wo.AssignedTo), wo.Checklist, wo.ScheduledAt,
		wo.StartedAt, wo.CompletedAt, wo.DueAt,
		wo.Notes, wo.PartsUsed, metadataJSON,
	).Scan(&wo.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("maintenance work order not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update maintenance work order: %w", err)
	}

	return nil
}

// CompleteWorkOrder marks a work order as completed with notes and sets completed_at to now.
func (r *MaintenanceRepository) CompleteWorkOrder(ctx context.Context, id uuid.UUID, notes string) error {
	query := `
		UPDATE maintenance_work_orders
		SET status = 'completed', completed_at = NOW(), notes = $2
		WHERE id = $1
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := r.db.QueryRowContext(ctx, query, id, notes).Scan(&updatedAt)
	if err == sql.ErrNoRows {
		return fmt.Errorf("maintenance work order not found")
	}
	if err != nil {
		return fmt.Errorf("failed to complete maintenance work order: %w", err)
	}

	return nil
}

// GetOverdueCount returns the number of work orders with status 'overdue'.
func (r *MaintenanceRepository) GetOverdueCount(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM maintenance_work_orders WHERE status = 'overdue'`

	var count int
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count overdue work orders: %w", err)
	}

	return count, nil
}

// GetByMachine retrieves all work orders for a specific machine.
// Results are ordered by priority ascending, then created_at descending.
func (r *MaintenanceRepository) GetByMachine(ctx context.Context, machineID uuid.UUID) ([]MaintenanceWorkOrder, error) {
	query := `
		SELECT id, tenant_id, schedule_id, machine_id, work_order_number,
		       title, description, status, priority, assigned_to,
		       checklist, scheduled_at, started_at, completed_at, due_at,
		       notes, parts_used, metadata, created_at, updated_at
		FROM maintenance_work_orders
		WHERE machine_id = $1
		ORDER BY priority ASC, created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, machineID)
	if err != nil {
		return nil, fmt.Errorf("failed to query work orders by machine: %w", err)
	}
	defer rows.Close()

	var workOrders []MaintenanceWorkOrder
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan work order: %w", err)
		}
		workOrders = append(workOrders, wo)
	}

	return workOrders, nil
}

// =============== Scan Helpers ===============

func scanSchedule(scanner interface {
	Scan(dest ...interface{}) error
}) (MaintenanceSchedule, error) {
	var s MaintenanceSchedule
	var description sql.NullString
	var intervalDays sql.NullInt64
	var intervalHours, lastDoneHours, nextDueHours sql.NullFloat64
	var intervalCycles, lastDoneCycles, nextDueCycles sql.NullInt64
	var conditionMetric sql.NullString
	var conditionThreshold sql.NullFloat64
	var lastDoneAt, nextDueAt sql.NullTime
	var assignedTo sql.NullString
	var metadataJSON []byte

	err := scanner.Scan(
		&s.ID, &s.TenantID, &s.MachineID, &s.Name, &description,
		&s.TriggerType, &s.Priority,
		&intervalDays, &intervalHours, &lastDoneHours, &nextDueHours,
		&intervalCycles, &lastDoneCycles, &nextDueCycles,
		&conditionMetric, &conditionThreshold,
		&lastDoneAt, &nextDueAt, &assignedTo, &s.IsActive,
		&metadataJSON, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return s, err
	}

	if description.Valid {
		s.Description = description.String
	}
	if intervalDays.Valid {
		v := int(intervalDays.Int64)
		s.IntervalDays = &v
	}
	if intervalHours.Valid {
		s.IntervalHours = &intervalHours.Float64
	}
	if lastDoneHours.Valid {
		s.LastDoneHours = &lastDoneHours.Float64
	}
	if nextDueHours.Valid {
		s.NextDueHours = &nextDueHours.Float64
	}
	if intervalCycles.Valid {
		v := int(intervalCycles.Int64)
		s.IntervalCycles = &v
	}
	if lastDoneCycles.Valid {
		v := int(lastDoneCycles.Int64)
		s.LastDoneCycles = &v
	}
	if nextDueCycles.Valid {
		v := int(nextDueCycles.Int64)
		s.NextDueCycles = &v
	}
	if conditionMetric.Valid {
		s.ConditionMetric = &conditionMetric.String
	}
	if conditionThreshold.Valid {
		s.ConditionThreshold = &conditionThreshold.Float64
	}
	if lastDoneAt.Valid {
		s.LastDoneAt = &lastDoneAt.Time
	}
	if nextDueAt.Valid {
		s.NextDueAt = &nextDueAt.Time
	}
	if assignedTo.Valid {
		id, _ := uuid.Parse(assignedTo.String)
		s.AssignedTo = &id
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &s.Metadata)
	}

	return s, nil
}

func scanWorkOrder(scanner interface {
	Scan(dest ...interface{}) error
}) (MaintenanceWorkOrder, error) {
	var wo MaintenanceWorkOrder
	var scheduleID, assignedTo sql.NullString
	var description, notes sql.NullString
	var scheduledAt, startedAt, completedAt, dueAt sql.NullTime
	var checklistJSON, partsUsedJSON, metadataJSON []byte

	err := scanner.Scan(
		&wo.ID, &wo.TenantID, &scheduleID, &wo.MachineID, &wo.WorkOrderNumber,
		&wo.Title, &description, &wo.Status, &wo.Priority, &assignedTo,
		&checklistJSON, &scheduledAt, &startedAt, &completedAt, &dueAt,
		&notes, &partsUsedJSON, &metadataJSON, &wo.CreatedAt, &wo.UpdatedAt,
	)
	if err != nil {
		return wo, err
	}

	if scheduleID.Valid {
		id, _ := uuid.Parse(scheduleID.String)
		wo.ScheduleID = &id
	}
	if assignedTo.Valid {
		id, _ := uuid.Parse(assignedTo.String)
		wo.AssignedTo = &id
	}
	if description.Valid {
		wo.Description = description.String
	}
	if notes.Valid {
		wo.Notes = notes.String
	}
	if scheduledAt.Valid {
		wo.ScheduledAt = &scheduledAt.Time
	}
	if startedAt.Valid {
		wo.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		wo.CompletedAt = &completedAt.Time
	}
	if dueAt.Valid {
		wo.DueAt = &dueAt.Time
	}
	if len(checklistJSON) > 0 {
		wo.Checklist = checklistJSON
	}
	if len(partsUsedJSON) > 0 {
		wo.PartsUsed = partsUsedJSON
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &wo.Metadata)
	}

	return wo, nil
}
