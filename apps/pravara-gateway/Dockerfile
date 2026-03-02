# =============================================================================
# PravaraMES Gateway Dockerfile (Centrifugo)
# Real-time WebSocket gateway for live updates
# =============================================================================

FROM centrifugo/centrifugo:v5

# Copy configuration
COPY config.json /centrifugo/config.json

# Expose WebSocket and internal ports
EXPOSE 8000 9000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8000/health || exit 1

# Run Centrifugo with config
CMD ["centrifugo", "-c", "/centrifugo/config.json"]
