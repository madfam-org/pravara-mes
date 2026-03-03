"""
Test Quality Prediction Model

Covers:
- Feature extraction and preprocessing
- Prediction input validation and edge cases
- Model inference with mocked ML models
- Output format, confidence scores, and quality classification
- Statistical process control (SPC) utilities
- Recommendation generation logic
"""

import pytest
import numpy as np
import pandas as pd
from unittest.mock import MagicMock, patch, PropertyMock

from models.quality_prediction import QualityPredictor


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def predictor():
    """Create a fresh QualityPredictor instance."""
    return QualityPredictor()


@pytest.fixture
def predictor_with_models(predictor):
    """QualityPredictor with mocked classifier and regressor."""
    # Mock defect classifier
    mock_classifier = MagicMock()
    mock_classifier.predict_proba.return_value = np.array([[0.8, 0.2]])
    predictor.defect_classifier = mock_classifier

    # Mock quality regressor
    mock_regressor = MagicMock()
    mock_regressor.predict.return_value = np.array([88.5])
    predictor.quality_regressor = mock_regressor

    # Fit the scaler so transform works
    dummy = np.random.randn(20, 15)
    predictor.scaler.fit(dummy)

    return predictor


@pytest.fixture
def predictor_with_neural(predictor_with_models):
    """QualityPredictor with mocked classifier, regressor, and neural model."""
    mock_neural = MagicMock()
    mock_neural.predict.return_value = np.array([[0.15]])
    predictor_with_models.neural_model = mock_neural
    return predictor_with_models


# ---------------------------------------------------------------------------
# Feature Extraction
# ---------------------------------------------------------------------------

class TestExtractQualityFeatures:
    """Tests for extract_quality_features method."""

    def test_extracts_all_15_features(self, predictor, standard_process_data):
        """Feature vector must contain exactly 15 elements in expected order."""
        features = predictor.extract_quality_features(standard_process_data)

        assert isinstance(features, np.ndarray)
        assert features.shape == (15,)

    def test_feature_order_matches_expected_keys(self, predictor, standard_process_data):
        """Features must appear in the documented extraction order."""
        expected_order = [
            "temperature", "pressure", "speed", "feed_rate", "tool_wear",
            "material_hardness", "material_thickness", "material_temperature",
            "humidity", "ambient_temperature",
            "vibration", "spindle_load", "axis_position_error",
            "process_time", "cycle_variation",
        ]
        features = predictor.extract_quality_features(standard_process_data)

        for idx, key in enumerate(expected_order):
            assert features[idx] == standard_process_data[key], (
                f"Feature at index {idx} should be '{key}'"
            )

    def test_missing_keys_default_to_zero(self, predictor):
        """Missing keys in input dict must default to 0."""
        features = predictor.extract_quality_features({})
        assert features.shape == (15,)
        assert np.all(features == 0)

    def test_partial_keys_fill_remaining_with_zero(self, predictor):
        """Only supplied keys contribute non-zero values."""
        partial = {"temperature": 99.0, "vibration": 7.5}
        features = predictor.extract_quality_features(partial)

        assert features[0] == 99.0   # temperature
        assert features[10] == 7.5   # vibration
        assert features[1] == 0      # pressure (missing)

    @pytest.mark.parametrize("key,value", [
        ("temperature", -273.15),
        ("pressure", 0.0),
        ("speed", 1e6),
        ("tool_wear", -0.5),
    ])
    def test_extreme_values_pass_through(self, predictor, key, value):
        """Extraction does not clamp or reject extreme values."""
        data = {key: value}
        features = predictor.extract_quality_features(data)
        idx = ["temperature", "pressure", "speed", "feed_rate", "tool_wear",
               "material_hardness", "material_thickness", "material_temperature",
               "humidity", "ambient_temperature", "vibration", "spindle_load",
               "axis_position_error", "process_time", "cycle_variation"].index(key)
        assert features[idx] == value


# ---------------------------------------------------------------------------
# Defect Probability Prediction
# ---------------------------------------------------------------------------

