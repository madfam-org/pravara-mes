"""
Test Process Optimizer Model

Covers:
- Optimization parameter input validation
- Objective function computation
- Simulation sub-functions (throughput, quality, energy, defect rate, cost)
- Constraint handling and checking
- Bayesian and genetic optimization (mocked scipy/optuna)
- Full optimize pipeline output format
- Recommendation generation
- Edge cases: infeasible constraints, convergence failure, empty inputs
"""

import pytest
import numpy as np
from unittest.mock import MagicMock, patch, AsyncMock
from datetime import datetime

from models.process_optimizer import ProcessOptimizer


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def optimizer():
    """Create a fresh ProcessOptimizer instance."""
    return ProcessOptimizer()


@pytest.fixture
def default_state():
    """Default current state for optimization."""
    return {
        "current_throughput": 100,
        "current_quality": 85,
        "current_defect_rate": 0.05,
        "machine_id": "CNC-001",
    }


@pytest.fixture
def default_features():
    """Default features dict for the optimize method."""
    return {
        "throughput": 100,
        "quality": 85,
        "defect_rate": 0.05,
        "machine_id": "CNC-001",
    }


@pytest.fixture
def five_params():
    """A 5-element parameter vector within bounds."""
    return np.array([0.3, -0.2, 0.6, 0.7, 0.5])


# ---------------------------------------------------------------------------
# Initialization
# ---------------------------------------------------------------------------

class TestInit:
    """Verify initial state of ProcessOptimizer."""

    def test_default_objectives(self, optimizer):
        assert optimizer.objectives["maximize_throughput"] == 1.0
        assert optimizer.objectives["minimize_defects"] == 1.0
        assert optimizer.objectives["minimize_energy"] == 0.5
        assert optimizer.objectives["minimize_cost"] == 0.7
        assert optimizer.objectives["maximize_quality"] == 1.0

    def test_no_trained_models(self, optimizer):
        assert optimizer.gp_model is None
        assert optimizer.rl_model is None

    def test_empty_history(self, optimizer):
        assert optimizer.optimization_history == []

    def test_initial_metrics(self, optimizer):
        m = optimizer.get_metrics()
        assert m["optimizations_performed"] == 0
        assert m["average_improvement"] == 0.0
        assert m["best_improvement"] == 0.0
        assert m["last_optimized"] is None
        assert m["version"] == "1.0.0"


# ---------------------------------------------------------------------------
# Simulation Sub-Functions
# ---------------------------------------------------------------------------

class TestSimulateThroughput:
    """Tests for simulate_throughput."""

    def test_baseline_throughput(self, optimizer, default_state):
        """Zero adjustments return baseline throughput."""
        result = optimizer.simulate_throughput(np.array([0.0, 0.0]), default_state)
        assert result == pytest.approx(100.0)

    def test_positive_adjustments_increase(self, optimizer, default_state):
        result = optimizer.simulate_throughput(np.array([0.5, 0.5]), default_state)
        assert result > 100.0

    def test_clipped_to_200(self, optimizer, default_state):
        result = optimizer.simulate_throughput(np.array([10.0, 10.0]), default_state)
        assert result <= 200.0

    def test_clipped_to_0(self, optimizer):
        state = {"current_throughput": 10}
        result = optimizer.simulate_throughput(np.array([-10.0, -10.0]), state)
        assert result >= 0.0

    def test_empty_params(self, optimizer, default_state):
        """Empty parameter array uses default factors."""
        result = optimizer.simulate_throughput(np.array([]), default_state)
        assert result == pytest.approx(100.0)


class TestSimulateQuality:
    """Tests for simulate_quality."""

    def test_baseline_quality(self, optimizer, default_state):
        result = optimizer.simulate_quality(np.array([0, 0]), default_state)
        assert result == pytest.approx(85.0)

    def test_high_precision_increases_quality(self, optimizer, default_state):
        # parameters[2] = precision, parameters[3] = temperature at optimal 0.7
        result = optimizer.simulate_quality(np.array([0, 0, 1.0, 0.7]), default_state)
        assert result > 85.0

    def test_far_from_optimal_temperature_decreases_quality(self, optimizer, default_state):
        result_optimal = optimizer.simulate_quality(np.array([0, 0, 0.5, 0.7]), default_state)
        result_bad = optimizer.simulate_quality(np.array([0, 0, 0.5, 0.0]), default_state)
        assert result_optimal > result_bad

    def test_clipped_to_0_100(self, optimizer, default_state):
        result = optimizer.simulate_quality(np.array([0, 0, 0, 10.0]), default_state)
        assert 0 <= result <= 100


