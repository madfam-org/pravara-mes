# =============================================================================
# PravaraMES Makefile
# =============================================================================

.PHONY: help dev build test lint clean migrate docker-up docker-down

# Default target
help:
	@echo "PravaraMES Development Commands"
	@echo "================================"
	@echo ""
	@echo "Development:"
	@echo "  make dev           - Start all services in development mode"
	@echo "  make dev-api       - Start only the API server"
	@echo "  make dev-ui        - Start only the UI server"
	@echo "  make dev-worker    - Start only the telemetry worker"
	@echo ""
	@echo "Build:"
	@echo "  make build         - Build all Go binaries"
	@echo "  make build-api     - Build the API binary"
	@echo "  make build-worker  - Build the telemetry worker binary"
	@echo ""
	@echo "Database:"
	@echo "  make migrate       - Run database migrations"
	@echo "  make migrate-down  - Rollback last migration"
	@echo "  make seed          - Seed database with sample data"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-up     - Start Docker Compose services"
	@echo "  make docker-down   - Stop Docker Compose services"
	@echo "  make docker-build  - Build Docker images"
	@echo ""
	@echo "Quality:"
	@echo "  make test          - Run all tests"
	@echo "  make lint          - Run linters"
	@echo "  make fmt           - Format code"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean         - Remove build artifacts"

# =============================================================================
# Development
# =============================================================================

dev: docker-up
	@echo "Starting development services..."
	@$(MAKE) -j3 dev-api dev-ui dev-worker

dev-api:
	@echo "Starting API server..."
	cd apps/pravara-api && go run ./cmd/api

dev-ui:
	@echo "Starting UI server..."
	cd apps/pravara-ui && pnpm dev

dev-worker:
	@echo "Starting telemetry worker..."
	cd apps/telemetry-worker && go run ./cmd/worker

# =============================================================================
# Build
# =============================================================================

build: build-api build-worker
	@echo "All binaries built successfully"

build-api:
	@echo "Building pravara-api..."
	cd apps/pravara-api && go build -o ../../bin/pravara-api ./cmd/api

build-worker:
	@echo "Building telemetry-worker..."
	cd apps/telemetry-worker && go build -o ../../bin/telemetry-worker ./cmd/worker

# =============================================================================
# Database
# =============================================================================

migrate:
	@echo "Running database migrations..."
	cd apps/pravara-api && go run ./cmd/api migrate up

migrate-down:
	@echo "Rolling back last migration..."
	cd apps/pravara-api && go run ./cmd/api migrate down 1

seed:
	@echo "Seeding database..."
	./scripts/seed-data.sh

# =============================================================================
# Docker
# =============================================================================

docker-up:
	@echo "Starting Docker Compose services..."
	docker compose up -d

docker-down:
	@echo "Stopping Docker Compose services..."
	docker compose down

docker-build:
	@echo "Building Docker images..."
	docker compose build

docker-logs:
	docker compose logs -f

# =============================================================================
# Quality
# =============================================================================

test:
	@echo "Running tests..."
	go test ./... -v -race -cover

lint:
	@echo "Running linters..."
	golangci-lint run ./...
	cd apps/pravara-ui && pnpm lint

fmt:
	@echo "Formatting code..."
	go fmt ./...
	cd apps/pravara-ui && pnpm format

# =============================================================================
# Cleanup
# =============================================================================

clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf apps/pravara-ui/.next/
	rm -rf apps/pravara-ui/node_modules/
	go clean -cache