class TestPredictDefectProbability:
    """Tests for predict_defect_probability method."""

    def test_returns_default_when_no_model(self, predictor):
        """Without a trained classifier, returns 0.1 default."""
        features = np.zeros(15)
        prob = predictor.predict_defect_probability(features)
        assert prob == 0.1

    def test_returns_model_probability(self, predictor):
        """With a classifier, returns class-1 probability."""
        mock_clf = MagicMock()
        mock_clf.predict_proba.return_value = np.array([[0.35, 0.65]])
        predictor.defect_classifier = mock_clf

        prob = predictor.predict_defect_probability(np.zeros(15))
        assert prob == pytest.approx(0.65)
        mock_clf.predict_proba.assert_called_once()

    def test_reshapes_1d_input(self, predictor):
        """1-D feature vector is reshaped to (1, n) for sklearn."""
        mock_clf = MagicMock()
        mock_clf.predict_proba.return_value = np.array([[0.9, 0.1]])
        predictor.defect_classifier = mock_clf

        predictor.predict_defect_probability(np.ones(15))
        call_arg = mock_clf.predict_proba.call_args[0][0]
        assert call_arg.shape == (1, 15)


# ---------------------------------------------------------------------------
# Quality Score Prediction
# ---------------------------------------------------------------------------

class TestPredictQualityScore:
    """Tests for predict_quality_score method."""

    def test_returns_default_when_no_model(self, predictor):
        """Without a trained regressor, returns 85.0 default."""
        score = predictor.predict_quality_score(np.zeros(15))
        assert score == 85.0

    def test_returns_model_score(self, predictor):
        """With a regressor, returns its prediction."""
        mock_reg = MagicMock()
        mock_reg.predict.return_value = np.array([92.3])
        predictor.quality_regressor = mock_reg

        score = predictor.predict_quality_score(np.zeros(15))
        assert score == pytest.approx(92.3)

    @pytest.mark.parametrize("raw,expected", [
        (105.0, 100.0),
        (-10.0, 0.0),
        (50.0, 50.0),
    ])
    def test_clips_output_to_0_100(self, predictor, raw, expected):
        """Quality score is clipped to [0, 100]."""
        mock_reg = MagicMock()
        mock_reg.predict.return_value = np.array([raw])
        predictor.quality_regressor = mock_reg

        score = predictor.predict_quality_score(np.zeros(15))
        assert score == pytest.approx(expected)


# ---------------------------------------------------------------------------
# Full Predict Pipeline (async)
# ---------------------------------------------------------------------------