class TestSimulateEnergyConsumption:
    """Tests for simulate_energy_consumption."""

    def test_base_energy(self, optimizer):
        result = optimizer.simulate_energy_consumption(np.array([]))
        assert result == 100.0

    def test_energy_increases_with_speed(self, optimizer):
        low = optimizer.simulate_energy_consumption(np.array([0.1, 0.0]))
        high = optimizer.simulate_energy_consumption(np.array([0.9, 0.0]))
        assert high > low

    def test_negative_params_use_abs(self, optimizer):
        pos = optimizer.simulate_energy_consumption(np.array([0.5, 0.3]))
        neg = optimizer.simulate_energy_consumption(np.array([-0.5, -0.3]))
        assert pos == pytest.approx(neg)


class TestSimulateDefectRate:
    """Tests for simulate_defect_rate."""

    def test_baseline_defect_rate(self, optimizer, default_state):
        result = optimizer.simulate_defect_rate(np.array([0.0, 0.0]), default_state)
        assert result == pytest.approx(0.05)

    def test_extreme_params_increase_defects(self, optimizer, default_state):
        result = optimizer.simulate_defect_rate(np.array([1.0, 1.0, 1.0]), default_state)
        assert result > 0.05

    def test_clipped_to_0_1(self, optimizer, default_state):
        result = optimizer.simulate_defect_rate(np.array([100.0]), default_state)
        assert 0 <= result <= 1

    def test_empty_params(self, optimizer, default_state):
        result = optimizer.simulate_defect_rate(np.array([]), default_state)
        assert result == pytest.approx(0.05)


class TestCalculateCost:
    """Tests for calculate_cost."""

    def test_cost_includes_energy_material_labor(self, optimizer):
        energy = 200.0
        cost = optimizer.calculate_cost(np.array([0.0]), energy)
        expected_energy_cost = 200.0 * 0.15
        labor = 50
        assert cost >= expected_energy_cost + labor

    def test_higher_energy_higher_cost(self, optimizer):
        cost_low = optimizer.calculate_cost(np.array([0.0]), 100)
        cost_high = optimizer.calculate_cost(np.array([0.0]), 500)
        assert cost_high > cost_low


# ---------------------------------------------------------------------------
# Objective Function
# ---------------------------------------------------------------------------

class TestObjectiveFunction:
    """Tests for objective_function."""

    def test_returns_float(self, optimizer, default_state, five_params):
        result = optimizer.objective_function(five_params, default_state)
        assert isinstance(result, float)

    def test_zero_params_baseline(self, optimizer, default_state):
        """Zero parameters produce a deterministic baseline objective."""
        result = optimizer.objective_function(np.zeros(5), default_state)
        assert np.isfinite(result)

    def test_different_params_different_objectives(self, optimizer, default_state):
        obj1 = optimizer.objective_function(np.array([0.1, 0.1, 0.5, 0.7, 0.5]), default_state)
        obj2 = optimizer.objective_function(np.array([0.9, 0.9, 0.1, 0.3, 0.1]), default_state)
        assert obj1 != obj2

    def test_objective_weights_affect_result(self, optimizer, default_state, five_params):
        """Changing objective weights changes the combined objective value."""
        obj_original = optimizer.objective_function(five_params, default_state)

        optimizer.objectives["minimize_energy"] = 5.0
        obj_heavy_energy = optimizer.objective_function(five_params, default_state)

        assert obj_original != obj_heavy_energy
        # Restore
        optimizer.objectives["minimize_energy"] = 0.5


# ---------------------------------------------------------------------------
# Constraint Handling
# ---------------------------------------------------------------------------

class TestCheckConstraints:
    """Tests for check_constraints."""

    def test_valid_parameters_pass(self, optimizer, five_params):
        assert optimizer.check_constraints(five_params) is True

    def test_out_of_bounds_fails(self, optimizer):
        params = np.array([1.5, 0, 0, 0.5, 0.5])
        assert optimizer.check_constraints(params) is False

    def test_temperature_too_low_fails(self, optimizer):
        params = np.array([0, 0, 0, 0.1, 0.5])  # temperature < 0.2
        assert optimizer.check_constraints(params) is False

    def test_temperature_too_high_fails(self, optimizer):
        params = np.array([0, 0, 0, 0.95, 0.5])  # temperature > 0.9
        assert optimizer.check_constraints(params) is False

    def test_boundary_temperature_passes(self, optimizer):
        params = np.array([0, 0, 0, 0.5, 0.5])  # within [0.2, 0.9]
        assert optimizer.check_constraints(params) is True

    def test_short_parameter_vector_skips_temperature_check(self, optimizer):
        params = np.array([0.5, -0.5, 0.3])
        assert optimizer.check_constraints(params) is True


