# Pravara edge node (machine adapter as a Sparkplug B Edge Node) for site boxes.
# Build from the repository root:
#   docker build -f deploy/edge/Dockerfile -t pravara-edge .
FROM golang:1.25.14-alpine AS builder
WORKDIR /src
COPY packages/sparkplug/ ./packages/sparkplug/
COPY packages/sdk-go/ ./packages/sdk-go/
COPY apps/machine-adapter/ ./apps/machine-adapter/
WORKDIR /src/apps/machine-adapter
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
RUN GOWORK=off CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-w -s -X main.version=${VERSION}" -o /out/pravara-edge ./cmd/adapter

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S pravara && adduser -S -G pravara -H -s /sbin/nologin pravara \
 && mkdir -p /etc/pravara-edge /var/lib/pravara-edge \
 && chown pravara:pravara /var/lib/pravara-edge
COPY --from=builder /out/pravara-edge /usr/local/bin/pravara-edge
USER pravara
VOLUME ["/var/lib/pravara-edge"]
EXPOSE 4503
ENTRYPOINT ["/usr/local/bin/pravara-edge"]
CMD ["-config", "/etc/pravara-edge/config.yaml"]