class TestPredict:
    """Tests for the async predict method."""

    @pytest.mark.asyncio
    async def test_output_structure(self, predictor_with_models, standard_process_data):
        """Prediction result contains required top-level and nested keys."""
        result = await predictor_with_models.predict(standard_process_data)

        assert "prediction" in result
        assert "confidence" in result
        assert "recommendations" in result

        pred = result["prediction"]
        assert "defect_probability" in pred
        assert "quality_score" in pred
        assert "quality_class" in pred
        assert "needs_intervention" in pred
        assert "critical_factors" in pred
        assert "estimated_yield" in pred
        assert "process_capability" in pred

    @pytest.mark.asyncio
    async def test_confidence_range(self, predictor_with_models, standard_process_data):
        """Confidence must be in (0, 1]."""
        result = await predictor_with_models.predict(standard_process_data)
        assert 0 < result["confidence"] <= 1.0

    @pytest.mark.asyncio
    @pytest.mark.parametrize("defect_prob,quality_score,expected_class", [
        (0.05, 96.0, "excellent"),
        (0.10, 90.0, "good"),
        (0.15, 78.0, "acceptable"),
        (0.20, 55.0, "marginal"),
        (0.50, 40.0, "reject"),
    ])
    async def test_quality_classification(
        self, predictor, defect_prob, quality_score, expected_class
    ):
        """Quality class is determined by quality_score thresholds."""
        mock_clf = MagicMock()
        mock_clf.predict_proba.return_value = np.array([[1 - defect_prob, defect_prob]])
        predictor.defect_classifier = mock_clf

        mock_reg = MagicMock()
        mock_reg.predict.return_value = np.array([quality_score])
        predictor.quality_regressor = mock_reg

        dummy = np.random.randn(20, 15)
        predictor.scaler.fit(dummy)

        result = await predictor.predict({"temperature": 80})
        assert result["prediction"]["quality_class"] == expected_class

    @pytest.mark.asyncio
    async def test_needs_intervention_high_defect(self, predictor_with_models, standard_process_data):
        """Intervention flagged when defect probability exceeds threshold."""
        predictor_with_models.defect_classifier.predict_proba.return_value = np.array([[0.2, 0.8]])
        result = await predictor_with_models.predict(standard_process_data)
        assert result["prediction"]["needs_intervention"] is True

    @pytest.mark.asyncio
    async def test_needs_intervention_low_quality(self, predictor_with_models, standard_process_data):
        """Intervention flagged when quality score is below minimum."""
        predictor_with_models.quality_regressor.predict.return_value = np.array([60.0])
        result = await predictor_with_models.predict(standard_process_data)
        assert result["prediction"]["needs_intervention"] is True

    @pytest.mark.asyncio
    async def test_estimated_yield_calculation(self, predictor_with_models, standard_process_data):
        """Estimated yield = (1 - defect_prob) * 100."""
        result = await predictor_with_models.predict(standard_process_data)
        defect_prob = result["prediction"]["defect_probability"]
        assert result["prediction"]["estimated_yield"] == pytest.approx((1 - defect_prob) * 100)

    @pytest.mark.asyncio
    async def test_neural_model_ensemble(self, predictor_with_neural, standard_process_data):
        """When neural model is present, defect probability is an ensemble."""
        result = await predictor_with_neural.predict(standard_process_data)
        # Ensemble: 0.6 * classifier_prob + 0.4 * neural_prob
        # classifier returns 0.2, neural returns 0.15
        expected = 0.6 * 0.2 + 0.4 * 0.15
        assert result["prediction"]["defect_probability"] == pytest.approx(expected, abs=1e-6)

    @pytest.mark.asyncio
    async def test_empty_features_dict(self, predictor_with_models):
        """Empty features dict should still produce a valid prediction."""
        result = await predictor_with_models.predict({})
        assert "prediction" in result
        assert result["prediction"]["quality_class"] in (
            "excellent", "good", "acceptable", "marginal", "reject"
        )


# ---------------------------------------------------------------------------
# Confidence Calculation
# ---------------------------------------------------------------------------

class TestCalculatePredictionConfidence:
    """Tests for calculate_prediction_confidence."""

    @pytest.mark.parametrize("defect_prob,quality_score", [
        (0.5, 75.0),
        (0.05, 95.0),
        (0.95, 30.0),
    ])
    def test_confidence_never_exceeds_cap(self, predictor, defect_prob, quality_score):
        conf = predictor.calculate_prediction_confidence(defect_prob, quality_score)
        assert conf <= 0.95

    def test_extreme_predictions_boost_confidence(self, predictor):
        """Extreme defect_prob (< 0.1 or > 0.9) increases confidence."""
        base = predictor.calculate_prediction_confidence(0.5, 75.0)
        extreme = predictor.calculate_prediction_confidence(0.05, 75.0)
        assert extreme > base

    def test_extreme_quality_boosts_confidence(self, predictor):
        """Extreme quality_score (< 50 or > 90) increases confidence."""
        base = predictor.calculate_prediction_confidence(0.5, 75.0)
        extreme = predictor.calculate_prediction_confidence(0.5, 95.0)
        assert extreme > base


# ---------------------------------------------------------------------------
# Recommendations
# ---------------------------------------------------------------------------

