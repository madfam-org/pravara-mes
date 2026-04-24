// Package services provides business logic services for PravaraMES.
package services

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// TaskInfo represents the minimal task information needed by the genealogy service
// to auto-create genealogy records when a task completes.
type TaskInfo struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	OrderID     *uuid.UUID `json:"order_id,omitempty"`
	OrderItemID *uuid.UUID `json:"order_item_id,omitempty"`
	MachineID   *uuid.UUID `json:"machine_id,omitempty"`
	ProductSKU  string     `json:"product_sku,omitempty"`
}

// GenealogyService manages product genealogy business logic.
type GenealogyService struct {
	genealogyRepo *repositories.GenealogyRepository
	productRepo   *repositories.ProductRepository
	publisher     *pubsub.Publisher
	log           *logrus.Logger
}

// NewGenealogyService creates a new genealogy service.
func NewGenealogyService(
	genealogyRepo *repositories.GenealogyRepository,
	productRepo *repositories.ProductRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *GenealogyService {
	return &GenealogyService{
		genealogyRepo: genealogyRepo,
		productRepo:   productRepo,
		publisher:     publisher,
		log:           log,
	}
}

// AutoCreateFromTask creates a draft genealogy record when a task completes.
// It links the order, task, and machine, and looks up the product definition
// by the order item's product SKU.
func (s *GenealogyService) AutoCreateFromTask(ctx context.Context, task TaskInfo) (*repositories.ProductGenealogy, error) {
	record := &repositories.ProductGenealogy{
		TenantID:  task.TenantID,
		OrderID:   task.OrderID,
		TaskID:    &task.ID,
		MachineID: task.MachineID,
		Status:    string(repositories.GenealogyStatusDraft),
	}

	if task.OrderItemID != nil {
		record.OrderItemID = task.OrderItemID
	}

	// Look up product definition by SKU if available
	if task.ProductSKU != "" {
		// Try to find the latest version of the product by SKU
		// We search without version to get any match, then use the first result
		filter := repositories.ProductFilter{
			Search: &task.ProductSKU,
			Limit:  1,
		}
		products, _, err := s.productRepo.List(ctx, filter)
		if err != nil {
			s.log.WithError(err).WithField("product_sku", task.ProductSKU).Warn("Failed to look up product definition by SKU")
		} else if len(products) > 0 {
			record.ProductDefinitionID = &products[0].ID
			s.log.WithFields(logrus.Fields{
				"product_sku":           task.ProductSKU,
				"product_definition_id": products[0].ID,
			}).Debug("Linked product definition to genealogy")
		}
	}

	if err := s.genealogyRepo.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("failed to create genealogy record from task: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"genealogy_id": record.ID,
		"task_id":      task.ID,
		"order_id":     task.OrderID,
	}).Info("Auto-created genealogy record from task completion")

	return record, nil
}

// SealRecord seals a genealogy record by computing a SHA-256 hash of all linked data,
// storing the hash, and marking the record as sealed. The birth certificate document
// generation (R2 upload) is logged but not yet implemented.
func (s *GenealogyService) SealRecord(ctx context.Context, id uuid.UUID, sealedBy uuid.UUID) error {
	// Retrieve the full genealogy tree for hashing
	tree, err := s.genealogyRepo.GetTree(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get genealogy tree for sealing: %w", err)
	}
	if tree == nil {
		return fmt.Errorf("genealogy record not found")
	}

	// Verify the record is not already sealed
	if tree.Genealogy.Status == string(repositories.GenealogyStatusSealed) {
		return fmt.Errorf("genealogy record is already sealed")
	}

	// Marshal the entire tree to JSON for hashing
	treeData, err := json.Marshal(tree)
	if err != nil {
		return fmt.Errorf("failed to marshal genealogy tree for hashing: %w", err)
	}

	// Compute SHA-256 hash
	hash := sha256.Sum256(treeData)
	hashHex := fmt.Sprintf("%x", hash)

	// Log that R2 upload would happen here
	s.log.WithFields(logrus.Fields{
		"genealogy_id": id,
		"seal_hash":    hashHex,
		"data_size":    len(treeData),
	}).Info("Birth certificate document would be uploaded to R2 (not yet implemented)")

	// Seal the record in the database
	if err := s.genealogyRepo.Seal(ctx, id, hashHex, "", sealedBy); err != nil {
		return fmt.Errorf("failed to seal genealogy record: %w", err)
	}

	s.log.WithFields(logrus.Fields{
		"genealogy_id": id,
		"sealed_by":    sealedBy,
		"seal_hash":    hashHex,
	}).Info("Genealogy record sealed")

	return nil
}
