package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

// PrinterProfile represents a printer configuration profile
type PrinterProfile struct {
	ID          uuid.UUID `db:"id" json:"id"`
	TenantID    uuid.UUID `db:"tenant_id" json:"tenant_id"`
	Name        string    `db:"name" json:"name"`
	Description *string   `db:"description" json:"description,omitempty"`

	// Specifications
	PrinterType  string  `db:"printer_type" json:"printer_type"`
	Manufacturer *string `db:"manufacturer" json:"manufacturer,omitempty"`
	Model        *string `db:"model" json:"model,omitempty"`

	// Build volume
	BuildVolumeX *float64 `db:"build_volume_x" json:"build_volume_x,omitempty"`
	BuildVolumeY *float64 `db:"build_volume_y" json:"build_volume_y,omitempty"`
	BuildVolumeZ *float64 `db:"build_volume_z" json:"build_volume_z,omitempty"`

	// Capabilities
	HeatedBed      bool `db:"heated_bed" json:"heated_bed"`
	HeatedChamber  bool `db:"heated_chamber" json:"heated_chamber"`
	AutoLeveling   bool `db:"auto_leveling" json:"auto_leveling"`
	FilamentSensor bool `db:"filament_sensor" json:"filament_sensor"`
	PowerRecovery  bool `db:"power_recovery" json:"power_recovery"`

	// Multi-tool capabilities
	Has3DPrinting bool `db:"has_3d_printing" json:"has_3d_printing"`
	HasLaser      bool `db:"has_laser" json:"has_laser"`
	HasCNC        bool `db:"has_cnc" json:"has_cnc"`
	HasPenPlotter bool `db:"has_pen_plotter" json:"has_pen_plotter"`

	// Temperature limits
	MinNozzleTemp int `db:"min_nozzle_temp" json:"min_nozzle_temp"`
	MaxNozzleTemp int `db:"max_nozzle_temp" json:"max_nozzle_temp"`
	MinBedTemp    int `db:"min_bed_temp" json:"min_bed_temp"`
	MaxBedTemp    int `db:"max_bed_temp" json:"max_bed_temp"`

	// Speed limits
	MaxPrintSpeed  int `db:"max_print_speed" json:"max_print_speed"`
	MaxTravelSpeed int `db:"max_travel_speed" json:"max_travel_speed"`
	MaxZSpeed      int `db:"max_z_speed" json:"max_z_speed"`

	// G-code
	GCodeFlavor      string  `db:"gcode_flavor" json:"gcode_flavor"`
	StartGCode       *string `db:"start_gcode" json:"start_gcode,omitempty"`
	EndGCode         *string `db:"end_gcode" json:"end_gcode,omitempty"`
	LayerChangeGCode *string `db:"layer_change_gcode" json:"layer_change_gcode,omitempty"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// PrinterConnection represents an active printer connection
type PrinterConnection struct {
	ID                  uuid.UUID  `db:"id" json:"id"`
	TenantID            uuid.UUID  `db:"tenant_id" json:"tenant_id"`
	MachineID           *uuid.UUID `db:"machine_id" json:"machine_id,omitempty"`
	DiscoveredMachineID *uuid.UUID `db:"discovered_machine_id" json:"discovered_machine_id,omitempty"`
	ProfileID           *uuid.UUID `db:"profile_id" json:"profile_id,omitempty"`

	// Basic info
	Name           string `db:"name" json:"name"`
	ConnectionType string `db:"connection_type" json:"connection_type"`

	// Connection parameters
	ConnectionURL *string `db:"connection_url" json:"connection_url,omitempty"`
	SerialPort    *string `db:"serial_port" json:"serial_port,omitempty"`
	BaudRate      int     `db:"baud_rate" json:"baud_rate"`
	APIKey        *string `db:"api_key" json:"api_key,omitempty"`

	// State
	IsActive           bool       `db:"is_active" json:"is_active"`
	IsConnected        bool       `db:"is_connected" json:"is_connected"`
	LastConnectedAt    *time.Time `db:"last_connected_at" json:"last_connected_at,omitempty"`
	LastDisconnectedAt *time.Time `db:"last_disconnected_at" json:"last_disconnected_at,omitempty"`
	ConnectionError    *string    `db:"connection_error" json:"connection_error,omitempty"`

	CurrentState string  `db:"current_state" json:"current_state"`
	CurrentTool  *string `db:"current_tool" json:"current_tool,omitempty"`

	// Temperatures
	NozzleTempCurrent  *float64 `db:"nozzle_temp_current" json:"nozzle_temp_current,omitempty"`
	NozzleTempTarget   *float64 `db:"nozzle_temp_target" json:"nozzle_temp_target,omitempty"`
	BedTempCurrent     *float64 `db:"bed_temp_current" json:"bed_temp_current,omitempty"`
	BedTempTarget      *float64 `db:"bed_temp_target" json:"bed_temp_target,omitempty"`
	ChamberTempCurrent *float64 `db:"chamber_temp_current" json:"chamber_temp_current,omitempty"`

	// Position
	PositionX *float64 `db:"position_x" json:"position_x,omitempty"`
	PositionY *float64 `db:"position_y" json:"position_y,omitempty"`
	PositionZ *float64 `db:"position_z" json:"position_z,omitempty"`
	PositionE *float64 `db:"position_e" json:"position_e,omitempty"`

	// Current job
	CurrentJobID *uuid.UUID `db:"current_job_id" json:"current_job_id,omitempty"`

	// Statistics
	TotalPrintTimeHours float64 `db:"total_print_time_hours" json:"total_print_time_hours"`
	TotalFilamentUsedM  float64 `db:"total_filament_used_m" json:"total_filament_used_m"`
	SuccessfulPrints    int     `db:"successful_prints" json:"successful_prints"`
	FailedPrints        int     `db:"failed_prints" json:"failed_prints"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// PrintJob represents a print job
type PrintJob struct {
	ID           uuid.UUID  `db:"id" json:"id"`
	TenantID     uuid.UUID  `db:"tenant_id" json:"tenant_id"`
	ConnectionID *uuid.UUID `db:"connection_id" json:"connection_id,omitempty"`
	ProfileID    *uuid.UUID `db:"profile_id" json:"profile_id,omitempty"`
	MaterialID   *uuid.UUID `db:"material_id" json:"material_id,omitempty"`

	// Job info
	JobName       string  `db:"job_name" json:"job_name"`
	FileName      *string `db:"file_name" json:"file_name,omitempty"`
	FileSizeBytes *int64  `db:"file_size_bytes" json:"file_size_bytes,omitempty"`
	Source        *string `db:"source" json:"source,omitempty"`

	// Estimates
	LayerCount           *int     `db:"layer_count" json:"layer_count,omitempty"`
	EstimatedTimeSeconds *int     `db:"estimated_time_seconds" json:"estimated_time_seconds,omitempty"`
	EstimatedFilamentMM  *float64 `db:"estimated_filament_mm" json:"estimated_filament_mm,omitempty"`
	EstimatedFilamentG   *float64 `db:"estimated_filament_g" json:"estimated_filament_g,omitempty"`

	// Actuals
	ActualTimeSeconds *int     `db:"actual_time_seconds" json:"actual_time_seconds,omitempty"`
	ActualFilamentMM  *float64 `db:"actual_filament_mm" json:"actual_filament_mm,omitempty"`

	// Status
	Status               string  `db:"status" json:"status"`
	ProgressPercent      float64 `db:"progress_percent" json:"progress_percent"`
	CurrentLayer         int     `db:"current_layer" json:"current_layer"`
	TimeElapsedSeconds   int     `db:"time_elapsed_seconds" json:"time_elapsed_seconds"`
	TimeRemainingSeconds *int    `db:"time_remaining_seconds" json:"time_remaining_seconds,omitempty"`

	// Timestamps
	QueuedAt    time.Time  `db:"queued_at" json:"queued_at"`
	StartedAt   *time.Time `db:"started_at" json:"started_at,omitempty"`
	PausedAt    *time.Time `db:"paused_at" json:"paused_at,omitempty"`
	ResumedAt   *time.Time `db:"resumed_at" json:"resumed_at,omitempty"`
	CompletedAt *time.Time `db:"completed_at" json:"completed_at,omitempty"`
	CancelledAt *time.Time `db:"cancelled_at" json:"cancelled_at,omitempty"`
	FailedAt    *time.Time `db:"failed_at" json:"failed_at,omitempty"`

	// Error info
	ErrorMessage *string `db:"error_message" json:"error_message,omitempty"`
	ErrorCode    *string `db:"error_code" json:"error_code,omitempty"`

	// Multi-tool
	ToolType *string `db:"tool_type" json:"tool_type,omitempty"`

	// Laser/CNC specific
	LaserPowerPercent *int     `db:"laser_power_percent" json:"laser_power_percent,omitempty"`
	SpindleSpeedRPM   *int     `db:"spindle_speed_rpm" json:"spindle_speed_rpm,omitempty"`
	FeedRateMMMin     *float64 `db:"feed_rate_mm_min" json:"feed_rate_mm_min,omitempty"`
	PassCount         int      `db:"pass_count" json:"pass_count"`
	CurrentPass       int      `db:"current_pass" json:"current_pass"`

	// Quality
	QualityScore    *float64 `db:"quality_score" json:"quality_score,omitempty"`
	DefectsDetected int      `db:"defects_detected" json:"defects_detected"`

	// References
	OrderID *uuid.UUID `db:"order_id" json:"order_id,omitempty"`
	TaskID  *uuid.UUID `db:"task_id" json:"task_id,omitempty"`

	// Metadata
	Metadata     json.RawMessage `db:"metadata" json:"metadata"`
	ThumbnailURL *string         `db:"thumbnail_url" json:"thumbnail_url,omitempty"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// MaterialProfile represents a material configuration
type MaterialProfile struct {
	ID           uuid.UUID `db:"id" json:"id"`
	TenantID     uuid.UUID `db:"tenant_id" json:"tenant_id"`
	Name         string    `db:"name" json:"name"`
	MaterialType string    `db:"material_type" json:"material_type"`
	Manufacturer *string   `db:"manufacturer" json:"manufacturer,omitempty"`
	Color        *string   `db:"color" json:"color,omitempty"`

	// Temperatures
	NozzleTemp  *int `db:"nozzle_temp" json:"nozzle_temp,omitempty"`
	BedTemp     *int `db:"bed_temp" json:"bed_temp,omitempty"`
	ChamberTemp *int `db:"chamber_temp" json:"chamber_temp,omitempty"`

	// Print settings
	PrintSpeed      *int     `db:"print_speed" json:"print_speed,omitempty"`
	RetractDistance *float64 `db:"retract_distance" json:"retract_distance,omitempty"`
	RetractSpeed    *int     `db:"retract_speed" json:"retract_speed,omitempty"`

	// Properties
	Density   *float64 `db:"density" json:"density,omitempty"`
	Diameter  float64  `db:"diameter" json:"diameter"`
	CostPerKg *float64 `db:"cost_per_kg" json:"cost_per_kg,omitempty"`

	Notes *string `db:"notes" json:"notes,omitempty"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// PrinterRepository handles database operations for printers
type PrinterRepository struct {
	db *sqlx.DB
}

// NewPrinterRepository creates a new printer repository
func NewPrinterRepository(db *sqlx.DB) *PrinterRepository {
	return &PrinterRepository{db: db}
}

// CreateProfile creates a new printer profile
func (r *PrinterRepository) CreateProfile(ctx context.Context, profile *PrinterProfile) error {
	query := `
		INSERT INTO printer_profiles (
			tenant_id, name, description, printer_type, manufacturer, model,
			build_volume_x, build_volume_y, build_volume_z,
			heated_bed, heated_chamber, auto_leveling, filament_sensor, power_recovery,
			has_3d_printing, has_laser, has_cnc, has_pen_plotter,
			min_nozzle_temp, max_nozzle_temp, min_bed_temp, max_bed_temp,
			max_print_speed, max_travel_speed, max_z_speed,
			gcode_flavor, start_gcode, end_gcode, layer_change_gcode
		) VALUES (
			:tenant_id, :name, :description, :printer_type, :manufacturer, :model,
			:build_volume_x, :build_volume_y, :build_volume_z,
			:heated_bed, :heated_chamber, :auto_leveling, :filament_sensor, :power_recovery,
			:has_3d_printing, :has_laser, :has_cnc, :has_pen_plotter,
			:min_nozzle_temp, :max_nozzle_temp, :min_bed_temp, :max_bed_temp,
			:max_print_speed, :max_travel_speed, :max_z_speed,
			:gcode_flavor, :start_gcode, :end_gcode, :layer_change_gcode
		) RETURNING id, created_at, updated_at`

	rows, err := r.db.NamedQueryContext(ctx, query, profile)
	if err != nil {
		return errors.Wrap(err, "failed to create printer profile")
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&profile.ID, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
			return errors.Wrap(err, "failed to scan created profile")
		}
	}

	return nil
}

// GetProfile retrieves a printer profile by ID
func (r *PrinterRepository) GetProfile(ctx context.Context, tenantID, profileID uuid.UUID) (*PrinterProfile, error) {
	var profile PrinterProfile
	query := `
		SELECT * FROM printer_profiles
		WHERE tenant_id = $1 AND id = $2`

	if err := r.db.GetContext(ctx, &profile, query, tenantID, profileID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, errors.Wrap(err, "failed to get printer profile")
	}

	return &profile, nil
}

// ListProfiles lists all printer profiles for a tenant
func (r *PrinterRepository) ListProfiles(ctx context.Context, tenantID uuid.UUID) ([]*PrinterProfile, error) {
	var profiles []*PrinterProfile
	query := `
		SELECT * FROM printer_profiles
		WHERE tenant_id = $1
		ORDER BY name`

	if err := r.db.SelectContext(ctx, &profiles, query, tenantID); err != nil {
		return nil, errors.Wrap(err, "failed to list printer profiles")
	}

	return profiles, nil
}

// CreateConnection creates a new printer connection
func (r *PrinterRepository) CreateConnection(ctx context.Context, conn *PrinterConnection) error {
	query := `
		INSERT INTO printer_connections (
			tenant_id, machine_id, discovered_machine_id, profile_id,
			name, connection_type, connection_url, serial_port, baud_rate, api_key,
			is_active, is_connected, current_state
		) VALUES (
			:tenant_id, :machine_id, :discovered_machine_id, :profile_id,
			:name, :connection_type, :connection_url, :serial_port, :baud_rate, :api_key,
			:is_active, :is_connected, :current_state
		) RETURNING id, created_at, updated_at`

	rows, err := r.db.NamedQueryContext(ctx, query, conn)
	if err != nil {
		return errors.Wrap(err, "failed to create printer connection")
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&conn.ID, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
			return errors.Wrap(err, "failed to scan created connection")
		}
	}

	return nil
}

