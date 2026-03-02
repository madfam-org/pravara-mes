# PravaraMES Billing Integration

This package provides usage tracking and billing integration for PravaraMES, designed to integrate with MADFAM's Dhanam billing service.

## Overview

The billing system tracks usage events per tenant for various billable activities:
- **API Calls**: Every authenticated API request
- **Telemetry Points**: Each telemetry data point ingested
- **Storage**: Storage usage in megabytes
- **WebSocket Connections**: Active WebSocket connection time
- **Active Machines**: Number of registered active machines
- **Orders Created**: Each new order
- **Certificates Issued**: Quality certificates approved/issued

## Architecture

```
┌─────────────────┐
│  API Requests   │──┐
└─────────────────┘  │
                     │  Usage Events
┌─────────────────┐  │  (async)
│ Order Creation  │──┼──────────────┐
└─────────────────┘  │              │
                     │              ▼
┌─────────────────┐  │      ┌──────────────┐
│  Certificates   │──┘      │    Redis     │
└─────────────────┘         │   Recorder   │
                            └──────┬───────┘
                                   │
                         ┌─────────┴─────────┐
                         │                   │
                         ▼                   ▼
                  ┌─────────────┐    ┌─────────────┐
                  │ Redis Keys  │    │  Dhanam API │
                  │ (90d TTL)   │    │   (Future)  │
                  └─────────────┘    └─────────────┘
```

## Components

### 1. Usage Events (`usage.go`)

Defines the core types for tracking billable events:

```go
type UsageEvent struct {
    ID        string
    TenantID  string
    EventType UsageEventType
    Quantity  int64
    Metadata  map[string]string
    Timestamp time.Time
}
```

### 2. Redis Recorder (`recorder.go`)

Implements async event recording with Redis backend:

- **Buffered Channel**: Events queued in memory (1000 buffer)
- **Batch Processing**: Groups events for efficient Redis writes
- **Auto-aggregation**: Uses Redis HINCRBY for atomic counters
- **90-day Retention**: Automatic expiry on usage keys
- **Background Flush**: Periodic sync to Dhanam API (stub)

Redis Key Format:
```
usage:{tenant_id}:{date}:{event_type}
Example: usage:550e8400-e29b-41d4-a716-446655440000:2024-03-01:api_call
```

### 3. Usage Middleware (`middleware/usage.go`)

Gin middleware for automatic API call tracking:

```go
router.Use(middleware.UsageTracking(recorder, log))
```

Features:
- Extracts tenant_id from auth context
- Non-blocking (goroutine + buffered channel)
- Skips health/metrics endpoints
- Records HTTP method, path, and status code

### 4. Billing Handlers (`api/billing_handlers.go`)

REST endpoints for usage reporting:

- `GET /v1/billing/usage` - Current tenant's usage summary
- `GET /v1/billing/usage/daily` - Daily breakdown
- `GET /v1/admin/billing/tenants/:id/usage` - Admin: any tenant's usage

## Usage

### API Server Integration

In `main.go`:

```go
import "github.com/madfam-org/pravara-mes/apps/pravara-api/internal/billing"

// Initialize recorder
recorder, err := billing.NewRedisUsageRecorder(billing.RecorderConfig{
    RedisURL:      cfg.Redis.URL,
    BufferSize:    1000,
    FlushInterval: 5 * time.Minute,
}, log)
defer recorder.Close()

// Add middleware
router.Use(middleware.UsageTracking(recorder, log))

// Wire up handlers
orderHandler.SetUsageRecorder(recorder)
qualityHandler.SetUsageRecorder(recorder)
```

### Recording Custom Events

In handlers:

```go
// Record order creation
event := billing.UsageEvent{
    TenantID:  tenantID,
    EventType: billing.UsageEventOrder,
    Quantity:  1,
    Metadata: map[string]string{
        "order_id": order.ID.String(),
    },
    Timestamp: time.Now(),
}

// Async recording (non-blocking)
go func() {
    if err := recorder.RecordEvent(ctx, event); err != nil {
        log.WithError(err).Warn("Failed to record usage")
    }
}()
```

### Batch Recording (Telemetry Worker)

For high-volume events:

```go
events := []billing.UsageEvent{
    {TenantID: "tenant-1", EventType: billing.UsageEventTelemetry, Quantity: 100},
    {TenantID: "tenant-2", EventType: billing.UsageEventTelemetry, Quantity: 50},
}

if err := recorder.RecordBatch(ctx, events); err != nil {
    log.WithError(err).Error("Failed to record batch")
}
```

## API Endpoints

### Get Usage Summary

```bash
GET /v1/billing/usage?from=2024-01-01&to=2024-01-31
Authorization: Bearer <token>
```

