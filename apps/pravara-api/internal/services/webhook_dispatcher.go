package services

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
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
	events, err := d.outboxRepo.GetPendingEvents(ctx, 100)
	if err != nil {
		d.log.WithError(err).Error("Failed to get pending events for dispatch")
		return
	}

	for _, event := range events {
		// Find matching subscriptions for this event
		subs, err := d.webhookRepo.GetActiveSubscriptionsForEvent(ctx, event.TenantID, event.EventType)
		if err != nil {
			d.log.WithError(err).WithField("event_id", event.ID).Error("Failed to get subscriptions for event")
			continue
		}

		// Create delivery records for each subscription
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

			// Attempt delivery
			d.attemptDelivery(ctx, delivery, &sub, event.Payload)
		}

		// Mark event as delivered (subscriptions found and processed)
		if err := d.outboxRepo.MarkDelivered(ctx, event.ID); err != nil {
			d.log.WithError(err).WithField("event_id", event.ID).Error("Failed to mark event as delivered")
		}
	}
}

func (d *WebhookDispatcher) retryFailedDeliveries(ctx context.Context) {
	deliveries, err := d.webhookRepo.GetPendingDeliveries(ctx, 50)
	if err != nil {
		d.log.WithError(err).Error("Failed to get pending deliveries for retry")
		return
	}

	for _, delivery := range deliveries {
		// Get subscription for this delivery
		sub, err := d.webhookRepo.GetSubscriptionByID(ctx, delivery.SubscriptionID)
		if err != nil || sub == nil || !sub.IsActive {
			continue
		}

		// Get event payload
		event, err := d.outboxRepo.GetEventByID(ctx, delivery.EventID)
		if err != nil || event == nil {
			continue
		}

		d.attemptDelivery(ctx, &delivery, sub, event.Payload)
	}
}

func (d *WebhookDispatcher) attemptDelivery(ctx context.Context, delivery *repositories.WebhookDelivery, sub *repositories.WebhookSubscription, payload json.RawMessage) {
	delivery.AttemptCount++

	// Build request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.URL, bytes.NewReader(payload))
	if err != nil {
		d.markDeliveryFailed(ctx, delivery, fmt.Sprintf("failed to create request: %v", err))
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
		d.markDeliveryFailed(ctx, delivery, fmt.Sprintf("request failed: %v", err))
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
		d.markDeliveryFailed(ctx, delivery, errMsg)
		return
	}

	if err := d.webhookRepo.UpdateDelivery(ctx, delivery); err != nil {
		d.log.WithError(err).Error("Failed to update delivery status")
	}
}

func (d *WebhookDispatcher) markDeliveryFailed(ctx context.Context, delivery *repositories.WebhookDelivery, errMsg string) {
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

	if err := d.webhookRepo.UpdateDelivery(ctx, delivery); err != nil {
		d.log.WithError(err).Error("Failed to update failed delivery")
	}
}

func (d *WebhookDispatcher) purgeOldEvents(ctx context.Context) {
	retentionDays := d.cfg.RetentionDays
	if retentionDays == 0 {
		retentionDays = 30
	}

	count, err := d.outboxRepo.PurgeOldEvents(ctx, retentionDays)
	if err != nil {
		d.log.WithError(err).Error("Failed to purge old outbox events")
		return
	}

	if count > 0 {
		d.log.WithField("purged_count", count).Info("Purged old outbox events")
	}
}