// GetConnection retrieves a printer connection by ID
func (r *PrinterRepository) GetConnection(ctx context.Context, tenantID, connectionID uuid.UUID) (*PrinterConnection, error) {
	var conn PrinterConnection
	query := `
		SELECT * FROM printer_connections
		WHERE tenant_id = $1 AND id = $2`

	if err := r.db.GetContext(ctx, &conn, query, tenantID, connectionID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, errors.Wrap(err, "failed to get printer connection")
	}

	return &conn, nil
}

// ListConnections lists all printer connections for a tenant
func (r *PrinterRepository) ListConnections(ctx context.Context, tenantID uuid.UUID) ([]*PrinterConnection, error) {
	var connections []*PrinterConnection
	query := `
		SELECT * FROM printer_connections
		WHERE tenant_id = $1
		ORDER BY name`

	if err := r.db.SelectContext(ctx, &connections, query, tenantID); err != nil {
		return nil, errors.Wrap(err, "failed to list printer connections")
	}

	return connections, nil
}

// UpdateConnectionState updates the state of a printer connection
func (r *PrinterRepository) UpdateConnectionState(ctx context.Context, tenantID, connectionID uuid.UUID, state string, connected bool) error {
	query := `
		UPDATE printer_connections
		SET current_state = $3, is_connected = $4, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	result, err := r.db.ExecContext(ctx, query, tenantID, connectionID, state, connected)
	if err != nil {
		return errors.Wrap(err, "failed to update connection state")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}

	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateConnectionTemperatures updates temperature readings for a connection
func (r *PrinterRepository) UpdateConnectionTemperatures(ctx context.Context, tenantID, connectionID uuid.UUID, temps map[string]float64) error {
	query := `
		UPDATE printer_connections
		SET nozzle_temp_current = $3,
		    nozzle_temp_target = $4,
		    bed_temp_current = $5,
		    bed_temp_target = $6,
		    chamber_temp_current = $7,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query,
		tenantID, connectionID,
		temps["nozzle_current"],
		temps["nozzle_target"],
		temps["bed_current"],
		temps["bed_target"],
		temps["chamber_current"],
	)

	return errors.Wrap(err, "failed to update temperatures")
}

