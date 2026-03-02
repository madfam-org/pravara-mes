package services

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func TestAgentService_CalculateAgentScore(t *testing.T) {
	log := logrus.New()
	service := &AgentService{
		log: log,
	}

	agent := &Agent{
		ID:                 uuid.New(),
		Name:               "Test Agent",
		Status:             AgentStatusAvailable,
		Skills:             []string{"cnc_operation", "quality_inspection"},
		MaxConcurrentTasks: 3,
		CurrentTaskCount:   1,
		ReliabilityScore:   0.95,
		PreferredMachines:  []uuid.UUID{},
		BlockedMachines:    []uuid.UUID{},
		ExperienceLevel:    "senior",
	}

	tests := []struct {
		name          string
		req           AssignmentRequest
		expectedScore float64
		minScore      float64
		maxScore      float64
	}{
		{
			name: "perfect skill match",
			req: AssignmentRequest{
				RequiredSkills: []string{"cnc_operation", "quality_inspection"},
			},
			minScore: 0.8,
			maxScore: 1.0,
		},
		{
			name: "partial skill match",
			req: AssignmentRequest{
				RequiredSkills: []string{"cnc_operation", "3d_printing"},
			},
			minScore: 0.4,
			maxScore: 0.7,
		},
		{
			name: "no skill requirements",
			req: AssignmentRequest{
				RequiredSkills: []string{},
			},
			minScore: 0.7,
			maxScore: 1.0,
		},
		{
			name: "with preferred agent boost",
			req: AssignmentRequest{
				RequiredSkills:  []string{"cnc_operation"},
				PreferredAgents: []uuid.UUID{agent.ID},
			},
			minScore: 0.8,
			maxScore: 1.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := service.calculateAgentScore(agent, tt.req)
			assert.Greater(t, score.Score, tt.minScore)
			assert.LessOrEqual(t, score.Score, tt.maxScore)

			// Check that reasons are populated
			assert.NotEmpty(t, score.Reasons)

			// Verify component scores
			assert.GreaterOrEqual(t, score.SkillMatch, 0.0)
			assert.LessOrEqual(t, score.SkillMatch, 1.0)
			assert.GreaterOrEqual(t, score.AvailabilityScore, 0.0)
			assert.LessOrEqual(t, score.AvailabilityScore, 1.0)
		})
	}
}

func TestAgentService_CalculateAgentScore_Unavailable(t *testing.T) {
	log := logrus.New()
	service := &AgentService{
		log: log,
	}

	unavailableAgent := &Agent{
		ID:               uuid.New(),
		Name:             "Unavailable Agent",
		Status:           AgentStatusOffline,
		Skills:           []string{"cnc_operation"},
	}

	req := AssignmentRequest{
		RequiredSkills: []string{"cnc_operation"},
	}

	score := service.calculateAgentScore(unavailableAgent, req)
	assert.Equal(t, 0.0, score.Score)
	assert.Equal(t, 0.0, score.AvailabilityScore)
	assert.NotEmpty(t, score.Warnings)
}

func TestAgentService_CalculateAgentScore_BlockedMachine(t *testing.T) {
	log := logrus.New()
	service := &AgentService{
		log: log,
	}

	machineID := uuid.New()
	agent := &Agent{
		ID:              uuid.New(),
		Name:            "Test Agent",
		Status:          AgentStatusAvailable,
		Skills:          []string{"cnc_operation"},
		BlockedMachines: []uuid.UUID{machineID},
	}

	req := AssignmentRequest{
		RequiredSkills: []string{"cnc_operation"},
		MachineID:      &machineID,
	}

	score := service.calculateAgentScore(agent, req)
	assert.Equal(t, 0.0, score.Score)
	assert.Contains(t, score.Warnings[0], "blocked")
}

func TestAgentService_AssignTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	log := logrus.New()
	taskRepo := repositories.NewTaskRepository(db)

	service := &AgentService{
		db:       db,
		taskRepo: taskRepo,
		log:      log,
	}

	ctx := context.WithValue(context.Background(), "tenant_id", uuid.New())
	taskID := uuid.New()
	agentID := uuid.New()
	assignedBy := uuid.New()

	// Expect transaction begin
	mock.ExpectBegin()

	// Expect assignment creation
	mock.ExpectExec("INSERT INTO agent_assignments").
		WithArgs(sqlmock.AnyArg(), taskID, agentID, &assignedBy).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect task update
	mock.ExpectExec("UPDATE tasks SET assigned_user_id").
		WithArgs(agentID, taskID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect agent task count update
	mock.ExpectExec("UPDATE agents SET current_task_count").
		WithArgs(agentID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect notification insert
	mock.ExpectExec("INSERT INTO agent_notifications").
		WithArgs(
			sqlmock.AnyArg(), // tenant_id
			agentID,
			"task_assigned",
			"normal",
			"New Task Assignment",
			"You have been assigned a new task",
			sqlmock.AnyArg(), // data
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Expect transaction commit
	mock.ExpectCommit()

	err = service.AssignTask(ctx, taskID, agentID, &assignedBy)
	assert.NoError(t, err)

	// Verify all expectations were met
	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestAgentService_GetAvailableAgents(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	log := logrus.New()
	service := &AgentService{
		db:  db,
		log: log,
	}

	ctx := context.WithValue(context.Background(), "tenant_id", uuid.New())

	// Mock query results
	rows := sqlmock.NewRows([]string{
		"id", "tenant_id", "user_id", "code", "name", "type", "status",
		"skills", "certifications", "experience_level", "shift_pattern",
		"available_from", "available_until", "max_concurrent_tasks",
		"current_task_count", "tasks_completed", "tasks_failed",
		"avg_task_duration_minutes", "quality_score", "reliability_score",
		"preferred_machines", "blocked_machines", "notification_preferences",
		"metadata", "last_activity_at", "created_at", "updated_at",
	}).AddRow(
		uuid.New(), uuid.New(), nil, "OP-001", "John Doe", "human", "available",
		`["cnc_operation"]`, `[]`, "senior", "day",
		nil, nil, 3, 1, 100, 2,
		45, 0.95, 0.98,
		`[]`, `[]`, `{}`,
		`{}`, time.Now(), time.Now(), time.Now(),
	)

	mock.ExpectQuery("SELECT .* FROM agents").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	agents, err := service.GetAvailableAgents(ctx, []string{})
	assert.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, "John Doe", agents[0].Name)
	assert.Equal(t, "OP-001", agents[0].Code)
}