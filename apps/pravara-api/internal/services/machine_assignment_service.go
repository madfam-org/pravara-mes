package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// Capability-matching contract
// ============================
//
// Task requirements are derived from the order item's `specifications` JSONB:
//
//	"process":      string  -> one requirement token (e.g. "3d_printing")
//	"material":     string  -> one requirement token (e.g. "pla")
//	"capabilities": array of strings, or a comma-separated string
//	                -> one requirement token per entry
//
// Machine capabilities come from `machines.capabilities` (JSONB array of
// strings, settable via the machine create/update API).
//
// Tokens on both sides are normalized: lower-cased, trimmed, and internal
// whitespace collapsed to underscores ("CNC Milling" == "cnc_milling").
//
// A machine is eligible for a task when its capability set is a superset of
// the task's requirement tokens. Items that express NO requirements are left
// for a human to route — auto-assignment never guesses a machine without at
// least one declared capability requirement (this is physical-operations
// software; an uninformed guess plus a hasty operator equals the wrong job
// on real hardware).
//
// Scoring (transparent, in preference order):
//  1. status weight  — idle 3.0, online 2.5, running 1.5, setup 1.0, offline 0.5
//     (error/maintenance machines are excluded by the repository query,
//     as are machines without an MQTT topic)
//  2. load penalty   — minus 0.5 per active task (backlog/queued/in_progress)
//  3. tie-breakers   — least-recently-assigned first (never-assigned wins),
//     then machine name for determinism.

// machineStatusWeights maps machine status to its assignment score weight.
var machineStatusWeights = map[types.MachineStatus]float64{
	types.MachineStatusIdle:    3.0,
	types.MachineStatusOnline:  2.5,
	types.MachineStatusRunning: 1.5,
	types.MachineStatusSetup:   1.0,
	types.MachineStatusOffline: 0.5,
}

// loadPenaltyPerTask is subtracted from the score for each active task
// already queued on a machine.
const loadPenaltyPerTask = 0.5

// CandidateScore explains how a machine scored for an assignment decision.
type CandidateScore struct {
	Machine        types.Machine `json:"machine"`
	Score          float64       `json:"score"`
	StatusWeight   float64       `json:"status_weight"`
	ActiveTasks    int           `json:"active_tasks"`
	LastAssignedAt *time.Time    `json:"last_assigned_at,omitempty"`
	CapabilityMatch bool         `json:"capability_match"`
}

// AssignmentResult is the outcome of one auto-assignment attempt.
type AssignmentResult struct {
	Machine              *types.Machine // nil when no machine was assigned
	RequiredCapabilities []string
	CandidatesEvaluated  int
	Reason               string // human-readable explanation when Machine is nil
}

// MachineAssignmentService picks the best machine for a task based on the
// capability-matching contract above.
type MachineAssignmentService struct {
	machineRepo *repositories.MachineRepository
	publisher   *pubsub.Publisher
	log         *logrus.Logger
}

// NewMachineAssignmentService creates a new machine assignment service.
// publisher may be nil; assignment then works without emitting events.
func NewMachineAssignmentService(
	machineRepo *repositories.MachineRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *MachineAssignmentService {
	return &MachineAssignmentService{
		machineRepo: machineRepo,
		publisher:   publisher,
		log:         log,
	}
}

// normalizeCapabilityToken lower-cases, trims, and collapses whitespace to
// underscores so "CNC Milling", "cnc milling", and "cnc_milling" all match.
func normalizeCapabilityToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), "_")
}

// RequirementsFromSpecifications extracts normalized capability requirement
// tokens from an order item's specifications per the contract above.
// Returns an empty slice when the item expresses no requirements.
func RequirementsFromSpecifications(specs map[string]any) []string {
	if len(specs) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var reqs []string
	add := func(raw string) {
		token := normalizeCapabilityToken(raw)
		if token != "" && !seen[token] {
			seen[token] = true
			reqs = append(reqs, token)
		}
	}

	if v, ok := specs["process"].(string); ok {
		add(v)
	}
	if v, ok := specs["material"].(string); ok {
		add(v)
	}
	switch v := specs["capabilities"].(type) {
	case []any:
		for _, entry := range v {
			if s, ok := entry.(string); ok {
				add(s)
			}
		}
	case []string:
		for _, s := range v {
			add(s)
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			add(s)
		}
	}

	return reqs
}

