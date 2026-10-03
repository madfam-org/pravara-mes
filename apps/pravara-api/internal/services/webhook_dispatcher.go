package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// retryBackoffs defines exponential backoff durations for webhook delivery retries.
var retryBackoffs = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
}

// WebhookDispatcher is a background service that delivers webhook events.
type WebhookDispatcher struct {
	outboxRepo  *repositories.OutboxRepository
	webhookRepo *repositories.WebhookRepository
	cfg         config.WebhooksConfig
	httpClient  *http.Client
	log         *logrus.Logger
	scopes      *dbScopes
}

// dbScopes opens the tenant and system scopes the dispatcher's statements run
// in. When nil (unit tests on a plain *sql.DB), fn runs directly.
type dbScopes struct {
	pool *sql.DB
}

func (s *dbScopes) tenant(ctx context.Context, tenantID uuid.UUID, fn func(ctx context.Context) error) error {
	if s == nil {
		return fn(ctx)
	}
	return db.RunInTenantTx(ctx, s.pool, tenantID.String(), fn)
}

func (s *dbScopes) system(ctx context.Context, purpose string, fn func(ctx context.Context) error) error {
	if s == nil {
		return fn(ctx)
	}
	return db.RunInSystemScope(ctx, s.pool, purpose, fn)
}

// UseTenantScopes makes the dispatcher discover work in the read-only system
// scope and process every event and delivery in its own tenant transaction.
// Required when the repositories are built on a db.TenantDB.
func (d *WebhookDispatcher) UseTenantScopes(pool *sql.DB) {
	d.scopes = &dbScopes{pool: pool}
}

