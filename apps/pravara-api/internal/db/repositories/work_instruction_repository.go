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

// WorkInstructionRepository handles work instruction database operations.
type WorkInstructionRepository struct {
	db *sql.DB
}

// NewWorkInstructionRepository creates a new work instruction repository.
func NewWorkInstructionRepository(db *sql.DB) *WorkInstructionRepository {
	return &WorkInstructionRepository{db: db}
}

// WorkInstruction represents a work instruction document for operator guidance.
type WorkInstruction struct {
	ID                  uuid.UUID        `json:"id"`
	TenantID            uuid.UUID        `json:"tenant_id"`
	Title               string           `json:"title"`
	Version             string           `json:"version"`
	Category            string           `json:"category"` // setup, operation, safety, maintenance
	Description         string           `json:"description"`
	ProductDefinitionID *uuid.UUID       `json:"product_definition_id,omitempty"`
	MachineType         *string          `json:"machine_type,omitempty"`
	Steps               json.RawMessage  `json:"steps,omitempty"`
	ToolsRequired       json.RawMessage  `json:"tools_required,omitempty"`
	PPERequired         json.RawMessage  `json:"ppe_required,omitempty"`
	IsActive            bool             `json:"is_active"`
	Metadata            map[string]any   `json:"metadata,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

// TaskWorkInstruction represents the association between a task and a work instruction.
type TaskWorkInstruction struct {
	ID                   uuid.UUID       `json:"id"`
	TenantID             uuid.UUID       `json:"tenant_id"`
	TaskID               uuid.UUID       `json:"task_id"`
	WorkInstructionID    uuid.UUID       `json:"work_instruction_id"`
	StepAcknowledgements json.RawMessage `json:"step_acknowledgements,omitempty"`
	AllAcknowledged      bool            `json:"all_acknowledged"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// WorkInstructionFilter defines filtering options for listing work instructions.
type WorkInstructionFilter struct {
	Category            *string
	ProductDefinitionID *uuid.UUID
	MachineType         *string
	IsActive            *bool
	Limit               int
	Offset              int
}

// List retrieves work instructions matching the given filter with pagination.
// Results are ordered by title ascending.
func (r *WorkInstructionRepository) List(ctx context.Context, filter WorkInstructionFilter) ([]WorkInstruction, int, error) {
	query := `
		SELECT id, tenant_id, title, version, category, description,
		       product_definition_id, machine_type, steps, tools_required,
		       ppe_required, is_active, metadata, created_at, updated_at
		FROM work_instructions
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM work_instructions WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Category != nil {
		query += fmt.Sprintf(" AND category = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND category = $%d", argIndex)
		args = append(args, *filter.Category)
		argIndex++
	}

	if filter.ProductDefinitionID != nil {
		query += fmt.Sprintf(" AND product_definition_id = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND product_definition_id = $%d", argIndex)
		args = append(args, *filter.ProductDefinitionID)
		argIndex++
	}

	if filter.MachineType != nil {
		query += fmt.Sprintf(" AND machine_type = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND machine_type = $%d", argIndex)
		args = append(args, *filter.MachineType)
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
		return nil, 0, fmt.Errorf("failed to count work instructions: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY title ASC"

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
		return nil, 0, fmt.Errorf("failed to query work instructions: %w", err)
	}
	defer rows.Close()

	var instructions []WorkInstruction
	for rows.Next() {
		wi, err := r.scanWorkInstruction(rows)
		if err != nil {
			return nil, 0, err
		}
		instructions = append(instructions, *wi)
	}

	return instructions, total, nil
}

// GetByID retrieves a work instruction by its unique identifier.
// Returns nil, nil if the work instruction is not found.
func (r *WorkInstructionRepository) GetByID(ctx context.Context, id uuid.UUID) (*WorkInstruction, error) {
	query := `
		SELECT id, tenant_id, title, version, category, description,
		       product_definition_id, machine_type, steps, tools_required,
		       ppe_required, is_active, metadata, created_at, updated_at
		FROM work_instructions
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	wi, err := r.scanWorkInstructionRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get work instruction: %w", err)
	}

	return wi, nil
}

