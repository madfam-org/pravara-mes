#!/usr/bin/env bash
# Upload converted GLB models to S3 and register them via the viz-engine API.
#
# Usage:
#   ./scripts/upload-machine-models.sh <glb_dir> [viz_engine_url] [bearer_token]
#
# Prerequisites:
#   - jq: brew install jq
#   - curl
#   - machine-model-manifest.json in the same directory as this script

set -euo pipefail

GLB_DIR="${1:?Usage: $0 <glb_dir> [viz_engine_url] [bearer_token]}"
VIZ_ENGINE_URL="${2:-http://localhost:4502}"
TOKEN="${3:-}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MANIFEST="${SCRIPT_DIR}/machine-model-manifest.json"

if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: machine-model-manifest.json not found at $MANIFEST"
    exit 1
fi

echo "=== Machine Model Upload Pipeline ==="
echo "Models dir:     $GLB_DIR"
echo "Viz-engine URL: $VIZ_ENGINE_URL"
echo ""

uploaded=0
skipped=0
failed=0

# Iterate through manifest entries
for machine_id in $(jq -r 'keys[] | select(. != "_description" and . != "_tiers")' "$MANIFEST"); do
    glb_file="${GLB_DIR}/${machine_id}.glb"

    if [ ! -f "$glb_file" ]; then
        echo "[SKIP] $machine_id — no GLB file found"
        ((skipped++))
        continue
    fi

    # Read dimensions from manifest
    dims=$(jq -r ".\"${machine_id}\".dimensions_mm" "$MANIFEST")
    x_mm=$(echo "$dims" | jq -r '.x // 300')
    y_mm=$(echo "$dims" | jq -r '.y // 300')
    z_mm=$(echo "$dims" | jq -r '.z // 300')

    echo "[UPLOAD] $machine_id (${x_mm}x${y_mm}x${z_mm}mm)"

    # Build auth header if token provided
    auth_header=""
    if [ -n "$TOKEN" ]; then
        auth_header="-H \"Authorization: Bearer $TOKEN\""
    fi

    # Upload via multipart form to viz-engine
    response=$(curl -s -w "\n%{http_code}" \
        -X POST "${VIZ_ENGINE_URL}/v1/models/upload" \
        ${TOKEN:+-H "Authorization: Bearer $TOKEN"} \
        -F "file=@${glb_file}" \
        -F "machine_type=${machine_id}" \
        -F "name=${machine_id}")

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$http_code" = "201" ] || [ "$http_code" = "200" ]; then
        model_id=$(echo "$body" | jq -r '.id // "unknown"')
        echo "  Created model: $model_id"
        ((uploaded++))
    else
        echo "  FAILED (HTTP $http_code): $body"
        ((failed++))
    fi
done

echo ""
echo "=== Upload Complete ==="
echo "Uploaded: $uploaded"
echo "Skipped:  $skipped"
echo "Failed:   $failed"
