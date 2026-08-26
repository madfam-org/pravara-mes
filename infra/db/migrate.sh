#!/usr/bin/env bash
# =============================================================================
# PravaraMES migration runner
# =============================================================================
# Thin psql-based runner for the SQL migrations in
# apps/pravara-api/internal/db/migrations. This is the script `make
# db-migrate` / `make db-rollback` have always pointed at; it did not exist
# until 2026-08 (the repo's migration story was aspirational), so migrations
# were applied by hand. Applied versions are tracked in a
# `schema_migrations` table.
#
# Usage:
#   ./migrate.sh up            # apply all pending *.up.sql
#   ./migrate.sh down [n]      # roll back the last n applied migrations (default 1)
#   ./migrate.sh status        # show applied vs pending
#
# Connection: set DATABASE_URL (postgres://user:pass@host:port/db?sslmode=..).
# No default is provided on purpose — this can point at production, so the
# operator must be explicit. Requires psql on PATH.
#
# Notes:
# - Files run WITHOUT an implicit wrapping transaction (psql default:
#   per-statement autocommit). This is required for ALTER TYPE ... ADD VALUE
#   (026). Individual migrations may open their own BEGIN/COMMIT if they
#   need atomicity.
# - Migration numbers are not contiguous: 002-008 never existed. The runner
#   orders by filename, so gaps are harmless.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-$SCRIPT_DIR/../../apps/pravara-api/internal/db/migrations}"

if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "ERROR: DATABASE_URL is not set." >&2
    echo "  export DATABASE_URL='postgres://user:pass@host:5432/pravara?sslmode=disable'" >&2
    exit 1
fi

if ! command -v psql >/dev/null 2>&1; then
    echo "ERROR: psql not found on PATH." >&2
    exit 1
fi

if [[ ! -d "$MIGRATIONS_DIR" ]]; then
    echo "ERROR: migrations directory not found: $MIGRATIONS_DIR" >&2
    exit 1
fi

PSQL=(psql "$DATABASE_URL" --no-psqlrc --quiet --set ON_ERROR_STOP=1)

ensure_tracking_table() {
    "${PSQL[@]}" -c "CREATE TABLE IF NOT EXISTS schema_migrations (
        version    TEXT PRIMARY KEY,
        applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    );"
}

# version_of 001_genesis.up.sql -> 001_genesis
version_of() {
    basename "$1" | sed -E 's/\.(up|down)\.sql$//'
}

is_applied() {
    local version="$1"
    local count
    count=$("${PSQL[@]}" -tA -c "SELECT COUNT(*) FROM schema_migrations WHERE version = '$version';")
    [[ "$count" == "1" ]]
}

cmd_up() {
    ensure_tracking_table
    local applied=0
    local file version
    while IFS= read -r file; do
        version=$(version_of "$file")
        if is_applied "$version"; then
            continue
        fi
        echo "==> applying $version"
        "${PSQL[@]}" -f "$file"
        "${PSQL[@]}" -c "INSERT INTO schema_migrations (version) VALUES ('$version');"
        applied=$((applied + 1))
    done < <(find "$MIGRATIONS_DIR" -name '*.up.sql' | sort)

    if [[ "$applied" -eq 0 ]]; then
        echo "Nothing to apply — database is up to date."
    else
        echo "Applied $applied migration(s)."
    fi
}

cmd_down() {
    ensure_tracking_table
    local steps="${1:-1}"
    local version file
    for ((i = 0; i < steps; i++)); do
        version=$("${PSQL[@]}" -tA -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;")
        if [[ -z "$version" ]]; then
            echo "No applied migrations to roll back."
            return 0
        fi
        file="$MIGRATIONS_DIR/$version.down.sql"
        if [[ ! -f "$file" ]]; then
            echo "ERROR: down migration missing: $file" >&2
            exit 1
        fi
        echo "==> rolling back $version"
        "${PSQL[@]}" -f "$file"
        "${PSQL[@]}" -c "DELETE FROM schema_migrations WHERE version = '$version';"
    done
}

cmd_status() {
    ensure_tracking_table
    local file version state
    printf '%-45s %s\n' "MIGRATION" "STATE"
    while IFS= read -r file; do
        version=$(version_of "$file")
        if is_applied "$version"; then
            state="applied"
        else
            state="pending"
        fi
        printf '%-45s %s\n' "$version" "$state"
    done < <(find "$MIGRATIONS_DIR" -name '*.up.sql' | sort)
}

case "${1:-}" in
    up)     cmd_up ;;
    down)   cmd_down "${2:-1}" ;;
    status) cmd_status ;;
    *)
        echo "Usage: $0 {up|down [n]|status}" >&2
        exit 1
        ;;
esac
