package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

func TestTaskRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		filter    TaskFilter
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name: "list all tasks",
			filter: TaskFilter{
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT").
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 1", "Description 1", types.TaskStatusQueued, 3, 60, 0,
						1, nil, nil, []byte("{}"), time.Now(), time.Now(),
					).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 2", "Description 2", types.TaskStatusInProgress, 5, 120, 30,
						2, time.Now(), nil, []byte("{}"), time.Now(), time.Now(),
					)

				mock.ExpectQuery("SELECT id, tenant_id").WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name: "filter by status",
			filter: TaskFilter{
				Status: func() *types.TaskStatus { s := types.TaskStatusInProgress; return &s }(),
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND status").
					WithArgs(types.TaskStatusInProgress).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 1", "Description 1", types.TaskStatusInProgress, 5, 120, 30,
						1, time.Now(), nil, []byte("{}"), time.Now(), time.Now(),
					)

				// Args: status=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND status").
					WithArgs(types.TaskStatusInProgress, 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "filter by machine ID",
			filter: TaskFilter{
				MachineID: func() *uuid.UUID { id := uuid.New(); return &id }(),
				Limit:     10,
				Offset:    0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND machine_id").
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 1", "Description 1", types.TaskStatusQueued, 3, 60, 0,
						1, nil, nil, []byte("{}"), time.Now(), time.Now(),
					)

				// Args: machine_id=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND machine_id").
					WithArgs(sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "filter by order ID",
			filter: TaskFilter{
				OrderID: func() *uuid.UUID { id := uuid.New(); return &id }(),
				Limit:   10,
				Offset:  0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND order_id").
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 1", "Description 1", types.TaskStatusQueued, 3, 60, 0,
						1, nil, nil, []byte("{}"), time.Now(), time.Now(),
					)

				// Args: order_id=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND order_id").
					WithArgs(sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
		{
			name: "filter by user ID",
			filter: TaskFilter{
				UserID: func() *uuid.UUID { id := uuid.New(); return &id }(),
				Limit:  10,
				Offset: 0,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT COUNT.*AND assigned_user_id").
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).
					AddRow(
						uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
						"Task 1", "Description 1", types.TaskStatusQueued, 3, 60, 0,
						1, nil, nil, []byte("{}"), time.Now(), time.Now(),
					)

				// Args: user_id=$1, limit=$2 (offset not added when 0)
				mock.ExpectQuery("SELECT id, tenant_id.*AND assigned_user_id").
					WithArgs(sqlmock.AnyArg(), 10).
					WillReturnRows(rows)
			},
			wantCount: 1,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			tasks, total, err := repo.List(context.Background(), tt.filter)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tasks) != tt.wantCount {
				t.Errorf("task count: got %d, want %d", len(tasks), tt.wantCount)
			}

			if total < tt.wantCount {
				t.Errorf("total count should be >= task count, got %d", total)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskRepository_GetByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		taskID    uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantTask  bool
		wantError bool
	}{
		{
			name:   "task found",
			taskID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				metadata := map[string]interface{}{"key": "value"}
				metadataJSON, _ := json.Marshal(metadata)

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
					"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
					"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
				}).AddRow(
					id, uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
					"Task 1", "Description 1", types.TaskStatusQueued, 3, 60, 0,
					1, nil, nil, metadataJSON, time.Now(), time.Now(),
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM tasks WHERE id").
					WithArgs(id).
					WillReturnRows(rows)
			},
			wantTask:  true,
			wantError: false,
		},
		{
			name:   "task not found",
			taskID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM tasks WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrNoRows)
			},
			wantTask:  false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.taskID)

			task, err := repo.GetByID(context.Background(), tt.taskID)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantTask && task == nil {
				t.Fatal("expected task, got nil")
			}
			if !tt.wantTask && task != nil {
				t.Fatalf("expected nil task, got %+v", task)
			}

			if tt.wantTask && task.ID != tt.taskID {
				t.Errorf("task ID: got %v, want %v", task.ID, tt.taskID)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		task      *types.Task
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create task success",
			task: &types.Task{
				TenantID:         uuid.New(),
				Title:            "New Task",
				Description:      "Task description",
				Status:           types.TaskStatusQueued,
				Priority:         3,
				EstimatedMinutes: 60,
				Metadata:         map[string]interface{}{"key": "value"},
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				// Expect kanban position query
				posRows := sqlmock.NewRows([]string{"max"}).AddRow(5)
				mock.ExpectQuery("SELECT COALESCE").
					WillReturnRows(posRows)

				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO tasks").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create task with nil ID generates new ID",
			task: &types.Task{
				ID:          uuid.Nil,
				TenantID:    uuid.New(),
				Title:       "Task B",
				Status:      types.TaskStatusQueued,
				Priority:    3,
				Description: "Description",
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				posRows := sqlmock.NewRows([]string{"max"}).AddRow(0)
				mock.ExpectQuery("SELECT COALESCE").
					WillReturnRows(posRows)

				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())

				mock.ExpectQuery("INSERT INTO tasks").
					WillReturnRows(rows)
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			originalID := tt.task.ID
			err := repo.Create(context.Background(), tt.task)

			if tt.wantError && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !tt.wantError {
				if tt.task.ID == uuid.Nil {
					t.Error("task ID should be generated if nil")
				}
				if originalID == uuid.Nil && tt.task.ID == uuid.Nil {
					t.Error("ID was nil and not generated")
				}
				if tt.task.KanbanPosition <= 0 {
					t.Error("kanban position should be set")
				}
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unfulfilled expectations: %v", err)
			}
		})
	}
}

