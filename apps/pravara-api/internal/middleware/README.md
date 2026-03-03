# Middleware

HTTP middleware for authentication, rate limiting, and observability.

## Overview

Middleware components applied to the Gin router:

| Middleware | Purpose |
|------------|---------|
| `Auth` | JWT validation and tenant extraction |
| `RateLimiter` | Request rate limiting per tenant |
| `Metrics` | Prometheus request metrics |
| `UsageTracking` | Billing usage recording |

## Authentication

### JWT Validation

```go
protected := v1.Group("")
protected.Use(middleware.Auth(cfg.Auth))
```

The auth middleware:
1. Extracts Bearer token from Authorization header
2. Validates JWT signature and expiry
3. Extracts tenant_id from token claims
4. Sets tenant context for downstream handlers

### Tenant Extraction

```go
func GetTenantID(c *gin.Context) (string, bool) {
    tenantID, exists := c.Get("tenant_id")
    if !exists {
        return "", false
    }
    return tenantID.(uuid.UUID).String(), true
}
```

The extracted `tenant_id` is also propagated to Prometheus metric labels by the Metrics middleware, enabling per-tenant request rate, latency, and error tracking.

### User Context

```go
userID, _ := c.Get("user_id")
email, _ := c.Get("user_email")
name, _ := c.Get("user_name")
```

## Rate Limiting

Token bucket rate limiting per tenant:

```go
router.Use(middleware.RateLimiter(log))
```

Configuration via environment:
- `RATE_LIMIT_REQUESTS` - Requests per window (default: 1000)
- `RATE_LIMIT_WINDOW` - Window duration (default: 1m)

## Metrics

Prometheus metrics collection:

```go
router.Use(middleware.Metrics())
```

Exposed metrics:
- `http_requests_total` - Request count by path, method, status
- `http_request_duration_seconds` - Request latency histogram

## Usage Tracking

Billing usage recording:

```go
if usageRecorder != nil {
    router.Use(middleware.UsageTracking(usageRecorder, log))
}
```

Tracks:
- API requests per tenant
- Request metadata for billing

## Files

| File | Description |
|------|-------------|
| `auth.go` | JWT validation middleware |
| `rate_limit.go` | Rate limiting |
| `metrics.go` | Prometheus metrics |
| `usage.go` | Billing tracking |
| `context.go` | Context helpers |

## Usage Pattern

```go
// In routes.go
func RegisterRoutes(r *gin.Engine, cfg *config.Config, log *logrus.Logger) {
    // Global middleware
    r.Use(gin.Recovery())
    r.Use(requestLogger(log))
    r.Use(middleware.RateLimiter(log))
    r.Use(middleware.Metrics())

    // Unprotected routes
    r.GET("/health", health.Health)

    // Protected routes
    protected := r.Group("/v1")
    protected.Use(middleware.Auth(cfg.Auth))

    protected.GET("/tasks", tasks.List)
}
```
