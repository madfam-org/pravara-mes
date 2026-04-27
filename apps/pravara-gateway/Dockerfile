# =============================================================================
# PravaraMES Gateway Dockerfile (Centrifugo)
# Real-time WebSocket gateway for live updates
# =============================================================================

FROM centrifugo/centrifugo:v5

# Refresh alpine system packages to pull latest security patches
# (zlib, musl, busybox CVEs flagged by Trivy on the upstream image baseline).
# Switch to root temporarily for apk operations, then drop privileges.
USER root
RUN apk update && apk upgrade --no-cache && rm -rf /var/cache/apk/*

# Copy configuration
COPY config.json /centrifugo/config.json

# Drop back to the centrifugo non-root user (uid 1000) for runtime after apk upgrade
USER centrifugo

# Expose WebSocket and internal ports
EXPOSE 8000 9000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8000/health || exit 1

# Run Centrifugo with config
CMD ["centrifugo", "-c", "/centrifugo/config.json"]