func TestTaskRepository_Update(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		task      *types.Task
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "update task success",
			task: &types.Task{
				ID:          uuid.New(),
				Title:       "Updated Task",
				Description: "Updated description",
				Status:      types.TaskStatusInProgress,
				Priority:    5,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"updated_at"}).
					AddRow(time.Now())

				mock.ExpectQuery("UPDATE tasks SET").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "update task not found",
			task: &types.Task{
				ID:     uuid.New(),
				Title:  "Task",
				Status: types.TaskStatusQueued,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("UPDATE tasks SET").
					WillReturnError(sql.ErrNoRows)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			err := repo.Update(context.Background(), tt.task)

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

func TestTaskRepository_AssignTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		taskID    uuid.UUID
		userID    *uuid.UUID
		machineID *uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:      "assign task to user and machine",
			taskID:    uuid.New(),
			userID:    func() *uuid.UUID { id := uuid.New(); return &id }(),
			machineID: func() *uuid.UUID { id := uuid.New(); return &id }(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE tasks SET assigned_user_id").
					WithArgs(id, sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:   "task not found",
			taskID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE tasks SET assigned_user_id").
					WithArgs(id, nil, nil).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.taskID)

			err := repo.AssignTask(context.Background(), tt.taskID, tt.userID, tt.machineID)

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

func TestTaskRepository_Delete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	tests := []struct {
		name      string
		taskID    uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:   "delete task success",
			taskID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM tasks WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:   "task not found",
			taskID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("DELETE FROM tasks WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.taskID)

			err := repo.Delete(context.Background(), tt.taskID)

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

func TestTaskRepository_MoveTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	taskID := uuid.New()
	tenantID := uuid.New()

	// Test moving task to different status
	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))

	// Get current task
	currentRows := sqlmock.NewRows([]string{"tenant_id", "status", "kanban_position"}).
		AddRow(tenantID, types.TaskStatusQueued, 3)
	mock.ExpectQuery("SELECT tenant_id, status, kanban_position FROM tasks WHERE id").
		WithArgs(taskID).
		WillReturnRows(currentRows)
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("0").WillReturnResult(sqlmock.NewResult(0, 0))

	// Shift positions in old column
	mock.ExpectExec("UPDATE tasks SET kanban_position = kanban_position - 1").
		WithArgs(tenantID, types.TaskStatusQueued, 3).
		WillReturnResult(sqlmock.NewResult(0, 2))

	// Shift positions in new column
	mock.ExpectExec("UPDATE tasks SET kanban_position = kanban_position \\+ 1").
		WithArgs(tenantID, types.TaskStatusInProgress, 2).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Update task status and position
	mock.ExpectExec("UPDATE tasks SET status").
		WithArgs(taskID, types.TaskStatusInProgress, 2).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	err = repo.MoveTask(context.Background(), taskID, types.TaskStatusInProgress, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// A move that states the status it leaves is refused, with nothing written,
// when the task is elsewhere (RFC 0064 §4.1, ruling PV-5).
func TestTaskRepository_MoveTaskExpecting_Conflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)
	taskID, tenantID := uuid.New(), uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id, status, kanban_position FROM tasks WHERE id = \$1 AND .* FOR NO KEY UPDATE`).
		WithArgs(taskID).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "status", "kanban_position"}).
			AddRow(tenantID, types.TaskStatusInProgress, 1))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("0").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	expected := types.TaskStatusQueued
	move, err := repo.MoveTaskExpecting(context.Background(), taskID, types.TaskStatusInProgress, nil, &expected)
	var conflict *TaskStatusConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("want a status conflict, got %v", err)
	}
	if conflict.Current != types.TaskStatusInProgress {
		t.Errorf("current status: got %s", conflict.Current)
	}
	if move != nil {
		t.Errorf("a refused move reports no move")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// Without a position the task goes to the end of the target column.
func TestTaskRepository_MoveTaskExpecting_EndOfColumn(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)
	taskID, tenantID := uuid.New(), uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id, status, kanban_position FROM tasks WHERE id`).
		WithArgs(taskID).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "status", "kanban_position"}).
			AddRow(tenantID, types.TaskStatusQueued, 2))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("0").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COALESCE\(MAX\(kanban_position\), 0\) FROM tasks`).
		WithArgs(tenantID, types.TaskStatusInProgress).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(4))
	mock.ExpectExec("UPDATE tasks SET kanban_position = kanban_position - 1").
		WithArgs(tenantID, types.TaskStatusQueued, 2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE tasks SET kanban_position = kanban_position \\+ 1").
		WithArgs(tenantID, types.TaskStatusInProgress, 5).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE tasks SET status").
		WithArgs(taskID, types.TaskStatusInProgress, 5).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	expected := types.TaskStatusQueued
	move, err := repo.MoveTaskExpecting(context.Background(), taskID, types.TaskStatusInProgress, nil, &expected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := TaskMove{OldStatus: types.TaskStatusQueued, OldPosition: 2, NewStatus: types.TaskStatusInProgress, NewPosition: 5}
	if *move != want {
		t.Errorf("move: got %+v, want %+v", *move, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// Without a position, within its own column, the task goes to the last place.
func TestTaskRepository_MoveTaskExpecting_EndOfOwnColumn(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)
	taskID, tenantID := uuid.New(), uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id, status, kanban_position FROM tasks WHERE id`).
		WithArgs(taskID).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "status", "kanban_position"}).
			AddRow(tenantID, types.TaskStatusQueued, 2))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("0").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT COALESCE\(MAX\(kanban_position\), 0\) FROM tasks`).
		WithArgs(tenantID, types.TaskStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(3))
	mock.ExpectExec("UPDATE tasks SET kanban_position = kanban_position - 1").
		WithArgs(tenantID, types.TaskStatusQueued, 2, 3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE tasks SET status").
		WithArgs(taskID, types.TaskStatusQueued, 3).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	move, err := repo.MoveTaskExpecting(context.Background(), taskID, types.TaskStatusQueued, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if move.NewPosition != 3 {
		t.Errorf("new position: got %d, want 3", move.NewPosition)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTaskRepository_MoveTaskExpecting_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)
	taskID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id, status, kanban_position FROM tasks WHERE id`).
		WithArgs(taskID).
		WillReturnRows(sqlmock.NewRows([]string{"tenant_id", "status", "kanban_position"}))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("0").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	if _, err := repo.MoveTaskExpecting(context.Background(), taskID, types.TaskStatusQueued, nil, nil); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
	if ErrTaskNotFound.Error() != "task not found" {
		t.Errorf("ErrTaskNotFound keeps the text existing callers match")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestTaskRepository_GetKanbanBoard(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()

	repo := NewTaskRepository(db)

	mock.ExpectQuery("SELECT COUNT").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "order_id", "order_item_id", "machine_id", "assigned_user_id",
		"title", "description", "status", "priority", "estimated_minutes", "actual_minutes",
		"kanban_position", "started_at", "completed_at", "metadata", "created_at", "updated_at",
	}).
		AddRow(
			uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
			"Task 1", "Description 1", types.TaskStatusBacklog, 3, 60, 0,
			1, nil, nil, []byte("{}"), time.Now(), time.Now(),
		).
		AddRow(
			uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
			"Task 2", "Description 2", types.TaskStatusInProgress, 5, 120, 30,
			1, time.Now(), nil, []byte("{}"), time.Now(), time.Now(),
		).
		AddRow(
			uuid.New(), uuid.New(), uuid.New().String(), uuid.New().String(), uuid.New().String(), uuid.New().String(),
			"Task 3", "Description 3", types.TaskStatusCompleted, 3, 90, 95,
			1, time.Now(), time.Now(), []byte("{}"), time.Now(), time.Now(),
		)

	mock.ExpectQuery("SELECT id, tenant_id").WillReturnRows(rows)

	board, err := repo.GetKanbanBoard(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(board) == 0 {
		t.Fatal("expected board with tasks, got empty")
	}

	// Verify all status columns exist
	expectedStatuses := []types.TaskStatus{
		types.TaskStatusBacklog,
		types.TaskStatusQueued,
		types.TaskStatusInProgress,
		types.TaskStatusQualityCheck,
		types.TaskStatusCompleted,
		types.TaskStatusBlocked,
	}

	for _, status := range expectedStatuses {
		if _, exists := board[status]; !exists {
			t.Errorf("expected status %s in board, not found", status)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// A move that cannot get the board within the lock timeout is refused as
// busy, with nothing written.
func TestTaskRepository_MoveTaskExpecting_BoardBusy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnError(&pq.Error{Code: "55P03", Message: "canceling statement due to lock timeout"})
	mock.ExpectRollback()

	if _, err := repo.MoveTaskExpecting(context.Background(), uuid.New(), types.TaskStatusInProgress, nil, nil); !errors.Is(err, ErrTaskBoardBusy) {
		t.Fatalf("want ErrTaskBoardBusy, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// A failure of the row-lock statement other than the lock timeout is
// returned as it is; lock_timeout is not restored on the aborted savepoint.
func TestTaskRepository_MoveTaskExpecting_RowReadFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}
	defer db.Close()
	repo := NewTaskRepository(db)
	taskID := uuid.New()

	mock.ExpectBegin()
	mock.ExpectQuery(`current_setting\('lock_timeout'\)`).WillReturnRows(sqlmock.NewRows([]string{"lock_timeout"}).AddRow("0"))
	mock.ExpectExec(`set_config\('lock_timeout'`).WithArgs("5000ms").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`SELECT tenant_id, status, kanban_position FROM tasks WHERE id`).WithArgs(taskID).
		WillReturnError(&pq.Error{Code: "40P01", Message: "deadlock detected"})
	mock.ExpectRollback()

	_, err = repo.MoveTaskExpecting(context.Background(), taskID, types.TaskStatusInProgress, nil, nil)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "40P01" || errors.Is(err, ErrTaskBoardBusy) {
		t.Fatalf("want the row read's own error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations (no restore after the failed statement): %v", err)
	}
}
