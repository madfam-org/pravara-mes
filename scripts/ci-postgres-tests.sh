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
# as the main Go job runs), every top-level test in every package of both
# modules whose skip message names one of them is a gated test, identified
# by package and name. Those tests then run with the variables set. The
# script fails if a module has none, if any test or subtest of them failed or
# skipped, or if fewer passed than were found. Each go test has a 12-minute
# timeout, so a hang ends with a goroutine dump inside the job's 30 minutes.
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

# module directory : packages (every package of the module)
SUITES=(
  "apps/pravara-api:./..."
  "apps/telemetry-worker:./..."
)

expected=0 passed=0 failed=0 skipped=0 rc=0
for suite in "${SUITES[@]}"; do
  module="${suite%%:*}"
  read -r -a pkgs <<<"${suite#*:}"
  name="$(basename "$module")"
  cd "$ROOT/$module"

  # 1. Discover: run without the variables. A gated test is identified by
  #    its package and its top-level name.
  if ! env -u PRAVARA_TEST_DATABASE_URL -u PRAVARA_ISOLATION_DB_URL -u PRAVARA_TEST_REDIS_URL \
      go test "${pkgs[@]}" -short -count=1 -timeout 12m -json >"$WORK/$name.discover.json"; then
    echo "::error::$module: go test failed while listing the database-gated tests"
    exit 1
  fi
  jq -r --arg re "$SKIP_PATTERN" \
    'select(.Action == "output" and .Test != null and (.Output | test($re))) | .Package + "\t" + (.Test | split("/")[0])' \
    "$WORK/$name.discover.json" | sort -u >"$WORK/$name.gated.tsv"
  n=$(wc -l <"$WORK/$name.gated.tsv")
  echo "== $module: $n database-gated tests"
  cat "$WORK/$name.gated.tsv"
  if [ "$n" -eq 0 ]; then
    echo "::error::$module: no database-gated tests found"
    exit 1
  fi
  expected=$((expected + n))

  # 2. Run them with the variables set. -run matches names in every
  #    package; only the discovered (package, name) pairs are counted.
  pattern="^($(cut -f2 "$WORK/$name.gated.tsv" | sort -u | paste -sd'|' -))\$"
  set +e
  go test "${pkgs[@]}" -count=1 -v -json -timeout 12m -run "$pattern" >"$WORK/$name.run.json"
  code=$?
  set -e
  jq -j 'select(.Action == "output") | .Output' "$WORK/$name.run.json"
  [ "$code" -eq 0 ] || rc=$code

  # (package, top-level name, action) for every test and subtest event.
  jq -r 'select(.Test != null and (.Action == "pass" or .Action == "fail" or .Action == "skip"))
         | .Package + "\t" + (.Test | split("/")[0]) + "\t" + .Action + "\t" + .Test' \
    "$WORK/$name.run.json" >"$WORK/$name.results.tsv"
  # Only events of discovered pairs count.
  awk -F'\t' 'NR == FNR { gated[$1 "\t" $2] = 1; next } ($1 "\t" $2) in gated' \
    "$WORK/$name.gated.tsv" "$WORK/$name.results.tsv" >"$WORK/$name.gated-results.tsv"
  p=$(awk -F'\t' '$3 == "pass" && $2 == $4 { print $1 "\t" $2 }' "$WORK/$name.gated-results.tsv" | sort -u | wc -l)
  f=$(awk -F'\t' '$3 == "fail"' "$WORK/$name.gated-results.tsv" | wc -l)
  # Every skip counts, subtests included: with the databases present none
  # should skip.
  s=$(awk -F'\t' '$3 == "skip" { print $1 " " $4 }' "$WORK/$name.gated-results.tsv" | sort -u)
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