class TestGenerateQualityRecommendations:
    """Tests for generate_quality_recommendations."""

    def test_high_defect_risk_recommendations(self, predictor):
        recs = predictor.generate_quality_recommendations(0.6, 90.0, {})
        assert any("Stop production" in r for r in recs)

    def test_elevated_defect_risk_recommendations(self, predictor):
        recs = predictor.generate_quality_recommendations(0.35, 90.0, {})
        assert any("inspection frequency" in r.lower() for r in recs)

    def test_low_quality_with_high_temperature(self, predictor):
        recs = predictor.generate_quality_recommendations(0.1, 60.0, {"temperature": 150})
        assert any("temperature" in r.lower() for r in recs)

    def test_low_quality_with_tool_wear(self, predictor):
        recs = predictor.generate_quality_recommendations(0.1, 60.0, {"tool_wear": 0.9})
        assert any("tooling" in r.lower() for r in recs)

    def test_low_quality_with_vibration(self, predictor):
        recs = predictor.generate_quality_recommendations(0.1, 60.0, {"vibration": 15})
        assert any("alignment" in r.lower() or "balance" in r.lower() for r in recs)

    def test_acceptable_quality_returns_maintain_message(self, predictor):
        recs = predictor.generate_quality_recommendations(0.05, 92.0, {})
        assert any("acceptable range" in r.lower() for r in recs)

    def test_marginal_cpk_recommendation(self, predictor):
        predictor.process_capability = {"current": {"cpk": 1.1}}
        recs = predictor.generate_quality_recommendations(0.05, 90.0, {})
        assert any("marginally capable" in r.lower() for r in recs)


# ---------------------------------------------------------------------------
# Critical Factors
# ---------------------------------------------------------------------------

class TestIdentifyCriticalFactors:
    """Tests for identify_critical_factors."""

    def test_feature_importance_top3(self, predictor):
        predictor.feature_importance = {
            "temperature": 0.4, "pressure": 0.3, "speed": 0.2, "humidity": 0.05
        }
        factors = predictor.identify_critical_factors({}, 90.0)
        assert factors[:3] == ["temperature", "pressure", "speed"]

    def test_rule_based_tool_wear_critical(self, predictor):
        factors = predictor.identify_critical_factors({"tool_wear": 0.9}, 70.0)
        assert "tool_wear_critical" in factors

    def test_rule_based_temperature_out_of_range(self, predictor):
        factors = predictor.identify_critical_factors({"temperature": 50}, 70.0)
        assert "temperature_out_of_range" in factors

        factors_high = predictor.identify_critical_factors({"temperature": 110}, 70.0)
        assert "temperature_out_of_range" in factors_high

    def test_rule_based_excessive_vibration(self, predictor):
        factors = predictor.identify_critical_factors({"vibration": 20}, 70.0)
        assert "excessive_vibration" in factors

    def test_no_critical_factors_for_normal_data(self, predictor):
        factors = predictor.identify_critical_factors(
            {"tool_wear": 0.3, "temperature": 80, "vibration": 3}, 90.0
        )
        assert len(factors) == 0


# ---------------------------------------------------------------------------
# Statistical Process Control (SPC)
# ---------------------------------------------------------------------------

class TestSPC:
    """Tests for update_control_limits and check_control_limits."""

    def test_update_control_limits_structure(self, predictor):
        measurements = np.array([10, 11, 10.5, 9.8, 10.2, 10.1])
        predictor.update_control_limits("diameter", measurements)

        limits = predictor.control_limits["diameter"]
        assert "ucl" in limits
        assert "lcl" in limits
        assert "uwl" in limits
        assert "lwl" in limits
        assert "cl" in limits
        assert "std" in limits
        assert limits["ucl"] > limits["uwl"] > limits["cl"] > limits["lwl"] > limits["lcl"]

    def test_check_normal_value(self, predictor):
        predictor.control_limits["diameter"] = {
            "ucl": 12, "lcl": 8, "uwl": 11, "lwl": 9, "cl": 10, "std": 0.67
        }
        result = predictor.check_control_limits("diameter", 10.0)
        assert result["in_control"] is True
        assert result["zone"] == "normal"

    def test_check_warning_zone(self, predictor):
        predictor.control_limits["diameter"] = {
            "ucl": 12, "lcl": 8, "uwl": 11, "lwl": 9, "cl": 10, "std": 0.67
        }
        result = predictor.check_control_limits("diameter", 11.5)
        assert result["in_control"] is True
        assert result["zone"] == "warning"

    def test_check_out_of_control(self, predictor):
        predictor.control_limits["diameter"] = {
            "ucl": 12, "lcl": 8, "uwl": 11, "lwl": 9, "cl": 10, "std": 0.67
        }
        result = predictor.check_control_limits("diameter", 13.0)
        assert result["in_control"] is False
        assert result["zone"] == "out_of_control"

    def test_unknown_parameter(self, predictor):
        result = predictor.check_control_limits("nonexistent", 5.0)
        assert result["in_control"] is True
        assert result["zone"] == "unknown"


