package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// MachineRepository handles machine database operations.
type MachineRepository struct {
	db *sql.DB
}

// NewMachineRepository creates a new machine repository.
func NewMachineRepository(db *sql.DB) *MachineRepository {
	return &MachineRepository{db: db}
}

// MachineFilter defines filtering options for listing machines.
type MachineFilter struct {
	Status *types.MachineStatus
	Type   *string
	Limit  int
	Offset int
}

// List retrieves machines with optional filtering.
func (r *MachineRepository) List(ctx context.Context, filter MachineFilter) ([]types.Machine, int, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE 1=1
	`
	countQuery := `SELECT COUNT(*) FROM machines WHERE 1=1`

	var args []interface{}
	argIndex := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, *filter.Status)
		argIndex++
	}

	if filter.Type != nil {
		query += fmt.Sprintf(" AND type = $%d", argIndex)
		countQuery += fmt.Sprintf(" AND type = $%d", argIndex)
		args = append(args, *filter.Type)
		argIndex++
	}

	// Get total count
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count machines: %w", err)
	}

	// Add ordering and pagination
	query += " ORDER BY name ASC"

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
		return nil, 0, fmt.Errorf("failed to query machines: %w", err)
	}
	defer rows.Close()

	var machines []types.Machine
	for rows.Next() {
		machine, err := r.scanMachine(rows)
		if err != nil {
			return nil, 0, err
		}
		machines = append(machines, *machine)
	}

	return machines, total, nil
}

// GetByID retrieves a machine by ID.
func (r *MachineRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Machine, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id)
	machine, err := r.scanMachineRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}

	return machine, nil
}

// GetByCode retrieves a machine by its unique code.
func (r *MachineRepository) GetByCode(ctx context.Context, code string) (*types.Machine, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE code = $1
	`

	row := r.db.QueryRowContext(ctx, query, code)
	machine, err := r.scanMachineRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get machine by code: %w", err)
	}

	return machine, nil
}

// Create inserts a new machine.
func (r *MachineRepository) Create(ctx context.Context, machine *types.Machine) error {
	query := `
		INSERT INTO machines (
			id, tenant_id, name, code, type, description, status,
			mqtt_topic, location, specifications, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at
	`

	if machine.ID == uuid.Nil {
		machine.ID = uuid.New()
	}

	specificationsJSON, _ := json.Marshal(machine.Specifications)
	metadataJSON, _ := json.Marshal(machine.Metadata)

	var description, mqttTopic, location *string
	if machine.Description != "" {
		description = &machine.Description
	}
	if machine.MQTTTopic != "" {
		mqttTopic = &machine.MQTTTopic
	}
	if machine.Location != "" {
		location = &machine.Location
	}

	err := r.db.QueryRowContext(ctx, query,
		machine.ID, machine.TenantID, machine.Name, machine.Code,
		machine.Type, description, machine.Status, mqttTopic,
		location, specificationsJSON, metadataJSON,
	).Scan(&machine.CreatedAt, &machine.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create machine: %w", err)
	}

	return nil
}

// Update modifies an existing machine.
func (r *MachineRepository) Update(ctx context.Context, machine *types.Machine) error {
	query := `
		UPDATE machines SET
			name = $2,
			code = $3,
			type = $4,
			description = $5,
			status = $6,
			mqtt_topic = $7,
			location = $8,
			specifications = $9,
			metadata = $10
		WHERE id = $1
		RETURNING updated_at
	`

	specificationsJSON, _ := json.Marshal(machine.Specifications)
	metadataJSON, _ := json.Marshal(machine.Metadata)

	var description, mqttTopic, location *string
	if machine.Description != "" {
		description = &machine.Description
	}
	if machine.MQTTTopic != "" {
		mqttTopic = &machine.MQTTTopic
	}
	if machine.Location != "" {
		location = &machine.Location
	}

	err := r.db.QueryRowContext(ctx, query,
		machine.ID, machine.Name, machine.Code, machine.Type,
		description, machine.Status, mqttTopic, location,
		specificationsJSON, metadataJSON,
	).Scan(&machine.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("machine not found")
	}
	if err != nil {
		return fmt.Errorf("failed to update machine: %w", err)
	}

	return nil
}

// UpdateStatus updates only the machine status.
func (r *MachineRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.MachineStatus) error {
	query := `UPDATE machines SET status = $2 WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, status)
	if err != nil {
		return fmt.Errorf("failed to update machine status: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("machine not found")
	}

	return nil
}

// UpdateHeartbeat updates the last heartbeat timestamp.
func (r *MachineRepository) UpdateHeartbeat(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE machines SET last_heartbeat = $2, status = 'online' WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("machine not found")
	}

	return nil
}

// Delete removes a machine.
func (r *MachineRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM machines WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete machine: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("machine not found")
	}

	return nil
}

// GetOfflineMachines returns machines that haven't sent a heartbeat recently.
func (r *MachineRepository) GetOfflineMachines(ctx context.Context, threshold time.Duration) ([]types.Machine, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE status = 'online'
		  AND (last_heartbeat IS NULL OR last_heartbeat < $1)
	`

	cutoff := time.Now().Add(-threshold)
	rows, err := r.db.QueryContext(ctx, query, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to query offline machines: %w", err)
	}
	defer rows.Close()

	var machines []types.Machine
	for rows.Next() {
		machine, err := r.scanMachine(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, *machine)
	}

	return machines, nil
}

// Helper functions

func (r *MachineRepository) scanMachine(rows *sql.Rows) (*types.Machine, error) {
	var machine types.Machine
	var description, mqttTopic, location sql.NullString
	var lastHeartbeat sql.NullTime
	var specificationsJSON, metadataJSON []byte

	err := rows.Scan(
		&machine.ID, &machine.TenantID, &machine.Name, &machine.Code,
		&machine.Type, &description, &machine.Status, &mqttTopic,
		&location, &specificationsJSON, &metadataJSON,
		&lastHeartbeat, &machine.CreatedAt, &machine.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan machine: %w", err)
	}

	if description.Valid {
		machine.Description = description.String
	}
	if mqttTopic.Valid {
		machine.MQTTTopic = mqttTopic.String
	}
	if location.Valid {
		machine.Location = location.String
	}
	if lastHeartbeat.Valid {
		machine.LastHeartbeat = &lastHeartbeat.Time
	}
	if len(specificationsJSON) > 0 {
		json.Unmarshal(specificationsJSON, &machine.Specifications)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &machine.Metadata)
	}

	return &machine, nil
}

func (r *MachineRepository) scanMachineRow(row *sql.Row) (*types.Machine, error) {
	var machine types.Machine
	var description, mqttTopic, location sql.NullString
	var lastHeartbeat sql.NullTime
	var specificationsJSON, metadataJSON []byte

	err := row.Scan(
		&machine.ID, &machine.TenantID, &machine.Name, &machine.Code,
		&machine.Type, &description, &machine.Status, &mqttTopic,
		&location, &specificationsJSON, &metadataJSON,
		&lastHeartbeat, &machine.CreatedAt, &machine.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if description.Valid {
		machine.Description = description.String
	}
	if mqttTopic.Valid {
		machine.MQTTTopic = mqttTopic.String
	}
	if location.Valid {
		machine.Location = location.String
	}
	if lastHeartbeat.Valid {
		machine.LastHeartbeat = &lastHeartbeat.Time
	}
	if len(specificationsJSON) > 0 {
		json.Unmarshal(specificationsJSON, &machine.Specifications)
	}
	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &machine.Metadata)
	}

	return &machine, nil
}
