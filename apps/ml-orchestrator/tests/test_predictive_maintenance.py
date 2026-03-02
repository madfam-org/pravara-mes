"""
Test Predictive Maintenance Model
"""

import pytest
import numpy as np
import pandas as pd
from datetime import datetime
from models.predictive_maintenance import PredictiveMaintenanceModel


@pytest.fixture
def model():
    """Create a predictive maintenance model instance"""
    return PredictiveMaintenanceModel()


@pytest.fixture
def sample_telemetry():
    """Create sample telemetry data"""
    return pd.DataFrame({
        'vibration': [5.2, 5.5, 5.8, 6.1, 6.5],
        'temperature': [72, 73, 74, 75, 76],
        'pressure': [45, 46, 45, 47, 46],
        'rpm': [1500, 1510, 1505, 1508, 1502],
        'power': [250, 252, 251, 253, 255],
        'operating_hours': [1000] * 5,
        'cycles': [50000] * 5
    })


@pytest.mark.asyncio
async def test_health_score_calculation(model, sample_telemetry):
    """Test health score calculation"""
    features = model.extract_features(sample_telemetry.iloc[0])
    health_score = model.calculate_health_score(features)

    assert 0 <= health_score <= 100
    assert isinstance(health_score, float)


@pytest.mark.asyncio
async def test_rul_estimation(model, sample_telemetry):
    """Test remaining useful life estimation"""
    rul = model.estimate_remaining_useful_life(sample_telemetry)

    assert rul >= 0
    assert isinstance(rul, float)
    # Default should be around 720 hours (30 days)
    assert rul == 720.0  # Since no model is trained


@pytest.mark.asyncio
async def test_degradation_pattern_detection(model, sample_telemetry):
    """Test degradation pattern detection"""
    patterns = model.detect_degradation_pattern(sample_telemetry)

    assert isinstance(patterns, dict)
    assert 'linear_degradation' in patterns
    assert 'exponential_degradation' in patterns
    assert 'cyclic_pattern' in patterns
    assert 'sudden_change' in patterns
    assert 'degradation_rate' in patterns


@pytest.mark.asyncio
async def test_prediction_output(model, sample_telemetry):
    """Test complete prediction output"""
    features = {
        'vibration': 6.5,
        'temperature': 76,
        'pressure': 46,
        'rpm': 1502,
        'power': 255
    }

    result = await model.predict(features, horizon_hours=24)

    assert 'prediction' in result
    assert 'confidence' in result
    assert 'recommendations' in result

    prediction = result['prediction']
    assert 'health_score' in prediction
    assert 'remaining_useful_life_hours' in prediction
    assert 'failure_probability_24h' in prediction
    assert 'maintenance_urgency' in prediction


@pytest.mark.asyncio
async def test_realtime_prediction(model):
    """Test real-time prediction"""
    telemetry = {
        'vibration': 12,
        'temperature': 85
    }

    result = await model.predict_realtime(telemetry)

    assert 'risk_score' in result
    assert 'ttf_hours' in result
    assert 'recommendations' in result
    assert 0 <= result['risk_score'] <= 1


def test_feature_extraction(model, sample_telemetry):
    """Test feature extraction"""
    features = model.extract_features(sample_telemetry)

    assert isinstance(features, np.ndarray)
    assert len(features) > 0
    assert not np.any(np.isnan(features))