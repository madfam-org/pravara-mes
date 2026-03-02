"""
Inference Service
Manages model inference, feature preparation, and caching
"""

import logging
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional
import numpy as np
import pandas as pd
from cachetools import TTLCache
import asyncio
from concurrent.futures import ThreadPoolExecutor

logger = logging.getLogger(__name__)

class InferenceService:
    """
    Service for model inference:
    - Feature engineering and preparation
    - Batch and real-time inference
    - Result caching and optimization
    - Model ensemble coordination
    """

    def __init__(self):
        # Cache for inference results (TTL = 5 minutes)
        self.prediction_cache = TTLCache(maxsize=1000, ttl=300)
        self.feature_cache = TTLCache(maxsize=500, ttl=600)

        # Thread pool for parallel inference
        self.executor = ThreadPoolExecutor(max_workers=4)

        # Feature engineering pipelines
        self.feature_pipelines = {
            "maintenance": self.prepare_maintenance_features,
            "quality": self.prepare_quality_features,
            "anomaly": self.prepare_anomaly_features,
            "optimization": self.prepare_optimization_features
        }

        # Inference statistics
        self.stats = {
            "total_predictions": 0,
            "cache_hits": 0,
            "avg_inference_time": 0,
            "last_inference": None
        }

    def prepare_features(
        self,
        raw_data: Dict[str, Any],
        additional_features: Optional[Dict[str, Any]] = None
    ) -> Dict[str, Any]:
        """Generic feature preparation"""
        features = {}

        # Extract numeric features
        numeric_fields = [
            "temperature", "pressure", "vibration", "rpm", "power",
            "speed", "feed_rate", "tool_wear", "humidity"
        ]

        for field in numeric_fields:
            if field in raw_data:
                features[field] = float(raw_data[field])
            elif additional_features and field in additional_features:
                features[field] = float(additional_features[field])
            else:
                features[field] = 0.0  # Default value

        # Extract categorical features
        categorical_fields = ["machine_type", "material_type", "operation_mode"]

        for field in categorical_fields:
            if field in raw_data:
                features[field] = raw_data[field]
            elif additional_features and field in additional_features:
                features[field] = additional_features[field]

        # Calculate derived features
        features.update(self.calculate_derived_features(features))

        return features

    def calculate_derived_features(self, features: Dict[str, Any]) -> Dict[str, Any]:
        """Calculate derived features from base features"""
        derived = {}

        # Temperature gradient
        if "temperature" in features:
            derived["temp_deviation"] = abs(features["temperature"] - 75)  # From optimal

        # Power efficiency
        if "power" in features and "rpm" in features and features["rpm"] > 0:
            derived["power_efficiency"] = features["power"] / features["rpm"]

        # Vibration intensity
        if "vibration" in features:
            derived["vibration_intensity"] = features["vibration"] ** 2

        # Combined stress indicator
        if "temperature" in features and "pressure" in features:
            derived["stress_indicator"] = (
                features["temperature"] / 100 * features["pressure"] / 50
            )

        # Operating intensity
        if "speed" in features and "feed_rate" in features:
            derived["operating_intensity"] = features["speed"] * features["feed_rate"]

        return derived

    async def prepare_maintenance_features(
        self,
        telemetry: Dict[str, Any],
        history: Optional[pd.DataFrame] = None
    ) -> np.ndarray:
        """Prepare features for maintenance prediction"""
        features = []

        # Current telemetry features
        features.extend([
            telemetry.get("vibration", 0),
            telemetry.get("temperature", 0),
            telemetry.get("pressure", 0),
            telemetry.get("rpm", 0),
            telemetry.get("power", 0)
        ])

        # Historical statistics if available
        if history is not None and len(history) > 0:
            # Vibration statistics
            features.extend([
                history["vibration"].mean() if "vibration" in history else 0,
                history["vibration"].std() if "vibration" in history else 0,
                history["vibration"].max() if "vibration" in history else 0
            ])

            # Temperature statistics
            features.extend([
                history["temperature"].mean() if "temperature" in history else 0,
                history["temperature"].std() if "temperature" in history else 0
            ])

            # Trend features
            if len(history) > 1:
                # Calculate trends
                time_index = np.arange(len(history))
                vib_trend = np.polyfit(time_index, history["vibration"], 1)[0] if "vibration" in history else 0
                temp_trend = np.polyfit(time_index, history["temperature"], 1)[0] if "temperature" in history else 0
                features.extend([vib_trend, temp_trend])
            else:
                features.extend([0, 0])
        else:
            # No historical data - use defaults
            features.extend([0] * 7)

        # Operating hours and maintenance info
        features.extend([
            telemetry.get("operating_hours", 0),
            telemetry.get("cycles", 0),
            telemetry.get("hours_since_maintenance", 0)
        ])

        return np.array(features)

    async def prepare_quality_features(
        self,
        process_data: Dict[str, Any],
        material_data: Optional[Dict[str, Any]] = None
    ) -> np.ndarray:
        """Prepare features for quality prediction"""
        features = []

        # Process parameters
        features.extend([
            process_data.get("temperature", 0),
            process_data.get("pressure", 0),
            process_data.get("speed", 0),
            process_data.get("feed_rate", 0),
            process_data.get("tool_wear", 0)
        ])

        # Material properties
        if material_data:
            features.extend([
                material_data.get("hardness", 0),
                material_data.get("thickness", 0),
                material_data.get("temperature", 0)
            ])
        else:
            features.extend([0, 0, 0])

        # Environmental factors
        features.extend([
            process_data.get("humidity", 50),
            process_data.get("ambient_temperature", 25)
        ])

        # Machine condition
        features.extend([
            process_data.get("vibration", 0),
            process_data.get("spindle_load", 0),
            process_data.get("axis_position_error", 0)
        ])

        # Process stability
        features.extend([
            process_data.get("process_time", 0),
            process_data.get("cycle_variation", 0)
        ])

        return np.array(features)

    async def prepare_anomaly_features(
        self,
        telemetry: Dict[str, Any],
        context: Optional[Dict[str, Any]] = None
    ) -> np.ndarray:
        """Prepare features for anomaly detection"""
        features = []

        # Core telemetry
        core_fields = [
            "vibration", "temperature", "pressure", "rpm",
            "power", "current", "voltage", "frequency"
        ]

        for field in core_fields:
            features.append(telemetry.get(field, 0))

        # Contextual features if available
        if context:
            features.extend([
                context.get("time_of_day", 12),  # Hour of day
                context.get("day_of_week", 1),   # Day of week
                context.get("shift", 1),         # Work shift
                context.get("operator_id", 0)    # Operator identifier
            ])
        else:
            features.extend([12, 1, 1, 0])

        # Derived anomaly indicators
        derived = self.calculate_anomaly_indicators(telemetry)
        features.extend(derived)

        return np.array(features)

    def calculate_anomaly_indicators(self, telemetry: Dict[str, Any]) -> List[float]:
        """Calculate specific anomaly indicators"""
        indicators = []

        # Vibration anomaly indicator
        vib = telemetry.get("vibration", 0)
        indicators.append(1.0 if vib > 15 else 0.0)

        # Temperature anomaly indicator
        temp = telemetry.get("temperature", 0)
        indicators.append(1.0 if temp > 90 or temp < 20 else 0.0)

        # Power anomaly indicator
        power = telemetry.get("power", 0)
        indicators.append(1.0 if power > 500 or power < 10 else 0.0)

        # Combined anomaly score
        combined = sum(indicators) / len(indicators)
        indicators.append(combined)

        return indicators

    async def prepare_optimization_features(
        self,
        current_state: Dict[str, Any],
        constraints: Optional[Dict[str, Any]] = None
    ) -> np.ndarray:
        """Prepare features for process optimization"""
        features = []

        # Current performance metrics
        features.extend([
            current_state.get("throughput", 100),
            current_state.get("quality_score", 85),
            current_state.get("defect_rate", 0.05),
            current_state.get("energy_consumption", 100),
            current_state.get("cycle_time", 60)
        ])

        # Current process parameters
        features.extend([
            current_state.get("speed", 0),
            current_state.get("feed_rate", 0),
            current_state.get("temperature", 0),
            current_state.get("pressure", 0),
            current_state.get("tool_offset", 0)
        ])

        # Constraint indicators
        if constraints:
            features.extend([
                constraints.get("max_speed", 100),
                constraints.get("max_temperature", 100),
                constraints.get("min_quality", 80),
                constraints.get("max_energy", 200)
            ])
        else:
            features.extend([100, 100, 80, 200])

        return np.array(features)

    async def batch_inference(
        self,
        model: Any,
        batch_data: List[Dict[str, Any]],
        model_type: str
    ) -> List[Dict[str, Any]]:
        """Perform batch inference"""
        results = []

        # Prepare features for entire batch
        feature_pipeline = self.feature_pipelines.get(model_type)
        if not feature_pipeline:
            raise ValueError(f"Unknown model type: {model_type}")

        # Process in parallel
        tasks = []
        for data in batch_data:
            task = asyncio.create_task(
                self.single_inference(model, data, model_type)
            )
            tasks.append(task)

        # Wait for all predictions
        results = await asyncio.gather(*tasks)

        return results

    async def single_inference(
        self,
        model: Any,
        data: Dict[str, Any],
        model_type: str
    ) -> Dict[str, Any]:
        """Perform single inference with caching"""
        # Check cache
        cache_key = f"{model_type}_{hash(frozenset(data.items()))}"
        if cache_key in self.prediction_cache:
            self.stats["cache_hits"] += 1
            return self.prediction_cache[cache_key]

        # Record start time
        start_time = datetime.utcnow()

        # Prepare features
        feature_pipeline = self.feature_pipelines.get(model_type)
        if not feature_pipeline:
            raise ValueError(f"Unknown model type: {model_type}")

        features = await feature_pipeline(data)

        # Make prediction
        if hasattr(model, "predict"):
            result = await model.predict({"features": features})
        else:
            # Fallback for models without async predict
            result = {
                "prediction": model.predict(features.reshape(1, -1))[0],
                "confidence": 0.85,
                "recommendations": []
            }

        # Calculate inference time
        inference_time = (datetime.utcnow() - start_time).total_seconds()

        # Update statistics
        self.stats["total_predictions"] += 1
        self.stats["avg_inference_time"] = (
            (self.stats["avg_inference_time"] * (self.stats["total_predictions"] - 1) +
             inference_time) / self.stats["total_predictions"]
        )
        self.stats["last_inference"] = datetime.utcnow()

        # Cache result
        self.prediction_cache[cache_key] = result

        return result

    async def ensemble_inference(
        self,
        models: Dict[str, Any],
        data: Dict[str, Any]
    ) -> Dict[str, Any]:
        """Perform ensemble inference using multiple models"""
        predictions = {}
        confidences = []

        # Get predictions from each model
        tasks = []
        for model_type, model in models.items():
            task = asyncio.create_task(
                self.single_inference(model, data, model_type)
            )
            tasks.append((model_type, task))

        # Collect results
        for model_type, task in tasks:
            try:
                result = await task
                predictions[model_type] = result
                confidences.append(result.get("confidence", 0.5))
            except Exception as e:
                logger.error(f"Ensemble inference error for {model_type}: {e}")
                predictions[model_type] = None

        # Combine predictions
        combined_result = self.combine_predictions(predictions, confidences)

        return combined_result

    def combine_predictions(
        self,
        predictions: Dict[str, Any],
        confidences: List[float]
    ) -> Dict[str, Any]:
        """Combine multiple model predictions"""
        # Simple weighted average based on confidence
        total_confidence = sum(confidences)
        if total_confidence == 0:
            total_confidence = 1

        # Initialize combined result
        combined = {
            "ensemble_prediction": {},
            "individual_predictions": predictions,
            "combined_confidence": np.mean(confidences),
            "recommendations": []
        }

        # Combine numeric predictions
        numeric_predictions = {}
        for model_type, pred in predictions.items():
            if pred and "prediction" in pred:
                prediction_data = pred["prediction"]
                if isinstance(prediction_data, dict):
                    for key, value in prediction_data.items():
                        if isinstance(value, (int, float)):
                            if key not in numeric_predictions:
                                numeric_predictions[key] = []
                            numeric_predictions[key].append(value)

        # Average numeric predictions
        for key, values in numeric_predictions.items():
            combined["ensemble_prediction"][key] = np.mean(values)

        # Combine recommendations
        all_recommendations = []
        for pred in predictions.values():
            if pred and "recommendations" in pred:
                all_recommendations.extend(pred["recommendations"])

        # Deduplicate recommendations
        combined["recommendations"] = list(set(all_recommendations))

        return combined

    def validate_features(
        self,
        features: np.ndarray,
        expected_shape: Optional[tuple] = None
    ) -> bool:
        """Validate feature array"""
        # Check for NaN or Inf values
        if np.any(np.isnan(features)) or np.any(np.isinf(features)):
            logger.warning("Invalid values (NaN or Inf) in features")
            return False

        # Check shape if specified
        if expected_shape and features.shape != expected_shape:
            logger.warning(f"Feature shape mismatch: expected {expected_shape}, got {features.shape}")
            return False

        # Check reasonable value ranges
        if np.any(np.abs(features) > 1e6):
            logger.warning("Extremely large values in features")
            return False

        return True

    def get_feature_importance(
        self,
        model: Any,
        model_type: str
    ) -> Dict[str, float]:
        """Get feature importance from model"""
        importance = {}

        if hasattr(model, "feature_importance"):
            # Direct feature importance
            importance = model.feature_importance
        elif hasattr(model, "feature_importances_"):
            # Sklearn models
            feature_names = self.get_feature_names(model_type)
            importance = dict(zip(feature_names, model.feature_importances_))
        elif hasattr(model, "coef_"):
            # Linear models
            feature_names = self.get_feature_names(model_type)
            importance = dict(zip(feature_names, np.abs(model.coef_)))

        return importance

    def get_feature_names(self, model_type: str) -> List[str]:
        """Get feature names for a model type"""
        if model_type == "maintenance":
            return [
                "vibration", "temperature", "pressure", "rpm", "power",
                "vib_mean", "vib_std", "vib_max", "temp_mean", "temp_std",
                "vib_trend", "temp_trend", "operating_hours", "cycles",
                "hours_since_maintenance"
            ]
        elif model_type == "quality":
            return [
                "temperature", "pressure", "speed", "feed_rate", "tool_wear",
                "material_hardness", "material_thickness", "material_temp",
                "humidity", "ambient_temp", "vibration", "spindle_load",
                "axis_error", "process_time", "cycle_variation"
            ]
        elif model_type == "anomaly":
            return [
                "vibration", "temperature", "pressure", "rpm", "power",
                "current", "voltage", "frequency", "time_of_day",
                "day_of_week", "shift", "operator", "vib_anomaly",
                "temp_anomaly", "power_anomaly", "combined_anomaly"
            ]
        else:
            return []

    def get_stats(self) -> Dict[str, Any]:
        """Get inference statistics"""
        return self.stats

    def reset_cache(self):
        """Clear all caches"""
        self.prediction_cache.clear()
        self.feature_cache.clear()
        logger.info("Inference caches cleared")