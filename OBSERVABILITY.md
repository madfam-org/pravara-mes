# PravaraMES Observability Implementation

## Overview

Comprehensive Prometheus observability has been implemented across all PravaraMES Go services with AlertManager integration for production monitoring.

## Architecture

### Services Instrumented

1. **pravara-api** (Port 4500)
   - HTTP metrics endpoint: `/metrics`
   - Database connection pool monitoring
   - Request latency and throughput tracking

2. **telemetry-worker** (Port 4502)
   - Dedicated metrics server with `/metrics` and `/health` endpoints
   - MQTT message processing metrics
   - Batch write performance tracking

3. **centrifugo** (Native Prometheus support)
   - Real-time connection monitoring
   - Message throughput tracking

## Per-Tenant Metrics

Request-scoped metrics in pravara-api and telemetry-worker include a `tenant_id` label, enabling per-tenant dashboards, alerting, and capacity planning.

- `pravara_api_http_requests_total{..., tenant_id}` - HTTP requests broken down by tenant
- `pravara_api_http_request_duration_seconds{..., tenant_id}` - Request latency per tenant
- `pravara_telemetry_mqtt_messages_processed_total{..., tenant_id}` - Telemetry messages per tenant

The `tenant_id` is extracted from the JWT token by the auth middleware and propagated to the Prometheus label set on every request-scoped metric.

## Grafana Dashboard ConfigMaps

Three pre-built Grafana dashboards are deployed as Kubernetes ConfigMaps in `infra/k8s/base/observability/grafana-dashboards/`:

| Dashboard | ConfigMap | Description |
|-----------|-----------|-------------|
| API Overview | `grafana-dashboard-api-overview` | Request rate, error rate, latency percentiles, DB pool |
| Telemetry Pipeline | `grafana-dashboard-telemetry-pipeline` | MQTT throughput, batch writes, queue depth |
| Realtime Gateway | `grafana-dashboard-realtime-gateway` | Centrifugo connections, message throughput |

These ConfigMaps are auto-discovered by the Grafana sidecar when the `grafana_dashboard: "1"` label is present.

## Metrics Exposed

### PravaraMES API (`pravara_api_*`)

#### HTTP Metrics
- `pravara_api_http_requests_total{method, path, status, tenant_id}` - Counter of HTTP requests
- `pravara_api_http_request_duration_seconds{method, path, tenant_id}` - Histogram of request durations
- `pravara_api_http_requests_in_flight` - Gauge of concurrent requests

#### Database Metrics
- `pravara_api_db_connections_open` - Current open connections
- `pravara_api_db_connections_in_use` - Active connections
- `pravara_api_db_connections_idle` - Idle connections in pool
- `pravara_api_db_connections_wait_count_total` - Total connections waited for
- `pravara_api_db_connections_wait_duration_seconds_total` - Time blocked waiting
- `pravara_api_db_connections_max_idle_closed_total` - Connections closed due to max idle
- `pravara_api_db_connections_max_lifetime_closed_total` - Connections closed due to lifetime

#### PubSub Metrics
- `pravara_api_pubsub_events_published_total{event_type, channel}` - Published events
- `pravara_api_pubsub_publish_errors_total{event_type, channel}` - Publish failures

### Telemetry Worker (`pravara_telemetry_*`)

#### MQTT Metrics
- `pravara_telemetry_mqtt_messages_received_total{topic_root, tenant_id}` - Messages received
- `pravara_telemetry_mqtt_messages_processed_total{metric_type, tenant_id}` - Successfully processed
- `pravara_telemetry_mqtt_messages_dropped_total{reason}` - Dropped messages
- `pravara_telemetry_mqtt_connection_status` - Connection status (1=connected, 0=disconnected)

#### Batch Processing Metrics
- `pravara_telemetry_batch_size` - Histogram of batch sizes
- `pravara_telemetry_batch_write_duration_seconds` - Write operation duration
- `pravara_telemetry_batch_write_retries_total` - Retry attempts
- `pravara_telemetry_batch_write_failures_total` - Failed write operations

#### Worker Metrics
- `pravara_telemetry_worker_queue_length` - Current queue depth
- `pravara_telemetry_telemetry_points_ingested_total` - Total data points processed
- `pravara_telemetry_db_connections_open` - Database connections
- `pravara_telemetry_db_connections_in_use` - Active database connections

## Implementation Details

### Files Created

#### PravaraMES API
```
apps/pravara-api/internal/observability/metrics.go
apps/pravara-api/internal/middleware/metrics.go
```

#### Telemetry Worker
```
apps/telemetry-worker/internal/observability/metrics.go
```

