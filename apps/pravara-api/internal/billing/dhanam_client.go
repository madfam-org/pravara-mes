package billing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DhanamClientConfig holds configuration for the Dhanam API client.
type DhanamClientConfig struct {
	APIURL       string
	APIKey       string
	RetryCount   int
	RetryDelay   time.Duration
	SyncInterval time.Duration
	Enabled      bool
}

// DhanamClient handles communication with the Dhanam billing API.
type DhanamClient struct {
	config     DhanamClientConfig
	httpClient *http.Client
	log        *logrus.Logger
	mu         sync.Mutex
}

// NewDhanamClient creates a new Dhanam API client.
func NewDhanamClient(cfg DhanamClientConfig, log *logrus.Logger) *DhanamClient {
	return &DhanamClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: log,
	}
}

// DhanamUsageReport represents usage data sent to Dhanam.
type DhanamUsageReport struct {
	TenantID  string                   `json:"tenant_id"`
	Date      string                   `json:"date"`
	UsageData map[UsageEventType]int64 `json:"usage_data"`
	Metadata  map[string]string        `json:"metadata,omitempty"`
}

// DhanamAPIResponse represents the response from Dhanam API.
type DhanamAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	ID      string `json:"id,omitempty"`
}

// IsEnabled returns whether Dhanam integration is enabled.
func (dc *DhanamClient) IsEnabled() bool {
	return dc.config.Enabled && dc.config.APIURL != "" && dc.config.APIKey != ""
}

// SendUsageReport sends usage data to Dhanam API with retry logic.
func (dc *DhanamClient) SendUsageReport(ctx context.Context, report DhanamUsageReport) error {
	if !dc.IsEnabled() {
		dc.log.Debug("Dhanam integration disabled, skipping usage report")
		return nil
	}

	dc.mu.Lock()
	defer dc.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt <= dc.config.RetryCount; attempt++ {
		if attempt > 0 {
			dc.log.WithFields(logrus.Fields{
				"attempt":   attempt,
				"tenant_id": report.TenantID,
			}).Debug("Retrying Dhanam API call")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(dc.config.RetryDelay):
			}
		}

		err := dc.sendRequest(ctx, report)
		if err == nil {
			dc.log.WithFields(logrus.Fields{
				"tenant_id": report.TenantID,
				"date":      report.Date,
			}).Info("Successfully sent usage report to Dhanam")
			return nil
		}

		lastErr = err
		dc.log.WithError(err).WithFields(logrus.Fields{
			"attempt":   attempt + 1,
			"tenant_id": report.TenantID,
		}).Warn("Failed to send usage report to Dhanam")
	}

	return fmt.Errorf("failed after %d attempts: %w", dc.config.RetryCount+1, lastErr)
}

// sendRequest makes the actual HTTP request to Dhanam API.
func (dc *DhanamClient) sendRequest(ctx context.Context, report DhanamUsageReport) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	url := fmt.Sprintf("%s/usage/report", dc.config.APIURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", dc.config.APIKey))
	req.Header.Set("X-Client", "pravara-mes")
	req.Header.Set("X-Client-Version", "1.0")

	resp, err := dc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var apiResp DhanamAPIResponse
		if err := json.Unmarshal(body, &apiResp); err == nil && apiResp.Error != "" {
			return fmt.Errorf("API error (status %d): %s", resp.StatusCode, apiResp.Error)
		}
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var apiResp DhanamAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		dc.log.WithError(err).Warn("Failed to parse Dhanam API response")
	} else if !apiResp.Success {
		return fmt.Errorf("API returned failure: %s", apiResp.Error)
	}

	return nil
}

// SendBatchReports sends multiple usage reports to Dhanam API.
func (dc *DhanamClient) SendBatchReports(ctx context.Context, reports []DhanamUsageReport) error {
	if !dc.IsEnabled() {
		dc.log.Debug("Dhanam integration disabled, skipping batch reports")
		return nil
	}

	var errs []error
	for _, report := range reports {
		if err := dc.SendUsageReport(ctx, report); err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", report.TenantID, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d reports failed to send", len(errs))
	}

	return nil
}

// HealthCheck verifies connectivity to Dhanam API.
func (dc *DhanamClient) HealthCheck(ctx context.Context) error {
	if !dc.IsEnabled() {
		return nil
	}

	url := fmt.Sprintf("%s/health", dc.config.APIURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", dc.config.APIKey))

	resp, err := dc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned status %d", resp.StatusCode)
	}

	return nil
}
