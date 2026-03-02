"""
Test Anomaly Detection Model
"""

import pytest
import numpy as np
import pandas as pd
from models.anomaly_detection import AnomalyDetector


@pytest.fixture
def detector():
    """Create an anomaly detector instance"""
    return AnomalyDetector()


@pytest.fixture
def normal_data():
    """Create normal telemetry data"""
    np.random.seed(42)
    return pd.DataFrame({
        'vibration': np.random.normal(5, 0.5, 100),
        'temperature': np.random.normal(75, 2, 100),
        'pressure': np.random.normal(50, 3, 100),
        'rpm': np.random.normal(1500, 50, 100),
        'power': np.random.normal(250, 10, 100)
    })


@pytest.fixture
def anomalous_data():
    """Create anomalous telemetry data"""
    return pd.DataFrame({
        'vibration': [25],  # Very high vibration
        'temperature': [105],  # Very high temperature
        'pressure': [120],  # Very high pressure
        'rpm': [2500],
        'power': [500]
    })


@pytest.mark.asyncio
async def test_anomaly_detection(detector, normal_data):
    """Test basic anomaly detection"""
    # Update baseline with normal data
    detector.update_baseline(normal_data)

    # Test with normal data point
    features = normal_data.iloc[0].to_dict()
    result = await detector.detect(features)

    assert 'is_anomaly' in result
    assert 'anomaly_score' in result
    assert 'type' in result
    assert 'severity' in result
    assert 0 <= result['anomaly_score'] <= 1


@pytest.mark.asyncio
async def test_realtime_anomaly_detection(detector):
    """Test real-time anomaly detection"""
    # Test with high vibration
    telemetry = {
        'vibration': 25,
        'temperature': 95,
        'pressure': 110
    }

    result = await detector.detect_realtime(telemetry)

    assert 'is_anomaly' in result
    assert result['is_anomaly'] == True
    assert 'severity' in result


def test_statistical_anomalies(detector, normal_data):
    """Test statistical anomaly detection"""
    detector.update_baseline(normal_data)

    # Create data with outliers
    test_data = normal_data.copy()
    test_data.loc[0, 'vibration'] = 50  # Extreme outlier

    anomalies = detector.detect_statistical_anomalies(test_data)

    assert isinstance(anomalies, np.ndarray)
    assert len(anomalies) == len(test_data)
    assert anomalies[0] > 0  # First row should be anomalous


def test_pattern_anomalies(detector, normal_data):
    """Test pattern-based anomaly detection"""
    patterns = detector.detect_pattern_anomalies(normal_data)

    assert isinstance(patterns, dict)
    assert 'sudden_spike' in patterns
    assert 'gradual_drift' in patterns
    assert 'periodic_anomaly' in patterns
    assert 'correlation_break' in patterns


def test_threshold_configuration(detector):
    """Test threshold configuration"""
    original_threshold = detector.thresholds['combined']

    # Modify threshold
    detector.thresholds['combined'] = 0.3

    assert detector.thresholds['combined'] == 0.3

    # Restore
    detector.thresholds['combined'] = original_threshold


@pytest.mark.asyncio
async def test_severity_classification(detector):
    """Test anomaly severity classification"""
    # High severity anomaly
    high_severity = {
        'vibration': 30,
        'temperature': 100,
        'pressure': 130
    }

    result = await detector.detect_realtime(high_severity)

    if result['is_anomaly']:
        assert result['severity'] in ['low', 'medium', 'high', 'critical']
        # With these extreme values, should be high or critical
        assert result['severity'] in ['high', 'critical']