// Create inserts a new work instruction into the database.
// If wi.ID is nil, a new UUID is generated automatically.
func (r *WorkInstructionRepository) Create(ctx context.Context, wi *WorkInstruction) error {
	query := `
		INSERT INTO work_instructions (
			id, tenant_id, title, version, category, description,
			product_definition_id, machine_type, steps, tools_required,
			ppe_required, is_active, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`

	if wi.ID == uuid.Nil {
		wi.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(wi.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		wi.ID, wi.TenantID, wi.Title, wi.Version, wi.Category, wi.Description,
		wi.ProductDefinitionID, wi.MachineType, wi.Steps, wi.ToolsRequired,
		wi.PPERequired, wi.IsActive, metadataJSON,
	).Scan(&wi.CreatedAt, &wi.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create work instruction: %w", err)
	}

	return nil
}

// Update modifies an existing work instruction's mutable fields.
func (r *WorkInstructionRepository) Update(ctx context.Context, wi *WorkInstruction) error {
	query := `
		UPDATE work_instructions SET
			title = $2,
			version = $3,
			category = $4,
			description = $5,
			product_definition_id = $6,
			machine_type = $7,
			steps = $8,
			tools_required = $9,
			ppe_required = $10,
			is_active = $11,
			metadata = $12
		WHERE id = $1
		RETURNING updated_at
	`

	metadataJSON, _ := json.Marshal(wi.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		wi.ID, wi.Title, wi.Version, wi.Category, wi.Description,
		wi.ProductDefinitionID, wi.MachineType, wi.Steps, wi.ToolsRequired,
		wi.PPERequired, wi.IsActive, metadataJSON,
	).Scan(&wi.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("work instruction not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update work instruction: %w", err)
	}

	return nil
}

// Delete permanently removes a work instruction from the database.
func (r *WorkInstructionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM work_instructions WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete work instruction: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("work instruction not found")
	}

	return nil
}

// GetByProductAndMachineType retrieves active work instructions matching a product definition
// and/or machine type. Used for auto-attaching work instructions to tasks.
func (r *WorkInstructionRepository) GetByProductAndMachineType(ctx context.Context, productDefID *uuid.UUID, machineType *string) ([]WorkInstruction, error) {
	query := `
		SELECT id, tenant_id, title, version, category, description,
		       product_definition_id, machine_type, steps, tools_required,
		       ppe_required, is_active, metadata, created_at, updated_at
		FROM work_instructions
		WHERE is_active = true
	`

	var args []interface{}
	argIndex := 1

	if productDefID != nil {
		query += fmt.Sprintf(" AND (product_definition_id = $%d OR product_definition_id IS NULL)", argIndex)
		args = append(args, *productDefID)
		argIndex++
	}

	if machineType != nil {
		query += fmt.Sprintf(" AND (machine_type = $%d OR machine_type IS NULL)", argIndex)
		args = append(args, *machineType)
		argIndex++
	}

	query += " ORDER BY category ASC, title ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query work instructions by product/machine: %w", err)
	}
	defer rows.Close()

	var instructions []WorkInstruction
	for rows.Next() {
		wi, err := r.scanWorkInstruction(rows)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, *wi)
	}

	return instructions, nil
}

// AttachToTask creates a task-work-instruction association record.
func (r *WorkInstructionRepository) AttachToTask(ctx context.Context, twi *TaskWorkInstruction) error {
	query := `
		INSERT INTO task_work_instructions (
			id, tenant_id, task_id, work_instruction_id,
			step_acknowledgements, all_acknowledged
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	if twi.ID == uuid.Nil {
		twi.ID = uuid.New()
	}

	err := r.db.QueryRowContext(ctx, query,
		twi.ID, twi.TenantID, twi.TaskID, twi.WorkInstructionID,
		twi.StepAcknowledgements, twi.AllAcknowledged,
	).Scan(&twi.CreatedAt, &twi.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to attach work instruction to task: %w", err)
	}

	return nil
}

// GetForTask retrieves all work instructions associated with a task,
// including their acknowledgement status.
func (r *WorkInstructionRepository) GetForTask(ctx context.Context, taskID uuid.UUID) ([]TaskWorkInstruction, error) {
	query := `
		SELECT id, tenant_id, task_id, work_instruction_id,
		       step_acknowledgements, all_acknowledged, created_at, updated_at
		FROM task_work_instructions
		WHERE task_id = $1
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to query task work instructions: %w", err)
	}
	defer rows.Close()

	var results []TaskWorkInstruction
	for rows.Next() {
		var twi TaskWorkInstruction
		err := rows.Scan(
			&twi.ID, &twi.TenantID, &twi.TaskID, &twi.WorkInstructionID,
			&twi.StepAcknowledgements, &twi.AllAcknowledged, &twi.CreatedAt, &twi.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task work instruction: %w", err)
		}
		results = append(results, twi)
	}

	return results, nil
}

