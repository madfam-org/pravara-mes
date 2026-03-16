# Contributing to Pravara MES

Thank you for your interest in contributing to Pravara MES! This guide covers everything you need to get started with our open-source Manufacturing Execution System.

## Prerequisites

- **Go** >= 1.24
- **Node.js** >= 20
- **pnpm** >= 9
- **Docker** >= 24

macOS:
```bash
brew install go node pnpm docker
```

## Project Structure

```
pravara-mes/
  apps/
    pravara-api/      # MES API (Go) - production tracking, IoT telemetry, MQTT
    pravara-ui/       # Operator dashboard (Next.js)
    pravara-admin/    # Admin console (Next.js)
  infra/
    k8s/              # Kubernetes manifests
```

## Local Development Setup

```bash
# 1. Clone the repo
git clone https://github.com/madfam-org/pravara-mes
cd pravara-mes

# 2. Install dependencies
pnpm install

# 3. Start local services (PostgreSQL, Redis, EMQX broker)
docker-compose up -d

# 4. Run database migrations
cd apps/pravara-api
go run cmd/migrate/main.go up

# 5. Start the API (in one terminal)
cd apps/pravara-api
go run cmd/api/main.go    # Starts on :4210

# 6. Start the UI (in another terminal)
cd apps/pravara-ui
pnpm dev                  # Starts on :3000

# 7. Start the admin console (optional, in another terminal)
cd apps/pravara-admin
pnpm dev                  # Starts on :3001
```

## Development Workflow

### Branch Strategy

We use **trunk-based development** on `main`.

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```
2. Make your changes with small, focused commits
3. Open a PR when ready for review

### Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(api): add work order batch endpoint
fix(ui): correct OEE calculation rounding
docs(readme): update MQTT topic reference
chore(deps): bump Go to 1.24
```

Common scopes: `api`, `ui`, `admin`, `infra`, `mqtt`, `telemetry`, `db`

### Validation Before Commit

Always run validation before pushing:

```bash
# Go (API)
cd apps/pravara-api
golangci-lint run ./...
go test ./...

# TypeScript (UI + Admin)
cd apps/pravara-ui
pnpm typecheck && pnpm lint

cd apps/pravara-admin
pnpm typecheck && pnpm lint
```

### Testing

```bash
# Go unit tests
cd apps/pravara-api
go test ./...

# Go tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# TypeScript tests
cd apps/pravara-ui && pnpm test
cd apps/pravara-admin && pnpm test
```

## Pull Request Process

1. Ensure all checks pass (lint + tests)
2. Write a clear PR description explaining **what** and **why**
3. Keep PRs focused -- one feature or fix per PR
4. Request review from a maintainer
5. Address review feedback with new commits (don't force-push)

## Domain Notes

Pravara MES is a Manufacturing Execution System. Key domain concepts:

- **Work Orders** -- production jobs tracked through manufacturing stages
- **OEE** -- Overall Equipment Effectiveness (availability x performance x quality)
- **Telemetry** -- IoT sensor data from production lines via MQTT/EMQX
- **Downtime Events** -- machine stoppage tracking and categorization
- **Quality Inspections** -- in-process and final quality checks

When contributing, please use domain-appropriate terminology in code, comments, and documentation.

## Code Style

- **Go**: Follow standard Go conventions; enforced by `golangci-lint`
- **TypeScript**: Follow existing patterns; enforced by ESLint + Prettier
- **Naming**: Match existing project conventions

## License

By contributing to Pravara MES, you agree that your contributions will be licensed under the [AGPL-3.0 License](./LICENSE).
