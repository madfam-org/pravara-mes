package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestMachineRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		filter    MachineFilter
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name: "list all machines",
			filter: MachineFilter{
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "code", "type", "description", "status",
					"mqtt_topic", "location", "specifications", "metadata",
					"last_heartbeat", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "Machine A", "MACH-001", "CNC Mill", "Description A", types.MachineStatusOnline,
						"pravara/machine/001", "Floor 1", []byte("{}"), []byte("{}"),
						time.Now(), time.Now(), time.Now(),
					).
					AddRow(
						uuid.New(), uuid.New(), "Machine B", "MACH-002", "3D Printer", "Description B", types.MachineStatusOffline,
						"pravara/machine/002", "Floor 2", []byte("{}"), []byte("{}"),
						nil, time.Now(), time.Now(),
					)

				mock.ExpectQuery("SELECT id, tenant_id").WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name: "filter by status",
			filter: MachineFilter{
				Status: func() *types.MachineStatus { s := types.MachineStatusOnline; return &s }(),
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND status").
					WithArgs(types.MachineStatusOnline).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "code", "type", "description", "status",
					"mqtt_topic", "location", "specifications", "metadata",
					"last_heartbeat", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "Machine A", "MACH-001", "CNC Mill", "Description A", types.MachineStatusOnline,
						"pravara/machine/001", "Floor 1", []byte("{}"), []byte("{}"),
						time.Now(), time.Now(), time.Now(),
					)

				// Args: status=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND status").
					WithArgs(types.MachineStatusOnline, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "filter by type",
			filter: MachineFilter{
				Type:   func() *string { t := "CNC Mill"; return &t }(),
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND type").
					WithArgs("CNC Mill").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "code", "type", "description", "status",
					"mqtt_topic", "location", "specifications", "metadata",
					"last_heartbeat", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "Machine A", "MACH-001", "CNC Mill", "Description A", types.MachineStatusOnline,
						"pravara/machine/001", "Floor 1", []byte("{}"), []byte("{}"),
						time.Now(), time.Now(), time.Now(),
					)

				// Args: type=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND type").
					WithArgs("CNC Mill", 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "pagination",
			filter: MachineFilter{
				Limit:  5,
				Offset: 10,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(20))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "code", "type", "description", "status",
					"mqtt_topic", "location", "specifications", "metadata",
					"last_heartbeat", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), "Machine K", "MACH-011", "CNC Mill", "Description K", types.MachineStatusOnline,
						"pravara/machine/011", "Floor 1", []byte("{}"), []byte("{}"),
						time.Now(), time.Now(), time.Now(),
					)

				mock.ExpectQuery("SELECT id, tenant_id.*LIMIT.*OFFSET").
					WithArgs(5, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			machines, total, err := repo.List(context.Background(), tt.filter)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(machines) != tt.wantCount {
				t.Errorf("machine count: got %d, want %d", len(machines), tt.wantCount)
			}

			if total < tt.wantCount {
				t.Errorf("total count should be >= machine count, got %d", total)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name        string
		machineID   uuid.UUID
		mockSetup   func(sqlmock.Sqlmock, uuid.UUID)
		wantMachine bool
		wantError   bool
	}{
		{
			name:      "machine found",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				specs := map[string]interface{}{"max_rpm": 5000}
				metadata := map[string]interface{}{"key": "value"}
				specsJSON, _ := json.Marshal(specs)
				metadataJSON, _ := json.Marshal(metadata)

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "code", "type", "description", "status",
					"mqtt_topic", "location", "specifications", "metadata",
					"last_heartbeat", "created_at", "updated_at",
				}).AddRow(
					id, uuid.New(), "Machine A", "MACH-001", "CNC Mill", "Description A", types.MachineStatusOnline,
					"pravara/machine/001", "Floor 1", specsJSON, metadataJSON,
					time.Now(), time.Now(), time.Now(),
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM machines WHERE id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantMachine: true,
			wantError:   false,
		},
		{
			name:      "machine not found",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM machines WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantMachine: false,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.machineID)

			machine, err := repo.GetByID(context.Background(), tt.machineID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantMachine && machine == nil {
				t.Fatal("expected machine, got nil")
			}
			if !tt.wantMachine && machine != nil {
				t.Fatalf("expected nil machine, got %+v", machine)
			}

			if tt.wantMachine && machine.ID != tt.machineID {
				t.Errorf("machine ID: got %v, want %v", machine.ID, tt.machineID)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_GetByCode(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	machineCode := "MACH-001"

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "type", "description", "status",
		"mqtt_topic", "location", "specifications", "metadata",
		"last_heartbeat", "created_at", "updated_at",
	}).AddRow(
		uuid.New(), uuid.New(), "Machine A", machineCode, "CNC Mill", "Description A", types.MachineStatusOnline,
		"pravara/machine/001", "Floor 1", []byte("{}"), []byte("{}"),
		time.Now(), time.Now(), time.Now(),
	)

	mock.ExpectQuery("SELECT id, tenant_id.*FROM machines WHERE code").
		WithArgs(machineCode).
		WillReturnRows(rows)

	machine, err := repo.GetByCode(context.Background(), machineCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if machine == nil {
		t.Fatal("expected machine, got nil")
	}

	if machine.Code != machineCode {
		t.Errorf("machine code: got %q, want %q", machine.Code, machineCode)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestMachineRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		machine   *types.Machine
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create machine success",
			machine: &types.Machine{
				TenantID:    uuid.New(),
				Name:        "Machine A",
				Code:        "MACH-001",
				Type:        "CNC Mill",
				Description: "Description A",
				Status:      types.MachineStatusOnline,
				MQTTTopic:   "pravara/machine/001",
				Location:    "Floor 1",
				Specifications: map[string]interface{}{
					"max_rpm": 5000,
				},
				Metadata: map[string]interface{}{"key": "value"},
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO machines").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create machine with nil ID generates new ID",
			machine: &types.Machine{
				ID:       uuid.Nil,
				TenantID: uuid.New(),
				Name:     "Machine B",
				Code:     "MACH-002",
				Type:     "3D Printer",
				Status:   types.MachineStatusOffline,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO machines").
					WillReturnRows(rows)
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			originalID := tt.machine.ID
			err := repo.Create(context.Background(), tt.machine)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantError {
				if tt.machine.ID == uuid.Nil {
					t.Error("machine ID should be generated if nil")
				}
				if originalID == uuid.Nil && tt.machine.ID == uuid.Nil {
					t.Error("ID was nil and not generated")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_Update(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		machine   *types.Machine
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "update machine success",
			machine: &types.Machine{
				ID:          uuid.New(),
				Name:        "Updated Machine",
				Code:        "MACH-001",
				Type:        "CNC Mill",
				Description: "Updated description",
				Status:      types.MachineStatusMaintenance,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"updated_at"}).
					AddRow(time.Now())

				mock.ExpectQuery("UPDATE machines SET").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "update machine not found",
			machine: &types.Machine{
				ID:     uuid.New(),
				Name:   "Machine",
				Code:   "MACH-999",
				Type:   "Unknown",
				Status: types.MachineStatusOffline,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("UPDATE machines SET").
					WillReturnError(sql.ErrNoRows)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			err := repo.Update(context.Background(), tt.machine)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_UpdateStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		machineID uuid.UUID
		status    types.MachineStatus
		mockSetup func(sqlmock.Sqlmock, uuid.UUID, types.MachineStatus)
		wantError bool
	}{
		{
			name:      "update status success",
			machineID: uuid.New(),
			status:    types.MachineStatusMaintenance,
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID, status types.MachineStatus) {
				mock.ExpectExec("UPDATE machines SET status").
					WithArgs(id, status).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "machine not found",
			machineID: uuid.New(),
			status:    types.MachineStatusOffline,
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID, status types.MachineStatus) {
				mock.ExpectExec("UPDATE machines SET status").
					WithArgs(id, status).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.machineID, tt.status)

			err := repo.UpdateStatus(context.Background(), tt.machineID, tt.status)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_UpdateHeartbeat(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		machineID uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:      "update heartbeat success",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE machines SET last_heartbeat").
					WithArgs(id, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "machine not found",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE machines SET last_heartbeat").
					WithArgs(id, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.machineID)

			err := repo.UpdateHeartbeat(context.Background(), tt.machineID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_Delete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	tests := []struct {
		name      string
		machineID uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:      "delete machine success",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM machines WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "machine not found",
			machineID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM machines WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.machineID)

			err := repo.Delete(context.Background(), tt.machineID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestMachineRepository_GetOfflineMachines(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewMachineRepository(db)

	threshold := 5 * time.Minute

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "name", "code", "type", "description", "status",
		"mqtt_topic", "location", "specifications", "metadata",
		"last_heartbeat", "created_at", "updated_at",
	}).
		AddRow(
			uuid.New(), uuid.New(), "Machine A", "MACH-001", "CNC Mill", "Description A", types.MachineStatusOnline,
			"pravara/machine/001", "Floor 1", []byte("{}"), []byte("{}"),
			time.Now().Add(-10*time.Minute), time.Now(), time.Now(),
		).
		AddRow(
			uuid.New(), uuid.New(), "Machine B", "MACH-002", "3D Printer", "Description B", types.MachineStatusOnline,
			"pravara/machine/002", "Floor 2", []byte("{}"), []byte("{}"),
			nil, time.Now(), time.Now(),
		)

	mock.ExpectQuery("SELECT id, tenant_id.*FROM machines.*WHERE status = 'online'").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	machines, err := repo.GetOfflineMachines(context.Background(), threshold)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(machines) != 2 {
		t.Errorf("expected 2 offline machines, got %d", len(machines))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}
