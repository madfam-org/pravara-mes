# PravaraMES Kubernetes Security Hardening

This directory contains comprehensive security hardening configurations for the PravaraMES multi-tenant manufacturing execution system.

## Overview

The security implementation follows defense-in-depth principles with multiple layers:

1. **Network Policies** - Zero-trust network segmentation
2. **RBAC** - Principle of least privilege access control
3. **Pod Security Standards** - Container runtime security constraints
4. **Rate Limiting** - Application-level DDoS protection

## Network Policies

### Default Deny All
All ingress and egress traffic is blocked by default (`default-deny-all`). Only explicitly allowed connections are permitted.

### Allowed Traffic Flows

#### pravara-api (port 4500)
**Ingress:**
- ← Ingress controller (ingress-nginx namespace) - External HTTP/HTTPS traffic
- ← pravara-ui - Internal API calls
- ← Monitoring (monitoring namespace) - Prometheus metrics scraping

**Egress:**
- → PostgreSQL (port 5432) - Database queries
- → Redis (port 6379) - Caching and session storage
- → Centrifugo (port 8000) - Real-time event publishing

#### pravara-ui (port 4501)
**Ingress:**
- ← Ingress controller - External HTTP/HTTPS traffic

**Egress:**
- → pravara-api (port 4500) - API calls

#### telemetry-worker (port 4502)
**Ingress:**
- None (worker service, no inbound connections)

**Egress:**
- → PostgreSQL (port 5432) - Telemetry data storage
- → Redis (port 6379) - Queue management
- → EMQX (port 1883) - MQTT message consumption
- → Centrifugo (port 8000) - Real-time event publishing

#### centrifugo (port 8000)
**Ingress:**
- ← pravara-api - Event publishing
- ← telemetry-worker - Event publishing
- ← Ingress controller - WebSocket client connections
- ← Monitoring - Metrics scraping

**Egress:**
- → Redis (port 6379) - Message broker backend

#### Infrastructure Services
**PostgreSQL:**
- Ingress from: pravara-api, telemetry-worker

**Redis:**
- Ingress from: pravara-api, centrifugo, telemetry-worker

**EMQX:**
- Ingress from: telemetry-worker, ingress controller

#### DNS
All pods are allowed to resolve DNS queries (UDP/TCP port 53) to kube-system namespace.

## RBAC (Role-Based Access Control)

### Service Accounts
Each service has a dedicated service account with minimal permissions:

- `pravara-api-sa` - For pravara-api pods
- `pravara-ui-sa` - For pravara-ui pods
- `telemetry-worker-sa` - For telemetry-worker pods
- `centrifugo-sa` - For centrifugo pods
- `postgres-sa` - For PostgreSQL pods
- `redis-sa` - For Redis pods
- `emqx-sa` - For EMQX pods

### Roles
**Application Role** (`pravara-app-role`):
- Get/list ConfigMaps - Read configuration
- Get/list Secrets - Read credentials
- Get/list Pods - Health checks and graceful shutdown

**Infrastructure Role** (`pravara-infra-role`):
- Get/list ConfigMaps - Read configuration
- Get/list Pods - Self-monitoring

### No Cluster-Wide Permissions
All permissions are scoped to the `pravara-mes` namespace only. No ClusterRole or ClusterRoleBinding is used.

## Pod Security Standards

### Enforcement Level: Restricted
The namespace enforces the **restricted** Pod Security Standard, which is the most restrictive policy.

### Required Pod Security Context
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000
  seccompProfile:
    type: RuntimeDefault
```

### Required Container Security Context
```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  runAsNonRoot: true
  runAsUser: 1000
  capabilities:
    drop:
      - ALL
```

### Writable Filesystem
For containers requiring writable directories (tmp, cache, logs), use emptyDir volumes:

```yaml
volumes:
  - name: tmp
    emptyDir: {}
  - name: cache
    emptyDir: {}

volumeMounts:
  - name: tmp
    mountPath: /tmp
  - name: cache
    mountPath: /app/cache
```

## Rate Limiting (Application Layer)

### Implementation
The pravara-api includes middleware-based rate limiting using token bucket algorithm.

### Configuration
Environment variables for rate limiting:

```bash
RATELIMIT_ENABLED=true                   # Enable/disable (default: true)
RATELIMIT_IP_PER_MINUTE=100              # Max requests per IP per minute
RATELIMIT_TENANT_PER_MINUTE=1000         # Max requests per tenant per minute
RATELIMIT_BURST=20                       # Burst allowance
```

### Rate Limit Levels

1. **Per-IP Rate Limiting**
   - Default: 100 requests per minute per IP address
   - Prevents single source from overwhelming the API
   - Returns 429 Too Many Requests when exceeded

2. **Per-Tenant Rate Limiting**
   - Default: 1000 requests per minute per tenant
   - Tenant ID extracted from JWT claims
   - Prevents individual tenant from consuming excessive resources
   - Multi-tenant fairness guarantee

### Response Format
When rate limit is exceeded:
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many requests. Please try again later.",
  "retry_after_seconds": 60
}
```

