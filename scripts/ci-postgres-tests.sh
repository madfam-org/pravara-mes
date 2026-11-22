#!/usr/bin/env bash
# =============================================================================
# Run every Go test that is gated on a database (CI job "PostgreSQL Tests (Go)")
# =============================================================================
# The suites below skip unless an environment variable points at a server:
#   PRAVARA_TEST_DATABASE_URL  a server where the role may CREATE DATABASE
#                              (pravara-api) or a migrated database
#                              (telemetry-worker)
#   PRAVARA_ISOLATION_DB_URL   a database migrated with infra/db/migrate.sh,
#                              connected as the NOSUPERUSER NOBYPASSRLS role
#                              pravara_app (tenant-isolation suites)
#   PRAVARA_TEST_REDIS_URL     a Redis (telemetry-worker command channel)
#
# The set is discovered, not hand-kept: with the variables unset (and -short,
# as the main Go job runs), every top-level test whose skip message names one
# of them is a gated test. Those tests then run with the variables set. The
# script fails if none were found, if none passed, if any test or subtest
# skipped, or if fewer passed than were found.
#
# Usage (from the repository root, with the three variables exported):
#   scripts/ci-postgres-tests.sh
set -euo pipefail

: "${PRAVARA_TEST_DATABASE_URL:?}" "${PRAVARA_ISOLATION_DB_URL:?}" "${PRAVARA_TEST_REDIS_URL:?}"
command -v jq >/dev/null

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="${RUNNER_TEMP:-$(mktemp -d)}/db-gated"
mkdir -p "$WORK"
SKIP_PATTERN='(PRAVARA_TEST_DATABASE_URL|PRAVARA_ISOLATION_DB_URL|PRAVARA_TEST_REDIS_URL) not set|needs PostgreSQL; skipped in -short mode'

# module directory : packages
SUITES=(
  "apps/pravara-api:./internal/api/ ./internal/db/..."
  "apps/telemetry-worker:./internal/db/..."
)

expected=0 passed=0 failed=0 skipped=0 rc=0
for suite in "${SUITES[@]}"; do
  module="${suite%%:*}"
  read -r -a pkgs <<<"${suite#*:}"
  name="$(basename "$module")"
  cd "$ROOT/$module"

  # 1. Discover: run without the variables.
  if ! env -u PRAVARA_TEST_DATABASE_URL -u PRAVARA_ISOLATION_DB_URL -u PRAVARA_TEST_REDIS_URL \
      go test "${pkgs[@]}" -short -count=1 -json >"$WORK/$name.discover.json"; then
    echo "::error::$module: go test failed while listing the database-gated tests"
    exit 1
  fi
  jq -r --arg re "$SKIP_PATTERN" \
    'select(.Action == "output" and .Test != null and (.Output | test($re))) | .Test | split("/")[0]' \
    "$WORK/$name.discover.json" | sort -u >"$WORK/$name.gated.txt"
  n=$(wc -l <"$WORK/$name.gated.txt")
  echo "== $module: $n database-gated tests"
  cat "$WORK/$name.gated.txt"
  expected=$((expected + n))
  [ "$n" -gt 0 ] || continue

  # 2. Run them with the variables set.
  pattern="^($(paste -sd'|' "$WORK/$name.gated.txt"))\$"
  set +e
  go test "${pkgs[@]}" -count=1 -v -json -timeout 20m -run "$pattern" >"$WORK/$name.run.json"
  code=$?
  set -e
  jq -j 'select(.Action == "output") | .Output' "$WORK/$name.run.json"
  [ "$code" -eq 0 ] || rc=$code

  top='select(.Test != null and (.Test | contains("/") | not))'
  p=$(jq -r "$top | select(.Action == \"pass\") | .Package + \" \" + .Test" "$WORK/$name.run.json" | sort -u | wc -l)
  f=$(jq -r "select(.Test != null and .Action == \"fail\") | .Package + \" \" + .Test" "$WORK/$name.run.json" | sort -u | wc -l)
  # Every skip counts, subtests included: with the databases present none
  # should skip.
  s=$(jq -r "select(.Test != null and .Action == \"skip\") | .Package + \" \" + .Test" "$WORK/$name.run.json" | sort -u)
  ns=$(printf '%s' "$s" | grep -c . || true)
  if [ "$ns" -gt 0 ]; then
    echo "::error::$module: tests skipped with the databases present:"
    echo "$s"
  fi
  if [ "$p" -lt "$n" ]; then
    echo "::error::$module: $n gated tests found, $p passed"
  fi
  passed=$((passed + p)) failed=$((failed + f)) skipped=$((skipped + ns))
done

summary="Database-gated tests found: $expected. Passed: $passed. Failed (tests and subtests): $failed. Skipped (tests and subtests): $skipped."
echo "$summary"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  { echo "### Database-gated Go tests"; echo "$summary"; } >>"$GITHUB_STEP_SUMMARY"
fi
if [ "$expected" -eq 0 ] || [ "$passed" -eq 0 ]; then
  echo "::error::no database-gated test ran"
  exit 1
fi
if [ "$skipped" -gt 0 ] || [ "$passed" -lt "$expected" ]; then
  exit 1
fi
exit "$rc"
