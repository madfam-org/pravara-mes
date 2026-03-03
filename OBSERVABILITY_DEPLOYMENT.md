# Observability Deployment Checklist

## Pre-Deployment Verification

### 1. Code Compilation
```bash
# Verify pravara-api builds
cd apps/pravara-api
go build ./cmd/api/main.go
rm main

# Verify telemetry-worker builds
cd apps/telemetry-worker
go build ./cmd/worker/main.go
rm main
```

### 2. Kubernetes Manifests Validation
```bash
# Validate observability manifests
kubectl apply -k infra/k8s/base/observability/ --dry-run=client

# Check for syntax errors
kubectl apply -k infra/k8s/base/ --dry-run=client
```

## Deployment Steps

### Step 1: Deploy Updated Services

```bash
# Rebuild and push Docker images with new observability code
# (Adjust registry and tags as needed)

# For pravara-api
cd apps/pravara-api
docker build -t <your-registry>/pravara-api:latest .
docker push <your-registry>/pravara-api:latest

# For telemetry-worker
cd apps/telemetry-worker
docker build -t <your-registry>/telemetry-worker:latest .
docker push <your-registry>/telemetry-worker:latest

# Rolling update deployments
kubectl rollout restart deployment/pravara-api -n pravara-mes
kubectl rollout restart deployment/telemetry-worker -n pravara-mes

# Wait for rollout completion
kubectl rollout status deployment/pravara-api -n pravara-mes
kubectl rollout status deployment/telemetry-worker -n pravara-mes
```

### Step 2: Deploy Observability Resources

```bash
# Apply observability manifests
kubectl apply -k infra/k8s/base/observability/

# Verify resources created
kubectl get servicemonitor -n pravara-mes
kubectl get podmonitor -n pravara-mes
kubectl get prometheusrule -n pravara-mes
```

### Step 3: Verify Metrics Endpoints

```bash
# Test pravara-api metrics endpoint
kubectl port-forward -n pravara-mes svc/pravara-api 4500:4500 &
curl -s http://localhost:4500/metrics | grep pravara_api | head -5

# Test telemetry-worker metrics endpoint
WORKER_POD=$(kubectl get pods -n pravara-mes -l app=telemetry-worker -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward -n pravara-mes pod/$WORKER_POD 4502:4502 &
curl -s http://localhost:4502/metrics | grep pravara_telemetry | head -5
curl -s http://localhost:4502/health

# Clean up port forwards
pkill -f "port-forward"
```

### Step 4: Verify Prometheus Integration

```bash
# Port forward to Prometheus
kubectl port-forward -n monitoring svc/prometheus-k8s 9090:9090 &

# Open browser to http://localhost:9090/targets
# Verify these targets are UP:
# - pravara-mes/pravara-api/0
# - pravara-mes/telemetry-worker/0
# - pravara-mes/centrifugo/0

# Check alerts loaded
# Navigate to http://localhost:9090/alerts
# Verify PravaraMES alert rules are present

# Test metric queries in Prometheus UI
# Try these queries:
# - pravara_api_http_requests_total
# - pravara_telemetry_mqtt_connection_status
# - rate(pravara_api_http_requests_total[5m])
```

### Step 5: Deploy Grafana Dashboard ConfigMaps

```bash
# Apply Grafana dashboard ConfigMaps
kubectl apply -k infra/k8s/base/observability/grafana-dashboards/

# Verify ConfigMaps created
kubectl get configmap -n pravara-mes -l app.kubernetes.io/component=grafana-dashboards

# The Grafana sidecar auto-discovers ConfigMaps with the grafana_dashboard label.
# Three dashboards are deployed:
#   - api-overview: Request rate, error rate, latency (P50/P95/P99), DB pool
#   - telemetry-pipeline: MQTT throughput, batch writes, queue depth
#   - realtime-gateway: Centrifugo connections, message throughput
```

### Step 6: Verify Grafana Dashboards

```bash
# Port forward to Grafana
kubectl port-forward -n monitoring svc/grafana 3000:3000 &

# Navigate to http://localhost:3000
# The three PravaraMES dashboards should appear automatically under
# the folder configured by the Grafana sidecar.
# Verify each dashboard loads and displays data from Prometheus.
```

## Post-Deployment Validation

### 1. Functional Testing

```bash
# Generate API traffic
for i in {1..100}; do
  curl -s http://localhost:4500/api/v1/health > /dev/null
done

# Check metrics reflect traffic
curl -s http://localhost:4500/metrics | grep pravara_api_http_requests_total
```

### 2. Alert Testing