HTTP Headers:
- Status: `429 Too Many Requests`
- `Retry-After: 60` - Suggests retry delay in seconds

### Automatic Cleanup
Inactive rate limiters are automatically cleaned up every 5 minutes to prevent memory leaks.

## Deployment Integration

### Apply Security Resources
```bash
kubectl apply -k infra/k8s/base/security/
```

### Verify Network Policies
```bash
kubectl get networkpolicies -n pravara-mes
kubectl describe networkpolicy -n pravara-mes
```

### Verify RBAC
```bash
kubectl get serviceaccounts -n pravara-mes
kubectl get roles,rolebindings -n pravara-mes
```

### Test Rate Limiting
```bash
# Test IP-based rate limiting
for i in {1..150}; do
  curl -w "%{http_code}\n" -o /dev/null -s https://api.pravara.madfam.io/health
done

# Should see 200 responses then 429 after limit exceeded
```

### Verify Pod Security Standards
```bash
kubectl get namespace pravara-mes -o yaml | grep pod-security
```

## Security Best Practices

### Container Images
1. Use specific image tags, never `:latest`
2. Scan images for vulnerabilities before deployment
3. Prefer distroless or minimal base images
4. Regularly update base images for security patches

### Secrets Management
1. Store all secrets in Kubernetes Secrets
2. Use external secret management (e.g., Vault) for sensitive data
3. Rotate secrets regularly
4. Never commit secrets to version control

### Monitoring and Auditing
1. Enable Kubernetes audit logging
2. Monitor security events and anomalies
3. Alert on NetworkPolicy violations
4. Track rate limiting metrics
5. Review RBAC permissions regularly

### High Availability
1. Implement pod disruption budgets
2. Use multiple replicas for critical services
3. Configure liveness and readiness probes
4. Implement graceful shutdown handling

### PostgreSQL Row-Level Security (RLS)
The application enforces multi-tenant isolation at the database level:
- Each tenant's data is isolated using PostgreSQL RLS policies
- Tenant ID is extracted from JWT claims
- Database connection includes tenant context
- Prevents cross-tenant data access even with SQL injection

## Compliance Considerations

This security implementation addresses:

- **CIS Kubernetes Benchmarks** - Network policies, RBAC, pod security
- **NIST Cybersecurity Framework** - Defense-in-depth, least privilege
- **OWASP Top 10** - Rate limiting, access control, security misconfiguration
- **SOC 2** - Access controls, audit logging, network segmentation
- **ISO 27001** - Information security controls

## Incident Response

### Rate Limit Exceeded
1. Review logs for client IP and tenant ID
2. Determine if traffic is legitimate or attack
3. Adjust rate limits if needed for legitimate spikes
4. Block malicious IPs at ingress controller level

### NetworkPolicy Violation
1. Review pod logs for connection attempts
2. Check if legitimate service-to-service communication
3. Update network policies if needed
4. Investigate potential compromise if unauthorized

### RBAC Violation
1. Review audit logs for unauthorized access attempts
2. Verify service account permissions are correct
3. Investigate potential credential compromise
4. Rotate credentials if necessary

## Testing

### Network Policy Testing
```bash
# Test blocked traffic (should fail)
kubectl run -it --rm debug --image=nicolaka/netshoot -n pravara-mes -- \
  curl http://pravara-api:4500/health

# Test allowed traffic (should succeed)
kubectl run -it --rm debug --image=nicolaka/netshoot -n ingress-nginx -- \
  curl http://pravara-api.pravara-mes:4500/health
```

### RBAC Testing
```bash
# Verify service account can read configmaps
kubectl auth can-i get configmaps --as=system:serviceaccount:pravara-mes:pravara-api-sa -n pravara-mes

# Verify service account cannot delete pods
kubectl auth can-i delete pods --as=system:serviceaccount:pravara-mes:pravara-api-sa -n pravara-mes
```

### Pod Security Testing
```bash
# This should fail due to restricted PSS
kubectl run -it --rm privileged-test --image=nginx --privileged -n pravara-mes
```

## Maintenance

### Regular Security Reviews
- Monthly: Review and update network policies
- Quarterly: Audit RBAC permissions and service accounts
- Quarterly: Review rate limit thresholds and adjust based on usage
- Annually: Comprehensive security audit

### Updates and Patches
- Monitor security advisories for Kubernetes and dependencies
- Test security updates in staging environment
- Apply security patches promptly
- Maintain security changelog

## References

- [Kubernetes Network Policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [Kubernetes RBAC](https://kubernetes.io/docs/reference/access-authn-authz/rbac/)
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/)
- [Rate Limiting Best Practices](https://cloud.google.com/architecture/rate-limiting-strategies-techniques)
- [PostgreSQL Row-Level Security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)
