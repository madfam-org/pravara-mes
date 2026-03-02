# =============================================================================
# PravaraMES API Dockerfile
# Multi-stage build for optimized production image
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: Build
# -----------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go workspace files
COPY go.work go.work.sum* ./

# Copy module files for dependency caching
COPY apps/pravara-api/go.mod apps/pravara-api/go.sum* ./apps/pravara-api/
COPY packages/sdk-go/go.mod packages/sdk-go/go.sum* ./packages/sdk-go/

# Download dependencies
RUN cd apps/pravara-api && go mod download

# Copy source code
COPY apps/pravara-api/ ./apps/pravara-api/
COPY packages/sdk-go/ ./packages/sdk-go/

# Build the binary
RUN cd apps/pravara-api && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /build/pravara-api ./cmd/api

# -----------------------------------------------------------------------------
# Stage 2: Production
# -----------------------------------------------------------------------------
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 pravara && \
    adduser -u 1000 -G pravara -s /bin/sh -D pravara

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/pravara-api /app/pravara-api

# Copy migrations
COPY --from=builder /build/apps/pravara-api/internal/db/migrations /app/migrations

# Set ownership
RUN chown -R pravara:pravara /app

# Switch to non-root user
USER pravara

# Expose port
EXPOSE 4500

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:4500/health/live || exit 1

# Run the binary
ENTRYPOINT ["/app/pravara-api"]