// machineSatisfies reports whether the machine's capability set covers every
// required token.
func machineSatisfies(machine types.Machine, requirements []string) bool {
	caps := map[string]bool{}
	for _, c := range machine.Capabilities {
		caps[normalizeCapabilityToken(c)] = true
	}
	for _, req := range requirements {
		if !caps[req] {
			return false
		}
	}
	return true
}

// scoreCandidate computes the transparent assignment score for a candidate.
func scoreCandidate(c repositories.AssignmentCandidate) CandidateScore {
	weight := machineStatusWeights[c.Machine.Status] // unknown statuses score 0
	return CandidateScore{
		Machine:         c.Machine,
		StatusWeight:    weight,
		ActiveTasks:     c.ActiveTasks,
		LastAssignedAt:  c.LastAssignedAt,
		Score:           weight - loadPenaltyPerTask*float64(c.ActiveTasks),
		CapabilityMatch: true,
	}
}

// AssignBestMachine evaluates the tenant's eligible machines against the
// task requirements and returns the best match. When requirements are empty
// or no machine satisfies them, Machine is nil and Reason explains why; the
// caller decides how to surface that (see NotifyAssignmentFailed).
func (s *MachineAssignmentService) AssignBestMachine(
	ctx context.Context,
	tenantID uuid.UUID,
	requirements []string,
) (*AssignmentResult, error) {
	result := &AssignmentResult{RequiredCapabilities: requirements}

	if len(requirements) == 0 {
		result.Reason = "item specifications express no capability requirements; left for human routing"
		return result, nil
	}

	candidates, err := s.machineRepo.ListAssignmentCandidates(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list assignment candidates: %w", err)
	}
	result.CandidatesEvaluated = len(candidates)

	var scored []CandidateScore
	for _, c := range candidates {
		if machineSatisfies(c.Machine, requirements) {
			scored = append(scored, scoreCandidate(c))
		}
	}

	if len(scored) == 0 {
		result.Reason = fmt.Sprintf(
			"no eligible machine satisfies required capabilities %v (evaluated %d)",
			requirements, len(candidates),
		)
		return result, nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Least-recently-assigned first; never-assigned (nil) wins.
		li, lj := scored[i].LastAssignedAt, scored[j].LastAssignedAt
		switch {
		case li == nil && lj != nil:
			return true
		case li != nil && lj == nil:
			return false
		case li != nil && lj != nil && !li.Equal(*lj):
			return li.Before(*lj)
		}
		return scored[i].Machine.Name < scored[j].Machine.Name
	})

	best := scored[0].Machine
	result.Machine = &best

	s.log.WithFields(logrus.Fields{
		"machine_id":   best.ID,
		"machine_name": best.Name,
		"requirements": requirements,
		"candidates":   len(candidates),
		"matched":      len(scored),
		"score":        scored[0].Score,
	}).Info("Auto-assignment selected machine")

	return result, nil
}

// NotifyAssignmentFailed publishes the task.assignment_failed event and a
// UI notification for a task that could not be auto-assigned. Fail visible,
// not silent. No-op when the publisher is nil.
func (s *MachineAssignmentService) NotifyAssignmentFailed(
	ctx context.Context,
	task *types.Task,
	result *AssignmentResult,
) {
	s.log.WithFields(logrus.Fields{
		"task_id":      task.ID,
		"requirements": result.RequiredCapabilities,
		"candidates":   result.CandidatesEvaluated,
		"reason":       result.Reason,
	}).Warn("Auto-assignment found no machine for task")

	if s.publisher == nil {
		return
	}

	requirements := result.RequiredCapabilities
	if requirements == nil {
		requirements = []string{}
	}

	if err := s.publisher.PublishTaskAssignmentFailed(ctx, task.TenantID, pubsub.TaskAssignmentFailedData{
		TaskID:               task.ID,
		TaskTitle:            task.Title,
		OrderID:              task.OrderID,
		OrderItemID:          task.OrderItemID,
		RequiredCapabilities: requirements,
		CandidatesEvaluated:  result.CandidatesEvaluated,
		Reason:               result.Reason,
		FailedAt:             time.Now().UTC(),
	}); err != nil {
		s.log.WithError(err).Warn("Failed to publish task.assignment_failed event")
	}

	if err := s.publisher.PublishNotification(ctx, task.TenantID, pubsub.NotificationData{
		Title:    "Task needs manual assignment",
		Message:  fmt.Sprintf("No machine could be auto-assigned for '%s': %s", task.Title, result.Reason),
		Severity: "warning",
		Source:   "task",
		SourceID: &task.ID,
	}); err != nil {
		s.log.WithError(err).Warn("Failed to publish assignment notification")
	}
}
