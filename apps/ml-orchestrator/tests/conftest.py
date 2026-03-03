"""
Shared fixtures for ml-orchestrator tests.
"""

import pytest
import numpy as np


@pytest.fixture(autouse=True)
def seed_random():
    """Ensure reproducible random state across all tests."""
    np.random.seed(42)


@pytest.fixture
def standard_process_data():
    """Standard process data dictionary with all expected feature keys."""
    return {
        "temperature": 80.0,
        "pressure": 50.0,
        "speed": 120.0,
        "feed_rate": 0.5,
        "tool_wear": 0.3,
        "material_hardness": 45.0,
        "material_thickness": 2.5,
        "material_temperature": 25.0,
        "humidity": 55.0,
        "ambient_temperature": 22.0,
        "vibration": 4.0,
        "spindle_load": 60.0,
        "axis_position_error": 0.01,
        "process_time": 30.0,
        "cycle_variation": 0.05,
    }
