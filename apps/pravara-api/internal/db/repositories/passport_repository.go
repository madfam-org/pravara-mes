package repositories

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Passport outbox kinds and statuses (migration 032).
const (
	PassportKindInstance = "instance"
	PassportKindEvent    = "event"

	PassportPending   = "pending"
	PassportDelivered = "delivered"
	PassportFailed    = "failed"
)

// ManufacturingRecordRow is a stored record.
type ManufacturingRecordRow struct {
	ID                uuid.UUID       `json:"id"`
	TenantID          uuid.UUID       `json:"tenant_id"`
	DispatchID        uuid.UUID       `json:"dispatch_id"`
	CommandID         uuid.UUID       `json:"command_id"`
	TaskID            *uuid.UUID      `json:"task_id,omitempty"`
	MachineID         *uuid.UUID      `json:"machine_id,omitempty"`
	GenealogyID       *uuid.UUID      `json:"genealogy_id,omitempty"`
	InstanceUUID      uuid.UUID       `json:"instance_uuid"`
	CompletionEventID *uuid.UUID      `json:"completion_event_id,omitempty"`
	Record            json.RawMessage `json:"record"`
	RecordSHA256      string          `json:"record_sha256"`
	CreatedAt         time.Time       `json:"created_at"`
}

// PassportOutboxRow is one delivery to asset-shells.
type PassportOutboxRow struct {
	ID                    uuid.UUID       `json:"id"`
	TenantID              uuid.UUID       `json:"tenant_id"`
	ManufacturingRecordID uuid.UUID       `json:"manufacturing_record_id"`
	Kind                  string          `json:"kind"`
	Sequence              int             `json:"sequence"`
	EventID               *uuid.UUID      `json:"event_id,omitempty"`
	Payload               json.RawMessage `json:"-"`
	PayloadSHA256         string          `json:"payload_sha256"`
	Status                string          `json:"status"`
	Attempts              int             `json:"attempts"`
	NextAttemptAt         time.Time       `json:"next_attempt_at"`
	LastError             string          `json:"last_error,omitempty"`
	LastStatusCode        int             `json:"last_status_code,omitempty"`
	DeliveredAt           *time.Time      `json:"delivered_at,omitempty"`
	InstanceUUID          uuid.UUID       `json:"instance_uuid"`
}

// PassportRepository stores manufacturing records and the passport outbox.
type PassportRepository struct {
	db DBTX
}

// NewPassportRepository creates the repository.
func NewPassportRepository(db DBTX) *PassportRepository { return &PassportRepository{db: db} }

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// InsertRecord stores a manufacturing record (append-only).
func (r *PassportRepository) InsertRecord(ctx context.Context, rec *ManufacturingRecordRow) error {
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO manufacturing_records (id, tenant_id, dispatch_id, command_id, task_id, machine_id, genealogy_id,
			instance_uuid, completion_event_id, record, record_sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING created_at`,
		rec.ID, rec.TenantID, rec.DispatchID, rec.CommandID, nullUUID(rec.TaskID), nullUUID(rec.MachineID),
		nullUUID(rec.GenealogyID), rec.InstanceUUID, nullUUID(rec.CompletionEventID), []byte(rec.Record), rec.RecordSHA256,
	).Scan(&rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert manufacturing record: %w", err)
	}
	return nil
}

// RecordByDispatch reads the record of a dispatch (nil when none).
func (r *PassportRepository) RecordByDispatch(ctx context.Context, dispatchID uuid.UUID) (*ManufacturingRecordRow, error) {
	rec := &ManufacturingRecordRow{}
	var task, machine, gen, ev uuid.NullUUID
	var body []byte
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, dispatch_id, command_id, task_id, machine_id, genealogy_id, instance_uuid,
		       completion_event_id, record, record_sha256, created_at
		FROM manufacturing_records WHERE dispatch_id = $1`, dispatchID).Scan(&rec.ID, &rec.TenantID, &rec.DispatchID,
		&rec.CommandID, &task, &machine, &gen, &rec.InstanceUUID, &ev, &body, &rec.RecordSHA256, &rec.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manufacturing record: %w", err)
	}
	rec.TaskID, rec.MachineID, rec.GenealogyID, rec.CompletionEventID = ptrUUID(task), ptrUUID(machine), ptrUUID(gen), ptrUUID(ev)
	rec.Record = body
	return rec, nil
}