// UpdateConnectionPosition updates position for a connection
func (r *PrinterRepository) UpdateConnectionPosition(ctx context.Context, tenantID, connectionID uuid.UUID, x, y, z, e float64) error {
	query := `
		UPDATE printer_connections
		SET position_x = $3,
		    position_y = $4,
		    position_z = $5,
		    position_e = $6,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, connectionID, x, y, z, e)
	return errors.Wrap(err, "failed to update position")
}

// CreatePrintJob creates a new print job
func (r *PrinterRepository) CreatePrintJob(ctx context.Context, job *PrintJob) error {
	query := `
		INSERT INTO print_jobs (
			tenant_id, connection_id, profile_id, material_id,
			job_name, file_name, file_size_bytes, source,
			layer_count, estimated_time_seconds, estimated_filament_mm, estimated_filament_g,
			status, tool_type, order_id, task_id, metadata
		) VALUES (
			:tenant_id, :connection_id, :profile_id, :material_id,
			:job_name, :file_name, :file_size_bytes, :source,
			:layer_count, :estimated_time_seconds, :estimated_filament_mm, :estimated_filament_g,
			:status, :tool_type, :order_id, :task_id, :metadata
		) RETURNING id, created_at, updated_at`

	rows, err := r.db.NamedQueryContext(ctx, query, job)
	if err != nil {
		return errors.Wrap(err, "failed to create print job")
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return errors.Wrap(err, "failed to scan created job")
		}
	}

	return nil
}

// GetPrintJob retrieves a print job by ID
func (r *PrinterRepository) GetPrintJob(ctx context.Context, tenantID, jobID uuid.UUID) (*PrintJob, error) {
	var job PrintJob
	query := `
		SELECT * FROM print_jobs
		WHERE tenant_id = $1 AND id = $2`

	if err := r.db.GetContext(ctx, &job, query, tenantID, jobID); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, errors.Wrap(err, "failed to get print job")
	}

	return &job, nil
}

// UpdatePrintJobStatus updates the status and progress of a print job
func (r *PrinterRepository) UpdatePrintJobStatus(ctx context.Context, tenantID, jobID uuid.UUID, status string, progress float64) error {
	query := `
		UPDATE print_jobs
		SET status = $3, progress_percent = $4, updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, jobID, status, progress)
	return errors.Wrap(err, "failed to update job status")
}

