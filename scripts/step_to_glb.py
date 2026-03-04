#!/usr/bin/env python3
"""Convert STEP files to GLTF/GLB using CadQuery + cadquery-ocp tessellation.

Usage:
    python step_to_glb.py input.step output.glb

Requirements:
    pip install cadquery OCP
"""

import sys

import cadquery as cq


def step_to_glb(input_path: str, output_path: str) -> None:
    """Convert a STEP file to GLB format."""
    result = cq.importers.importStep(input_path)
    cq.exporters.export(result, output_path, exportType="GLTF")
    print(f"Converted {input_path} -> {output_path}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(f"Usage: {sys.argv[0]} <input.step> <output.glb>")
        sys.exit(1)

    step_to_glb(sys.argv[1], sys.argv[2])
