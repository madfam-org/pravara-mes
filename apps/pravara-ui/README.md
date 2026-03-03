# PravaraMES UI

Next.js 15 dashboard for the PravaraMES manufacturing execution system.

## Overview

A modern, real-time manufacturing dashboard featuring:
- **Kanban Board** - Drag-and-drop task management
- **Machine Control** - Live status and command dispatch
- **Order Management** - Order lifecycle visualization
- **Quality Dashboard** - Certificates and inspections
- **Real-time Updates** - WebSocket-powered live data

## Quick Start

```bash
# Install dependencies
npm install

# Start development server
npm run dev

# UI available at http://localhost:4501
```

## Configuration

Environment variables (set in `.env.local`):

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXT_PUBLIC_API_URL` | Backend API URL | http://localhost:4500 |
| `NEXT_PUBLIC_WS_URL` | WebSocket gateway URL | ws://localhost:8000 |
| `AUTH_SECRET` | NextAuth.js secret | required |
| `AUTH_KEYCLOAK_ID` | Keycloak client ID | required |
| `AUTH_KEYCLOAK_SECRET` | Keycloak client secret | required |
| `AUTH_KEYCLOAK_ISSUER` | Keycloak issuer URL | required |

## Directory Structure

```
apps/pravara-ui/
├── app/                    # Next.js App Router
│   ├── (auth)/            # Authentication pages
│   ├── (protected)/       # Protected dashboard pages
│   │   ├── kanban/        # Kanban board
│   │   ├── machines/      # Machine management
│   │   ├── orders/        # Order management
│   │   └── quality/       # Quality dashboard
│   ├── api/               # API routes
│   └── layout.tsx         # Root layout
├── components/
│   ├── data-table/        # Reusable data tables
│   ├── dialogs/           # Modal dialogs
│   ├── kanban/            # Kanban components
│   ├── machines/          # Machine components
│   └── ui/                # shadcn/ui primitives
├── hooks/                  # Custom React hooks
├── lib/
│   ├── api.ts             # API client
│   ├── realtime/          # WebSocket client
│   └── utils.ts           # Utility functions
├── stores/                 # Zustand state stores
└── types/                  # TypeScript types
```

## Authentication

### Token Refresh

The NextAuth.js configuration requests the `offline_access` scope from Keycloak, enabling automatic token rotation. Access tokens are refreshed with a 60-second buffer before expiry. If token refresh fails, the session is invalidated and the user is redirected to re-login.

## Testing

Unit and component tests use Vitest with React Testing Library. Run with:

```bash
npm run test
```

## Key Patterns

### Server Components
Pages use React Server Components for initial data fetching, with client components for interactivity.

### React Query
Data fetching and caching via TanStack Query with automatic background refetching.

### Zustand Stores
Lightweight client state management for UI preferences and optimistic updates.

### Real-time Updates
Centrifugo WebSocket client subscribes to tenant-scoped channels for live data.

### shadcn/ui
UI components built on Radix primitives with Tailwind CSS styling.

## Development

```bash
# Run development server with turbopack
npm run dev

# Type checking
npm run typecheck

# Linting
npm run lint

# Build for production
npm run build
```

## Pages

| Route | Description |
|-------|-------------|
| `/` | Dashboard overview |
| `/kanban` | Kanban task board |
| `/machines` | Machine management |
| `/orders` | Order management |
| `/quality` | Quality certificates and inspections |

## Components

### Kanban Board
- Drag-and-drop via @dnd-kit
- Real-time position updates
- Machine assignment dialog
- Priority and due date indicators

### Machine Control Panel
- Live status monitoring
- Command dispatch (start, pause, stop)
- Telemetry charts
- Error state handling

### Data Tables
- Server-side pagination
- Column sorting and filtering
- Row actions and bulk operations
- Export functionality