// UpdatePrintJobProgress updates detailed progress of a print job
func (r *PrinterRepository) UpdatePrintJobProgress(ctx context.Context, tenantID, jobID uuid.UUID, progress float64, currentLayer, timeElapsed, timeRemaining int) error {
	query := `
		UPDATE print_jobs
		SET progress_percent = $3,
		    current_layer = $4,
		    time_elapsed_seconds = $5,
		    time_remaining_seconds = $6,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, jobID, progress, currentLayer, timeElapsed, timeRemaining)
	return errors.Wrap(err, "failed to update job progress")
}

// StartPrintJob marks a job as started
func (r *PrinterRepository) StartPrintJob(ctx context.Context, tenantID, jobID uuid.UUID) error {
	query := `
		UPDATE print_jobs
		SET status = 'printing',
		    started_at = NOW(),
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, jobID)
	return errors.Wrap(err, "failed to start print job")
}

// CompletePrintJob marks a job as completed
func (r *PrinterRepository) CompletePrintJob(ctx context.Context, tenantID, jobID uuid.UUID, actualTime int, actualFilament float64) error {
	query := `
		UPDATE print_jobs
		SET status = 'completed',
		    progress_percent = 100,
		    completed_at = NOW(),
		    actual_time_seconds = $3,
		    actual_filament_mm = $4,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, jobID, actualTime, actualFilament)
	return errors.Wrap(err, "failed to complete print job")
}

// FailPrintJob marks a job as failed
func (r *PrinterRepository) FailPrintJob(ctx context.Context, tenantID, jobID uuid.UUID, errorMessage, errorCode string) error {
	query := `
		UPDATE print_jobs
		SET status = 'failed',
		    failed_at = NOW(),
		    error_message = $3,
		    error_code = $4,
		    updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`

	_, err := r.db.ExecContext(ctx, query, tenantID, jobID, errorMessage, errorCode)
	return errors.Wrap(err, "failed to fail print job")
}

// ListPrintJobs lists print jobs with optional filtering
func (r *PrinterRepository) ListPrintJobs(ctx context.Context, tenantID uuid.UUID, connectionID *uuid.UUID, status *string, limit int) ([]*PrintJob, error) {
	var jobs []*PrintJob
	var args []interface{}
	args = append(args, tenantID)
	argCount := 1

	query := `SELECT * FROM print_jobs WHERE tenant_id = $1`

	if connectionID != nil {
		argCount++
		query += fmt.Sprintf(" AND connection_id = $%d", argCount)
		args = append(args, *connectionID)
	}

	if status != nil {
		argCount++
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, *status)
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		argCount++
		query += fmt.Sprintf(" LIMIT $%d", argCount)
		args = append(args, limit)
	}

	if err := r.db.SelectContext(ctx, &jobs, query, args...); err != nil {
		return nil, errors.Wrap(err, "failed to list print jobs")
	}

	return jobs, nil
}