// AcknowledgeStep records that an operator acknowledged a specific step in a work instruction.
// It updates the step_acknowledgements JSON and checks whether all steps are now acknowledged.
func (r *WorkInstructionRepository) AcknowledgeStep(ctx context.Context, taskID, wiID uuid.UUID, stepNumber int, userID uuid.UUID) error {
	// Build the acknowledgement entry
	ackEntry, _ := json.Marshal(map[string]interface{}{
		"user_id":         userID,
		"acknowledged_at": time.Now().UTC(),
	})

	// Use jsonb_set to add/update the step acknowledgement
	query := `
		UPDATE task_work_instructions SET
			step_acknowledgements = COALESCE(step_acknowledgements, '{}'::jsonb) || jsonb_build_object($3::text, $4::jsonb),
			updated_at = NOW()
		WHERE task_id = $1 AND work_instruction_id = $2
		RETURNING id
	`

	var id uuid.UUID
	err := r.db.QueryRowContext(ctx, query,
		taskID, wiID, fmt.Sprintf("%d", stepNumber), json.RawMessage(ackEntry),
	).Scan(&id)

	if err == sql.ErrNoRows {
		return fmt.Errorf("task work instruction not found")
	}
	if err != nil {
		return fmt.Errorf("failed to acknowledge step: %w", err)
	}

	// Check if all steps are now acknowledged by comparing acknowledgement count
	// against the number of steps in the work instruction
	updateAllAcked := `
		UPDATE task_work_instructions twi SET
			all_acknowledged = (
				SELECT jsonb_object_keys_count >= jsonb_array_length(wi.steps)
				FROM (
					SELECT COUNT(*) as jsonb_object_keys_count
					FROM jsonb_object_keys(twi.step_acknowledgements)
				) counts,
				work_instructions wi
				WHERE wi.id = twi.work_instruction_id
			)
		WHERE twi.task_id = $1 AND twi.work_instruction_id = $2
	`

	_, _ = r.db.ExecContext(ctx, updateAllAcked, taskID, wiID)

	return nil
}

// Helper functions

func (r *WorkInstructionRepository) scanWorkInstruction(rows *sql.Rows) (*WorkInstruction, error) {
	var wi WorkInstruction
	var description sql.NullString
	var productDefID *uuid.UUID
	var machineType sql.NullString
	var steps, toolsRequired, ppeRequired []byte
	var metadataJSON []byte

	err := rows.Scan(
		&wi.ID, &wi.TenantID, &wi.Title, &wi.Version, &wi.Category, &description,
		&productDefID, &machineType, &steps, &toolsRequired,
		&ppeRequired, &wi.IsActive, &metadataJSON, &wi.CreatedAt, &wi.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan work instruction: %w", err)
	}

	if description.Valid {
		wi.Description = description.String
	}
	wi.ProductDefinitionID = productDefID
	if machineType.Valid {
		wi.MachineType = &machineType.String
	}
	if len(steps) > 0 {
		wi.Steps = steps
	}
	if len(toolsRequired) > 0 {
		wi.ToolsRequired = toolsRequired
	}
	if len(ppeRequired) > 0 {
		wi.PPERequired = ppeRequired
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &wi.Metadata)
	}

	return &wi, nil
}

func (r *WorkInstructionRepository) scanWorkInstructionRow(row *sql.Row) (*WorkInstruction, error) {
	var wi WorkInstruction
	var description sql.NullString
	var productDefID *uuid.UUID
	var machineType sql.NullString
	var steps, toolsRequired, ppeRequired []byte
	var metadataJSON []byte

	err := row.Scan(
		&wi.ID, &wi.TenantID, &wi.Title, &wi.Version, &wi.Category, &description,
		&productDefID, &machineType, &steps, &toolsRequired,
		&ppeRequired, &wi.IsActive, &metadataJSON, &wi.CreatedAt, &wi.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if description.Valid {
		wi.Description = description.String
	}
	wi.ProductDefinitionID = productDefID
	if machineType.Valid {
		wi.MachineType = &machineType.String
	}
	if len(steps) > 0 {
		wi.Steps = steps
	}
	if len(toolsRequired) > 0 {
		wi.ToolsRequired = toolsRequired
	}
	if len(ppeRequired) > 0 {
		wi.PPERequired = ppeRequired
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &wi.Metadata)
	}

	return &wi, nil
}