// NewWebhookDispatcher creates a new webhook dispatcher.
func NewWebhookDispatcher(
	outboxRepo *repositories.OutboxRepository,
	webhookRepo *repositories.WebhookRepository,
	cfg config.WebhooksConfig,
	log *logrus.Logger,
) *WebhookDispatcher {
	return &WebhookDispatcher{
		outboxRepo:  outboxRepo,
		webhookRepo: webhookRepo,
		cfg:         cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// Start begins the background dispatch loop. It blocks until ctx is cancelled.
func (d *WebhookDispatcher) Start(ctx context.Context) {
	interval := time.Duration(d.cfg.DispatchInterval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Daily cleanup ticker
	cleanupTicker := time.NewTicker(24 * time.Hour)
	defer cleanupTicker.Stop()

	d.log.Info("Webhook dispatcher started")

	for {
		select {
		case <-ctx.Done():
			d.log.Info("Webhook dispatcher stopping")
			return
		case <-ticker.C:
			d.dispatchPendingEvents(ctx)
			d.retryFailedDeliveries(ctx)
		case <-cleanupTicker.C:
			d.purgeOldEvents(ctx)
		}
	}
}

func (d *WebhookDispatcher) dispatchPendingEvents(ctx context.Context) {
	var events []repositories.OutboxEvent
	err := d.scopes.system(ctx, "webhooks.pending_events", func(ctx context.Context) error {
		var err error
		events, err = d.outboxRepo.GetPendingEvents(ctx, 100)
		return err
	})
	if err != nil {
		d.log.WithError(err).Error("Failed to get pending events for dispatch")
		return
	}

	for _, event := range events {
		d.dispatchEvent(ctx, event)
	}
}

// dispatchEvent fans one event out to its tenant's subscriptions. Database
// work runs in the event's tenant scope; HTTP calls run outside any
// transaction.
func (d *WebhookDispatcher) dispatchEvent(ctx context.Context, event repositories.OutboxEvent) {
	type pending struct {
		delivery *repositories.WebhookDelivery
		sub      repositories.WebhookSubscription
	}
	var work []pending
	err := d.scopes.tenant(ctx, event.TenantID, func(ctx context.Context) error {
		subs, err := d.webhookRepo.GetActiveSubscriptionsForEvent(ctx, event.TenantID, event.EventType)
		if err != nil {
			return fmt.Errorf("get subscriptions: %w", err)
		}
		for _, sub := range subs {
			delivery := &repositories.WebhookDelivery{
				SubscriptionID: sub.ID,
				EventID:        event.ID,
				Status:         "pending",
			}
			if err := d.webhookRepo.CreateDelivery(ctx, delivery); err != nil {
				d.log.WithError(err).Error("Failed to create webhook delivery record")
				continue
			}
			work = append(work, pending{delivery: delivery, sub: sub})
		}
		return nil
	})
	if err != nil {
		d.log.WithError(err).WithField("event_id", event.ID).Error("Failed to prepare webhook deliveries")
		return
	}

	for _, w := range work {
		d.attemptDelivery(ctx, event.TenantID, w.delivery, &w.sub, event.Payload)
	}

	// Mark event as delivered (subscriptions found and processed)
	err = d.scopes.tenant(ctx, event.TenantID, func(ctx context.Context) error {
		return d.outboxRepo.MarkDelivered(ctx, event.ID)
	})
	if err != nil {
		d.log.WithError(err).WithField("event_id", event.ID).Error("Failed to mark event as delivered")
	}
}

func (d *WebhookDispatcher) retryFailedDeliveries(ctx context.Context) {
	var deliveries []repositories.WebhookDelivery
	err := d.scopes.system(ctx, "webhooks.pending_deliveries", func(ctx context.Context) error {
		var err error
		deliveries, err = d.webhookRepo.GetPendingDeliveries(ctx, 50)
		return err
	})
	if err != nil {
		d.log.WithError(err).Error("Failed to get pending deliveries for retry")
		return
	}

	for i := range deliveries {
		delivery := deliveries[i]
		var sub *repositories.WebhookSubscription
		var event *repositories.OutboxEvent
		err := d.scopes.tenant(ctx, delivery.TenantID, func(ctx context.Context) error {
			var err error
			if sub, err = d.webhookRepo.GetSubscriptionByID(ctx, delivery.SubscriptionID); err != nil || sub == nil {
				return err
			}
			event, err = d.outboxRepo.GetEventByID(ctx, delivery.EventID)
			return err
		})
		if err != nil || sub == nil || !sub.IsActive || event == nil {
			continue
		}

		d.attemptDelivery(ctx, delivery.TenantID, &delivery, sub, event.Payload)
	}
}

func (d *WebhookDispatcher) attemptDelivery(ctx context.Context, tenantID uuid.UUID, delivery *repositories.WebhookDelivery, sub *repositories.WebhookSubscription, payload json.RawMessage) {
	delivery.AttemptCount++

	// Build request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(payload))
	if err != nil {
		d.markDeliveryFailed(ctx, tenantID, delivery, fmt.Sprintf("failed to create request: %v", err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "PravaraMES-Webhook/1.0")

	// HMAC signature
	mac := hmac.New(sha256.New, []byte(sub.Secret))
	mac.Write(payload)
	signature := fmt.Sprintf("sha256=%x", mac.Sum(nil))
	req.Header.Set("X-Pravara-Signature", signature)

	// Execute request
	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.markDeliveryFailed(ctx, tenantID, delivery, fmt.Sprintf("request failed: %v", err))
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	httpStatus := resp.StatusCode
	delivery.HTTPStatus = &httpStatus

	if httpStatus >= 200 && httpStatus < 300 {
		// Success
		delivery.Status = "delivered"
		delivery.NextRetryAt = nil
		observability.WebhookDeliveriesTotal.WithLabelValues("success").Inc()
	} else {
		errMsg := fmt.Sprintf("HTTP %d", httpStatus)
		d.markDeliveryFailed(ctx, tenantID, delivery, errMsg)
		return
	}

	if err := d.updateDelivery(ctx, tenantID, delivery); err != nil {
		d.log.WithError(err).Error("Failed to update delivery status")
	}
}

func (d *WebhookDispatcher) markDeliveryFailed(ctx context.Context, tenantID uuid.UUID, delivery *repositories.WebhookDelivery, errMsg string) {
	delivery.LastError = &errMsg

	maxRetries := d.cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 5
	}

	if delivery.AttemptCount >= maxRetries {
		delivery.Status = "dead"
		delivery.NextRetryAt = nil
		observability.WebhookDeliveriesTotal.WithLabelValues("dead").Inc()
	} else {
		delivery.Status = "failed"
		backoffIdx := delivery.AttemptCount - 1
		if backoffIdx >= len(retryBackoffs) {
			backoffIdx = len(retryBackoffs) - 1
		}
		nextRetry := time.Now().Add(retryBackoffs[backoffIdx])
		delivery.NextRetryAt = &nextRetry
		observability.WebhookDeliveriesTotal.WithLabelValues("retry").Inc()
	}

	if err := d.updateDelivery(ctx, tenantID, delivery); err != nil {
		d.log.WithError(err).Error("Failed to update failed delivery")
	}
}

func (d *WebhookDispatcher) updateDelivery(ctx context.Context, tenantID uuid.UUID, delivery *repositories.WebhookDelivery) error {
	return d.scopes.tenant(ctx, tenantID, func(ctx context.Context) error {
		return d.webhookRepo.UpdateDelivery(ctx, delivery)
	})
}

func (d *WebhookDispatcher) purgeOldEvents(ctx context.Context) {
	retentionDays := d.cfg.RetentionDays
	if retentionDays == 0 {
		retentionDays = 30
	}

	var tenants []uuid.UUID
	if err := d.scopes.system(ctx, "webhooks.purge_discovery", func(ctx context.Context) error {
		var err error
		tenants, err = d.outboxRepo.TenantsWithPurgeableEvents(ctx, retentionDays)
		return err
	}); err != nil {
		d.log.WithError(err).Error("Failed to find tenants with purgeable outbox events")
		return
	}

	var count int64
	for _, tenantID := range tenants {
		err := d.scopes.tenant(ctx, tenantID, func(ctx context.Context) error {
			n, err := d.outboxRepo.PurgeOldEvents(ctx, retentionDays)
			count += n
			return err
		})
		if err != nil {
			d.log.WithError(err).WithField("tenant_id", tenantID).Error("Failed to purge old outbox events")
		}
	}

	if count > 0 {
		d.log.WithField("purged_count", count).Info("Purged old outbox events")
	}
}
