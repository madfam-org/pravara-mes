package repositories

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrinterRepository_CreateProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	profileID := uuid.New()
	now := time.Now()

	profile := &PrinterProfile{
		TenantID:      tenantID,
		Name:          "Snapmaker A350",
		PrinterType:   "multi_tool",
		Manufacturer:  stringPtr("Snapmaker"),
		Model:         stringPtr("A350"),
		BuildVolumeX:  float64Ptr(350),
		BuildVolumeY:  float64Ptr(350),
		BuildVolumeZ:  float64Ptr(350),
		Has3DPrinting: true,
		HasLaser:      true,
		HasCNC:        true,
		MaxNozzleTemp: 275,
		MaxBedTemp:    110,
		GCodeFlavor:   "marlin",
	}

	mock.ExpectQuery(`INSERT INTO printer_profiles`).
		WithArgs(
			profile.TenantID,
			profile.Name,
			profile.Description,
			profile.PrinterType,
			profile.Manufacturer,
			profile.Model,
			profile.BuildVolumeX,
			profile.BuildVolumeY,
			profile.BuildVolumeZ,
			profile.HeatedBed,
			profile.HeatedChamber,
			profile.AutoLeveling,
			profile.FilamentSensor,
			profile.PowerRecovery,
			profile.Has3DPrinting,
			profile.HasLaser,
			profile.HasCNC,
			profile.HasPenPlotter,
			profile.MinNozzleTemp,
			profile.MaxNozzleTemp,
			profile.MinBedTemp,
			profile.MaxBedTemp,
			profile.MaxPrintSpeed,
			profile.MaxTravelSpeed,
			profile.MaxZSpeed,
			profile.GCodeFlavor,
			profile.StartGCode,
			profile.EndGCode,
			profile.LayerChangeGCode,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow(profileID, now, now))

	err = repo.CreateProfile(context.Background(), profile)
	assert.NoError(t, err)
	assert.Equal(t, profileID, profile.ID)
	assert.Equal(t, now, profile.CreatedAt)
	assert.Equal(t, now, profile.UpdatedAt)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_GetProfile(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	profileID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "printer_type", "manufacturer", "model",
		"build_volume_x", "build_volume_y", "build_volume_z",
		"heated_bed", "heated_chamber", "auto_leveling", "filament_sensor", "power_recovery",
		"has_3d_printing", "has_laser", "has_cnc", "has_pen_plotter",
		"min_nozzle_temp", "max_nozzle_temp", "min_bed_temp", "max_bed_temp",
		"max_print_speed", "max_travel_speed", "max_z_speed",
		"gcode_flavor", "start_gcode", "end_gcode", "layer_change_gcode",
		"created_at", "updated_at", "description",
	}).AddRow(
		profileID, tenantID, "Test Printer", "fdm", "TestMaker", "Model X",
		200.0, 200.0, 200.0,
		true, false, true, false, true,
		true, false, false, false,
		180, 260, 0, 100,
		150, 300, 10,
		"marlin", nil, nil, nil,
		time.Now(), time.Now(), nil,
	)

	mock.ExpectQuery(`SELECT \* FROM printer_profiles WHERE tenant_id = \$1 AND id = \$2`).
		WithArgs(tenantID, profileID).
		WillReturnRows(rows)

	profile, err := repo.GetProfile(context.Background(), tenantID, profileID)
	assert.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, profileID, profile.ID)
	assert.Equal(t, "Test Printer", profile.Name)
	assert.Equal(t, "fdm", profile.PrinterType)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_CreateConnection(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	connectionID := uuid.New()
	machineID := uuid.New()
	now := time.Now()

	conn := &PrinterConnection{
		TenantID:       tenantID,
		MachineID:      &machineID,
		Name:           "Main Printer",
		ConnectionType: "octoprint",
		ConnectionURL:  stringPtr("http://octopi.local"),
		BaudRate:       115200,
		IsActive:       true,
		IsConnected:    false,
		CurrentState:   "idle",
	}

	mock.ExpectQuery(`INSERT INTO printer_connections`).
		WithArgs(
			conn.TenantID,
			conn.MachineID,
			conn.DiscoveredMachineID,
			conn.ProfileID,
			conn.Name,
			conn.ConnectionType,
			conn.ConnectionURL,
			conn.SerialPort,
			conn.BaudRate,
			conn.APIKey,
			conn.IsActive,
			conn.IsConnected,
			conn.CurrentState,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow(connectionID, now, now))

	err = repo.CreateConnection(context.Background(), conn)
	assert.NoError(t, err)
	assert.Equal(t, connectionID, conn.ID)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_UpdateConnectionState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	connectionID := uuid.New()

	mock.ExpectExec(`UPDATE printer_connections SET current_state = \$3, is_connected = \$4`).
		WithArgs(tenantID, connectionID, "printing", true).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.UpdateConnectionState(context.Background(), tenantID, connectionID, "printing", true)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_CreatePrintJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	jobID := uuid.New()
	connectionID := uuid.New()
	now := time.Now()

	job := &PrintJob{
		TenantID:             tenantID,
		ConnectionID:         &connectionID,
		JobName:              "test-print.gcode",
		FileName:             stringPtr("test-print.gcode"),
		FileSizeBytes:        int64Ptr(1024000),
		LayerCount:           intPtr(100),
		EstimatedTimeSeconds: intPtr(3600),
		EstimatedFilamentMM:  float64Ptr(5000),
		Status:               "pending",
		ToolType:             stringPtr("3d_printing"),
		Metadata:             []byte(`{}`),
	}

	mock.ExpectQuery(`INSERT INTO print_jobs`).
		WithArgs(
			job.TenantID,
			job.ConnectionID,
			job.ProfileID,
			job.MaterialID,
			job.JobName,
			job.FileName,
			job.FileSizeBytes,
			job.Source,
			job.LayerCount,
			job.EstimatedTimeSeconds,
			job.EstimatedFilamentMM,
			job.EstimatedFilamentG,
			job.Status,
			job.ToolType,
			job.OrderID,
			job.TaskID,
			job.Metadata,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow(jobID, now, now))

	err = repo.CreatePrintJob(context.Background(), job)
	assert.NoError(t, err)
	assert.Equal(t, jobID, job.ID)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_UpdatePrintJobProgress(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	jobID := uuid.New()

	mock.ExpectExec(`UPDATE print_jobs SET progress_percent = \$3`).
		WithArgs(tenantID, jobID, 50.5, 50, 1800, 1800).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.UpdatePrintJobProgress(context.Background(), tenantID, jobID, 50.5, 50, 1800, 1800)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_CompletePrintJob(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	jobID := uuid.New()

	mock.ExpectExec(`UPDATE print_jobs SET status = 'completed'`).
		WithArgs(tenantID, jobID, 3700, 5100.5).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = repo.CompletePrintJob(context.Background(), tenantID, jobID, 3700, 5100.5)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPrinterRepository_ListPrintJobs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sqlxDB := sqlx.NewDb(db, "postgres")
	repo := NewPrinterRepository(sqlxDB)

	tenantID := uuid.New()
	connectionID := uuid.New()
	status := "printing"

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "connection_id", "job_name", "status",
		"progress_percent", "current_layer", "time_elapsed_seconds",
		"queued_at", "created_at", "updated_at",
		"profile_id", "material_id", "file_name", "file_size_bytes",
		"source", "layer_count", "estimated_time_seconds", "estimated_filament_mm",
		"estimated_filament_g", "actual_time_seconds", "actual_filament_mm",
		"time_remaining_seconds", "started_at", "paused_at", "resumed_at",
		"completed_at", "cancelled_at", "failed_at", "error_message",
		"error_code", "tool_type", "laser_power_percent", "spindle_speed_rpm",
		"feed_rate_mm_min", "pass_count", "current_pass", "quality_score",
		"defects_detected", "order_id", "task_id", "metadata", "thumbnail_url",
	})

	for i := 0; i < 2; i++ {
		rows.AddRow(
			uuid.New(), tenantID, connectionID, "test.gcode", status,
			50.0, 50, 1800,
			time.Now(), time.Now(), time.Now(),
			nil, nil, nil, nil,
			nil, nil, nil, nil,
			nil, nil, nil,
			nil, nil, nil, nil,
			nil, nil, nil, nil,
			nil, nil, nil, nil,
			nil, 1, 0, nil,
			0, nil, nil, []byte(`{}`), nil,
		)
	}

	mock.ExpectQuery(`SELECT \* FROM print_jobs WHERE tenant_id = \$1 AND connection_id = \$2 AND status = \$3`).
		WithArgs(tenantID, connectionID, status, 10).
		WillReturnRows(rows)

	jobs, err := repo.ListPrintJobs(context.Background(), tenantID, &connectionID, &status, 10)
	assert.NoError(t, err)
	assert.Len(t, jobs, 2)
	assert.Equal(t, "test.gcode", jobs[0].JobName)
	assert.Equal(t, status, jobs[0].Status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func int64Ptr(i int64) *int64 {
	return &i
}

func float64Ptr(f float64) *float64 {
	return &f
}