# ---------------------------------------------------------------------------
# Process Capability
# ---------------------------------------------------------------------------

class TestCalculateProcessCapability:
    """Tests for calculate_process_capability."""

    def test_capable_process(self, predictor):
        measurements = np.random.normal(50, 1, 200)
        result = predictor.calculate_process_capability(measurements, (45, 55))
        assert result["cp"] > 1.0
        assert result["process_capable"] is True

    def test_incapable_process(self, predictor):
        measurements = np.random.normal(50, 5, 200)
        result = predictor.calculate_process_capability(measurements, (48, 52))
        assert result["cpk"] < 1.33
        assert result["process_capable"] is False

    def test_zero_std_returns_zero(self, predictor):
        measurements = np.array([50, 50, 50, 50])
        result = predictor.calculate_process_capability(measurements, (45, 55))
        assert result["cp"] == 0
        assert result["cpk"] == 0
        assert result["process_capable"] is False

    def test_off_center_process(self, predictor):
        """Cpk should be lower than Cp when process mean is off-center."""
        measurements = np.random.normal(53, 1, 200)
        result = predictor.calculate_process_capability(measurements, (45, 55))
        assert result["cpk"] < result["cp"]


# ---------------------------------------------------------------------------
# Quality Trend Detection
# ---------------------------------------------------------------------------

class TestDetectQualityTrends:
    """Tests for detect_quality_trends."""

    def test_insufficient_data_returns_stable(self, predictor):
        df = pd.DataFrame({"quality_score": [90, 91, 89]})
        trends = predictor.detect_quality_trends(df)
        assert trends["stable"] is True

    def test_improving_trend(self, predictor):
        scores = np.linspace(70, 95, 30)
        df = pd.DataFrame({"quality_score": scores})
        trends = predictor.detect_quality_trends(df)
        assert trends["improving"] is True

    def test_degrading_trend(self, predictor):
        scores = np.linspace(95, 70, 30)
        df = pd.DataFrame({"quality_score": scores})
        trends = predictor.detect_quality_trends(df)
        assert trends["degrading"] is True

    def test_shift_detection_with_outlier(self, predictor):
        """A single extreme outlier triggers shift detection (Western Electric rule 1)."""
        scores = np.random.normal(85, 1, 30)
        scores[15] = 120  # extreme outlier
        df = pd.DataFrame({"quality_score": scores})
        trends = predictor.detect_quality_trends(df)
        assert trends["shift_detected"] is True

    def test_missing_quality_score_column(self, predictor):
        """DataFrame without 'quality_score' column returns default trends."""
        df = pd.DataFrame({"other_metric": np.random.randn(20)})
        trends = predictor.detect_quality_trends(df)
        # No quality_scores branch taken, only the len >= 10 check passes
        # but quality_scores is None so all trends remain False except stable set before
        assert isinstance(trends, dict)


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------

class TestGetMetrics:
    """Tests for get_metrics."""

    def test_initial_metrics_structure(self, predictor):
        metrics = predictor.get_metrics()
        assert metrics["accuracy"] == 0.0
        assert metrics["precision"] == 0.0
        assert metrics["recall"] == 0.0
        assert metrics["f1_score"] == 0.0
        assert metrics["last_trained"] is None
        assert metrics["version"] == "1.0.0"