Response:
```json
{
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
  "period": "2024-01-01 to 2024-01-31",
  "from_date": "2024-01-01T00:00:00Z",
  "to_date": "2024-01-31T23:59:59Z",
  "api_call_count": 15234,
  "telemetry_points": 1250000,
  "storage_mb": 450,
  "websocket_minutes": 3600,
  "active_machines": 12,
  "orders_created": 45,
  "certificates": 8
}
```

### Get Daily Breakdown

```bash
GET /v1/billing/usage/daily?from=2024-01-01&to=2024-01-07
Authorization: Bearer <token>
```

Response:
```json
{
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
  "from_date": "2024-01-01",
  "to_date": "2024-01-07",
  "daily_usage": [
    {
      "date": "2024-01-01",
      "api_call_count": 523,
      "telemetry_points": 42000,
      "storage_mb": 65,
      "websocket_minutes": 120,
      "active_machines": 12,
      "orders_created": 3,
      "certificates": 1
    },
    // ... more days
  ]
}
```

### Admin: Get Any Tenant's Usage

```bash
GET /v1/admin/billing/tenants/550e8400-e29b-41d4-a716-446655440000/usage?from=2024-01-01&to=2024-12-31
Authorization: Bearer <admin-token>
X-User-Role: admin
```

## Configuration

Environment variables:

```bash
# Required for usage tracking
REDIS_URL=redis://localhost:6379/0

# Optional: Tune performance
PRAVARA_BILLING_BUFFER_SIZE=1000
PRAVARA_BILLING_FLUSH_INTERVAL=5m
```

## Performance Characteristics

- **Async Recording**: API requests return immediately, usage tracked in background
- **Batching**: Groups up to 100 events per Redis transaction
- **Memory**: ~1000 events buffered in memory (~100KB)
- **Redis Load**: 1 INCRBY + 1 EXPIRE per event type per tenant per day
- **Latency**: <1ms overhead per API request (channel send only)

## Future: Dhanam Integration

The `DhanamClient` in `recorder.go` is a stub for future integration:

1. **Periodic Aggregation**: Background job aggregates previous day's usage
2. **API Call**: POST to Dhanam billing endpoint with usage report
3. **Retry Logic**: Exponential backoff for failed sends
4. **Idempotency**: Track sent reports to prevent duplicates
5. **Cleanup**: Archive or delete Redis keys after successful sync

Example report structure:
```json
{
  "tenant_id": "550e8400-e29b-41d4-a716-446655440000",
  "date": "2024-01-15",
  "usage_data": {
    "api_call": 1234,
    "telemetry_point": 50000,
    "order_created": 5,
    "certificate_issued": 2
  },
  "metadata": {
    "source": "pravara-mes",
    "version": "1.0"
  }
}
```

## Testing

### Manual Testing

```bash
# Start Redis
docker run -d -p 6379:6379 redis:7-alpine

# Check recorded usage
redis-cli
> KEYS usage:*
> GET usage:550e8400-e29b-41d4-a716-446655440000:2024-03-01:api_call
```

### Integration Testing

```go
func TestUsageRecording(t *testing.T) {
    recorder, _ := billing.NewRedisUsageRecorder(cfg, log)
    defer recorder.Close()

    event := billing.UsageEvent{
        TenantID:  "test-tenant",
        EventType: billing.UsageEventAPICall,
        Quantity:  1,
    }

    err := recorder.RecordEvent(context.Background(), event)
    assert.NoError(t, err)

    time.Sleep(2 * time.Second) // Wait for background flush

    summary, _ := recorder.GetTenantUsage(ctx, "test-tenant", from, to)
    assert.Equal(t, int64(1), summary.APICallCount)
}
```

## Monitoring

Key metrics to track:

- **Event Channel Depth**: Monitor buffer fill rate
- **Redis Connection Health**: Track connection failures
- **Recording Errors**: Alert on persistent failures
- **Flush Latency**: Background job performance
- **Data Completeness**: Compare event counts with business metrics

## Security Considerations

1. **Tenant Isolation**: All queries filtered by tenant_id from JWT
2. **Admin Access**: Admin endpoints require explicit role check
3. **Rate Limiting**: Usage middleware respects existing rate limits
4. **Data Retention**: Auto-expire after 90 days (GDPR compliance)
5. **API Key Protection**: Future Dhanam API key stored in secrets manager

## Troubleshooting

### Events not recorded

1. Check Redis connection: `redis-cli PING`
2. Verify middleware order (must be after auth middleware)
3. Check logs for channel full warnings
4. Ensure tenant_id present in auth context

### High Redis memory usage

1. Check TTL on keys: `redis-cli TTL usage:...`
2. Verify expiry is being set (90 days)
3. Consider reducing retention period
4. Implement aggressive aggregation to Dhanam

### Missing usage data

1. Check date range (max 90 days for regular users)
2. Verify tenant_id matches JWT claim
3. Check Redis key existence
4. Review background worker logs

## Support

For issues or questions:
- Internal: #pravara-support Slack channel
- Documentation: https://docs.madfam.io/pravara/billing
- Dhanam Team: #billing-integrations
