package pubsub

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// OutboxPublisher wraps the existing Publisher with outbox persistence.
// Every Publish() and PublishToEntity() call also inserts into event_outbox.
// The real-time path (Centrifugo) is never blocked by outbox failures.
type OutboxPublisher struct {
	*Publisher
	outboxRepo *repositories.OutboxRepository
	log        *logrus.Logger
}

// NewOutboxPublisher creates a new OutboxPublisher wrapping the given publisher.
func NewOutboxPublisher(publisher *Publisher, outboxRepo *repositories.OutboxRepository, log *logrus.Logger) *OutboxPublisher {
	return &OutboxPublisher{
		Publisher:  publisher,
		outboxRepo: outboxRepo,
		log:        log,
	}
}

// Publish publishes an event to Centrifugo and persists it in the outbox.
func (p *OutboxPublisher) Publish(ctx context.Context, namespace ChannelNamespace, tenantID uuid.UUID, event *Event) error {
	// Always publish to Centrifugo first (real-time path)
	err := p.Publisher.Publish(ctx, namespace, tenantID, event)

	// Persist to outbox (best-effort, never blocks real-time)
	p.persistToOutbox(ctx, tenantID, event, namespace)

	return err
}

// PublishToEntity publishes an event to an entity channel and persists it in the outbox.
func (p *OutboxPublisher) PublishToEntity(ctx context.Context, namespace ChannelNamespace, tenantID, entityID uuid.UUID, event *Event) error {
	// Always publish to Centrifugo first (real-time path)
	err := p.Publisher.PublishToEntity(ctx, namespace, tenantID, entityID, event)

	// Persist to outbox (best-effort, never blocks real-time)
	p.persistToOutbox(ctx, tenantID, event, namespace)

	return err
}

// persistToOutbox inserts the event into the outbox table. Failures are logged but not propagated.
func (p *OutboxPublisher) persistToOutbox(ctx context.Context, tenantID uuid.UUID, event *Event, namespace ChannelNamespace) {
	payload, err := json.Marshal(event)
	if err != nil {
		p.log.WithError(err).WithField("event_type", event.Type).Warn("Failed to marshal event for outbox")
		return
	}

	_, err = p.outboxRepo.InsertEvent(ctx, tenantID, string(event.Type), string(namespace), payload)
	if err != nil {
		p.log.WithError(err).WithFields(logrus.Fields{
			"event_type": event.Type,
			"tenant_id":  tenantID,
		}).Warn("Failed to persist event to outbox")
	}
}
