// Package services provides business logic services for PravaraMES.
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// InventoryService manages inventory business logic including stock adjustments
// and low-stock alerting.
type InventoryService struct {
	inventoryRepo *repositories.InventoryRepository
	publisher     *pubsub.Publisher
	log           *logrus.Logger
}

// NewInventoryService creates a new inventory service.
func NewInventoryService(
	inventoryRepo *repositories.InventoryRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *InventoryService {
	return &InventoryService{
		inventoryRepo: inventoryRepo,
		publisher:     publisher,
		log:           log,
	}
}

// AdjustQuantity modifies inventory stock and publishes events for low-stock alerts.
func (s *InventoryService) AdjustQuantity(ctx context.Context, tenantID uuid.UUID, itemID uuid.UUID, quantity float64, txnType string, refType *string, refID *uuid.UUID, userID *uuid.UUID, notes *string) error {
	if err := s.inventoryRepo.AdjustQuantity(ctx, itemID, quantity, txnType, refType, refID, userID, notes); err != nil {
		return fmt.Errorf("failed to adjust inventory: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"item_id":  itemID,
		"quantity": quantity,
		"txn_type": txnType,
	}).Info("Inventory adjusted")

	// Check if item is now low stock and publish event
	item, err := s.inventoryRepo.GetItemByID(ctx, itemID)
	if err != nil {
		s.log.WithError(err).Warn("Failed to check low stock after adjustment")
		return nil // Non-critical
	}

	if item != nil && item.ReorderPoint > 0 && item.QuantityAvailable <= item.ReorderPoint {
		s.publishLowStockAlert(ctx, tenantID, item)
	}

	// Publish inventory updated event
	if s.publisher != nil {
		eventData := pubsub.InventoryEventData{
			ItemID:    itemID,
			SKU:       item.SKU,
			ItemName:  item.Name,
			Action:    txnType,
			Quantity:  quantity,
			NewOnHand: item.QuantityOnHand,
		}
		if err := s.publisher.PublishInventoryEvent(ctx, tenantID, pubsub.EventInventoryUpdated, eventData); err != nil {
			s.log.WithError(err).Warn("Failed to publish inventory updated event")
		}
	}

	return nil
}

// CheckLowStock checks all inventory items for low stock and publishes alerts.
func (s *InventoryService) CheckLowStock(ctx context.Context, tenantID uuid.UUID) ([]repositories.InventoryItem, error) {
	items, err := s.inventoryRepo.GetLowStock(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get low stock items: %w", err)
	}

	for i := range items {
		s.publishLowStockAlert(ctx, tenantID, &items[i])
	}

	return items, nil
}

func (s *InventoryService) publishLowStockAlert(ctx context.Context, tenantID uuid.UUID, item *repositories.InventoryItem) {
	if s.publisher == nil {
		return
	}

	eventData := pubsub.InventoryEventData{
		ItemID:    item.ID,
		SKU:       item.SKU,
		ItemName:  item.Name,
		Action:    "low_stock",
		Quantity:  item.QuantityAvailable,
		NewOnHand: item.QuantityOnHand,
	}

	if err := s.publisher.PublishInventoryEvent(ctx, tenantID, pubsub.EventInventoryLowStock, eventData); err != nil {
		s.log.WithError(err).WithField("item_id", item.ID).Warn("Failed to publish low stock event")
	}

	// Also publish a notification
	notifData := pubsub.NotificationData{
		Title:    fmt.Sprintf("Low Stock: %s", item.Name),
		Message:  fmt.Sprintf("Inventory item %s (%s) has %.1f available, below reorder point of %.1f", item.Name, item.SKU, item.QuantityAvailable, item.ReorderPoint),
		Severity: "warning",
		Source:   "inventory",
		SourceID: &item.ID,
	}

	if err := s.publisher.PublishNotification(ctx, tenantID, notifData); err != nil {
		s.log.WithError(err).Warn("Failed to publish low stock notification")
	}
}
