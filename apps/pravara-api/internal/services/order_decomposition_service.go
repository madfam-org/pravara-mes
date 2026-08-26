package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// OrderDecompositionService turns an order's line items into production
// tasks so orders no longer sit inert until a human creates tasks by hand.
//
// One task is created per order item, in backlog status (the existing task
// vocabulary's entry column), carrying the item's specifications and CAD
// file URL forward in the task metadata. When auto-assignment is enabled,
// each task is offered to the MachineAssignmentService before insert; tasks
// whose requirements no machine satisfies stay unassigned and a
// task.assignment_failed event makes that visible.
//
// Decomposition only creates rows and publishes events. It never dispatches
// a machine command: dispatch still happens exclusively when a task is
// moved to in_progress (AutomationService.OnTaskStatusChange).
type OrderDecompositionService struct {
	taskRepo   *repositories.TaskRepository
	assignment *MachineAssignmentService
	publisher  *pubsub.Publisher
	autoAssign bool
	log        *logrus.Logger
}

// NewOrderDecompositionService creates a new order decomposition service.
// assignment and publisher may be nil; the corresponding steps are skipped.
func NewOrderDecompositionService(
	taskRepo *repositories.TaskRepository,
	assignment *MachineAssignmentService,
	publisher *pubsub.Publisher,
	autoAssign bool,
	log *logrus.Logger,
) *OrderDecompositionService {
	return &OrderDecompositionService{
		taskRepo:   taskRepo,
		assignment: assignment,
		publisher:  publisher,
		autoAssign: autoAssign,
		log:        log,
	}
}

// taskTitleForItem builds the production task title from the item's product
// name and quantity, e.g. "Bracket v2 ×3".
func taskTitleForItem(item *types.OrderItem) string {
	if item.Quantity > 1 {
		return fmt.Sprintf("%s ×%d", item.ProductName, item.Quantity)
	}
	return item.ProductName
}

// DecomposeOrder creates one production task per order item. It is called
// from both intake paths (POST /v1/orders with items, and the Cotiza
// webhook) and from AddItem for incremental item creation. Item-level
// failures are logged and skipped so one bad item does not lose the rest;
// the created tasks are returned.
func (s *OrderDecompositionService) DecomposeOrder(
	ctx context.Context,
	order *types.Order,
	items []types.OrderItem,
	actorID uuid.UUID,
) []types.Task {
	created := make([]types.Task, 0, len(items))

	for i := range items {
		item := &items[i]

		task, err := s.decomposeItem(ctx, order, item, actorID)
		if err != nil {
			s.log.WithError(err).WithFields(logrus.Fields{
				"order_id":      order.ID,
				"order_item_id": item.ID,
				"product_name":  item.ProductName,
			}).Error("Failed to create task for order item")
			continue
		}

		created = append(created, *task)
	}

	if len(created) > 0 {
		s.log.WithFields(logrus.Fields{
			"order_id":   order.ID,
			"task_count": len(created),
		}).Info("Order decomposed into production tasks")
	}

	return created
}

// decomposeItem creates the production task for a single order item,
// attempting capability-based auto-assignment first when enabled.
func (s *OrderDecompositionService) decomposeItem(
	ctx context.Context,
	order *types.Order,
	item *types.OrderItem,
	actorID uuid.UUID,
) (*types.Task, error) {
	metadata := map[string]any{
		"source":       "order_decomposition",
		"product_name": item.ProductName,
		"quantity":     item.Quantity,
	}
	if item.ProductSKU != "" {
		metadata["product_sku"] = item.ProductSKU
	}
	if item.Specifications != nil {
		metadata["specifications"] = item.Specifications
	}
	if item.CADFileURL != "" {
		metadata["cad_file_url"] = item.CADFileURL
	}

	task := &types.Task{
		TenantID:    order.TenantID,
		OrderID:     &order.ID,
		OrderItemID: &item.ID,
		Title:       taskTitleForItem(item),
		Description: fmt.Sprintf("Produce %d × %s for order %s", item.Quantity, item.ProductName, orderLabel(order)),
		Status:      types.TaskStatusBacklog,
		Priority:    order.Priority,
		Metadata:    metadata,
	}
	if task.Priority == 0 {
		task.Priority = 5
	}

	// Capability-based auto-assignment (before insert, so the task lands
	// with machine_id already set when a machine matches).
	requirements := RequirementsFromSpecifications(item.Specifications)
	var assignResult *AssignmentResult
	if s.autoAssign && s.assignment != nil {
		var err error
		assignResult, err = s.assignment.AssignBestMachine(ctx, order.TenantID, requirements)
		if err != nil {
			// Assignment infrastructure failure: create the task unassigned
			// rather than losing it, but log loudly.
			s.log.WithError(err).WithField("order_item_id", item.ID).Error("Auto-assignment failed; creating task unassigned")
			assignResult = nil
		} else if assignResult.Machine != nil {
			task.MachineID = &assignResult.Machine.ID
			metadata["assignment_basis"] = "capability_match"
			metadata["assigned_machine_code"] = assignResult.Machine.Code
		}
	}

	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}

	// Fail visible: requirements existed but no machine satisfied them.
	if assignResult != nil && assignResult.Machine == nil && len(requirements) > 0 && s.assignment != nil {
		s.assignment.NotifyAssignmentFailed(ctx, task, assignResult)
	}

	s.publishTaskCreated(ctx, task, actorID)

	return task, nil
}

// orderLabel prefers the external (Cotiza) ID for human-readable task text.
func orderLabel(order *types.Order) string {
	if order.ExternalID != "" {
		return order.ExternalID
	}
	return order.ID.String()
}

// publishTaskCreated emits task.created through the (outbox-backed)
// publisher. No-op when the publisher is nil.
func (s *OrderDecompositionService) publishTaskCreated(ctx context.Context, task *types.Task, actorID uuid.UUID) {
	if s.publisher == nil {
		return
	}

	metadata := map[string]any{
		"source": "order_decomposition",
	}
	if task.OrderID != nil {
		metadata["order_id"] = task.OrderID.String()
	}
	if task.OrderItemID != nil {
		metadata["order_item_id"] = task.OrderItemID.String()
	}
	if task.MachineID != nil {
		metadata["machine_id"] = task.MachineID.String()
	}

	err := s.publisher.PublishEntityCreated(ctx, pubsub.NamespaceTasks, task.TenantID, pubsub.EventTaskCreated, pubsub.EntityCreatedData{
		EntityID:   task.ID,
		EntityType: "task",
		Name:       task.Title,
		CreatedBy:  actorID,
		CreatedAt:  time.Now().UTC(),
		Metadata:   metadata,
	})
	if err != nil {
		s.log.WithError(err).WithField("task_id", task.ID).Warn("Failed to publish task.created event")
	}
}
