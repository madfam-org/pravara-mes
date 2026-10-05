package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/packages/sparkplug/brokerauth"
)

// Edge registry errors.
var (
	ErrEnrollmentNotFound   = errors.New("enrollment not found")
	ErrEnrollmentExpired    = errors.New("enrollment expired")
	ErrEnrollmentMismatch   = errors.New("enrollment is for another edge node")
	ErrTooManyPending       = errors.New("too many pending enrollments")
	ErrUserCodeTaken        = errors.New("user code already in use")
	ErrSparkplugDeviceTaken = errors.New("another machine is already this Sparkplug device")
)

// EdgeNode is a registered site edge node (no credential material).
type EdgeNode struct {
	ID                  uuid.UUID  `json:"id"`
	TenantID            uuid.UUID  `json:"tenant_id"`
	EdgeNodeID          string     `json:"edge_node_id"`
	MQTTUsername        string     `json:"mqtt_username"`
	CredentialRotatedAt time.Time  `json:"credential_rotated_at"`
	ApprovedBy          string     `json:"approved_by,omitempty"`
	DisabledAt          *time.Time `json:"disabled_at,omitempty"`
	Online              bool       `json:"online"`
	LastBdSeq           *int64     `json:"last_bdseq,omitempty"`
	LastBirthAt         *time.Time `json:"last_birth_at,omitempty"`
	LastDeathAt         *time.Time `json:"last_death_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

// EdgeEnrollment is a credential registration awaiting approval.
type EdgeEnrollment struct {
	ID           uuid.UUID `json:"id"`
	TenantID     uuid.UUID `json:"-"`
	EdgeNodeID   string    `json:"edge_node_id"`
	MQTTUsername string    `json:"mqtt_username"`
	PasswordHash string    `json:"-"`
	UserCode     string    `json:"user_code"`
	Status       string    `json:"status"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
	GroupID      string    `json:"group_id,omitempty"`
}

// MachineLiveState is a machine's Sparkplug binding and latest live state
// (machine_live_state, written by the telemetry worker's primary host).
type MachineLiveState struct {
	MachineID       uuid.UUID       `json:"machine_id"`
	Code            string          `json:"code"`
	SparkplugEdgeID *string         `json:"sparkplug_edge_id"`
	Reported        bool            `json:"reported"`
	Online          bool            `json:"online"`
	StateStatus     *string         `json:"state_status"`
	Progress        *float64        `json:"progress"`
	HotendTempC     *float64        `json:"hotend_temp_c"`
	BedTempC        *float64        `json:"bed_temp_c"`
	MaterialSlots   json.RawMessage `json:"material_slots"`
	Capabilities    json.RawMessage `json:"capabilities"`
	Properties      json.RawMessage `json:"properties"`
	JobID           *string         `json:"job_id"`
	JobStatus       *string         `json:"job_status"`
	CommandLastID   *string         `json:"command_last_id"`
	CommandStatus   *string         `json:"command_status"`
	CommandError    *string         `json:"command_error"`
	BornAt          *time.Time      `json:"born_at"`
	DiedAt          *time.Time      `json:"died_at"`
	ReportedAt      *time.Time      `json:"reported_at"`
	UpdatedAt       *time.Time      `json:"updated_at"`
}

// EdgeRegistryRepository persists edge nodes, enrollments, Sparkplug machine
// bindings and reads live state. Statements run in the scope carried by ctx
// (request tenant scope, RunInTenantTx, or RunInSystemScope for the two
// lookups that start from a username or an enrollment id).
type EdgeRegistryRepository struct {
	db DBTX
}

// NewEdgeRegistryRepository creates the repository.
func NewEdgeRegistryRepository(db DBTX) *EdgeRegistryRepository {
	return &EdgeRegistryRepository{db: db}
}

// LookupCredential implements brokerauth.CredentialStore. Run it in the
// read-only system scope (031 system_scope_read on edge_nodes).
func (r *EdgeRegistryRepository) LookupCredential(ctx context.Context, username string) (*brokerauth.Credential, error) {
	var c brokerauth.Credential
	err := r.db.QueryRowContext(ctx, `
		SELECT n.mqtt_username, t.slug, n.edge_node_id, n.password_hash, n.disabled_at IS NOT NULL
		FROM edge_nodes n JOIN tenants t ON t.id = n.tenant_id
		WHERE n.mqtt_username = $1`, username,
	).Scan(&c.Username, &c.Group, &c.EdgeNodeID, &c.PasswordHash, &c.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup edge credential: %w", err)
	}
	return &c, nil
}

