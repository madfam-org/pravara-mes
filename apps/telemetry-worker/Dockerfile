# =============================================================================
# PravaraMES Telemetry Worker Dockerfile
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
COPY apps/telemetry-worker/go.mod apps/telemetry-worker/go.sum* ./apps/telemetry-worker/
COPY packages/sdk-go/go.mod packages/sdk-go/go.sum* ./packages/sdk-go/

# Download dependencies
RUN cd apps/telemetry-worker && go mod download

# Copy source code
COPY apps/telemetry-worker/ ./apps/telemetry-worker/
COPY packages/sdk-go/ ./packages/sdk-go/

# Build the binary
RUN cd apps/telemetry-worker && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /build/telemetry-worker ./cmd/worker

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
COPY --from=builder /build/telemetry-worker /app/telemetry-worker

# Set ownership
RUN chown -R pravara:pravara /app

# Switch to non-root user
USER pravara

# Expose metrics port (if needed)
EXPOSE 4502

# Health check using simple process check
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD pgrep telemetry-worker || exit 1

# Run the binary
ENTRYPOINT ["/app/telemetry-worker"]
