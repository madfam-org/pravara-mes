# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o video-streaming ./cmd/server

# Final stage
FROM alpine:3.19

# Install runtime dependencies including FFmpeg
RUN apk --no-cache add ca-certificates tzdata ffmpeg

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/video-streaming .

# Create directories for recordings
RUN mkdir -p /app/recordings

# Create non-root user
RUN adduser -D -u 1000 appuser && \
    chown -R appuser:appuser /app
USER appuser

# Expose ports (WebRTC signaling and media)
EXPOSE 4506
EXPOSE 50000-50100/udp

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:4506/health || exit 1

# Run the binary
CMD ["./video-streaming"]