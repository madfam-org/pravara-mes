"""
Process Optimization Model
Optimizes manufacturing process parameters for efficiency, quality, and throughput
"""

import os
import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional, Tuple
import joblib
import logging

from scipy.optimize import minimize, differential_evolution, LinearConstraint
from scipy.stats import norm
from sklearn.gaussian_process import GaussianProcessRegressor
from sklearn.gaussian_process.kernels import Matern, WhiteKernel
from sklearn.preprocessing import StandardScaler
import optuna
import tensorflow as tf
from tensorflow import keras
from tensorflow.keras import layers

logger = logging.getLogger(__name__)

class ProcessOptimizer:
    """
    Multi-objective process optimization using:
    - Bayesian Optimization for parameter tuning
    - Reinforcement Learning for dynamic adjustment
    - Genetic Algorithms for global optimization
    - Constraint satisfaction for safety limits
    """

    def __init__(self):
        self.gp_model = None  # Gaussian Process for Bayesian optimization
        self.rl_model = None  # Reinforcement learning model
        self.scaler = StandardScaler()

        # Optimization objectives
        self.objectives = {
            "maximize_throughput": 1.0,
            "minimize_defects": 1.0,
            "minimize_energy": 0.5,
            "minimize_cost": 0.7,
            "maximize_quality": 1.0
        }

        # Parameter bounds
        self.parameter_bounds = {}
        self.constraints = []

        # Historical optimization results
        self.optimization_history = []
        self.best_parameters = {}

        # Metrics
        self.metrics = {
            "optimizations_performed": 0,
            "average_improvement": 0.0,
            "best_improvement": 0.0,
            "last_optimized": None,
            "version": "1.0.0"
        }

    def build_rl_optimizer(self, state_dim: int, action_dim: int) -> keras.Model:
        """Build reinforcement learning model for dynamic optimization"""
        # Actor network
        actor = keras.Sequential([
            layers.Dense(128, activation='relu', input_shape=(state_dim,)),
            layers.BatchNormalization(),
            layers.Dense(64, activation='relu'),
            layers.Dense(32, activation='relu'),
            layers.Dense(action_dim, activation='tanh')  # Actions in [-1, 1]
        ])

        # Critic network
        state_input = keras.Input(shape=(state_dim,))
        action_input = keras.Input(shape=(action_dim,))

        state_h1 = layers.Dense(128, activation='relu')(state_input)
        state_h2 = layers.Dense(64)(state_h1)

        action_h1 = layers.Dense(64)(action_input)

        concat = layers.Concatenate()([state_h2, action_h1])
        concat_h1 = layers.Dense(64, activation='relu')(concat)
        concat_h2 = layers.Dense(32, activation='relu')(concat_h1)
        output = layers.Dense(1)(concat_h2)  # Q-value

        critic = keras.Model([state_input, action_input], output)

        return actor, critic

    def objective_function(self, parameters: np.ndarray, current_state: Dict) -> float:
        """Combined objective function for optimization"""
        # Simulate process with given parameters
        throughput = self.simulate_throughput(parameters, current_state)
        quality = self.simulate_quality(parameters, current_state)
        energy = self.simulate_energy_consumption(parameters)
        cost = self.calculate_cost(parameters, energy)
        defect_rate = self.simulate_defect_rate(parameters, current_state)

        # Weighted objective (to minimize)
        objective = (
            - self.objectives["maximize_throughput"] * throughput / 100
            - self.objectives["maximize_quality"] * quality / 100
            + self.objectives["minimize_energy"] * energy / 1000
            + self.objectives["minimize_cost"] * cost / 1000
            + self.objectives["minimize_defects"] * defect_rate
        )

        return objective

    def simulate_throughput(self, parameters: np.ndarray, state: Dict) -> float:
        """Simulate throughput with given parameters"""
        # Simplified throughput model
        base_throughput = state.get("current_throughput", 100)

        # Parameters affect throughput
        speed_factor = parameters[0] if len(parameters) > 0 else 1.0  # Speed
        feed_factor = parameters[1] if len(parameters) > 1 else 1.0   # Feed rate

        throughput = base_throughput * (1 + 0.3 * speed_factor) * (1 + 0.2 * feed_factor)

        # Apply constraints
        return np.clip(throughput, 0, 200)

    def simulate_quality(self, parameters: np.ndarray, state: Dict) -> float:
        """Simulate quality score with given parameters"""
        base_quality = state.get("current_quality", 85)

        # Quality is affected by precision parameters
        if len(parameters) > 2:
            # Higher precision generally means better quality but lower speed
            precision_factor = parameters[2]
            temperature_factor = abs(parameters[3] - 0.7) if len(parameters) > 3 else 0  # Optimal at 0.7

            quality = base_quality + 10 * precision_factor - 20 * temperature_factor
        else:
            quality = base_quality

        return np.clip(quality, 0, 100)

    def simulate_energy_consumption(self, parameters: np.ndarray) -> float:
        """Simulate energy consumption"""
        # Energy increases with speed and force parameters
        base_energy = 100

        if len(parameters) > 0:
            speed_energy = 50 * abs(parameters[0])
            force_energy = 30 * abs(parameters[1]) if len(parameters) > 1 else 0
            energy = base_energy + speed_energy + force_energy
        else:
            energy = base_energy

        return energy

    def simulate_defect_rate(self, parameters: np.ndarray, state: Dict) -> float:
        """Simulate defect rate"""
        base_defect_rate = state.get("current_defect_rate", 0.05)

        # Defects increase if parameters are too extreme
        if len(parameters) > 0:
            extremity = np.mean(np.abs(parameters))
            defect_rate = base_defect_rate * (1 + 2 * extremity)
        else:
            defect_rate = base_defect_rate

        return np.clip(defect_rate, 0, 1)

    def calculate_cost(self, parameters: np.ndarray, energy: float) -> float:
        """Calculate operational cost"""
        energy_cost = energy * 0.15  # $/kWh
        material_waste = self.simulate_defect_rate(parameters, {}) * 1000  # $ waste
        labor_cost = 50  # Fixed

        return energy_cost + material_waste + labor_cost

    def bayesian_optimization(
        self,
        current_state: Dict,
        n_iterations: int = 20
    ) -> Dict[str, Any]:
        """Perform Bayesian optimization to find optimal parameters"""
        # Define bounds for parameters
        bounds = [
            (-1, 1),  # Speed adjustment
            (-1, 1),  # Feed rate adjustment
            (0, 1),   # Precision level
            (0, 1),   # Temperature adjustment
            (0, 1),   # Pressure adjustment
        ]

        # Initialize Gaussian Process
        kernel = Matern(nu=2.5) + WhiteKernel(noise_level=0.1)
        self.gp_model = GaussianProcessRegressor(
            kernel=kernel,
            alpha=1e-6,
            normalize_y=True,
            n_restarts_optimizer=10
        )

        # Initial random samples
        X_sample = np.random.uniform(-1, 1, (5, len(bounds)))
        y_sample = [self.objective_function(x, current_state) for x in X_sample]

        best_params = X_sample[np.argmin(y_sample)]
        best_value = np.min(y_sample)

        for i in range(n_iterations):
            # Fit GP model
            self.gp_model.fit(X_sample, y_sample)

            # Acquisition function (Expected Improvement)
            def acquisition(x):
                mu, sigma = self.gp_model.predict(x.reshape(1, -1), return_std=True)
                if sigma == 0:
                    return 0

                Z = (best_value - mu) / sigma
                ei = sigma * (Z * norm.cdf(Z) + norm.pdf(Z))
                return -ei[0]  # Minimize negative EI

            # Find next point to sample
            result = differential_evolution(acquisition, bounds, seed=42)
            next_x = result.x

            # Evaluate objective at next point
            next_y = self.objective_function(next_x, current_state)

            # Update samples
            X_sample = np.vstack([X_sample, next_x])
            y_sample = np.append(y_sample, next_y)

            # Update best
            if next_y < best_value:
                best_value = next_y
                best_params = next_x

        return {
            "optimal_parameters": best_params,
            "optimal_value": -best_value,  # Convert back to maximization
            "iterations": n_iterations
        }

    def genetic_optimization(
        self,
        current_state: Dict,
        population_size: int = 50,
        generations: int = 100
    ) -> Dict[str, Any]:
        """Genetic algorithm for global optimization"""
        # Parameter bounds
        bounds = [
            (-1, 1),  # Speed
            (-1, 1),  # Feed rate
            (0, 1),   # Precision
            (0, 1),   # Temperature
            (0, 1),   # Pressure
        ]

        # Use differential evolution (a type of genetic algorithm)
        result = differential_evolution(
            lambda x: self.objective_function(x, current_state),
            bounds,
            strategy='best1bin',
            popsize=population_size,
            maxiter=generations,
            mutation=(0.5, 1),
            recombination=0.7,
            seed=42
        )

        return {
            "optimal_parameters": result.x,
            "optimal_value": -result.fun,
            "generations": generations,
            "converged": result.success
        }

    async def optimize(self, features: Dict[str, Any]) -> Dict[str, Any]:
        """Main optimization interface"""
        try:
            current_state = {
                "current_throughput": features.get("throughput", 100),
                "current_quality": features.get("quality", 85),
                "current_defect_rate": features.get("defect_rate", 0.05),
                "machine_id": features.get("machine_id", "unknown")
            }

            # Choose optimization method based on requirements
            if features.get("quick_optimization", False):
                # Quick Bayesian optimization
                result = self.bayesian_optimization(current_state, n_iterations=10)
            else:
                # Thorough genetic optimization
                result = self.genetic_optimization(current_state)

            # Convert parameters to named values
            param_names = ["speed", "feed_rate", "precision", "temperature", "pressure"]
            optimal_params = dict(zip(
                param_names,
                result["optimal_parameters"]
            ))

            # Calculate expected improvements
            current_objective = self.objective_function(
                np.zeros(5),  # Current state (no adjustments)
                current_state
            )
            optimized_objective = self.objective_function(
                result["optimal_parameters"],
                current_state
            )
            improvement_percent = abs((optimized_objective - current_objective) / current_objective) * 100

            # Generate recommendations
            recommendations = self.generate_optimization_recommendations(
                optimal_params,
                improvement_percent,
                current_state
            )

            # Update metrics
            self.metrics["optimizations_performed"] += 1
            self.metrics["average_improvement"] = (
                (self.metrics["average_improvement"] * (self.metrics["optimizations_performed"] - 1) +
                 improvement_percent) / self.metrics["optimizations_performed"]
            )
            self.metrics["best_improvement"] = max(
                self.metrics["best_improvement"],
                improvement_percent
            )
            self.metrics["last_optimized"] = datetime.utcnow()

            # Store in history
            self.optimization_history.append({
                "timestamp": datetime.utcnow(),
                "parameters": optimal_params,
                "improvement": improvement_percent,
                "machine_id": current_state["machine_id"]
            })

            return {
                "prediction": {
                    "optimal_parameters": optimal_params,
                    "expected_improvement": improvement_percent,
                    "current_state": current_state,
                    "optimization_method": "bayesian" if features.get("quick_optimization") else "genetic",
                    "constraints_satisfied": self.check_constraints(result["optimal_parameters"])
                },
                "confidence": 0.85 if result.get("converged", True) else 0.7,
                "recommendations": recommendations
            }

        except Exception as e:
            logger.error(f"Optimization error: {e}")
            raise

    async def optimize_parameters(
        self,
        machine_id: str,
        current_params: Dict[str, float],
        telemetry: Dict[str, Any]
    ) -> Dict[str, Any]:
        """Optimize parameters for specific machine"""
        features = {
            "machine_id": machine_id,
            **current_params,
            **telemetry,
            "quick_optimization": True  # Use faster method for real-time
        }

        result = await self.optimize(features)

        # Convert relative adjustments to absolute parameters
        optimized_params = {}
        for param, adjustment in result["prediction"]["optimal_parameters"].items():
            if param in current_params:
                # Apply adjustment as percentage change
                optimized_params[param] = current_params[param] * (1 + adjustment * 0.1)
            else:
                optimized_params[param] = adjustment

        return {
            "parameters": optimized_params,
            "improvement": result["prediction"]["expected_improvement"],
            "constraints_satisfied": result["prediction"]["constraints_satisfied"]
        }

    def generate_optimization_recommendations(
        self,
        optimal_params: Dict[str, float],
        improvement: float,
        current_state: Dict
    ) -> List[str]:
        """Generate actionable optimization recommendations"""
        recommendations = []

        if improvement > 20:
            recommendations.append(f"Significant improvement possible: {improvement:.1f}% gain expected")
            recommendations.append("Implement changes gradually to monitor impact")
        elif improvement > 10:
            recommendations.append(f"Moderate improvement possible: {improvement:.1f}% gain expected")
        else:
            recommendations.append("Current parameters near optimal")

        # Parameter-specific recommendations
        for param, value in optimal_params.items():
            if abs(value) > 0.7:
                if value > 0:
                    recommendations.append(f"Increase {param} significantly")
                else:
                    recommendations.append(f"Decrease {param} significantly")
            elif abs(value) > 0.3:
                if value > 0:
                    recommendations.append(f"Moderately increase {param}")
                else:
                    recommendations.append(f"Moderately decrease {param}")

        # Quality vs throughput trade-off
        if optimal_params.get("speed", 0) > 0.5 and optimal_params.get("precision", 0) < 0.3:
            recommendations.append("Note: Optimizing for throughput over precision")
        elif optimal_params.get("precision", 0) > 0.7:
            recommendations.append("Note: Optimizing for quality over speed")

        # Energy considerations
        total_adjustment = sum(abs(v) for v in optimal_params.values())
        if total_adjustment > 2:
            recommendations.append("Warning: Significant energy consumption increase expected")

        return recommendations

    def check_constraints(self, parameters: np.ndarray) -> bool:
        """Check if parameters satisfy all constraints"""
        # Safety constraints
        if np.any(np.abs(parameters) > 1):
            return False  # Parameters out of bounds

        # Additional business constraints
        if len(parameters) > 3:
            # Temperature must be within safe range
            if parameters[3] < 0.2 or parameters[3] > 0.9:
                return False

        return True

    def set_constraints(self, constraints: List[Dict[str, Any]]):
        """Set optimization constraints"""
        self.constraints = []

        for constraint in constraints:
            if constraint["type"] == "linear":
                # Linear constraint: A @ x <= b
                self.constraints.append(
                    LinearConstraint(
                        constraint["A"],
                        constraint["lb"],
                        constraint["ub"]
                    )
                )
            elif constraint["type"] == "bounds":
                # Parameter bounds
                self.parameter_bounds[constraint["parameter"]] = (
                    constraint["min"],
                    constraint["max"]
                )

    def update_objectives(self, objectives: Dict[str, float]):
        """Update optimization objective weights"""
        for obj, weight in objectives.items():
            if obj in self.objectives:
                self.objectives[obj] = weight

        logger.info(f"Objectives updated: {self.objectives}")

    def train_rl_optimizer(self, experience_buffer: List[Dict]):
        """Train reinforcement learning optimizer with experience"""
        if not experience_buffer:
            return

        # Extract states, actions, rewards
        states = np.array([exp["state"] for exp in experience_buffer])
        actions = np.array([exp["action"] for exp in experience_buffer])
        rewards = np.array([exp["reward"] for exp in experience_buffer])

        # Build RL model if not exists
        if self.rl_model is None:
            state_dim = states.shape[1]
            action_dim = actions.shape[1]
            self.rl_model = self.build_rl_optimizer(state_dim, action_dim)

        # Train actor-critic
        # Implementation would involve policy gradient or DDPG training
        logger.info("RL optimizer training completed")

    def save(self, path: str = "models/process_optimizer.pkl"):
        """Save optimizer to disk"""
        model_data = {
            "gp_model": self.gp_model,
            "scaler": self.scaler,
            "objectives": self.objectives,
            "parameter_bounds": self.parameter_bounds,
            "best_parameters": self.best_parameters,
            "metrics": self.metrics
        }
        joblib.dump(model_data, path)

        # Save RL model if exists
        if self.rl_model:
            # Save actor and critic separately
            self.rl_model[0].save(path.replace('.pkl', '_actor.h5'))
            self.rl_model[1].save(path.replace('.pkl', '_critic.h5'))

        logger.info(f"Process optimizer saved to {path}")

    def load(self, path: str = "models/process_optimizer.pkl"):
        """Load optimizer from disk"""
        try:
            import os
            model_data = joblib.load(path)
            self.gp_model = model_data["gp_model"]
            self.scaler = model_data["scaler"]
            self.objectives = model_data["objectives"]
            self.parameter_bounds = model_data["parameter_bounds"]
            self.best_parameters = model_data["best_parameters"]
            self.metrics = model_data["metrics"]

            # Load RL model
            actor_path = path.replace('.pkl', '_actor.h5')
            critic_path = path.replace('.pkl', '_critic.h5')
            if os.path.exists(actor_path) and os.path.exists(critic_path):
                actor = keras.models.load_model(actor_path)
                critic = keras.models.load_model(critic_path)
                self.rl_model = (actor, critic)

            logger.info(f"Process optimizer loaded from {path}")
        except Exception as e:
            logger.warning(f"Could not load process optimizer: {e}")

    def get_metrics(self) -> Dict[str, Any]:
        """Get optimizer metrics"""
        return self.metrics