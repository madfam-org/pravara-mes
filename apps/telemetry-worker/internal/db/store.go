// Package db provides database access for the telemetry worker.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/config"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// Store provides database operations for telemetry data.
type Store struct {
	db *sql.DB
}

// NewStore creates a new database store.
func NewStore(cfg *config.DatabaseConfig) (*Store, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Stats returns database connection pool statistics.
func (s *Store) Stats() sql.DBStats {
	return s.db.Stats()
}

// CreateBatch inserts multiple telemetry records efficiently.
func (s *Store) CreateBatch(ctx context.Context, records []types.Telemetry) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO telemetry (
			id, tenant_id, machine_id, timestamp, metric_type, value, unit, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for i := range records {
		metadataJSON, _ := json.Marshal(records[i].Metadata)

		_, err = stmt.ExecContext(ctx,
			records[i].ID, records[i].TenantID, records[i].MachineID,
			records[i].Timestamp, records[i].MetricType, records[i].Value,
			records[i].Unit, metadataJSON,
		)
		if err != nil {
			return fmt.Errorf("failed to insert telemetry: %w", err)
		}
	}

	return tx.Commit()
}

// GetMachineByCode retrieves a machine by its unique code.
func (s *Store) GetMachineByCode(ctx context.Context, code string) (*types.Machine, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE code = $1
	`

	var machine types.Machine
	var description, mqttTopic, location sql.NullString
	var lastHeartbeat sql.NullTime
	var specificationsJSON, metadataJSON []byte

	err := s.db.QueryRowContext(ctx, query, code).Scan(
		&machine.ID, &machine.TenantID, &machine.Name, &machine.Code,
		&machine.Type, &description, &machine.Status, &mqttTopic,
		&location, &specificationsJSON, &metadataJSON,
		&lastHeartbeat, &machine.CreatedAt, &machine.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get machine by code: %w", err)
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

// UpdateMachineHeartbeat updates the last heartbeat timestamp for a machine.
func (s *Store) UpdateMachineHeartbeat(ctx context.Context, machineID uuid.UUID) error {
	query := `UPDATE machines SET last_heartbeat = $2, status = 'online' WHERE id = $1`

	_, err := s.db.ExecContext(ctx, query, machineID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	return nil
}

// GetMachineByID retrieves a machine by ID.
func (s *Store) GetMachineByID(ctx context.Context, id uuid.UUID) (*types.Machine, error) {
	query := `
		SELECT id, tenant_id, name, code, type, description, status,
		       mqtt_topic, location, specifications, metadata,
		       last_heartbeat, created_at, updated_at
		FROM machines
		WHERE id = $1
	`

	var machine types.Machine
	var description, mqttTopic, location sql.NullString
	var lastHeartbeat sql.NullTime
	var specificationsJSON, metadataJSON []byte

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&machine.ID, &machine.TenantID, &machine.Name, &machine.Code,
		&machine.Type, &description, &machine.Status, &mqttTopic,
		&location, &specificationsJSON, &metadataJSON,
		&lastHeartbeat, &machine.CreatedAt, &machine.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
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
