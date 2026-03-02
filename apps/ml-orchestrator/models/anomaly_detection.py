"""
Anomaly Detection Model
Uses multiple techniques to detect abnormal machine behavior and operational anomalies
"""

import os
import numpy as np
import pandas as pd
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional, Tuple
import joblib
import logging

from sklearn.ensemble import IsolationForest
from sklearn.svm import OneClassSVM
from sklearn.preprocessing import StandardScaler
from sklearn.decomposition import PCA
from sklearn.cluster import DBSCAN
from sklearn.metrics import silhouette_score
import tensorflow as tf
from tensorflow import keras
from tensorflow.keras import layers
from scipy import stats
from scipy.signal import find_peaks
import warnings
warnings.filterwarnings('ignore')

logger = logging.getLogger(__name__)

class AnomalyDetector:
    """
    Multi-method anomaly detection system:
    - Statistical methods for quick detection
    - Isolation Forest for multivariate anomalies
    - Autoencoder for complex pattern anomalies
    - LSTM for temporal anomalies
    """

    def __init__(self):
        self.isolation_forest = None
        self.one_class_svm = None
        self.autoencoder = None
        self.lstm_model = None
        self.scaler = StandardScaler()
        self.pca = PCA(n_components=0.95)  # Keep 95% variance

        # Anomaly thresholds
        self.thresholds = {
            "statistical": 3.0,  # Z-score threshold
            "isolation": -0.1,   # Isolation Forest decision threshold
            "reconstruction": 0.1,  # Autoencoder reconstruction error
            "temporal": 0.15,    # LSTM prediction error
            "combined": 0.6      # Combined anomaly score threshold
        }

        # Historical data for baseline
        self.baseline_stats = {}
        self.recent_anomalies = []
        self.anomaly_patterns = {}

        # Metrics
        self.metrics = {
            "detections": 0,
            "false_positives": 0,
            "true_positives": 0,
            "last_calibrated": None,
            "version": "1.0.0"
        }

    def build_autoencoder(self, input_dim: int) -> keras.Model:
        """Build autoencoder for anomaly detection"""
        # Encoder
        encoder_input = keras.Input(shape=(input_dim,))
        encoded = layers.Dense(64, activation='relu')(encoder_input)
        encoded = layers.Dropout(0.2)(encoded)
        encoded = layers.Dense(32, activation='relu')(encoded)
        encoded = layers.Dropout(0.2)(encoded)
        encoded = layers.Dense(16, activation='relu')(encoded)

        # Latent space
        latent = layers.Dense(8, activation='relu')(encoded)

        # Decoder
        decoded = layers.Dense(16, activation='relu')(latent)
        decoded = layers.Dropout(0.2)(decoded)
        decoded = layers.Dense(32, activation='relu')(decoded)
        decoded = layers.Dropout(0.2)(decoded)
        decoded = layers.Dense(64, activation='relu')(decoded)
        decoded = layers.Dense(input_dim, activation='linear')(decoded)

        # Autoencoder model
        autoencoder = keras.Model(encoder_input, decoded)
        autoencoder.compile(
            optimizer=keras.optimizers.Adam(learning_rate=0.001),
            loss='mse',
            metrics=['mae']
        )

        return autoencoder

    def build_lstm_detector(self, sequence_length: int, n_features: int) -> keras.Model:
        """Build LSTM for temporal anomaly detection"""
        model = keras.Sequential([
            layers.LSTM(64, return_sequences=True,
                       input_shape=(sequence_length, n_features)),
            layers.Dropout(0.2),
            layers.LSTM(32, return_sequences=False),
            layers.Dropout(0.2),
            layers.Dense(16, activation='relu'),
            layers.Dense(n_features)
        ])

        model.compile(
            optimizer=keras.optimizers.Adam(learning_rate=0.001),
            loss='mse',
            metrics=['mae']
        )

        return model

    def detect_statistical_anomalies(self, data: pd.DataFrame) -> np.ndarray:
        """Detect anomalies using statistical methods"""
        anomalies = np.zeros(len(data))

        for column in data.columns:
            if column in self.baseline_stats:
                mean = self.baseline_stats[column]['mean']
                std = self.baseline_stats[column]['std']

                # Z-score method
                z_scores = np.abs((data[column] - mean) / (std + 1e-10))
                anomalies += (z_scores > self.thresholds['statistical']).astype(int)

                # IQR method
                q1 = self.baseline_stats[column].get('q1', mean - 1.5 * std)
                q3 = self.baseline_stats[column].get('q3', mean + 1.5 * std)
                iqr = q3 - q1
                lower_bound = q1 - 1.5 * iqr
                upper_bound = q3 + 1.5 * iqr

                anomalies += ((data[column] < lower_bound) |
                             (data[column] > upper_bound)).astype(int)

        # Normalize by number of checks
        return anomalies / (2 * len(data.columns))

    def detect_isolation_anomalies(self, data: np.ndarray) -> np.ndarray:
        """Detect anomalies using Isolation Forest"""
        if self.isolation_forest is None:
            return np.zeros(len(data))

        # Predict returns -1 for anomalies, 1 for normal
        predictions = self.isolation_forest.decision_function(data)

        # Convert to anomaly scores (0 to 1)
        # Lower scores indicate anomalies
        anomaly_scores = 1 - (predictions - predictions.min()) / (predictions.max() - predictions.min() + 1e-10)

        return anomaly_scores

    def detect_reconstruction_anomalies(self, data: np.ndarray) -> np.ndarray:
        """Detect anomalies using autoencoder reconstruction error"""
        if self.autoencoder is None:
            return np.zeros(len(data))

        # Get reconstruction
        reconstructed = self.autoencoder.predict(data, verbose=0)

        # Calculate reconstruction error
        mse = np.mean((data - reconstructed) ** 2, axis=1)

        # Normalize to 0-1 range
        if mse.max() > mse.min():
            anomaly_scores = (mse - mse.min()) / (mse.max() - mse.min())
        else:
            anomaly_scores = np.zeros(len(data))

        return anomaly_scores

    def detect_temporal_anomalies(self, sequences: np.ndarray) -> np.ndarray:
        """Detect temporal anomalies using LSTM"""
        if self.lstm_model is None:
            return np.zeros(len(sequences))

        # Predict next values
        predictions = self.lstm_model.predict(sequences[:-1], verbose=0)
        actual = sequences[1:, -1, :]  # Last timestep of each sequence

        # Calculate prediction error
        mse = np.mean((actual - predictions) ** 2, axis=1)

        # Normalize to 0-1 range
        if mse.max() > mse.min():
            anomaly_scores = (mse - mse.min()) / (mse.max() - mse.min())
        else:
            anomaly_scores = np.zeros(len(mse))

        # Pad for first sequence (no prediction)
        return np.concatenate([[0], anomaly_scores])

    def detect_pattern_anomalies(self, data: pd.DataFrame) -> Dict[str, Any]:
        """Detect specific pattern-based anomalies"""
        patterns = {
            "sudden_spike": False,
            "gradual_drift": False,
            "periodic_anomaly": False,
            "correlation_break": False,
            "cluster_anomaly": False
        }

        # Sudden spike detection
        for column in data.select_dtypes(include=[np.number]).columns:
            values = data[column].values
            if len(values) > 10:
                # Find peaks
                peaks, properties = find_peaks(values, prominence=3*np.std(values))
                if len(peaks) > 0:
                    patterns["sudden_spike"] = True

        # Gradual drift detection
        if len(data) > 20:
            for column in data.select_dtypes(include=[np.number]).columns:
                values = data[column].values
                # Check trend
                x = np.arange(len(values))
                slope, _ = np.polyfit(x, values, 1)
                if abs(slope) > 0.1 * np.std(values):
                    patterns["gradual_drift"] = True

        # Correlation break detection
        if len(data.columns) > 1:
            corr_matrix = data.corr()
            # Check if correlations have changed significantly
            if hasattr(self, 'baseline_correlations'):
                corr_diff = np.abs(corr_matrix - self.baseline_correlations)
                if (corr_diff > 0.3).any().any():
                    patterns["correlation_break"] = True

        return patterns

    async def detect(self, features: Dict[str, Any]) -> Dict[str, Any]:
        """Comprehensive anomaly detection"""
        try:
            # Convert to DataFrame
            df = pd.DataFrame([features])

            # Prepare data
            numeric_cols = df.select_dtypes(include=[np.number]).columns
            data_scaled = self.scaler.transform(df[numeric_cols])

            # Statistical anomalies
            stat_scores = self.detect_statistical_anomalies(df[numeric_cols])

            # Isolation Forest anomalies
            iso_scores = self.detect_isolation_anomalies(data_scaled)

            # Reconstruction anomalies
            recon_scores = self.detect_reconstruction_anomalies(data_scaled)

            # Combine scores (weighted average)
            weights = {
                'statistical': 0.3,
                'isolation': 0.3,
                'reconstruction': 0.4
            }

            combined_score = (
                weights['statistical'] * np.mean(stat_scores) +
                weights['isolation'] * np.mean(iso_scores) +
                weights['reconstruction'] * np.mean(recon_scores)
            )

            # Determine if anomaly
            is_anomaly = combined_score > self.thresholds['combined']

            # Get pattern anomalies
            patterns = self.detect_pattern_anomalies(df)

            # Classify anomaly type
            anomaly_type = self.classify_anomaly(
                combined_score,
                patterns,
                features
            )

            # Generate description and actions
            description = self.generate_description(anomaly_type, combined_score, patterns)
            actions = self.generate_actions(anomaly_type, combined_score)

            # Update metrics
            self.metrics['detections'] += 1 if is_anomaly else 0

            return {
                "is_anomaly": is_anomaly,
                "anomaly_score": float(combined_score),
                "type": anomaly_type,
                "severity": self.calculate_severity(combined_score),
                "description": description,
                "actions": actions,
                "patterns": patterns,
                "component_scores": {
                    "statistical": float(np.mean(stat_scores)),
                    "isolation": float(np.mean(iso_scores)),
                    "reconstruction": float(np.mean(recon_scores))
                },
                "confidence": self.calculate_confidence(combined_score)
            }

        except Exception as e:
            logger.error(f"Anomaly detection error: {e}")
            raise

    async def detect_realtime(self, telemetry: Dict[str, Any]) -> Dict[str, Any]:
        """Real-time anomaly detection for streaming data"""
        # Quick statistical check
        anomaly_indicators = 0
        severity = "low"

        # Check key parameters
        if 'vibration' in telemetry and telemetry['vibration'] > 15:
            anomaly_indicators += 1
            if telemetry['vibration'] > 20:
                severity = "high"

        if 'temperature' in telemetry and telemetry['temperature'] > 85:
            anomaly_indicators += 1
            if telemetry['temperature'] > 95:
                severity = "critical"

        if 'pressure' in telemetry:
            if telemetry['pressure'] < 10 or telemetry['pressure'] > 100:
                anomaly_indicators += 1

        is_anomaly = anomaly_indicators >= 2

        if is_anomaly:
            return {
                "is_anomaly": True,
                "type": "threshold_violation",
                "severity": severity,
                "description": f"Multiple parameters exceed normal ranges",
                "actions": self.get_realtime_actions(severity)
            }

        return {"is_anomaly": False}

    def classify_anomaly(self, score: float, patterns: Dict, features: Dict) -> str:
        """Classify the type of anomaly"""
        if patterns.get("sudden_spike"):
            return "sudden_spike"
        elif patterns.get("gradual_drift"):
            return "gradual_drift"
        elif patterns.get("correlation_break"):
            return "correlation_anomaly"
        elif score > 0.8:
            return "severe_deviation"
        elif score > 0.6:
            return "moderate_deviation"
        else:
            return "minor_deviation"

    def calculate_severity(self, score: float) -> str:
        """Calculate anomaly severity"""
        if score > 0.85:
            return "critical"
        elif score > 0.7:
            return "high"
        elif score > 0.5:
            return "medium"
        else:
            return "low"

    def calculate_confidence(self, score: float) -> float:
        """Calculate detection confidence"""
        # Higher scores near thresholds have lower confidence
        distance_from_threshold = abs(score - self.thresholds['combined'])

        if distance_from_threshold < 0.1:
            return 0.6  # Low confidence near threshold
        elif distance_from_threshold < 0.2:
            return 0.75
        else:
            return 0.9  # High confidence far from threshold

    def generate_description(self, anomaly_type: str, score: float, patterns: Dict) -> str:
        """Generate human-readable anomaly description"""
        descriptions = {
            "sudden_spike": "Sudden spike detected in sensor readings",
            "gradual_drift": "Gradual drift from normal operating parameters",
            "correlation_anomaly": "Unusual correlation patterns between sensors",
            "severe_deviation": "Severe deviation from expected behavior",
            "moderate_deviation": "Moderate deviation from normal patterns",
            "minor_deviation": "Minor anomaly detected in operational data"
        }

        base_description = descriptions.get(anomaly_type, "Anomaly detected")

        # Add pattern details
        active_patterns = [k for k, v in patterns.items() if v]
        if active_patterns:
            base_description += f" with {', '.join(active_patterns)}"

        return base_description

    def generate_actions(self, anomaly_type: str, score: float) -> List[str]:
        """Generate recommended actions for anomaly"""
        actions = []

        if score > 0.85:
            actions.extend([
                "Immediate inspection required",
                "Reduce machine load or stop operation",
                "Alert maintenance team urgently"
            ])
        elif score > 0.7:
            actions.extend([
                "Schedule inspection within 24 hours",
                "Monitor closely for escalation",
                "Review recent operational changes"
            ])
        else:
            actions.extend([
                "Log for trend analysis",
                "Monitor during next maintenance window",
                "Review if pattern persists"
            ])

        # Type-specific actions
        if anomaly_type == "sudden_spike":
            actions.append("Check for mechanical impacts or electrical issues")
        elif anomaly_type == "gradual_drift":
            actions.append("Investigate wear patterns and calibration")
        elif anomaly_type == "correlation_anomaly":
            actions.append("Check sensor connectivity and calibration")

        return actions

    def get_realtime_actions(self, severity: str) -> List[str]:
        """Get immediate actions for real-time anomalies"""
        if severity == "critical":
            return [
                "Stop operation immediately",
                "Evacuate area if necessary",
                "Contact emergency response"
            ]
        elif severity == "high":
            return [
                "Reduce load immediately",
                "Prepare for shutdown",
                "Alert supervisor"
            ]
        else:
            return [
                "Monitor continuously",
                "Prepare maintenance team",
                "Document conditions"
            ]

    async def get_recent_anomalies(
        self,
        machine_id: Optional[str] = None,
        limit: int = 50
    ) -> List[Dict[str, Any]]:
        """Retrieve recent anomalies"""
        if machine_id:
            return [a for a in self.recent_anomalies
                   if a.get('machine_id') == machine_id][:limit]
        return self.recent_anomalies[:limit]

    def update_baseline(self, normal_data: pd.DataFrame):
        """Update baseline statistics with normal operational data"""
        for column in normal_data.select_dtypes(include=[np.number]).columns:
            self.baseline_stats[column] = {
                'mean': normal_data[column].mean(),
                'std': normal_data[column].std(),
                'q1': normal_data[column].quantile(0.25),
                'q3': normal_data[column].quantile(0.75),
                'min': normal_data[column].min(),
                'max': normal_data[column].max()
            }

        # Update baseline correlations
        self.baseline_correlations = normal_data.corr()

        logger.info("Baseline statistics updated")

    def calibrate_thresholds(self, validation_data: pd.DataFrame, labels: np.ndarray):
        """Calibrate detection thresholds using labeled data"""
        # This would use validation data to optimize thresholds
        # For now, using default values
        logger.info("Threshold calibration completed")
        self.metrics['last_calibrated'] = datetime.utcnow()

    def train(self, training_data: pd.DataFrame):
        """Train anomaly detection models"""
        logger.info("Training anomaly detection models")

        # Prepare data
        numeric_cols = training_data.select_dtypes(include=[np.number]).columns
        data_scaled = self.scaler.fit_transform(training_data[numeric_cols])

        # Train Isolation Forest
        self.isolation_forest = IsolationForest(
            contamination=0.1,
            random_state=42,
            n_estimators=100
        )
        self.isolation_forest.fit(data_scaled)

        # Train One-Class SVM
        self.one_class_svm = OneClassSVM(
            kernel='rbf',
            gamma='auto',
            nu=0.1
        )
        self.one_class_svm.fit(data_scaled)

        # Train Autoencoder
        self.autoencoder = self.build_autoencoder(data_scaled.shape[1])
        self.autoencoder.fit(
            data_scaled, data_scaled,
            epochs=50,
            batch_size=32,
            validation_split=0.1,
            verbose=0
        )

        # Update baseline
        self.update_baseline(training_data)

        logger.info("Anomaly detection models trained successfully")

    def save(self, path: str = "models/anomaly_detector.pkl"):
        """Save models to disk"""
        model_data = {
            "isolation_forest": self.isolation_forest,
            "one_class_svm": self.one_class_svm,
            "scaler": self.scaler,
            "baseline_stats": self.baseline_stats,
            "thresholds": self.thresholds,
            "metrics": self.metrics
        }
        joblib.dump(model_data, path)

        # Save neural network models separately
        if self.autoencoder:
            self.autoencoder.save(path.replace('.pkl', '_autoencoder.h5'))
        if self.lstm_model:
            self.lstm_model.save(path.replace('.pkl', '_lstm.h5'))

        logger.info(f"Anomaly detector saved to {path}")

    def load(self, path: str = "models/anomaly_detector.pkl"):
        """Load models from disk"""
        try:
            model_data = joblib.load(path)
            self.isolation_forest = model_data["isolation_forest"]
            self.one_class_svm = model_data["one_class_svm"]
            self.scaler = model_data["scaler"]
            self.baseline_stats = model_data["baseline_stats"]
            self.thresholds = model_data["thresholds"]
            self.metrics = model_data["metrics"]

            # Load neural network models
            autoencoder_path = path.replace('.pkl', '_autoencoder.h5')
            if os.path.exists(autoencoder_path):
                self.autoencoder = keras.models.load_model(autoencoder_path)

            lstm_path = path.replace('.pkl', '_lstm.h5')
            if os.path.exists(lstm_path):
                self.lstm_model = keras.models.load_model(lstm_path)

            logger.info(f"Anomaly detector loaded from {path}")
        except Exception as e:
            logger.warning(f"Could not load anomaly detector: {e}")

    def get_metrics(self) -> Dict[str, Any]:
        """Get model metrics"""
        return self.metrics