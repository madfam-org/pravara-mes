package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestTaskCommandRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	tests := []struct {
		name      string
		cmd       *TaskCommand
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create command success",
			cmd: &TaskCommand{
				TenantID:    uuid.New(),
				TaskID:      uuid.New(),
				MachineID:   uuid.New(),
				CommandID:   uuid.New(),
				CommandType: "start_job",
				Status:      "pending",
				Parameters:  map[string]interface{}{"task_title": "Test Task"},
				IssuedAt:    time.Now().UTC(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO task_commands").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create command with nil ID generates new ID",
			cmd: &TaskCommand{
				ID:          uuid.Nil,
				TenantID:    uuid.New(),
				TaskID:      uuid.New(),
				MachineID:   uuid.New(),
				CommandID:   uuid.New(),
				CommandType: "start_job",
				Status:      "pending",
				IssuedAt:    time.Now().UTC(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO task_commands").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create command with nil parameters",
			cmd: &TaskCommand{
				TenantID:    uuid.New(),
				TaskID:      uuid.New(),
				MachineID:   uuid.New(),
				CommandID:   uuid.New(),
				CommandType: "stop",
				Status:      "pending",
				Parameters:  nil,
				IssuedAt:    time.Now().UTC(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO task_commands").
					WillReturnRows(rows)
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			originalID := tt.cmd.ID
			err := repo.Create(context.Background(), tt.cmd)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantError {
				if tt.cmd.ID == uuid.Nil {
					t.Error("command ID should be generated if nil")
				}
				if originalID == uuid.Nil && tt.cmd.ID == uuid.Nil {
					t.Error("ID was nil and not generated")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskCommandRepository_UpdateStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	tests := []struct {
		name      string
		commandID uuid.UUID
		status    string
		errorMsg  string
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:      "update status to sent",
			commandID: uuid.New(),
			status:    "sent",
			errorMsg:  "",
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE task_commands").
					WithArgs(id, "sent", "").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "update status to acknowledged",
			commandID: uuid.New(),
			status:    "acknowledged",
			errorMsg:  "",
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE task_commands").
					WithArgs(id, "acknowledged", "").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "update status to completed",
			commandID: uuid.New(),
			status:    "completed",
			errorMsg:  "",
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE task_commands").
					WithArgs(id, "completed", "").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "update status to failed with error message",
			commandID: uuid.New(),
			status:    "failed",
			errorMsg:  "Machine timeout",
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE task_commands").
					WithArgs(id, "failed", "Machine timeout").
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:      "command not found",
			commandID: uuid.New(),
			status:    "completed",
			errorMsg:  "",
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE task_commands").
					WithArgs(id, "completed", "").
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.commandID)

			err := repo.UpdateStatus(context.Background(), tt.commandID, tt.status, tt.errorMsg)

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

func TestTaskCommandRepository_GetByCommandID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	tests := []struct {
		name      string
		commandID uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantCmd   bool
		wantError bool
	}{
		{
			name:      "command found",
			commandID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "task_id", "machine_id", "command_id",
					"command_type", "status", "parameters", "issued_by", "issued_at",
					"acked_at", "completed_at", "error_message", "created_at", "updated_at",
				}).AddRow(
					uuid.New(), uuid.New(), uuid.New(), uuid.New(), id,
					"start_job", "completed", []byte(`{"task_title":"Test"}`), uuid.New().String(), time.Now(),
					time.Now(), time.Now(), nil, time.Now(), time.Now(),
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE command_id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantCmd:   true,
			wantError: false,
		},
		{
			name:      "command not found",
			commandID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE command_id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantCmd:   false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.commandID)

			cmd, err := repo.GetByCommandID(context.Background(), tt.commandID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantCmd && cmd == nil {
				t.Fatal("expected command, got nil")
			}
			if !tt.wantCmd && cmd != nil {
				t.Fatalf("expected nil command, got %+v", cmd)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskCommandRepository_GetActiveByTaskID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	taskID := uuid.New()

	tests := []struct {
		name      string
		mockSetup func(sqlmock.Sqlmock)
		wantCmd   bool
		wantError bool
	}{
		{
			name: "active command found",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "task_id", "machine_id", "command_id",
					"command_type", "status", "parameters", "issued_by", "issued_at",
					"acked_at", "completed_at", "error_message", "created_at", "updated_at",
				}).AddRow(
					uuid.New(), uuid.New(), taskID, uuid.New(), uuid.New(),
					"start_job", "sent", []byte(`{}`), nil, time.Now(),
					nil, nil, nil, time.Now(), time.Now(),
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE task_id.*status IN").
					WithArgs(taskID).
					WillReturnRows(rows)
			},
			wantCmd:   true,
			wantError: false,
		},
		{
			name: "no active command",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE task_id.*status IN").
					WithArgs(taskID).
					WillReturnError(sql.ErrNoRows)
			},
			wantCmd:   false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			cmd, err := repo.GetActiveByTaskID(context.Background(), taskID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantCmd && cmd == nil {
				t.Fatal("expected command, got nil")
			}
			if !tt.wantCmd && cmd != nil {
				t.Fatalf("expected nil command, got %+v", cmd)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskCommandRepository_GetByTaskID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	taskID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "task_id", "machine_id", "command_id",
		"command_type", "status", "parameters", "issued_by", "issued_at",
		"acked_at", "completed_at", "error_message", "created_at", "updated_at",
	}).
		AddRow(
			uuid.New(), uuid.New(), taskID, uuid.New(), uuid.New(),
			"start_job", "completed", []byte(`{}`), nil, time.Now(),
			time.Now(), time.Now(), nil, time.Now(), time.Now(),
		).
		AddRow(
			uuid.New(), uuid.New(), taskID, uuid.New(), uuid.New(),
			"start_job", "failed", []byte(`{}`), nil, time.Now(),
			nil, time.Now(), "timeout", time.Now().Add(-time.Hour), time.Now().Add(-time.Hour),
		)

	mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE task_id.*ORDER BY created_at DESC").
		WithArgs(taskID).
		WillReturnRows(rows)

	commands, err := repo.GetByTaskID(context.Background(), taskID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(commands))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTaskCommandRepository_GetActiveByMachineID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskCommandRepository(db)

	machineID := uuid.New()

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "task_id", "machine_id", "command_id",
		"command_type", "status", "parameters", "issued_by", "issued_at",
		"acked_at", "completed_at", "error_message", "created_at", "updated_at",
	}).
		AddRow(
			uuid.New(), uuid.New(), uuid.New(), machineID, uuid.New(),
			"start_job", "sent", []byte(`{}`), nil, time.Now(),
			nil, nil, nil, time.Now(), time.Now(),
		)

	mock.ExpectQuery("SELECT id, tenant_id.*FROM task_commands WHERE machine_id.*status IN").
		WithArgs(machineID).
		WillReturnRows(rows)

	commands, err := repo.GetActiveByMachineID(context.Background(), machineID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(commands))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTaskCommand_Struct(t *testing.T) {
	// Test that TaskCommand struct fields are correctly typed
	now := time.Now()
	userID := uuid.New()
	errorMsg := "test error"

	cmd := TaskCommand{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		TaskID:       uuid.New(),
		MachineID:    uuid.New(),
		CommandID:    uuid.New(),
		CommandType:  "start_job",
		Status:       "pending",
		Parameters:   map[string]interface{}{"key": "value"},
		IssuedBy:     &userID,
		IssuedAt:     now,
		AckedAt:      &now,
		CompletedAt:  &now,
		ErrorMessage: &errorMsg,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if cmd.ID == uuid.Nil {
		t.Error("ID should be set")
	}
	if cmd.CommandType != "start_job" {
		t.Errorf("CommandType = %s, want start_job", cmd.CommandType)
	}
	if cmd.Status != "pending" {
		t.Errorf("Status = %s, want pending", cmd.Status)
	}
	if cmd.Parameters["key"] != "value" {
		t.Error("Parameters not set correctly")
	}
}