#### Kubernetes Manifests
```
infra/k8s/base/observability/
├── kustomization.yaml
├── servicemonitor-api.yaml
├── podmonitor-telemetry.yaml
├── servicemonitor-centrifugo.yaml
├── alertmanager-rules.yaml
└── grafana-dashboards/
    ├── kustomization.yaml
    └── configmap.yaml          # api-overview, telemetry-pipeline, realtime-gateway
```

### Code Integration

#### pravara-api main.go
- Added metrics middleware to Gin router
- Exposed `/metrics` endpoint via `promhttp.Handler()`
- Started background goroutine collecting DB stats every 15 seconds
- Proper graceful shutdown handling

#### telemetry-worker main.go
- Added dedicated HTTP server on port 4502
- Exposed `/metrics` and `/health` endpoints
- Started background DB stats collection
- Integrated metrics server shutdown in graceful shutdown flow

### Middleware Implementation

The Gin metrics middleware:
- Skips `/metrics` endpoint to avoid recursive metrics
- Tracks in-flight requests using gauge
- Records request duration as histogram
- Counts requests by method, path, and status
- Uses `c.FullPath()` for route templates to avoid high cardinality

### Database Stats Collection

Both services collect DB stats every 15 seconds:
- Connection pool utilization
- Wait times and counts
- Connection lifecycle events

## Kubernetes Integration

### ServiceMonitor (pravara-api)
- Targets: Pods matching `app: pravara-api`
- Scrape interval: 30s
- Endpoint: `http` port at `/metrics`

### PodMonitor (telemetry-worker)
- Targets: Pods matching `app: telemetry-worker`
- Scrape interval: 30s
- Endpoint: `metrics` port (4502) at `/metrics`

### ServiceMonitor (centrifugo)
- Targets: Pods matching `app: centrifugo`
- Scrape interval: 30s
- Endpoint: `internal` port at `/metrics`

## AlertManager Rules

### Critical Alerts

#### PravaraAPIHighErrorRate
- **Condition**: >5% 5xx errors for 5 minutes
- **Action**: Immediate investigation required
- **Impact**: Users experiencing service failures

#### PravaraAPIDown
- **Condition**: API unreachable for 1 minute
- **Action**: Check pod status, logs, and infrastructure
- **Impact**: Complete service outage

#### TelemetryWorkerMQTTDisconnected
- **Condition**: MQTT disconnected for 2 minutes
- **Action**: Check MQTT broker and network connectivity
- **Impact**: No telemetry data ingestion

#### TelemetryWorkerBatchWriteFailures
- **Condition**: >0.1 failures/second for 2 minutes
- **Action**: Check database connectivity and schema
- **Impact**: Data loss risk

#### TelemetryWorkerDown
- **Condition**: Worker unreachable for 1 minute
- **Action**: Check pod status and deployment
- **Impact**: No telemetry processing

#### CentrifugoDown
- **Condition**: Centrifugo unreachable for 1 minute
- **Action**: Check pod and service configuration
- **Impact**: No real-time updates to clients

### Warning Alerts

#### PravaraAPIHighLatency
- **Condition**: P95 latency >2s for 5 minutes
- **Action**: Investigate slow queries and performance bottlenecks
- **Impact**: Degraded user experience

#### PravaraAPIHighDBConnectionUsage
- **Condition**: >80% connection pool usage for 5 minutes
- **Action**: Consider scaling connection pool or optimizing queries
- **Impact**: Potential connection exhaustion

#### TelemetryWorkerQueueBacklog
- **Condition**: >1000 messages queued for 5 minutes
- **Action**: Check processing performance and consider scaling
- **Impact**: Increased data latency

#### TelemetryWorkerHighMessageDropRate
- **Condition**: >0.01 drops/second for 5 minutes
- **Action**: Investigate drop reasons and fix root cause
- **Impact**: Data loss

#### TelemetryWorkerBatchWriteLatency
- **Condition**: P95 batch write >1s for 5 minutes
- **Action**: Optimize database performance or batch size
- **Impact**: Reduced throughput

#### CentrifugoHighConnectionCount
- **Condition**: >10,000 connections for 5 minutes
- **Action**: Monitor for capacity limits, consider scaling
- **Impact**: Potential resource exhaustion

## Deployment

### Apply Observability Configuration

```bash
# Apply all observability resources
kubectl apply -k infra/k8s/base/observability/

# Verify ServiceMonitors
kubectl get servicemonitor -n pravara-mes

# Verify PodMonitor
kubectl get podmonitor -n pravara-mes

# Verify PrometheusRule
kubectl get prometheusrule -n pravara-mes
```

### Verify Metrics Endpoints

```bash
# Port-forward to pravara-api
kubectl port-forward -n pravara-mes svc/pravara-api 4500:4500
curl http://localhost:4500/metrics

# Port-forward to telemetry-worker pod
kubectl port-forward -n pravara-mes pod/<telemetry-worker-pod> 4502:4502
curl http://localhost:4502/metrics
curl http://localhost:4502/health
```

