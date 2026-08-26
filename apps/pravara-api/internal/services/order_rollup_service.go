package services

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// OrderRollupService keeps an order's status in sync with the aggregate
// state of its tasks:
//
//   - first task entering in_progress (or quality_check)
//     -> order advances to in_progress
//   - every task completed
//     -> order advances to completed
//
// Both transitions are single guarded SQL statements (see OrderRepository.
// MarkInProgressIfStarted / CompleteIfAllTasksDone), so they are idempotent
// and never regress shipped/cancelled orders.
//
// This service is hooked into the API task-status paths (task move, task
// update). The telemetry-worker's machine-ack path only ever advances tasks
// to quality_check — the completed transition always flows through the API,
// which is why the roll-up lives here and not in the worker.
type OrderRollupService struct {
	orderRepo *repositories.OrderRepository
	publisher *pubsub.Publisher
	log       *logrus.Logger
}

// NewOrderRollupService creates a new order roll-up service.
// publisher may be nil; roll-up then works without emitting events.
func NewOrderRollupService(
	orderRepo *repositories.OrderRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *OrderRollupService {
	return &OrderRollupService{
		orderRepo: orderRepo,
		publisher: publisher,
		log:       log,
	}
}

// OnTaskStatusChanged evaluates roll-up rules after a task status change.
// Errors are logged, not returned: roll-up is best-effort relative to the
// task operation that triggered it.
func (s *OrderRollupService) OnTaskStatusChanged(ctx context.Context, task *types.Task, newStatus types.TaskStatus) {
	if task == nil || task.OrderID == nil {
		return
	}

	switch newStatus {
	case types.TaskStatusInProgress, types.TaskStatusQualityCheck:
		s.markInProgress(ctx, task)
	case types.TaskStatusCompleted:
		s.tryComplete(ctx, task)
	}
}

func (s *OrderRollupService) markInProgress(ctx context.Context, task *types.Task) {
	previous, changed, err := s.orderRepo.MarkInProgressIfStarted(ctx, *task.OrderID)
	if err != nil {
		s.log.WithError(err).WithField("order_id", *task.OrderID).Error("Order roll-up to in_progress failed")
		return
	}
	if !changed {
		return
	}

	s.log.WithFields(logrus.Fields{
		"order_id":   *task.OrderID,
		"old_status": previous,
		"trigger":    task.ID,
	}).Info("Order rolled up to in_progress (first task started)")

	s.publishStatusChange(ctx, task, previous, types.OrderStatusInProgress)
}

func (s *OrderRollupService) tryComplete(ctx context.Context, task *types.Task) {
	previous, changed, err := s.orderRepo.CompleteIfAllTasksDone(ctx, *task.OrderID)
	if err != nil {
		s.log.WithError(err).WithField("order_id", *task.OrderID).Error("Order roll-up to completed failed")
		return
	}
	if !changed {
		return
	}

	s.log.WithFields(logrus.Fields{
		"order_id":   *task.OrderID,
		"old_status": previous,
		"trigger":    task.ID,
	}).Info("Order rolled up to completed (all tasks done)")

	s.publishStatusChange(ctx, task, previous, types.OrderStatusCompleted)
}

func (s *OrderRollupService) publishStatusChange(ctx context.Context, task *types.Task, oldStatus, newStatus types.OrderStatus) {
	if s.publisher == nil {
		return
	}

	err := s.publisher.PublishOrderStatus(ctx, task.TenantID, pubsub.OrderStatusData{
		OrderID:   *task.OrderID,
		OldStatus: string(oldStatus),
		NewStatus: string(newStatus),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		s.log.WithError(err).WithField("order_id", *task.OrderID).Warn("Failed to publish order.status_changed event")
	}
}
