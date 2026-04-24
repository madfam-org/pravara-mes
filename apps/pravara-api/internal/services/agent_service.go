// Package services provides business logic services for PravaraMES.
package services

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// AgentStatus represents the availability status of an agent.
type AgentStatus string

const (
	AgentStatusOffline     AgentStatus = "offline"
	AgentStatusAvailable   AgentStatus = "available"
	AgentStatusBusy        AgentStatus = "busy"
	AgentStatusBreak       AgentStatus = "break"
	AgentStatusMaintenance AgentStatus = "maintenance"
)

// AgentType represents the type of agent.
type AgentType string

const (
	AgentTypeHuman  AgentType = "human"
	AgentTypeBot    AgentType = "bot"
	AgentTypeHybrid AgentType = "hybrid"
)

// Agent represents a human operator or automated agent.
type Agent struct {
	ID                 uuid.UUID              `json:"id"`
	TenantID           uuid.UUID              `json:"tenant_id"`
	UserID             *uuid.UUID             `json:"user_id,omitempty"`
	Code               string                 `json:"code"`
	Name               string                 `json:"name"`
	Type               AgentType              `json:"type"`
	Status             AgentStatus            `json:"status"`
	Skills             []string               `json:"skills"`
	Certifications     []Certification        `json:"certifications"`
	ExperienceLevel    string                 `json:"experience_level,omitempty"`
	ShiftPattern       string                 `json:"shift_pattern,omitempty"`
	AvailableFrom      *time.Time             `json:"available_from,omitempty"`
	AvailableUntil     *time.Time             `json:"available_until,omitempty"`
	MaxConcurrentTasks int                    `json:"max_concurrent_tasks"`
	CurrentTaskCount   int                    `json:"current_task_count"`
	TasksCompleted     int                    `json:"tasks_completed"`
	TasksFailed        int                    `json:"tasks_failed"`
	AvgTaskDurationMin int                    `json:"avg_task_duration_minutes,omitempty"`
	QualityScore       float64                `json:"quality_score,omitempty"`
	ReliabilityScore   float64                `json:"reliability_score,omitempty"`
	PreferredMachines  []uuid.UUID            `json:"preferred_machines,omitempty"`
	BlockedMachines    []uuid.UUID            `json:"blocked_machines,omitempty"`
	NotificationPrefs  map[string]bool        `json:"notification_preferences,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	LastActivityAt     *time.Time             `json:"last_activity_at,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

// Certification represents an agent's certification.
type Certification struct {
	Name      string    `json:"name"`
	Authority string    `json:"authority,omitempty"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AgentMachineAuth represents agent authorization for a machine.
type AgentMachineAuth struct {
	ID                   uuid.UUID  `json:"id"`
	AgentID              uuid.UUID  `json:"agent_id"`
	MachineID            uuid.UUID  `json:"machine_id"`
	CanOperate           bool       `json:"can_operate"`
	CanMaintain          bool       `json:"can_maintain"`
	CanConfigure         bool       `json:"can_configure"`
	CanEmergencyStop     bool       `json:"can_emergency_stop"`
	ProficiencyLevel     string     `json:"proficiency_level,omitempty"`
	TrainingCompletedAt  *time.Time `json:"training_completed_at,omitempty"`
	CertificationExpires *time.Time `json:"certification_expires_at,omitempty"`
	HoursOperated        int        `json:"hours_operated"`
	RequiresSupervisor   bool       `json:"requires_supervisor"`
	RestrictedOperations []string   `json:"restricted_operations,omitempty"`
}

// AssignmentRequest represents a task assignment request.
type AssignmentRequest struct {
	TaskID            uuid.UUID   `json:"task_id"`
	RequiredSkills    []string    `json:"required_skills"`
	MachineID         *uuid.UUID  `json:"machine_id,omitempty"`
	Priority          int         `json:"priority"`
	EstimatedMinutes  int         `json:"estimated_minutes"`
	PreferredAgents   []uuid.UUID `json:"preferred_agents,omitempty"`
	RequiresExpertise string      `json:"requires_expertise,omitempty"`
}

// AssignmentScore represents an agent's suitability score for a task.
type AssignmentScore struct {
	Agent             *Agent   `json:"agent"`
	Score             float64  `json:"score"`
	SkillMatch        float64  `json:"skill_match"`
	AvailabilityScore float64  `json:"availability_score"`
	ProficiencyScore  float64  `json:"proficiency_score"`
	WorkloadScore     float64  `json:"workload_score"`
	ReliabilityScore  float64  `json:"reliability_score"`
	Reasons           []string `json:"reasons"`
	Warnings          []string `json:"warnings,omitempty"`
}

// AgentService manages agents and task assignments.
type AgentService struct {
	db        *sql.DB
	taskRepo  *repositories.TaskRepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewAgentService creates a new agent service.
func NewAgentService(
	db *sql.DB,
	taskRepo *repositories.TaskRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *AgentService {
	return &AgentService{
		db:        db,
		taskRepo:  taskRepo,
		publisher: publisher,
		log:       log,
	}
}

// FindBestAgent finds the most suitable agent for a task.
func (s *AgentService) FindBestAgent(ctx context.Context, req AssignmentRequest) (*AssignmentScore, error) {
	scores, err := s.ScoreAgents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to score agents: %w", err)
	}

	if len(scores) == 0 {
		return nil, fmt.Errorf("no suitable agents found")
	}

	// Sort by score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	return &scores[0], nil
}

// ScoreAgents scores all eligible agents for a task assignment.
func (s *AgentService) ScoreAgents(ctx context.Context, req AssignmentRequest) ([]AssignmentScore, error) {
	agents, err := s.GetAvailableAgents(ctx, req.RequiredSkills)
	if err != nil {
		return nil, err
	}

	var scores []AssignmentScore

	for _, agent := range agents {
		score := s.calculateAgentScore(agent, req)
		if score.Score > 0 {
			scores = append(scores, score)
		}
	}

	return scores, nil
}

// calculateAgentScore calculates how suitable an agent is for a task.
func (s *AgentService) calculateAgentScore(agent *Agent, req AssignmentRequest) AssignmentScore {
	score := AssignmentScore{
		Agent:   agent,
		Reasons: []string{},
	}

	// 1. Skill match (40% weight)
	if len(req.RequiredSkills) > 0 {
		matchedSkills := 0
		for _, required := range req.RequiredSkills {
			for _, skill := range agent.Skills {
				if skill == required {
					matchedSkills++
					break
				}
			}
		}
		score.SkillMatch = float64(matchedSkills) / float64(len(req.RequiredSkills))
		if score.SkillMatch == 1.0 {
			score.Reasons = append(score.Reasons, "Perfect skill match")
		} else if score.SkillMatch > 0 {
			score.Reasons = append(score.Reasons, fmt.Sprintf("Partial skill match (%.0f%%)", score.SkillMatch*100))
		}
	} else {
		score.SkillMatch = 1.0 // No specific skills required
	}

	// 2. Availability (30% weight)
	if agent.Status == AgentStatusAvailable {
		score.AvailabilityScore = 1.0
		score.Reasons = append(score.Reasons, "Currently available")
	} else if agent.Status == AgentStatusBusy {
		// Check if will be available soon
		if agent.CurrentTaskCount < agent.MaxConcurrentTasks {
			score.AvailabilityScore = 0.5
			score.Reasons = append(score.Reasons, "Busy but has capacity")
		} else {
			score.AvailabilityScore = 0.1
			score.Warnings = append(score.Warnings, "At maximum capacity")
		}
	} else {
		score.AvailabilityScore = 0
		score.Warnings = append(score.Warnings, fmt.Sprintf("Agent status: %s", agent.Status))
		return score // Not available at all
	}

	// 3. Machine proficiency (20% weight)
	score.ProficiencyScore = 0.5 // Default if no machine specified
	if req.MachineID != nil {
		// Check if agent is authorized for this machine
		for _, blocked := range agent.BlockedMachines {
			if blocked == *req.MachineID {
				score.ProficiencyScore = 0
				score.Warnings = append(score.Warnings, "Agent blocked from this machine")
				return score
			}
		}

		for _, preferred := range agent.PreferredMachines {
			if preferred == *req.MachineID {
				score.ProficiencyScore = 1.0
				score.Reasons = append(score.Reasons, "Prefers this machine")
				break
			}
		}
	}

	// 4. Workload balance (5% weight)
	if agent.CurrentTaskCount == 0 {
		score.WorkloadScore = 1.0
		score.Reasons = append(score.Reasons, "No current tasks")
	} else {
		score.WorkloadScore = 1.0 - (float64(agent.CurrentTaskCount) / float64(agent.MaxConcurrentTasks))
		if score.WorkloadScore < 0.5 {
			score.Warnings = append(score.Warnings, fmt.Sprintf("High workload (%d/%d tasks)",
				agent.CurrentTaskCount, agent.MaxConcurrentTasks))
		}
	}

	// 5. Reliability (5% weight)
	if agent.ReliabilityScore > 0 {
		score.ReliabilityScore = agent.ReliabilityScore
		if agent.ReliabilityScore > 0.9 {
			score.Reasons = append(score.Reasons, "Highly reliable")
		} else if agent.ReliabilityScore < 0.7 {
			score.Warnings = append(score.Warnings, "Lower reliability score")
		}
	} else {
		score.ReliabilityScore = 0.8 // Default for new agents
	}

	// Calculate weighted total score
	score.Score = (score.SkillMatch * 0.4) +
		(score.AvailabilityScore * 0.3) +
		(score.ProficiencyScore * 0.2) +
		(score.WorkloadScore * 0.05) +
		(score.ReliabilityScore * 0.05)

	// Apply priority boost for preferred agents
	for _, preferred := range req.PreferredAgents {
		if agent.ID == preferred {
			score.Score *= 1.2 // 20% boost for preferred agents
			score.Reasons = append(score.Reasons, "Preferred agent")
			break
		}
	}

	// Apply expertise requirement penalty
	if req.RequiresExpertise != "" {
		switch agent.ExperienceLevel {
		case "expert":
			score.Score *= 1.1
			score.Reasons = append(score.Reasons, "Expert level")
		case "senior":
			// No change
		case "mid":
			score.Score *= 0.8
			score.Warnings = append(score.Warnings, "May lack required expertise")
		default:
			score.Score *= 0.5
			score.Warnings = append(score.Warnings, "Insufficient experience level")
		}
	}

	return score
}

// GetAvailableAgents retrieves agents that are available for assignment.
func (s *AgentService) GetAvailableAgents(ctx context.Context, requiredSkills []string) ([]*Agent, error) {
	query := `
		SELECT id, tenant_id, user_id, code, name, type, status,
		       skills, certifications, experience_level, shift_pattern,
		       available_from, available_until, max_concurrent_tasks,
		       current_task_count, tasks_completed, tasks_failed,
		       avg_task_duration_minutes, quality_score, reliability_score,
		       preferred_machines, blocked_machines, notification_preferences,
		       metadata, last_activity_at, created_at, updated_at
		FROM agents
		WHERE tenant_id = $1
		  AND status IN ('available', 'busy')
		  AND (available_until IS NULL OR available_until > NOW())
	`

	// Add skill filter if required
	args := []interface{}{ctx.Value("tenant_id")}
	if len(requiredSkills) > 0 {
		query += " AND skills ?| $2"
		args = append(args, requiredSkills)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		agent := &Agent{}
		err := rows.Scan(
			&agent.ID, &agent.TenantID, &agent.UserID, &agent.Code, &agent.Name,
			&agent.Type, &agent.Status, &agent.Skills, &agent.Certifications,
			&agent.ExperienceLevel, &agent.ShiftPattern, &agent.AvailableFrom,
			&agent.AvailableUntil, &agent.MaxConcurrentTasks, &agent.CurrentTaskCount,
			&agent.TasksCompleted, &agent.TasksFailed, &agent.AvgTaskDurationMin,
			&agent.QualityScore, &agent.ReliabilityScore, &agent.PreferredMachines,
			&agent.BlockedMachines, &agent.NotificationPrefs, &agent.Metadata,
			&agent.LastActivityAt, &agent.CreatedAt, &agent.UpdatedAt,
		)
		if err != nil {
			s.log.WithError(err).Error("Failed to scan agent")
			continue
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

// AssignTask assigns a task to an agent.
func (s *AgentService) AssignTask(ctx context.Context, taskID, agentID uuid.UUID, assignedBy *uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create assignment record
	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_assignments (tenant_id, task_id, agent_id, assigned_by, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`, ctx.Value("tenant_id"), taskID, agentID, assignedBy)
	if err != nil {
		return fmt.Errorf("failed to create assignment: %w", err)
	}

	// Update task with assigned agent
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks SET assigned_user_id = $1, updated_at = NOW()
		WHERE id = $2 AND tenant_id = $3
	`, agentID, taskID, ctx.Value("tenant_id"))
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}

	// Increment agent's current task count
	_, err = tx.ExecContext(ctx, `
		UPDATE agents SET current_task_count = current_task_count + 1
		WHERE id = $1 AND tenant_id = $2
	`, agentID, ctx.Value("tenant_id"))
	if err != nil {
		return fmt.Errorf("failed to update agent task count: %w", err)
	}

	// Send notification to agent
	err = s.notifyAgent(ctx, tx, agentID, "task_assigned", taskID)
	if err != nil {
		s.log.WithError(err).Warn("Failed to notify agent")
		// Don't fail the assignment if notification fails
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// notifyAgent sends a notification to an agent.
func (s *AgentService) notifyAgent(ctx context.Context, tx *sql.Tx, agentID uuid.UUID, notificationType string, taskID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO agent_notifications (tenant_id, agent_id, type, priority, title, message, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, ctx.Value("tenant_id"), agentID, notificationType, "normal",
		"New Task Assignment",
		"You have been assigned a new task",
		map[string]interface{}{"task_id": taskID})

	return err
}