## Grafana Dashboard Queries

### API Performance

```promql
# Request rate by endpoint
sum(rate(pravara_api_http_requests_total[5m])) by (method, path)

# Error rate
sum(rate(pravara_api_http_requests_total{status=~"5.."}[5m]))
  / sum(rate(pravara_api_http_requests_total[5m]))

# P95 latency
histogram_quantile(0.95,
  sum(rate(pravara_api_http_request_duration_seconds_bucket[5m])) by (le, path)
)

# Concurrent requests
pravara_api_http_requests_in_flight
```

### Database Health

```promql
# Connection pool utilization
pravara_api_db_connections_in_use / pravara_api_db_connections_open

# Connection wait time
rate(pravara_api_db_connections_wait_duration_seconds_total[5m])
```

### Telemetry Worker

```promql
# Message throughput
rate(pravara_telemetry_mqtt_messages_processed_total[5m])

# Message drop rate
rate(pravara_telemetry_mqtt_messages_dropped_total[5m])

# Batch write performance
histogram_quantile(0.95,
  sum(rate(pravara_telemetry_batch_write_duration_seconds_bucket[5m])) by (le)
)

# Queue depth
pravara_telemetry_worker_queue_length
```

## Dependencies Added

### pravara-api
```
github.com/prometheus/client_golang v1.23.2
```

### telemetry-worker
```
github.com/prometheus/client_golang v1.23.2
```

## Testing

### Local Testing

```bash
# Start pravara-api
cd apps/pravara-api
go run cmd/api/main.go

# Check metrics
curl http://localhost:4500/metrics | grep pravara_api

# Start telemetry-worker
cd apps/telemetry-worker
go run cmd/worker/main.go

# Check metrics
curl http://localhost:4502/metrics | grep pravara_telemetry
curl http://localhost:4502/health
```

### Kubernetes Testing

```bash
# Check ServiceMonitor targets in Prometheus UI
kubectl port-forward -n monitoring svc/prometheus-k8s 9090:9090

# Navigate to http://localhost:9090/targets
# Verify pravara-api, telemetry-worker, and centrifugo targets are UP

# Check alerts
# Navigate to http://localhost:9090/alerts
# Verify PravaraMES alert rules are loaded
```

## Performance Impact

### Metrics Collection Overhead
- **HTTP middleware**: <1ms per request
- **DB stats collection**: ~5ms every 15 seconds
- **Memory overhead**: ~2-5MB per service for Prometheus registry

### Cardinality Considerations
- Path labels use route templates (e.g., `/api/v1/machines/:id`) to prevent explosion
- Topic labels extract root topic only
- Status codes grouped (2xx, 4xx, 5xx)
- `tenant_id` label is bounded by the number of active tenants
- Estimated cardinality: <500 unique metric series per service per tenant

## Security Considerations

1. **Metrics Endpoint Access**
   - Currently unauthenticated (standard for Prometheus scraping)
   - Should be on internal network only
   - Not exposed via Ingress

2. **Sensitive Data**
   - No user PII in metrics labels
   - No authentication tokens in metrics
   - Query parameters excluded from path labels

3. **Resource Limits**
   - Set appropriate scrape intervals (30s default)
   - Monitor Prometheus storage growth
   - Configure retention policies

## Troubleshooting

### Metrics Not Appearing

1. Check service is running: `kubectl get pods -n pravara-mes`
2. Verify metrics endpoint: `kubectl port-forward ... && curl .../metrics`
3. Check ServiceMonitor/PodMonitor: `kubectl describe servicemonitor -n pravara-mes`
4. Verify Prometheus targets: Check Prometheus UI `/targets`

### High Cardinality Issues

1. Check unique series: `prometheus_tsdb_symbol_table_size_bytes`
2. Audit metric labels for unbounded values
3. Use `topk()` to identify high-cardinality metrics
4. Consider aggregating or dropping labels

### Alert Not Firing

1. Verify PrometheusRule loaded: `kubectl get prometheusrule -n pravara-mes`
2. Check alert expression in Prometheus UI
3. Verify AlertManager configuration
4. Check notification channels

## Future Enhancements

1. **Distributed Tracing**
   - OpenTelemetry integration
   - Jaeger or Tempo backend
   - Request correlation across services

2. **Custom Business Metrics**
   - Order completion rates
   - Production throughput
   - Machine utilization percentages

3. **SLI/SLO Tracking**
   - Define service level indicators
   - Track error budgets
   - Automated SLO reporting

4. **Log Aggregation Integration**
   - Correlate metrics with logs
   - LogQL queries for Loki
   - Unified observability dashboard

## References

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Prometheus Go Client](https://github.com/prometheus/client_golang)
- [Prometheus Operator](https://prometheus-operator.dev/)
- [Grafana Dashboards](https://grafana.com/docs/)
