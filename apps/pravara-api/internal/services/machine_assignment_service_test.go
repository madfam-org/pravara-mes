package services

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func quietLog() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	return log
}

// candidateColumns matches MachineRepository.ListAssignmentCandidates.
var candidateColumns = []string{
	"id", "tenant_id", "name", "code", "type", "status",
	"capabilities", "mqtt_topic", "last_heartbeat",
	"created_at", "updated_at", "active_tasks", "last_assigned_at",
}

func candidateRow(rows *sqlmock.Rows, tenantID uuid.UUID, name, status, capsJSON string, activeTasks int, lastAssigned interface{}) *sqlmock.Rows {
	now := time.Now()
	return rows.AddRow(
		uuid.New(), tenantID, name, name, "3d_printer", status,
		[]byte(capsJSON), "madfam/site/area/line/"+name, now,
		now, now, activeTasks, lastAssigned,
	)
}

func TestRequirementsFromSpecifications(t *testing.T) {
	tests := []struct {
		name  string
		specs map[string]any
		want  []string
	}{
		{
			name:  "nil specs express no requirements",
			specs: nil,
			want:  nil,
		},
		{
			name:  "process and material become tokens",
			specs: map[string]any{"process": "3D Printing", "material": "PLA"},
			want:  []string{"3d_printing", "pla"},
		},
		{
			name:  "capabilities array is honored",
			specs: map[string]any{"capabilities": []any{"CNC Milling", "aluminum"}},
			want:  []string{"cnc_milling", "aluminum"},
		},
		{
			name:  "comma-separated capabilities string is honored",
			specs: map[string]any{"capabilities": "laser_cutting, acrylic"},
			want:  []string{"laser_cutting", "acrylic"},
		},
		{
			name:  "duplicates are collapsed after normalization",
			specs: map[string]any{"process": "cnc milling", "capabilities": []any{"CNC_Milling"}},
			want:  []string{"cnc_milling"},
		},
		{
			name:  "unrelated spec keys express no requirements",
			specs: map[string]any{"color": "red", "tolerance_mm": 0.1},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequirementsFromSpecifications(tt.specs)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMachineAssignment_SelectsCapabilityMatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	rows := sqlmock.NewRows(candidateColumns)
	candidateRow(rows, tenantID, "printer-1", "idle", `["3d_printing","pla"]`, 0, nil)
	candidateRow(rows, tenantID, "mill-1", "idle", `["cnc_milling"]`, 0, nil)
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	svc := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())

	result, err := svc.AssignBestMachine(context.Background(), tenantID, []string{"3d_printing", "pla"})
	require.NoError(t, err)
	require.NotNil(t, result.Machine, "a machine with matching capabilities must be assigned")
	assert.Equal(t, "printer-1", result.Machine.Name)
	assert.Equal(t, 2, result.CandidatesEvaluated)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMachineAssignment_NoMatchReturnsNilWithReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	rows := sqlmock.NewRows(candidateColumns)
	candidateRow(rows, tenantID, "printer-1", "idle", `["3d_printing"]`, 0, nil)
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	svc := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())

	result, err := svc.AssignBestMachine(context.Background(), tenantID, []string{"cnc_milling"})
	require.NoError(t, err)
	assert.Nil(t, result.Machine, "no machine satisfies cnc_milling")
	assert.Contains(t, result.Reason, "cnc_milling")
	assert.Equal(t, 1, result.CandidatesEvaluated)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMachineAssignment_EmptyRequirementsLeftForHuman(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// No SQL expectations: an item with no requirements never queries machines.
	svc := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())

	result, err := svc.AssignBestMachine(context.Background(), uuid.New(), nil)
	require.NoError(t, err)
	assert.Nil(t, result.Machine)
	assert.Contains(t, result.Reason, "human routing")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMachineAssignment_ScoringPrefersIdleAndLeastLoaded(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	rows := sqlmock.NewRows(candidateColumns)
	// idle but loaded: 3.0 - 2*0.5 = 2.0
	candidateRow(rows, tenantID, "busy-idle", "idle", `["3d_printing"]`, 2, time.Now())
	// running, free: 1.5
	candidateRow(rows, tenantID, "runner", "running", `["3d_printing"]`, 0, time.Now())
	// idle, free: 3.0 -> winner
	candidateRow(rows, tenantID, "free-idle", "idle", `["3d_printing"]`, 0, time.Now())
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	svc := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())

	result, err := svc.AssignBestMachine(context.Background(), tenantID, []string{"3d_printing"})
	require.NoError(t, err)
	require.NotNil(t, result.Machine)
	assert.Equal(t, "free-idle", result.Machine.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMachineAssignment_TieBreakPrefersLeastRecentlyAssigned(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tenantID := uuid.New()
	rows := sqlmock.NewRows(candidateColumns)
	// Same score (idle, 0 tasks); assigned an hour ago.
	candidateRow(rows, tenantID, "assigned-recently", "idle", `["3d_printing"]`, 0, time.Now())
	// Same score; never assigned -> wins the tie-break.
	candidateRow(rows, tenantID, "never-assigned", "idle", `["3d_printing"]`, 0, nil)
	mock.ExpectQuery("FROM machines m").WithArgs(tenantID).WillReturnRows(rows)

	svc := NewMachineAssignmentService(repositories.NewMachineRepository(db), nil, quietLog())

	result, err := svc.AssignBestMachine(context.Background(), tenantID, []string{"3d_printing"})
	require.NoError(t, err)
	require.NotNil(t, result.Machine)
	assert.Equal(t, "never-assigned", result.Machine.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNormalizeCapabilityToken(t *testing.T) {
	assert.Equal(t, "cnc_milling", normalizeCapabilityToken("  CNC   Milling "))
	assert.Equal(t, "pla", normalizeCapabilityToken("PLA"))
	assert.Equal(t, "", normalizeCapabilityToken("   "))
}
