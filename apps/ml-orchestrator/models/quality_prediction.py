"""
Quality Prediction Model
Predicts product quality outcomes and defect probability based on process parameters
"""

import os
import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional, Tuple
import joblib
import logging

from sklearn.ensemble import RandomForestClassifier, GradientBoostingRegressor
from sklearn.linear_model import LogisticRegression
from sklearn.preprocessing import StandardScaler, LabelEncoder
from sklearn.model_selection import train_test_split, cross_val_score
from sklearn.metrics import (
    accuracy_score, precision_score, recall_score, f1_score,
    confusion_matrix, classification_report, roc_auc_score
)
import xgboost as xgb
import tensorflow as tf
from tensorflow import keras
from tensorflow.keras import layers

logger = logging.getLogger(__name__)

class QualityPredictor:
    """
    Quality prediction using ensemble methods:
    - Random Forest for defect classification
    - XGBoost for quality score regression
    - Neural network for complex pattern recognition
    - Statistical process control (SPC) for real-time monitoring
    """

    def __init__(self):
        self.defect_classifier = None
        self.quality_regressor = None
        self.neural_model = None
        self.scaler = StandardScaler()
        self.label_encoder = LabelEncoder()

        # Quality thresholds
        self.thresholds = {
            "defect_probability": 0.3,
            "quality_score_min": 85.0,
            "process_capability": 1.33,  # Cpk threshold
            "confidence_min": 0.7
        }

        # Process control limits
        self.control_limits = {}
        self.process_capability = {}

        # Feature importance tracking
        self.feature_importance = {}

        # Metrics
        self.metrics = {
            "accuracy": 0.0,
            "precision": 0.0,
            "recall": 0.0,
            "f1_score": 0.0,
            "defect_rate": 0.0,
            "last_trained": None,
            "version": "1.0.0"
        }

    def build_neural_quality_model(self, input_dim: int) -> keras.Model:
        """Build neural network for quality prediction"""
        model = keras.Sequential([
            layers.Dense(128, activation='relu', input_shape=(input_dim,)),
            layers.BatchNormalization(),
            layers.Dropout(0.3),
            layers.Dense(64, activation='relu'),
            layers.BatchNormalization(),
            layers.Dropout(0.2),
            layers.Dense(32, activation='relu'),
            layers.Dense(16, activation='relu'),
            layers.Dense(1, activation='sigmoid')  # Quality probability
        ])

        model.compile(
            optimizer=keras.optimizers.Adam(learning_rate=0.001),
            loss='binary_crossentropy',
            metrics=['accuracy', keras.metrics.Precision(), keras.metrics.Recall()]
        )

        return model

    def extract_quality_features(self, process_data: Dict[str, Any]) -> np.ndarray:
        """Extract features relevant to quality prediction"""
        features = []

        # Process parameters
        features.append(process_data.get('temperature', 0))
        features.append(process_data.get('pressure', 0))
        features.append(process_data.get('speed', 0))
        features.append(process_data.get('feed_rate', 0))
        features.append(process_data.get('tool_wear', 0))

        # Material properties
        features.append(process_data.get('material_hardness', 0))
        features.append(process_data.get('material_thickness', 0))
        features.append(process_data.get('material_temperature', 0))

        # Environmental factors
        features.append(process_data.get('humidity', 0))
        features.append(process_data.get('ambient_temperature', 0))

        # Machine condition
        features.append(process_data.get('vibration', 0))
        features.append(process_data.get('spindle_load', 0))
        features.append(process_data.get('axis_position_error', 0))

        # Process stability indicators
        features.append(process_data.get('process_time', 0))
        features.append(process_data.get('cycle_variation', 0))

        return np.array(features)

    def calculate_process_capability(self, measurements: np.ndarray,
                                    spec_limits: Tuple[float, float]) -> Dict[str, float]:
        """Calculate process capability indices (Cp, Cpk)"""
        lower_spec, upper_spec = spec_limits

        mean = np.mean(measurements)
        std = np.std(measurements)

        if std == 0:
            return {"cp": 0, "cpk": 0, "process_capable": False}

        # Calculate Cp (potential capability)
        cp = (upper_spec - lower_spec) / (6 * std)

        # Calculate Cpk (actual capability)
        cpu = (upper_spec - mean) / (3 * std)
        cpl = (mean - lower_spec) / (3 * std)
        cpk = min(cpu, cpl)

        return {
            "cp": cp,
            "cpk": cpk,
            "cpu": cpu,
            "cpl": cpl,
            "process_capable": cpk >= self.thresholds["process_capability"]
        }

    def detect_quality_trends(self, quality_history: pd.DataFrame) -> Dict[str, Any]:
        """Detect quality trends and patterns"""
        trends = {
            "improving": False,
            "degrading": False,
            "stable": False,
            "cyclic_pattern": False,
            "shift_detected": False
        }

        if len(quality_history) < 10:
            trends["stable"] = True
            return trends

        quality_scores = quality_history['quality_score'].values if 'quality_score' in quality_history else None

        if quality_scores is not None:
            # Trend analysis
            x = np.arange(len(quality_scores))
            slope, _ = np.polyfit(x, quality_scores, 1)

            if slope > 0.1:
                trends["improving"] = True
            elif slope < -0.1:
                trends["degrading"] = True
            else:
                trends["stable"] = True

            # Western Electric rules for shift detection
            mean = np.mean(quality_scores)
            std = np.std(quality_scores)

            # Rule 1: One point beyond 3 sigma
            if np.any(np.abs(quality_scores - mean) > 3 * std):
                trends["shift_detected"] = True

            # Rule 2: 9 points in a row on same side of mean
            above_mean = quality_scores > mean
            consecutive = 0
            for above in above_mean:
                if above == above_mean[0]:
                    consecutive += 1
                    if consecutive >= 9:
                        trends["shift_detected"] = True
                        break
                else:
                    break

            # Cyclic pattern detection (simplified)
            if len(quality_scores) > 20:
                fft = np.fft.fft(quality_scores - mean)
                frequencies = np.fft.fftfreq(len(quality_scores))
                dominant_freq = frequencies[np.argmax(np.abs(fft[1:len(fft)//2])) + 1]
                if abs(dominant_freq) > 0.1:
                    trends["cyclic_pattern"] = True

        return trends

    def predict_defect_probability(self, features: np.ndarray) -> float:
        """Predict probability of defect"""
        if self.defect_classifier is None:
            return 0.1  # Default low probability

        prob = self.defect_classifier.predict_proba(features.reshape(1, -1))[0][1]
        return float(prob)

    def predict_quality_score(self, features: np.ndarray) -> float:
        """Predict quality score (0-100)"""
        if self.quality_regressor is None:
            return 85.0  # Default acceptable quality

        score = self.quality_regressor.predict(features.reshape(1, -1))[0]
        return np.clip(score, 0, 100)

    async def predict(self, features: Dict[str, Any]) -> Dict[str, Any]:
        """Comprehensive quality prediction"""
        try:
            # Extract features
            feature_vector = self.extract_quality_features(features)

            # Scale features
            if len(feature_vector) > 0:
                feature_scaled = self.scaler.transform(feature_vector.reshape(1, -1))
            else:
                feature_scaled = feature_vector.reshape(1, -1)

            # Predict defect probability
            defect_prob = self.predict_defect_probability(feature_scaled)

            # Predict quality score
            quality_score = self.predict_quality_score(feature_scaled)

            # Neural network prediction if available
            if self.neural_model:
                nn_prob = self.neural_model.predict(feature_scaled, verbose=0)[0][0]
                # Ensemble with other predictions
                defect_prob = 0.6 * defect_prob + 0.4 * nn_prob

            # Determine quality class
            if quality_score >= 95:
                quality_class = "excellent"
            elif quality_score >= 85:
                quality_class = "good"
            elif quality_score >= 70:
                quality_class = "acceptable"
            elif quality_score >= 50:
                quality_class = "marginal"
            else:
                quality_class = "reject"

            # Check if intervention needed
            needs_intervention = (
                defect_prob > self.thresholds["defect_probability"] or
                quality_score < self.thresholds["quality_score_min"]
            )

            # Generate recommendations
            recommendations = self.generate_quality_recommendations(
                defect_prob,
                quality_score,
                features
            )

            # Identify critical factors
            critical_factors = self.identify_critical_factors(features, quality_score)

            return {
                "prediction": {
                    "defect_probability": defect_prob,
                    "quality_score": quality_score,
                    "quality_class": quality_class,
                    "needs_intervention": needs_intervention,
                    "critical_factors": critical_factors,
                    "estimated_yield": (1 - defect_prob) * 100,
                    "process_capability": self.process_capability.get("current", {})
                },
                "confidence": self.calculate_prediction_confidence(defect_prob, quality_score),
                "recommendations": recommendations
            }

        except Exception as e:
            logger.error(f"Quality prediction error: {e}")
            raise

    def generate_quality_recommendations(
        self,
        defect_prob: float,
        quality_score: float,
        features: Dict[str, Any]
    ) -> List[str]:
        """Generate quality improvement recommendations"""
        recommendations = []

        if defect_prob > 0.5:
            recommendations.append("High defect risk - Stop production and inspect process")
            recommendations.append("Review and calibrate critical process parameters")
        elif defect_prob > 0.3:
            recommendations.append("Elevated defect risk - Increase inspection frequency")
            recommendations.append("Monitor process parameters closely")

        if quality_score < 70:
            recommendations.append("Quality below acceptable level - Adjust process immediately")

            # Parameter-specific recommendations
            if features.get('temperature', 0) > 100:
                recommendations.append("Reduce process temperature to optimal range")
            if features.get('tool_wear', 0) > 0.7:
                recommendations.append("Replace or recondition tooling")
            if features.get('vibration', 0) > 10:
                recommendations.append("Check machine alignment and balance")

        elif quality_score < 85:
            recommendations.append("Quality marginal - Process optimization recommended")

        # Process capability recommendations
        if hasattr(self, 'process_capability') and self.process_capability:
            cpk = self.process_capability.get("current", {}).get("cpk", 0)
            if cpk < 1.0:
                recommendations.append("Process not capable - Major improvements required")
            elif cpk < 1.33:
                recommendations.append("Process marginally capable - Continue improvement efforts")

        if not recommendations:
            recommendations.append("Quality within acceptable range - Maintain current parameters")

        return recommendations

    def identify_critical_factors(self, features: Dict[str, Any], quality_score: float) -> List[str]:
        """Identify factors most critical to quality"""
        critical = []

        # Use feature importance if available
        if self.feature_importance:
            # Get top 3 most important features
            sorted_features = sorted(
                self.feature_importance.items(),
                key=lambda x: x[1],
                reverse=True
            )[:3]
            critical = [f[0] for f in sorted_features]

        # Add rule-based critical factors
        if features.get('tool_wear', 0) > 0.8:
            critical.append("tool_wear_critical")
        if features.get('temperature', 0) < 60 or features.get('temperature', 0) > 100:
            critical.append("temperature_out_of_range")
        if features.get('vibration', 0) > 15:
            critical.append("excessive_vibration")

        return critical

    def calculate_prediction_confidence(self, defect_prob: float, quality_score: float) -> float:
        """Calculate confidence in prediction"""
        # Base confidence on model performance
        base_confidence = 0.75

        # Adjust based on prediction extremity
        if defect_prob < 0.1 or defect_prob > 0.9:
            base_confidence += 0.1  # More confident in extreme predictions

        if quality_score < 50 or quality_score > 90:
            base_confidence += 0.05

        # Cap at reasonable maximum
        return min(base_confidence, 0.95)

    def update_control_limits(self, parameter: str, measurements: np.ndarray):
        """Update statistical process control limits"""
        mean = np.mean(measurements)
        std = np.std(measurements)

        self.control_limits[parameter] = {
            "ucl": mean + 3 * std,  # Upper control limit
            "lcl": mean - 3 * std,  # Lower control limit
            "uwl": mean + 2 * std,  # Upper warning limit
            "lwl": mean - 2 * std,  # Lower warning limit
            "cl": mean,              # Center line
            "std": std
        }

    def check_control_limits(self, parameter: str, value: float) -> Dict[str, Any]:
        """Check if parameter is within control limits"""
        if parameter not in self.control_limits:
            return {"in_control": True, "zone": "unknown"}

        limits = self.control_limits[parameter]

        if value > limits["ucl"] or value < limits["lcl"]:
            return {"in_control": False, "zone": "out_of_control"}
        elif value > limits["uwl"] or value < limits["lwl"]:
            return {"in_control": True, "zone": "warning"}
        else:
            return {"in_control": True, "zone": "normal"}

    def train(self, training_data: pd.DataFrame, quality_labels: np.ndarray):
        """Train quality prediction models"""
        logger.info("Training quality prediction models")

        # Prepare features
        feature_columns = [col for col in training_data.columns
                          if col not in ['quality_score', 'defect', 'timestamp']]
        X = training_data[feature_columns].values
        X_scaled = self.scaler.fit_transform(X)

        # Split data
        X_train, X_test, y_train, y_test = train_test_split(
            X_scaled, quality_labels,
            test_size=0.2,
            random_state=42,
            stratify=quality_labels
        )

        # Train Random Forest classifier for defects
        self.defect_classifier = RandomForestClassifier(
            n_estimators=100,
            max_depth=10,
            min_samples_split=5,
            random_state=42,
            n_jobs=-1
        )
        self.defect_classifier.fit(X_train, y_train)

        # Get feature importance
        self.feature_importance = dict(zip(
            feature_columns,
            self.defect_classifier.feature_importances_
        ))

        # Train XGBoost regressor for quality score
        if 'quality_score' in training_data:
            quality_scores = training_data['quality_score'].values
            self.quality_regressor = xgb.XGBRegressor(
                n_estimators=100,
                max_depth=6,
                learning_rate=0.1,
                random_state=42
            )
            self.quality_regressor.fit(X_train, quality_scores[:len(X_train)])

        # Train neural network
        self.neural_model = self.build_neural_quality_model(X_scaled.shape[1])
        self.neural_model.fit(
            X_train, y_train,
            epochs=50,
            batch_size=32,
            validation_split=0.1,
            verbose=0
        )

        # Evaluate model
        predictions = self.defect_classifier.predict(X_test)
        self.metrics["accuracy"] = accuracy_score(y_test, predictions)
        self.metrics["precision"] = precision_score(y_test, predictions, average='weighted')
        self.metrics["recall"] = recall_score(y_test, predictions, average='weighted')
        self.metrics["f1_score"] = f1_score(y_test, predictions, average='weighted')
        self.metrics["last_trained"] = datetime.utcnow()

        logger.info(f"Quality model trained with accuracy: {self.metrics['accuracy']:.3f}")

    def save(self, path: str = "models/quality_predictor.pkl"):
        """Save models to disk"""
        model_data = {
            "defect_classifier": self.defect_classifier,
            "quality_regressor": self.quality_regressor,
            "scaler": self.scaler,
            "feature_importance": self.feature_importance,
            "control_limits": self.control_limits,
            "thresholds": self.thresholds,
            "metrics": self.metrics
        }
        joblib.dump(model_data, path)

        # Save neural network
        if self.neural_model:
            self.neural_model.save(path.replace('.pkl', '_neural.h5'))

        logger.info(f"Quality predictor saved to {path}")

    def load(self, path: str = "models/quality_predictor.pkl"):
        """Load models from disk"""
        try:
            model_data = joblib.load(path)
            self.defect_classifier = model_data["defect_classifier"]
            self.quality_regressor = model_data["quality_regressor"]
            self.scaler = model_data["scaler"]
            self.feature_importance = model_data["feature_importance"]
            self.control_limits = model_data["control_limits"]
            self.thresholds = model_data["thresholds"]
            self.metrics = model_data["metrics"]

            # Load neural network
            neural_path = path.replace('.pkl', '_neural.h5')
            if os.path.exists(neural_path):
                self.neural_model = keras.models.load_model(neural_path)

            logger.info(f"Quality predictor loaded from {path}")
        except Exception as e:
            logger.warning(f"Could not load quality predictor: {e}")

    def get_metrics(self) -> Dict[str, Any]:
        """Get model metrics"""
        return self.metrics