class TestSetConstraints:
    """Tests for set_constraints."""

    def test_bounds_constraint(self, optimizer):
        constraints = [
            {"type": "bounds", "parameter": "speed", "min": -0.5, "max": 0.5}
        ]
        optimizer.set_constraints(constraints)
        assert optimizer.parameter_bounds["speed"] == (-0.5, 0.5)

    def test_linear_constraint(self, optimizer):
        constraints = [
            {"type": "linear", "A": np.eye(2), "lb": -1, "ub": 1}
        ]
        optimizer.set_constraints(constraints)
        assert len(optimizer.constraints) == 1

    def test_multiple_constraints(self, optimizer):
        constraints = [
            {"type": "bounds", "parameter": "speed", "min": -0.5, "max": 0.5},
            {"type": "bounds", "parameter": "pressure", "min": 0, "max": 1},
        ]
        optimizer.set_constraints(constraints)
        assert len(optimizer.parameter_bounds) == 2


class TestUpdateObjectives:
    """Tests for update_objectives."""

    def test_updates_known_objectives(self, optimizer):
        optimizer.update_objectives({"minimize_energy": 2.0, "maximize_quality": 0.5})
        assert optimizer.objectives["minimize_energy"] == 2.0
        assert optimizer.objectives["maximize_quality"] == 0.5

    def test_ignores_unknown_objectives(self, optimizer):
        original = dict(optimizer.objectives)
        optimizer.update_objectives({"nonexistent_objective": 99.0})
        assert optimizer.objectives == original


# ---------------------------------------------------------------------------
# Optimization Methods (mocked)
# ---------------------------------------------------------------------------

class TestBayesianOptimization:
    """Tests for bayesian_optimization with minimal iterations."""

    def test_returns_expected_structure(self, optimizer, default_state):
        result = optimizer.bayesian_optimization(default_state, n_iterations=2)

        assert "optimal_parameters" in result
        assert "optimal_value" in result
        assert "iterations" in result
        assert result["iterations"] == 2
        assert isinstance(result["optimal_parameters"], np.ndarray)
        assert len(result["optimal_parameters"]) == 5

    def test_optimal_value_is_finite(self, optimizer, default_state):
        result = optimizer.bayesian_optimization(default_state, n_iterations=2)
        assert np.isfinite(result["optimal_value"])


class TestGeneticOptimization:
    """Tests for genetic_optimization with mocked differential_evolution."""

    @patch("models.process_optimizer.differential_evolution")
    def test_returns_expected_structure(self, mock_de, optimizer, default_state):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, -0.1, 0.5, 0.7, 0.4]),
            fun=-1.5,
            success=True,
        )
        result = optimizer.genetic_optimization(default_state, population_size=5, generations=5)

        assert "optimal_parameters" in result
        assert "optimal_value" in result
        assert "generations" in result
        assert "converged" in result
        assert result["converged"] is True

    @patch("models.process_optimizer.differential_evolution")
    def test_convergence_failure(self, mock_de, optimizer, default_state):
        mock_de.return_value = MagicMock(
            x=np.array([0.0, 0.0, 0.0, 0.5, 0.5]),
            fun=-0.1,
            success=False,
        )
        result = optimizer.genetic_optimization(default_state)
        assert result["converged"] is False


# ---------------------------------------------------------------------------
# Full Optimize Pipeline (async)
# ---------------------------------------------------------------------------

