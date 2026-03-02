"""
Predictive Maintenance Model
Uses machine learning to predict equipment failures and maintenance needs
"""

import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional, Tuple
import joblib
import logging

from sklearn.ensemble import RandomForestRegressor, GradientBoostingRegressor
from sklearn.preprocessing import StandardScaler
from sklearn.model_selection import train_test_split, cross_val_score
from sklearn.metrics import mean_absolute_error, mean_squared_error, r2_score
import tensorflow as tf
from tensorflow import keras
from tensorflow.keras import layers
from prophet import Prophet

logger = logging.getLogger(__name__)

class PredictiveMaintenanceModel:
    """
    Predictive maintenance model using ensemble of techniques:
    - Random Forest for failure probability
    - LSTM for time-series prediction
    - Prophet for seasonal patterns
    """

    def __init__(self):
        self.rf_model = None
        self.lstm_model = None
        self.prophet_model = None
        self.scaler = StandardScaler()
        self.feature_importance = {}
        self.metrics = {
            "accuracy": 0.0,
            "precision": 0.0,
            "recall": 0.0,
            "f1_score": 0.0,
            "last_trained": None,
            "training_samples": 0,
            "version": "1.0.0"
        }

    def build_lstm_model(self, input_shape: Tuple[int, int]) -> keras.Model:
        """Build LSTM model for time-series prediction"""
        model = keras.Sequential([
            layers.LSTM(128, return_sequences=True, input_shape=input_shape),
            layers.Dropout(0.2),
            layers.LSTM(64, return_sequences=True),
            layers.Dropout(0.2),
            layers.LSTM(32),
            layers.Dropout(0.2),
            layers.Dense(16, activation='relu'),
            layers.Dense(1, activation='sigmoid')
        ])

        model.compile(
            optimizer=keras.optimizers.Adam(learning_rate=0.001),
            loss='binary_crossentropy',
            metrics=['accuracy', keras.metrics.Precision(), keras.metrics.Recall()]
        )

        return model

    def extract_features(self, telemetry_data: pd.DataFrame) -> np.ndarray:
        """Extract features from telemetry data"""
        features = []

        # Statistical features
        features.extend([
            telemetry_data['vibration'].mean(),
            telemetry_data['vibration'].std(),
            telemetry_data['vibration'].max(),
            telemetry_data['vibration'].min(),
            telemetry_data['temperature'].mean(),
            telemetry_data['temperature'].std(),
            telemetry_data['temperature'].max(),
            telemetry_data['pressure'].mean() if 'pressure' in telemetry_data else 0,
            telemetry_data['rpm'].mean() if 'rpm' in telemetry_data else 0,
            telemetry_data['power'].mean() if 'power' in telemetry_data else 0,
        ])

        # Trend features
        if len(telemetry_data) > 1:
            features.extend([
                np.polyfit(range(len(telemetry_data)), telemetry_data['vibration'], 1)[0],
                np.polyfit(range(len(telemetry_data)), telemetry_data['temperature'], 1)[0],
            ])
        else:
            features.extend([0, 0])

        # Frequency domain features (FFT)
        if len(telemetry_data) > 10:
            fft_vibration = np.fft.fft(telemetry_data['vibration'].values)
            features.extend([
                np.abs(fft_vibration).max(),
                np.abs(fft_vibration).mean(),
            ])
        else:
            features.extend([0, 0])

        # Operating hours and cycles
        features.append(telemetry_data.get('operating_hours', [0]).iloc[-1])
        features.append(telemetry_data.get('cycles', [0]).iloc[-1])

        return np.array(features)

    def calculate_health_score(self, features: np.ndarray) -> float:
        """Calculate overall machine health score (0-100)"""
        if self.rf_model is None:
            return 75.0  # Default healthy score

        # Predict failure probability
        failure_prob = self.rf_model.predict_proba(features.reshape(1, -1))[0][1]

        # Convert to health score (inverse of failure probability)
        health_score = (1 - failure_prob) * 100

        # Apply bounds
        return np.clip(health_score, 0, 100)

    def estimate_remaining_useful_life(self, telemetry_data: pd.DataFrame) -> float:
        """Estimate remaining useful life in hours"""
        if self.lstm_model is None:
            return 720.0  # Default 30 days

        # Prepare sequence data
        sequence = self.prepare_sequence(telemetry_data)

        # Predict with LSTM
        rul_normalized = self.lstm_model.predict(sequence.reshape(1, *sequence.shape))[0][0]

        # Denormalize (assuming max RUL of 2000 hours)
        rul_hours = rul_normalized * 2000

        return max(0, rul_hours)

    def prepare_sequence(self, telemetry_data: pd.DataFrame, sequence_length: int = 24) -> np.ndarray:
        """Prepare sequence data for LSTM"""
        # Select relevant columns
        columns = ['vibration', 'temperature', 'rpm', 'power']
        available_columns = [col for col in columns if col in telemetry_data.columns]

        if not available_columns:
            # Return dummy sequence if no data
            return np.zeros((sequence_length, len(columns)))

        # Extract values
        values = telemetry_data[available_columns].values

        # Pad or truncate to sequence length
        if len(values) < sequence_length:
            # Pad with last value
            padding = np.repeat(values[-1:], sequence_length - len(values), axis=0)
            sequence = np.vstack([padding, values])
        else:
            sequence = values[-sequence_length:]

        # Add missing columns as zeros
        if len(available_columns) < len(columns):
            zeros = np.zeros((sequence_length, len(columns) - len(available_columns)))
            sequence = np.hstack([sequence, zeros])

        return sequence

    def detect_degradation_pattern(self, telemetry_data: pd.DataFrame) -> Dict[str, Any]:
        """Detect degradation patterns in telemetry"""
        patterns = {
            "linear_degradation": False,
            "exponential_degradation": False,
            "cyclic_pattern": False,
            "sudden_change": False,
            "degradation_rate": 0.0
        }

        if len(telemetry_data) < 10:
            return patterns

        # Check vibration trend
        vibration = telemetry_data['vibration'].values
        time_points = np.arange(len(vibration))

        # Linear degradation
        linear_fit = np.polyfit(time_points, vibration, 1)
        patterns["linear_degradation"] = linear_fit[0] > 0.01
        patterns["degradation_rate"] = linear_fit[0]

        # Exponential degradation
        try:
            exp_fit = np.polyfit(time_points, np.log(vibration + 1e-10), 1)
            patterns["exponential_degradation"] = exp_fit[0] > 0.02
        except:
            pass

        # Cyclic pattern (using FFT)
        fft = np.fft.fft(vibration)
        frequencies = np.fft.fftfreq(len(vibration))
        dominant_freq = frequencies[np.argmax(np.abs(fft[1:len(fft)//2]))]
        patterns["cyclic_pattern"] = abs(dominant_freq) > 0.1

        # Sudden change detection
        diff = np.diff(vibration)
        patterns["sudden_change"] = np.any(np.abs(diff) > 3 * np.std(diff))

        return patterns

    async def predict(self, features: Dict[str, Any], horizon_hours: int = 24) -> Dict[str, Any]:
        """Make maintenance prediction"""
        try:
            # Convert features to DataFrame
            df = pd.DataFrame([features])

            # Extract feature vector
            feature_vector = self.extract_features(df)

            # Calculate health score
            health_score = self.calculate_health_score(feature_vector)

            # Estimate RUL
            rul = self.estimate_remaining_useful_life(df)

            # Detect patterns
            patterns = self.detect_degradation_pattern(df)

            # Determine maintenance urgency
            if health_score < 30 or rul < 48:
                urgency = "critical"
            elif health_score < 50 or rul < 168:
                urgency = "high"
            elif health_score < 70 or rul < 336:
                urgency = "medium"
            else:
                urgency = "low"

            # Generate recommendations
            recommendations = self.generate_recommendations(
                health_score,
                rul,
                patterns,
                urgency
            )

            return {
                "prediction": {
                    "health_score": health_score,
                    "remaining_useful_life_hours": rul,
                    "failure_probability_24h": 1 - (health_score / 100) * 0.95,
                    "maintenance_urgency": urgency,
                    "degradation_patterns": patterns,
                    "estimated_downtime_hours": self.estimate_downtime(urgency),
                    "cost_if_failure": self.estimate_failure_cost(features)
                },
                "confidence": 0.85 if self.rf_model else 0.5,
                "recommendations": recommendations
            }

        except Exception as e:
            logger.error(f"Prediction error: {e}")
            raise

    async def predict_realtime(self, telemetry: Dict[str, Any]) -> Dict[str, Any]:
        """Real-time prediction for streaming telemetry"""
        # Simplified real-time prediction
        vibration = telemetry.get('vibration', 0)
        temperature = telemetry.get('temperature', 0)

        # Simple threshold-based risk scoring
        risk_score = 0.0

        if vibration > 10:
            risk_score += 0.3
        if vibration > 15:
            risk_score += 0.3

        if temperature > 80:
            risk_score += 0.2
        if temperature > 90:
            risk_score += 0.2

        # Estimate time to failure
        if risk_score > 0.7:
            ttf_hours = 24
        elif risk_score > 0.5:
            ttf_hours = 168
        else:
            ttf_hours = 720

        return {
            "risk_score": min(risk_score, 1.0),
            "ttf_hours": ttf_hours,
            "recommendations": self.get_quick_recommendations(risk_score)
        }

    def generate_recommendations(
        self,
        health_score: float,
        rul: float,
        patterns: Dict[str, Any],
        urgency: str
    ) -> List[str]:
        """Generate maintenance recommendations"""
        recommendations = []

        if urgency == "critical":
            recommendations.append("Schedule immediate maintenance to prevent failure")
            recommendations.append("Prepare replacement parts and maintenance team")
        elif urgency == "high":
            recommendations.append(f"Schedule maintenance within {int(rul/24)} days")
            recommendations.append("Order necessary spare parts")

        if patterns["linear_degradation"]:
            recommendations.append("Monitor degradation trend closely")

        if patterns["exponential_degradation"]:
            recommendations.append("Degradation accelerating - reduce operational load")

        if patterns["cyclic_pattern"]:
            recommendations.append("Investigate source of cyclic stress")

        if patterns["sudden_change"]:
            recommendations.append("Recent sudden change detected - perform inspection")

        if health_score < 50:
            recommendations.append("Consider preventive component replacement")

        return recommendations

    def get_quick_recommendations(self, risk_score: float) -> List[str]:
        """Get quick recommendations for real-time alerts"""
        if risk_score > 0.7:
            return [
                "High risk detected - reduce load immediately",
                "Schedule emergency maintenance",
                "Monitor continuously"
            ]
        elif risk_score > 0.5:
            return [
                "Elevated risk - plan maintenance soon",
                "Increase monitoring frequency"
            ]
        else:
            return ["Continue normal operation with standard monitoring"]

    def estimate_downtime(self, urgency: str) -> float:
        """Estimate maintenance downtime in hours"""
        downtime_map = {
            "critical": 8.0,
            "high": 6.0,
            "medium": 4.0,
            "low": 2.0
        }
        return downtime_map.get(urgency, 4.0)

    def estimate_failure_cost(self, features: Dict[str, Any]) -> float:
        """Estimate cost of failure"""
        # Simplified cost model
        base_cost = 10000

        # Adjust based on machine type
        machine_type = features.get('machine_type', 'default')
        type_multiplier = {
            'cnc': 2.0,
            '3d_printer': 1.0,
            'laser': 1.5,
            'robot': 2.5
        }.get(machine_type, 1.0)

        # Adjust based on criticality
        criticality = features.get('criticality', 1.0)

        return base_cost * type_multiplier * criticality

    def train(self, training_data: pd.DataFrame, labels: np.ndarray):
        """Train the predictive maintenance model"""
        logger.info("Training predictive maintenance model")

        # Split data
        X_train, X_test, y_train, y_test = train_test_split(
            training_data,
            labels,
            test_size=0.2,
            random_state=42
        )

        # Train Random Forest
        self.rf_model = RandomForestRegressor(
            n_estimators=100,
            max_depth=10,
            random_state=42,
            n_jobs=-1
        )
        self.rf_model.fit(X_train, y_train)

        # Evaluate
        predictions = self.rf_model.predict(X_test)
        self.metrics["accuracy"] = r2_score(y_test, predictions)
        self.metrics["last_trained"] = datetime.utcnow()
        self.metrics["training_samples"] = len(training_data)

        # Feature importance
        self.feature_importance = dict(zip(
            training_data.columns,
            self.rf_model.feature_importances_
        ))

        logger.info(f"Model trained with R² score: {self.metrics['accuracy']}")

    def save(self, path: str = "models/predictive_maintenance.pkl"):
        """Save model to disk"""
        model_data = {
            "rf_model": self.rf_model,
            "scaler": self.scaler,
            "feature_importance": self.feature_importance,
            "metrics": self.metrics
        }
        joblib.dump(model_data, path)
        logger.info(f"Model saved to {path}")

    def load(self, path: str = "models/predictive_maintenance.pkl"):
        """Load model from disk"""
        try:
            model_data = joblib.load(path)
            self.rf_model = model_data["rf_model"]
            self.scaler = model_data["scaler"]
            self.feature_importance = model_data["feature_importance"]
            self.metrics = model_data["metrics"]
            logger.info(f"Model loaded from {path}")
        except Exception as e:
            logger.warning(f"Could not load model from {path}: {e}")

    def get_metrics(self) -> Dict[str, Any]:
        """Get model metrics"""
        return self.metrics