```bash
# Verify alerts can fire (optional - test in non-prod first)
# Stop a service temporarily to trigger down alert
kubectl scale deployment/pravara-api --replicas=0 -n pravara-mes

# Wait 1-2 minutes and check Prometheus alerts
# Navigate to http://localhost:9090/alerts
# Should see PravaraAPIDown alert firing

# Restore service
kubectl scale deployment/pravara-api --replicas=2 -n pravara-mes
```

### 3. Performance Validation

```bash
# Check metrics collection overhead
# Before: Note baseline CPU/memory from `kubectl top pods`
kubectl top pods -n pravara-mes

# Generate load and verify metrics overhead is acceptable (<5% CPU increase)
```

## Rollback Plan

If issues occur during deployment:

```bash
# Rollback deployments
kubectl rollout undo deployment/pravara-api -n pravara-mes
kubectl rollout undo deployment/telemetry-worker -n pravara-mes

# Remove observability resources (optional)
kubectl delete -k infra/k8s/base/observability/

# Verify services restored
kubectl get pods -n pravara-mes
kubectl logs -n pravara-mes deployment/pravara-api --tail=50
kubectl logs -n pravara-mes deployment/telemetry-worker --tail=50
```

## Common Issues and Solutions

### Issue: ServiceMonitor targets not appearing in Prometheus

**Solution:**
```bash
# Check ServiceMonitor label selectors match service labels
kubectl get svc pravara-api -n pravara-mes --show-labels
kubectl describe servicemonitor pravara-api -n pravara-mes

# Verify Prometheus is configured to discover ServiceMonitors
kubectl get prometheus -n monitoring -o yaml | grep serviceMonitorSelector
```

### Issue: Metrics endpoint returns 404

**Solution:**
```bash
# Verify service is using new image with observability code
kubectl describe pod <pod-name> -n pravara-mes | grep Image

# Check pod logs for startup errors
kubectl logs <pod-name> -n pravara-mes

# Verify metrics endpoint registered
kubectl exec -it <pod-name> -n pravara-mes -- wget -O- http://localhost:4500/metrics
```

### Issue: High memory usage after deployment

**Solution:**
```bash
# Check metric cardinality
curl -s http://localhost:9090/api/v1/query?query=count(pravara_api_http_requests_total)

# If cardinality is high, review label usage
# Consider aggregating or dropping high-cardinality labels

# Restart Prometheus if needed
kubectl rollout restart statefulset/prometheus-k8s -n monitoring
```

### Issue: Alerts not firing

**Solution:**
```bash
# Verify PrometheusRule created
kubectl get prometheusrule pravara-mes-alerts -n pravara-mes -o yaml

# Check Prometheus alert rule status
# Navigate to http://localhost:9090/alerts and click on alert
# Check "Expression" tab for evaluation errors

# Verify AlertManager configuration
kubectl get secret alertmanager-main -n monitoring -o yaml
```

## Monitoring the Monitoring

Once deployed, monitor these metrics about the observability system itself:

```promql
# Prometheus scrape success rate
up{job=~"pravara-.*"}

# Prometheus scrape duration
scrape_duration_seconds{job=~"pravara-.*"}

# Alert evaluation failures
prometheus_rule_evaluation_failures_total

# Time series count (cardinality monitoring)
count(pravara_api_http_requests_total)
count(pravara_telemetry_mqtt_messages_received_total)
```

## Success Criteria

Deployment is successful when:

- [ ] All pods are running and healthy
- [ ] Metrics endpoints return valid Prometheus data
- [ ] Prometheus shows all targets as UP
- [ ] Alert rules loaded in Prometheus
- [ ] Grafana dashboard ConfigMaps deployed and auto-discovered
- [ ] Grafana can query PravaraMES metrics
- [ ] No significant performance degradation
- [ ] Logs show no observability-related errors
- [ ] Memory usage within acceptable limits
- [ ] Alerts can fire and notify correctly (test in non-prod)

## Next Steps

After successful deployment:

1. Create Grafana dashboards for operational teams
2. Configure AlertManager notification channels (Slack, PagerDuty, etc.)
3. Document runbooks for each alert
4. Set up SLO tracking and error budget monitoring
5. Train operations team on new observability tools
6. Schedule review of alert thresholds after 1 week of production data

## Support

For issues or questions:
- Review logs: `kubectl logs -n pravara-mes <pod-name>`
- Check metrics: `curl http://<service>:<port>/metrics`
- Prometheus UI: Port-forward and access at http://localhost:9090
- Documentation: See OBSERVABILITY.md for detailed information