class TestOptimize:
    """Tests for the async optimize method."""

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_output_structure(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.2, -0.1, 0.6, 0.7, 0.4]),
            fun=-1.2,
            success=True,
        )
        result = await optimizer.optimize(default_features)

        assert "prediction" in result
        assert "confidence" in result
        assert "recommendations" in result

        pred = result["prediction"]
        assert "optimal_parameters" in pred
        assert "expected_improvement" in pred
        assert "current_state" in pred
        assert "optimization_method" in pred
        assert "constraints_satisfied" in pred

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_genetic_method_by_default(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        result = await optimizer.optimize(default_features)
        assert result["prediction"]["optimization_method"] == "genetic"

    @pytest.mark.asyncio
    async def test_quick_optimization_uses_bayesian(self, optimizer):
        features = {
            "throughput": 100,
            "quality": 85,
            "defect_rate": 0.05,
            "machine_id": "CNC-001",
            "quick_optimization": True,
        }
        # Bayesian runs actual scipy calls; use minimal iterations
        with patch.object(optimizer, "bayesian_optimization") as mock_bo:
            mock_bo.return_value = {
                "optimal_parameters": np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
                "optimal_value": 1.5,
                "iterations": 10,
            }
            result = await optimizer.optimize(features)
            assert result["prediction"]["optimization_method"] == "bayesian"
            mock_bo.assert_called_once()

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_metrics_updated_after_optimize(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.2, 0.2, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        await optimizer.optimize(default_features)

        m = optimizer.get_metrics()
        assert m["optimizations_performed"] == 1
        assert m["average_improvement"] > 0
        assert m["last_optimized"] is not None

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_history_appended(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        await optimizer.optimize(default_features)
        assert len(optimizer.optimization_history) == 1
        entry = optimizer.optimization_history[0]
        assert "timestamp" in entry
        assert "parameters" in entry
        assert "improvement" in entry
        assert "machine_id" in entry

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_confidence_lower_when_not_converged(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.0, 0.0, 0.5, 0.5, 0.5]),
            fun=-0.5,
            success=False,
        )
        result = await optimizer.optimize(default_features)
        assert result["confidence"] == 0.7

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_confidence_higher_when_converged(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        result = await optimizer.optimize(default_features)
        assert result["confidence"] == 0.85

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_optimal_parameters_are_named(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, -0.2, 0.6, 0.7, 0.4]),
            fun=-1.0,
            success=True,
        )
        result = await optimizer.optimize(default_features)
        params = result["prediction"]["optimal_parameters"]
        expected_keys = {"speed", "feed_rate", "precision", "temperature", "pressure"}
        assert set(params.keys()) == expected_keys

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_constraints_satisfied_in_output(self, mock_de, optimizer, default_features):
        mock_de.return_value = MagicMock(
            x=np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        result = await optimizer.optimize(default_features)
        assert isinstance(result["prediction"]["constraints_satisfied"], bool)


# ---------------------------------------------------------------------------
# Recommendations
# ---------------------------------------------------------------------------

class TestGenerateOptimizationRecommendations:
    """Tests for generate_optimization_recommendations."""

    def test_significant_improvement(self, optimizer, default_state):
        params = {"speed": 0.8, "feed_rate": -0.1, "precision": 0.5, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 25.0, default_state)
        assert any("Significant improvement" in r for r in recs)
        assert any("gradually" in r.lower() for r in recs)

    def test_moderate_improvement(self, optimizer, default_state):
        params = {"speed": 0.1, "feed_rate": 0.1, "precision": 0.5, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 15.0, default_state)
        assert any("Moderate improvement" in r for r in recs)

    def test_near_optimal(self, optimizer, default_state):
        params = {"speed": 0.05, "feed_rate": 0.02, "precision": 0.5, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 3.0, default_state)
        assert any("near optimal" in r.lower() for r in recs)

    def test_significant_param_change_recommendation(self, optimizer, default_state):
        params = {"speed": 0.9, "feed_rate": -0.8, "precision": 0.5, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 20.0, default_state)
        assert any("Increase speed" in r for r in recs)
        assert any("Decrease feed_rate" in r for r in recs)

    def test_throughput_over_precision_note(self, optimizer, default_state):
        params = {"speed": 0.6, "feed_rate": 0.1, "precision": 0.2, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 15.0, default_state)
        assert any("throughput over precision" in r.lower() for r in recs)

    def test_quality_over_speed_note(self, optimizer, default_state):
        params = {"speed": 0.1, "feed_rate": 0.1, "precision": 0.8, "temperature": 0.7, "pressure": 0.5}
        recs = optimizer.generate_optimization_recommendations(params, 15.0, default_state)
        assert any("quality over speed" in r.lower() for r in recs)

    def test_energy_warning(self, optimizer, default_state):
        params = {"speed": 0.9, "feed_rate": 0.8, "precision": 0.7, "temperature": 0.7, "pressure": 0.6}
        recs = optimizer.generate_optimization_recommendations(params, 20.0, default_state)
        assert any("energy consumption" in r.lower() for r in recs)


# ---------------------------------------------------------------------------
# Optimize Parameters (async wrapper)
# ---------------------------------------------------------------------------

class TestOptimizeParameters:
    """Tests for optimize_parameters method."""

    @pytest.mark.asyncio
    async def test_returns_expected_structure(self, optimizer):
        with patch.object(optimizer, "optimize", new_callable=AsyncMock) as mock_opt:
            mock_opt.return_value = {
                "prediction": {
                    "optimal_parameters": {"speed": 0.1, "feed_rate": -0.05},
                    "expected_improvement": 12.5,
                    "constraints_satisfied": True,
                },
                "confidence": 0.85,
                "recommendations": ["Test rec"],
            }

            result = await optimizer.optimize_parameters(
                machine_id="CNC-001",
                current_params={"speed": 1500, "feed_rate": 0.5},
                telemetry={"vibration": 4.0},
            )

            assert "parameters" in result
            assert "improvement" in result
            assert "constraints_satisfied" in result

    @pytest.mark.asyncio
    async def test_applies_relative_adjustment(self, optimizer):
        with patch.object(optimizer, "optimize", new_callable=AsyncMock) as mock_opt:
            mock_opt.return_value = {
                "prediction": {
                    "optimal_parameters": {"speed": 0.5, "feed_rate": -0.2},
                    "expected_improvement": 10.0,
                    "constraints_satisfied": True,
                },
                "confidence": 0.85,
                "recommendations": [],
            }

            result = await optimizer.optimize_parameters(
                machine_id="CNC-001",
                current_params={"speed": 1000, "feed_rate": 2.0},
                telemetry={},
            )

            # speed: 1000 * (1 + 0.5 * 0.1) = 1050
            assert result["parameters"]["speed"] == pytest.approx(1050.0)
            # feed_rate: 2.0 * (1 + (-0.2) * 0.1) = 1.96
            assert result["parameters"]["feed_rate"] == pytest.approx(1.96)


# ---------------------------------------------------------------------------
# Edge Cases
# ---------------------------------------------------------------------------

class TestEdgeCases:
    """Edge case and boundary condition tests."""

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_zero_throughput_state(self, mock_de, optimizer):
        """Optimization with zero throughput should not crash."""
        mock_de.return_value = MagicMock(
            x=np.array([0, 0, 0.5, 0.5, 0.5]),
            fun=-0.5,
            success=True,
        )
        features = {"throughput": 0, "quality": 0, "defect_rate": 1.0, "machine_id": "X"}
        result = await optimizer.optimize(features)
        assert "prediction" in result

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_missing_optional_features(self, mock_de, optimizer):
        """Optimize with minimal features (no machine_id, no quality)."""
        mock_de.return_value = MagicMock(
            x=np.array([0, 0, 0.5, 0.5, 0.5]),
            fun=-0.5,
            success=True,
        )
        result = await optimizer.optimize({})
        assert result["prediction"]["current_state"]["machine_id"] == "unknown"

    @pytest.mark.asyncio
    @patch("models.process_optimizer.differential_evolution")
    async def test_multiple_optimizations_update_average(self, mock_de, optimizer, default_features):
        """Running optimize twice correctly updates average improvement."""
        mock_de.return_value = MagicMock(
            x=np.array([0.1, 0.1, 0.5, 0.7, 0.5]),
            fun=-1.0,
            success=True,
        )
        await optimizer.optimize(default_features)
        first_avg = optimizer.metrics["average_improvement"]

        mock_de.return_value = MagicMock(
            x=np.array([0.3, 0.3, 0.5, 0.7, 0.5]),
            fun=-2.0,
            success=True,
        )
        await optimizer.optimize(default_features)

        assert optimizer.metrics["optimizations_performed"] == 2
        assert optimizer.metrics["average_improvement"] != first_avg

    def test_all_parameters_at_bounds(self, optimizer):
        """Parameters exactly at +/- 1 boundary should pass constraint check."""
        params = np.array([1.0, -1.0, 0.0, 0.5, 0.0])
        assert optimizer.check_constraints(params) is True

    def test_nan_parameters_in_objective(self, optimizer, default_state):
        """NaN parameters propagate to NaN objective (does not crash)."""
        params = np.array([np.nan, 0, 0, 0.5, 0.5])
        result = optimizer.objective_function(params, default_state)
        # Result may be NaN but should not raise
        assert isinstance(result, float)

    def test_train_rl_with_empty_buffer(self, optimizer):
        """Training RL with empty buffer should be a no-op."""
        optimizer.train_rl_optimizer([])
        assert optimizer.rl_model is None
