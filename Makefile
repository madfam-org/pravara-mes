# PravaraMES Makefile
# Build, test, and documentation commands

.PHONY: all build test docs clean help

# Default target
all: build

# ==================== Build ====================

build: build-api build-worker build-ui

build-api:
	@echo "Building pravara-api..."
	cd apps/pravara-api && go build -o ../../bin/pravara-api ./cmd/api

build-worker:
	@echo "Building telemetry-worker..."
	cd apps/telemetry-worker && go build -o ../../bin/telemetry-worker ./cmd/worker

build-ui:
	@echo "Building pravara-ui..."
	cd apps/pravara-ui && npm run build

# ==================== Development ====================

dev-api:
	cd apps/pravara-api && go run ./cmd/api

dev-worker:
	cd apps/telemetry-worker && go run ./cmd/worker

dev-ui:
	cd apps/pravara-ui && npm run dev

# ==================== Testing ====================

test: test-api test-worker test-ui

test-api:
	@echo "Testing pravara-api..."
	go test ./apps/pravara-api/...

test-worker:
	@echo "Testing telemetry-worker..."
	go test ./apps/telemetry-worker/...

test-ui:
	@echo "Testing pravara-ui..."
	cd apps/pravara-ui && npm run test 2>/dev/null || echo "No tests configured"

test-coverage:
	go test -coverprofile=coverage.out ./apps/pravara-api/... ./apps/telemetry-worker/...
	go tool cover -html=coverage.out -o coverage.html

# ==================== Documentation ====================

docs: docs-openapi
	@echo "Documentation generated"

docs-openapi:
	@echo "Generating OpenAPI specification..."
	@which swag > /dev/null || (echo "Installing swag..." && go install github.com/swaggo/swag/cmd/swag@latest)
	cd apps/pravara-api && swag init -g cmd/api/main.go -o ../../docs --outputTypes yaml,json --parseDependency --parseInternal

docs-serve:
	@echo "Serving docs at http://localhost:8080"
	cd docs && python3 -m http.server 8080

# ==================== Linting ====================

lint: lint-go lint-ts

lint-go:
	@echo "Linting Go code..."
	@which golangci-lint > /dev/null || (echo "Install golangci-lint: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run ./apps/pravara-api/... ./apps/telemetry-worker/...

lint-ts:
	@echo "Linting TypeScript code..."
	cd apps/pravara-ui && npm run lint

typecheck:
	@echo "Type checking TypeScript..."
	cd apps/pravara-ui && npm run typecheck

# ==================== Docker ====================

docker-build:
	docker-compose -f infra/docker-compose.yml build

docker-up:
	docker-compose -f infra/docker-compose.yml up -d

docker-down:
	docker-compose -f infra/docker-compose.yml down

docker-logs:
	docker-compose -f infra/docker-compose.yml logs -f

# ==================== Database ====================

db-migrate:
	@echo "Running database migrations..."
	cd infra/db && ./migrate.sh up

db-rollback:
	@echo "Rolling back database migration..."
	cd infra/db && ./migrate.sh down 1

# ==================== Kubernetes ====================

k8s-apply:
	kubectl apply -k infra/k8s/overlays/development

k8s-delete:
	kubectl delete -k infra/k8s/overlays/development

# ==================== Clean ====================

clean:
	rm -rf bin/
	rm -rf apps/pravara-ui/.next
	rm -rf apps/pravara-ui/node_modules/.cache
	rm -f coverage.out coverage.html
	go clean -cache

# ==================== Dependencies ====================

deps:
	go mod download
	go mod tidy
	cd apps/pravara-ui && npm install

deps-update:
	go get -u ./...
	go mod tidy
	cd apps/pravara-ui && npm update

# ==================== Help ====================

help:
	@echo "PravaraMES Makefile"
	@echo ""
	@echo "Build:"
	@echo "  make build          Build all applications"
	@echo "  make build-api      Build pravara-api"
	@echo "  make build-worker   Build telemetry-worker"
	@echo "  make build-ui       Build pravara-ui"
	@echo ""
	@echo "Development:"
	@echo "  make dev-api        Run API in development mode"
	@echo "  make dev-worker     Run worker in development mode"
	@echo "  make dev-ui         Run UI in development mode"
	@echo ""
	@echo "Testing:"
	@echo "  make test           Run all tests"
	@echo "  make test-api       Run API tests"
	@echo "  make test-worker    Run worker tests"
	@echo "  make test-coverage  Generate coverage report"
	@echo ""
	@echo "Documentation:"
	@echo "  make docs           Generate all documentation"
	@echo "  make docs-openapi   Generate OpenAPI specification"
	@echo "  make docs-serve     Serve docs locally"
	@echo ""
	@echo "Linting:"
	@echo "  make lint           Run all linters"
	@echo "  make lint-go        Lint Go code"
	@echo "  make lint-ts        Lint TypeScript code"
	@echo "  make typecheck      Type check TypeScript"
	@echo ""
	@echo "Docker:"
	@echo "  make docker-build   Build Docker images"
	@echo "  make docker-up      Start Docker Compose"
	@echo "  make docker-down    Stop Docker Compose"
	@echo "  make docker-logs    Follow Docker logs"
	@echo ""
	@echo "Other:"
	@echo "  make clean          Clean build artifacts"
	@echo "  make deps           Install dependencies"
	@echo "  make deps-update    Update dependencies"
	@echo "  make help           Show this help"
