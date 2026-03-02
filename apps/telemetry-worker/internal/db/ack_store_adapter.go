// Package db provides database access for the telemetry worker.
package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/telemetry-worker/internal/command"
)

// AckStoreAdapter wraps a Store to implement the command.AckStore interface.
type AckStoreAdapter struct {
	store *Store
}

// NewAckStoreAdapter creates a new adapter that wraps the given store.
func NewAckStoreAdapter(store *Store) *AckStoreAdapter {
	return &AckStoreAdapter{store: store}
}

// GetMachineByCode retrieves machine info by its code.
func (a *AckStoreAdapter) GetMachineByCode(ctx context.Context, code string) (*command.MachineInfo, error) {
	info, err := a.store.GetMachineInfoByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return &command.MachineInfo{
		ID:       info.ID,
		TenantID: info.TenantID,
		Code:     info.Code,
		Name:     info.Name,
	}, nil
}

// UpdateCommandStatus updates the status of a command.
func (a *AckStoreAdapter) UpdateCommandStatus(ctx context.Context, commandID uuid.UUID, status string, message string) error {
	return a.store.UpdateCommandStatus(ctx, commandID, status, message)
}

// GetTaskCommandByCommandID retrieves task command info by command ID.
func (a *AckStoreAdapter) GetTaskCommandByCommandID(ctx context.Context, commandID uuid.UUID) (*command.TaskCommandInfo, error) {
	info, err := a.store.GetTaskCommandByCommandID(ctx, commandID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}
	return &command.TaskCommandInfo{
		ID:          info.ID,
		TaskID:      info.TaskID,
		TenantID:    info.TenantID,
		MachineID:   info.MachineID,
		CommandType: info.CommandType,
	}, nil
}

// UpdateTaskStatusOnJobComplete updates task status when a job completes.
func (a *AckStoreAdapter) UpdateTaskStatusOnJobComplete(ctx context.Context, taskID uuid.UUID, newStatus string, completedAt time.Time) error {
	return a.store.UpdateTaskStatusOnJobComplete(ctx, taskID, newStatus, completedAt)
}

// Compile-time check that AckStoreAdapter implements command.AckStore.
var _ command.AckStore = (*AckStoreAdapter)(nil)
