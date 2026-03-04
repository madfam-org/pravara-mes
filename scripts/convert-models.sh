#!/usr/bin/env bash
# Batch convert STEP/STL files to optimized GLB for the factory floor digital twin.
#
# Usage:
#   ./scripts/convert-models.sh <input_dir> <output_dir>
#
# Prerequisites:
#   - Python 3 with cadquery: pip install cadquery OCP
#   - gltf-transform CLI: npm install -g @gltf-transform/cli
#   - jq: brew install jq
#
# The script processes files in <input_dir>/ named by machine registry ID:
#   snapmaker_a350.step → snapmaker_a350.glb (optimized)

set -euo pipefail

INPUT_DIR="${1:?Usage: $0 <input_dir> <output_dir>}"
OUTPUT_DIR="${2:?Usage: $0 <input_dir> <output_dir>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RAW_DIR="${OUTPUT_DIR}/.raw"

mkdir -p "$OUTPUT_DIR" "$RAW_DIR"

echo "=== Machine Model Conversion Pipeline ==="
echo "Input:  $INPUT_DIR"
echo "Output: $OUTPUT_DIR"
echo ""

convert_step() {
    local input="$1" output="$2" name="$3"
    echo "[STEP→GLB] $name"
    python3 "$SCRIPT_DIR/step_to_glb.py" "$input" "$output"
}

convert_stl() {
    local input="$1" output="$2" name="$3"
    echo "[STL→GLB] $name"
    # Use gltf-transform to merge STL into GLB
    gltf-transform merge "$input" "$output" 2>/dev/null || {
        echo "  WARNING: gltf-transform merge failed, trying direct copy with extension rename"
        cp "$input" "$output"
    }
}

optimize_glb() {
    local input="$1" output="$2" name="$3"
    echo "[OPTIMIZE] $name"

    # Simplify mesh, apply Draco compression, center pivot at base
    gltf-transform optimize "$input" "$output" \
        --simplify \
        --simplify-ratio 0.75 \
        --compress draco \
        2>/dev/null || {
        echo "  WARNING: optimization failed, using raw GLB"
        cp "$input" "$output"
        return
    }

    # STEP files use mm; GLTF standard is meters → scale is handled at render time
    # The viz-engine applies scale from the machine_models DB record

    local raw_size optimized_size
    raw_size=$(stat -f%z "$input" 2>/dev/null || stat -c%s "$input" 2>/dev/null || echo "0")
    optimized_size=$(stat -f%z "$output" 2>/dev/null || stat -c%s "$output" 2>/dev/null || echo "0")
    echo "  Size: ${raw_size} → ${optimized_size} bytes"
}

# Process all files in input directory
converted=0
failed=0

for file in "$INPUT_DIR"/*; do
    [ -f "$file" ] || continue

    basename="$(basename "$file")"
    name="${basename%.*}"
    ext="${basename##*.}"
    ext_lower="$(echo "$ext" | tr '[:upper:]' '[:lower:]')"

    raw_glb="${RAW_DIR}/${name}.glb"
    final_glb="${OUTPUT_DIR}/${name}.glb"

    case "$ext_lower" in
        step|stp)
            if convert_step "$file" "$raw_glb" "$name"; then
                optimize_glb "$raw_glb" "$final_glb" "$name"
                ((converted++))
            else
                echo "  FAILED: $name"
                ((failed++))
            fi
            ;;
        stl)
            if convert_stl "$file" "$raw_glb" "$name"; then
                optimize_glb "$raw_glb" "$final_glb" "$name"
                ((converted++))
            else
                echo "  FAILED: $name"
                ((failed++))
            fi
            ;;
        glb|gltf)
            echo "[COPY] $name (already GLB/GLTF)"
            optimize_glb "$file" "$final_glb" "$name"
            ((converted++))
            ;;
        *)
            echo "[SKIP] $basename (unsupported format)"
            ;;
    esac
done

echo ""
echo "=== Pipeline Complete ==="
echo "Converted: $converted"
echo "Failed:    $failed"
echo "Output:    $OUTPUT_DIR/"

# Clean up raw intermediates
rm -rf "$RAW_DIR"