// TenantIDBySlug resolves a tenant slug (tenants has no row-level security).
func (r *EdgeRegistryRepository) TenantIDBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// CreateEnrollment records a pending enrollment in the tenant scope of ctx.
// Expired pending rows are closed first; an older pending enrollment of the
// same edge node is superseded. It fails with ErrTooManyPending at the cap
// and ErrUserCodeTaken on a user-code collision (retry with a new code in a
// new transaction).
func (r *EdgeRegistryRepository) CreateEnrollment(ctx context.Context, e *EdgeEnrollment, maxPending int) error {
	if _, err := r.db.ExecContext(ctx, `
		UPDATE edge_enrollments SET status = 'expired', password_hash = ''
		WHERE status = 'pending' AND expires_at < NOW() AND `+tenantMatch); err != nil {
		return fmt.Errorf("expire enrollments: %w", err)
	}
	var pending int
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM edge_enrollments WHERE status = 'pending' AND edge_node_id <> $1 AND `+tenantMatch,
		e.EdgeNodeID).Scan(&pending); err != nil {
		return fmt.Errorf("count enrollments: %w", err)
	}
	if pending >= maxPending {
		return ErrTooManyPending
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE edge_enrollments SET status = 'superseded', password_hash = '', decided_at = NOW()
		WHERE status = 'pending' AND edge_node_id = $1 AND `+tenantMatch, e.EdgeNodeID); err != nil {
		return fmt.Errorf("supersede enrollments: %w", err)
	}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO edge_enrollments (tenant_id, edge_node_id, mqtt_username, password_hash, user_code, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, status, created_at`,
		e.TenantID, e.EdgeNodeID, e.MQTTUsername, e.PasswordHash, e.UserCode, e.ExpiresAt,
	).Scan(&e.ID, &e.Status, &e.CreatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrUserCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create enrollment: %w", err)
	}
	return nil
}

// GetEnrollmentStatus reads an enrollment by id for the box's status poll.
// Run it in the read-only system scope; it returns no credential material.
func (r *EdgeRegistryRepository) GetEnrollmentStatus(ctx context.Context, id uuid.UUID) (*EdgeEnrollment, error) {
	var e EdgeEnrollment
	err := r.db.QueryRowContext(ctx, `
		SELECT e.id, e.edge_node_id, e.mqtt_username, e.user_code,
		       CASE WHEN e.status = 'pending' AND e.expires_at < NOW() THEN 'expired' ELSE e.status END,
		       e.expires_at, e.created_at, t.slug
		FROM edge_enrollments e JOIN tenants t ON t.id = e.tenant_id
		WHERE e.id = $1`, id,
	).Scan(&e.ID, &e.EdgeNodeID, &e.MQTTUsername, &e.UserCode, &e.Status, &e.ExpiresAt, &e.CreatedAt, &e.GroupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get enrollment: %w", err)
	}
	return &e, nil
}

// ListPendingEnrollments lists the tenant's pending, unexpired enrollments.
func (r *EdgeRegistryRepository) ListPendingEnrollments(ctx context.Context) ([]EdgeEnrollment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, edge_node_id, mqtt_username, user_code, status, expires_at, created_at
		FROM edge_enrollments
		WHERE status = 'pending' AND expires_at >= NOW() AND `+tenantMatch+`
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list enrollments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []EdgeEnrollment{}
	for rows.Next() {
		var e EdgeEnrollment
		if err := rows.Scan(&e.ID, &e.EdgeNodeID, &e.MQTTUsername, &e.UserCode, &e.Status, &e.ExpiresAt, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ApproveEnrollment activates the pending enrollment with userCode in the
// tenant scope of ctx: the edge node's credential becomes the enrollment's
// hash (created, or rotated and re-enabled), and the enrollment is closed.
// Only enrollments of the approving tenant are visible.
func (r *EdgeRegistryRepository) ApproveEnrollment(ctx context.Context, userCode, edgeNodeID, approvedBy string) (*EdgeNode, error) {
	var (
		id                     uuid.UUID
		tenantID               uuid.UUID
		edge, username, pwHash string
		expiresAt              time.Time
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, edge_node_id, mqtt_username, password_hash, expires_at
		FROM edge_enrollments
		WHERE user_code = $1 AND status = 'pending' AND `+tenantMatch+`
		FOR UPDATE`, userCode,
	).Scan(&id, &tenantID, &edge, &username, &pwHash, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load enrollment: %w", err)
	}
	if edge != edgeNodeID {
		return nil, ErrEnrollmentMismatch
	}
	if time.Now().After(expiresAt) {
		return nil, ErrEnrollmentExpired
	}
	n, err := scanEdgeNode(r.db.QueryRowContext(ctx, `
		INSERT INTO edge_nodes (tenant_id, edge_node_id, mqtt_username, password_hash, approved_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (tenant_id, edge_node_id) DO UPDATE SET
			mqtt_username = EXCLUDED.mqtt_username, password_hash = EXCLUDED.password_hash,
			credential_rotated_at = NOW(), approved_by = EXCLUDED.approved_by,
			disabled_at = NULL, updated_at = NOW()
		RETURNING `+edgeNodeColumns, tenantID, edge, username, pwHash, approvedBy))
	if err != nil {
		return nil, fmt.Errorf("activate edge node: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE edge_enrollments
		SET status = 'approved', password_hash = '', decided_at = NOW(), decided_by = $2, edge_node_ref = $3
		WHERE id = $1 AND `+tenantMatch, id, approvedBy, n.ID); err != nil {
		return nil, fmt.Errorf("close enrollment: %w", err)
	}
	return n, nil
}

const edgeNodeColumns = `id, tenant_id, edge_node_id, mqtt_username, credential_rotated_at,
	COALESCE(approved_by, ''), disabled_at, online, last_bdseq, last_birth_at, last_death_at, created_at`

func scanEdgeNode(row interface{ Scan(...any) error }) (*EdgeNode, error) {
	var n EdgeNode
	var disabled, birth, death sql.NullTime
	var bdseq sql.NullInt64
	if err := row.Scan(&n.ID, &n.TenantID, &n.EdgeNodeID, &n.MQTTUsername, &n.CredentialRotatedAt,
		&n.ApprovedBy, &disabled, &n.Online, &bdseq, &birth, &death, &n.CreatedAt); err != nil {
		return nil, err
	}
	n.DisabledAt = nullTime(disabled)
	n.LastBirthAt = nullTime(birth)
	n.LastDeathAt = nullTime(death)
	if bdseq.Valid {
		n.LastBdSeq = &bdseq.Int64
	}
	return &n, nil
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// ListEdgeNodes lists the tenant's edge nodes.
func (r *EdgeRegistryRepository) ListEdgeNodes(ctx context.Context) ([]EdgeNode, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+edgeNodeColumns+` FROM edge_nodes WHERE `+tenantMatch+` ORDER BY edge_node_id`)
	if err != nil {
		return nil, fmt.Errorf("list edge nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []EdgeNode{}
	for rows.Next() {
		n, err := scanEdgeNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// DisableEdgeNode revokes an edge node's credential (disabled_at). The broker
// refuses its next CONNECT and every further publish or subscribe.
func (r *EdgeRegistryRepository) DisableEdgeNode(ctx context.Context, id uuid.UUID) (*EdgeNode, error) {
	n, err := scanEdgeNode(r.db.QueryRowContext(ctx, `
		UPDATE edge_nodes SET disabled_at = COALESCE(disabled_at, NOW()), online = FALSE, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch+` RETURNING `+edgeNodeColumns, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

// BindMachine sets (or clears, with nil) a machine's Sparkplug edge node.
// It returns the machine code, which is the Sparkplug device_id.
func (r *EdgeRegistryRepository) BindMachine(ctx context.Context, machineID uuid.UUID, edgeNodeID *string) (string, error) {
	var code string
	err := r.db.QueryRowContext(ctx, `
		UPDATE machines SET sparkplug_edge_id = $2, updated_at = NOW()
		WHERE id = $1 AND `+tenantMatch+` RETURNING code`, machineID, edgeNodeID).Scan(&code)
	var pqErr *pq.Error
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", ErrNotFound
	case errors.As(err, &pqErr) && pqErr.Code == "23505":
		return "", ErrSparkplugDeviceTaken
	case err != nil:
		return "", fmt.Errorf("bind machine: %w", err)
	}
	return code, nil
}

// MachineCode returns a machine's code in the tenant scope of ctx.
func (r *EdgeRegistryRepository) MachineCode(ctx context.Context, machineID uuid.UUID) (string, error) {
	var code string
	err := r.db.QueryRowContext(ctx, `SELECT code FROM machines WHERE id = $1 AND `+tenantMatch, machineID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return code, err
}

const liveStateQuery = `
	SELECT m.id, m.code, m.sparkplug_edge_id, ls.machine_id IS NOT NULL, COALESCE(ls.online, FALSE),
	       ls.state_status, ls.progress, ls.hotend_temp_c, ls.bed_temp_c,
	       COALESCE(ls.material_slots, '[]'::jsonb), COALESCE(ls.capabilities, '{}'::jsonb),
	       COALESCE(ls.properties, '{}'::jsonb), ls.job_id, ls.job_status, ls.command_last_id,
	       ls.command_status, ls.command_error, ls.born_at, ls.died_at, ls.reported_at, ls.updated_at
	FROM machines m
	LEFT JOIN machine_live_state ls ON ls.machine_id = m.id AND ls.tenant_id = m.tenant_id
	WHERE ` + "m." + tenantMatch

func scanLiveState(row interface{ Scan(...any) error }) (*MachineLiveState, error) {
	var s MachineLiveState
	var edge, status, jobID, jobStatus, cmdID, cmdStatus, cmdErr sql.NullString
	var progress, hotend, bed sql.NullFloat64
	var born, died, reported, updated sql.NullTime
	var slots, caps, props []byte
	if err := row.Scan(&s.MachineID, &s.Code, &edge, &s.Reported, &s.Online, &status, &progress, &hotend, &bed,
		&slots, &caps, &props, &jobID, &jobStatus, &cmdID, &cmdStatus, &cmdErr, &born, &died, &reported, &updated); err != nil {
		return nil, err
	}
	str := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		x := v.String
		return &x
	}
	num := func(v sql.NullFloat64) *float64 {
		if !v.Valid {
			return nil
		}
		x := v.Float64
		return &x
	}
	s.SparkplugEdgeID, s.StateStatus = str(edge), str(status)
	s.JobID, s.JobStatus, s.CommandLastID, s.CommandStatus, s.CommandError = str(jobID), str(jobStatus), str(cmdID), str(cmdStatus), str(cmdErr)
	s.Progress, s.HotendTempC, s.BedTempC = num(progress), num(hotend), num(bed)
	s.MaterialSlots, s.Capabilities, s.Properties = slots, caps, props
	s.BornAt, s.DiedAt, s.ReportedAt, s.UpdatedAt = nullTime(born), nullTime(died), nullTime(reported), nullTime(updated)
	return &s, nil
}

// GetLiveState returns a machine's Sparkplug binding and live state.
func (r *EdgeRegistryRepository) GetLiveState(ctx context.Context, machineID uuid.UUID) (*MachineLiveState, error) {
	s, err := scanLiveState(r.db.QueryRowContext(ctx, liveStateQuery+` AND m.id = $1`, machineID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get live state: %w", err)
	}
	return s, nil
}

// ListLiveStates returns the binding and live state of every
// Sparkplug-registered machine of the tenant (matchmaking input).
func (r *EdgeRegistryRepository) ListLiveStates(ctx context.Context) ([]MachineLiveState, error) {
	rows, err := r.db.QueryContext(ctx, liveStateQuery+` AND m.sparkplug_edge_id IS NOT NULL ORDER BY m.code`)
	if err != nil {
		return nil, fmt.Errorf("list live states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []MachineLiveState{}
	for rows.Next() {
		s, err := scanLiveState(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}
