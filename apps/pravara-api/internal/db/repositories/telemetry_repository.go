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

// TelemetryRepository handles telemetry database operations.
type TelemetryRepository struct {
	db *sql.DB
}

// NewTelemetryRepository creates a new telemetry repository.
func NewTelemetryRepository(db *sql.DB) *TelemetryRepository {
	return &TelemetryRepository{db: db}
}

// TelemetryFilter defines filtering options for listing telemetry.
type TelemetryFilter struct {
	MachineID  *uuid.UUID
	MetricType *string
	FromTime   *time.Time
	ToTime     *time.Time
	Limit      int
}

// List retrieves telemetry with filtering.
func (r *TelemetryRepository) List(ctx context.Context, filter TelemetryFilter) ([]types.Telemetry, error) {
	query := `
		SELECT id, tenant_id, machine_id, timestamp, metric_type, value, unit, metadata, created_at
		FROM telemetry
		WHERE 1=1
	`

	var args []interface{}
	argIndex := 1

	if filter.MachineID != nil {
		query += fmt.Sprintf(" AND machine_id = $%d", argIndex)
		args = append(args, *filter.MachineID)
		argIndex++
	}

	if filter.MetricType != nil {
		query += fmt.Sprintf(" AND metric_type = $%d", argIndex)
		args = append(args, *filter.MetricType)
		argIndex++
	}

	if filter.FromTime != nil {
		query += fmt.Sprintf(" AND timestamp >= $%d", argIndex)
		args = append(args, *filter.FromTime)
		argIndex++
	}

	if filter.ToTime != nil {
		query += fmt.Sprintf(" AND timestamp <= $%d", argIndex)
		args = append(args, *filter.ToTime)
		argIndex++
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argIndex)
		args = append(args, filter.Limit)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query telemetry: %w", err)
	}
	defer rows.Close()

	var telemetry []types.Telemetry
	for rows.Next() {
		t, err := r.scanTelemetry(rows)
		if err != nil {
			return nil, err
		}
		telemetry = append(telemetry, *t)
	}

	return telemetry, nil
}

// Create inserts a new telemetry record.
func (r *TelemetryRepository) Create(ctx context.Context, telemetry *types.Telemetry) error {
	query := `
		INSERT INTO telemetry (
			id, tenant_id, machine_id, timestamp, metric_type, value, unit, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at
	`

	if telemetry.ID == uuid.Nil {
		telemetry.ID = uuid.New()
	}

	metadataJSON, _ := json.Marshal(telemetry.Metadata)

	err := r.db.QueryRowContext(ctx, query,
		telemetry.ID, telemetry.TenantID, telemetry.MachineID,
		telemetry.Timestamp, telemetry.MetricType, telemetry.Value,
		telemetry.Unit, metadataJSON,
	).Scan(&telemetry.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create telemetry: %w", err)
	}

	return nil
}

// CreateBatch inserts multiple telemetry records efficiently.
func (r *TelemetryRepository) CreateBatch(ctx context.Context, records []types.Telemetry) error {
	if len(records) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
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
		if records[i].ID == uuid.Nil {
			records[i].ID = uuid.New()
		}
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

// GetLatest retrieves the most recent telemetry for a machine by metric type.
func (r *TelemetryRepository) GetLatest(ctx context.Context, machineID uuid.UUID, metricType string) (*types.Telemetry, error) {
	query := `
		SELECT id, tenant_id, machine_id, timestamp, metric_type, value, unit, metadata, created_at
		FROM telemetry
		WHERE machine_id = $1 AND metric_type = $2
		ORDER BY timestamp DESC
		LIMIT 1
	`

	row := r.db.QueryRowContext(ctx, query, machineID, metricType)
	t, err := r.scanTelemetryRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest telemetry: %w", err)
	}

	return t, nil
}

// GetAggregated retrieves aggregated telemetry data.
func (r *TelemetryRepository) GetAggregated(ctx context.Context, machineID uuid.UUID, metricType string, fromTime, toTime time.Time, interval string) ([]map[string]interface{}, error) {
	// Determine the time bucket based on interval
	var timeBucket string
	switch interval {
	case "minute":
		timeBucket = "date_trunc('minute', timestamp)"
	case "hour":
		timeBucket = "date_trunc('hour', timestamp)"
	case "day":
		timeBucket = "date_trunc('day', timestamp)"
	default:
		timeBucket = "date_trunc('hour', timestamp)"
	}

	query := fmt.Sprintf(`
		SELECT
			%s as bucket,
			AVG(value) as avg_value,
			MIN(value) as min_value,
			MAX(value) as max_value,
			COUNT(*) as count
		FROM telemetry
		WHERE machine_id = $1 AND metric_type = $2
		  AND timestamp >= $3 AND timestamp <= $4
		GROUP BY bucket
		ORDER BY bucket ASC
	`, timeBucket)

	rows, err := r.db.QueryContext(ctx, query, machineID, metricType, fromTime, toTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query aggregated telemetry: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var bucket time.Time
		var avgValue, minValue, maxValue float64
		var count int

		if err := rows.Scan(&bucket, &avgValue, &minValue, &maxValue, &count); err != nil {
			return nil, fmt.Errorf("failed to scan aggregated row: %w", err)
		}

		results = append(results, map[string]interface{}{
			"timestamp": bucket,
			"avg":       avgValue,
			"min":       minValue,
			"max":       maxValue,
			"count":     count,
		})
	}

	return results, nil
}

// DeleteOlderThan removes telemetry records older than the specified time.
func (r *TelemetryRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM telemetry WHERE timestamp < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old telemetry: %w", err)
	}
	return result.RowsAffected()
}

// Helper functions

func (r *TelemetryRepository) scanTelemetry(rows *sql.Rows) (*types.Telemetry, error) {
	var t types.Telemetry
	var metadataJSON []byte

	err := rows.Scan(
		&t.ID, &t.TenantID, &t.MachineID, &t.Timestamp,
		&t.MetricType, &t.Value, &t.Unit, &metadataJSON, &t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan telemetry: %w", err)
	}

	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &t.Metadata)
	}

	return &t, nil
}

func (r *TelemetryRepository) scanTelemetryRow(row *sql.Row) (*types.Telemetry, error) {
	var t types.Telemetry
	var metadataJSON []byte

	err := row.Scan(
		&t.ID, &t.TenantID, &t.MachineID, &t.Timestamp,
		&t.MetricType, &t.Value, &t.Unit, &metadataJSON, &t.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	if len(metadataJSON) > 0 {
		json.Unmarshal(metadataJSON, &t.Metadata)
	}

	return &t, nil
}