// Enqueue adds a delivery. Events are ordered after the instance by
// sequence and are delivered only once every earlier row is delivered.
func (r *PassportRepository) Enqueue(ctx context.Context, row *PassportOutboxRow) error {
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.PayloadSHA256 = digest(row.Payload)
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO passport_outbox (id, tenant_id, manufacturing_record_id, kind, sequence, event_id, payload, payload_sha256)
		VALUES ($1, $2, $3, $4,
			COALESCE((SELECT max(sequence) + 1 FROM passport_outbox WHERE manufacturing_record_id = $3), 0),
			$5, $6, $7)
		RETURNING sequence, status, next_attempt_at`,
		row.ID, row.TenantID, row.ManufacturingRecordID, row.Kind, nullUUID(row.EventID), []byte(row.Payload), row.PayloadSHA256,
	).Scan(&row.Sequence, &row.Status, &row.NextAttemptAt)
	if err != nil {
		return fmt.Errorf("enqueue passport delivery: %w", err)
	}
	return nil
}

// DuePassportDeliveries returns pending rows whose attempt is due and whose
// earlier rows (same record, lower sequence) are all delivered. Delivery is
// idempotent at asset-shells (a replayed instance or eventId answers 200),
// so two runners delivering the same row is harmless.
func (r *PassportRepository) DuePassportDeliveries(ctx context.Context, limit int) ([]PassportOutboxRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT o.id, o.tenant_id, o.manufacturing_record_id, o.kind, o.sequence, o.event_id, o.payload,
		       o.payload_sha256, o.status, o.attempts, o.next_attempt_at, m.instance_uuid
		FROM passport_outbox o JOIN manufacturing_records m ON m.id = o.manufacturing_record_id
		WHERE o.status = 'pending' AND o.next_attempt_at <= NOW()
		  AND NOT EXISTS (SELECT 1 FROM passport_outbox p
		                  WHERE p.manufacturing_record_id = o.manufacturing_record_id
		                    AND p.sequence < o.sequence AND p.status <> 'delivered')
		ORDER BY o.next_attempt_at, o.sequence
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list passport deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PassportOutboxRow
	for rows.Next() {
		var row PassportOutboxRow
		var ev uuid.NullUUID
		var payload []byte
		if err := rows.Scan(&row.ID, &row.TenantID, &row.ManufacturingRecordID, &row.Kind, &row.Sequence, &ev,
			&payload, &row.PayloadSHA256, &row.Status, &row.Attempts, &row.NextAttemptAt, &row.InstanceUUID); err != nil {
			return nil, fmt.Errorf("scan passport delivery: %w", err)
		}
		row.EventID, row.Payload = ptrUUID(ev), payload
		out = append(out, row)
	}
	return out, rows.Err()
}

// MarkDelivered records a successful delivery.
func (r *PassportRepository) MarkDelivered(ctx context.Context, id uuid.UUID, status int, response []byte) error {
	if len(response) == 0 || !json.Valid(response) {
		response = []byte("null")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE passport_outbox SET status = 'delivered', attempts = attempts + 1, last_status_code = $2,
			response = $3, delivered_at = NOW(), last_error = NULL, updated_at = NOW()
		WHERE id = $1`, id, status, response)
	if err != nil {
		return fmt.Errorf("mark passport delivered: %w", err)
	}
	return nil
}

// MarkAttemptFailed records a failed attempt: retried at next, or failed
// for good when terminal. The row (and its payload) is kept either way.
func (r *PassportRepository) MarkAttemptFailed(ctx context.Context, id uuid.UUID, status int, msg string, terminal bool, next time.Time) error {
	newStatus := PassportPending
	if terminal {
		newStatus = PassportFailed
	}
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE passport_outbox SET status = $2, attempts = attempts + 1, last_status_code = NULLIF($3, 0),
			last_error = $4, next_attempt_at = $5, updated_at = NOW()
		WHERE id = $1`, id, newStatus, status, msg, next)
	if err != nil {
		return fmt.Errorf("record passport attempt: %w", err)
	}
	return nil
}

// Deliveries lists the outbox rows of a record.
func (r *PassportRepository) Deliveries(ctx context.Context, recordID uuid.UUID) ([]PassportOutboxRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, manufacturing_record_id, kind, sequence, event_id, payload_sha256, status, attempts,
		       next_attempt_at, COALESCE(last_error, ''), COALESCE(last_status_code, 0), delivered_at
		FROM passport_outbox WHERE manufacturing_record_id = $1 ORDER BY sequence`, recordID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PassportOutboxRow
	for rows.Next() {
		var row PassportOutboxRow
		var ev uuid.NullUUID
		var delivered sql.NullTime
		if err := rows.Scan(&row.ID, &row.TenantID, &row.ManufacturingRecordID, &row.Kind, &row.Sequence, &ev,
			&row.PayloadSHA256, &row.Status, &row.Attempts, &row.NextAttemptAt, &row.LastError, &row.LastStatusCode,
			&delivered); err != nil {
			return nil, err
		}
		row.EventID = ptrUUID(ev)
		if delivered.Valid {
			row.DeliveredAt = &delivered.Time
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
