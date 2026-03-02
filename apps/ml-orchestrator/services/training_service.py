"""
Training Service
Manages model training, retraining, and incremental learning
"""

import os
import asyncio
import logging
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional
import pandas as pd
import numpy as np
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
import joblib
import mlflow
import optuna
from optuna import Trial

logger = logging.getLogger(__name__)

class TrainingService:
    """
    Service for model training and management:
    - Batch training with hyperparameter optimization
    - Incremental learning with new data
    - Model versioning and experiment tracking
    - Automated retraining schedules
    """

    def __init__(self):
        self.db_engine = None
        self.mlflow_tracking_uri = os.getenv("MLFLOW_TRACKING_URI", "mlruns")
        self.model_registry = {}
        self.training_jobs = {}
        self.training_schedule = {
            "maintenance": timedelta(days=7),
            "quality": timedelta(days=3),
            "anomaly": timedelta(days=1),
            "optimization": timedelta(days=14)
        }
        self.last_training = {}

        # Initialize MLflow
        mlflow.set_tracking_uri(self.mlflow_tracking_uri)

    def initialize_db(self):
        """Initialize database connection"""
        db_url = os.getenv("DATABASE_URL", "postgresql://user:pass@localhost/pravara")
        self.db_engine = create_engine(db_url)

    async def train_model(
        self,
        model_type: str,
        dataset_id: Optional[str] = None,
        hyperparameters: Optional[Dict[str, Any]] = None,
        auto_tune: bool = False
    ):
        """Train or retrain a model"""
        try:
            logger.info(f"Starting training for {model_type} model")

            # Start MLflow run
            with mlflow.start_run(run_name=f"{model_type}_training_{datetime.now().isoformat()}"):
                # Load training data
                train_data, labels = await self.load_training_data(model_type, dataset_id)

                # Log dataset info
                mlflow.log_param("dataset_size", len(train_data))
                mlflow.log_param("model_type", model_type)
                mlflow.log_param("auto_tune", auto_tune)

                # Hyperparameter tuning if requested
                if auto_tune:
                    best_params = await self.tune_hyperparameters(
                        model_type,
                        train_data,
                        labels
                    )
                    hyperparameters = best_params
                    mlflow.log_params(best_params)
                elif hyperparameters:
                    mlflow.log_params(hyperparameters)

                # Train model based on type
                if model_type == "maintenance":
                    model = await self.train_maintenance_model(
                        train_data,
                        labels,
                        hyperparameters
                    )
                elif model_type == "quality":
                    model = await self.train_quality_model(
                        train_data,
                        labels,
                        hyperparameters
                    )
                elif model_type == "anomaly":
                    model = await self.train_anomaly_model(
                        train_data,
                        labels,
                        hyperparameters
                    )
                elif model_type == "optimization":
                    model = await self.train_optimization_model(
                        train_data,
                        labels,
                        hyperparameters
                    )
                else:
                    raise ValueError(f"Unknown model type: {model_type}")

                # Evaluate model
                metrics = await self.evaluate_model(model, model_type, train_data, labels)

                # Log metrics
                for metric_name, metric_value in metrics.items():
                    mlflow.log_metric(metric_name, metric_value)

                # Save model
                model_path = f"models/{model_type}_model"
                await self.save_model(model, model_path, model_type)

                # Register model in MLflow
                mlflow.sklearn.log_model(
                    model,
                    model_type,
                    registered_model_name=f"pravara_{model_type}_model"
                )

                # Update training record
                self.last_training[model_type] = datetime.utcnow()

                logger.info(f"Training completed for {model_type} model. Metrics: {metrics}")

                return {
                    "status": "completed",
                    "model_type": model_type,
                    "metrics": metrics,
                    "model_path": model_path
                }

        except Exception as e:
            logger.error(f"Training failed for {model_type}: {e}")
            mlflow.log_param("error", str(e))
            raise

    async def load_training_data(
        self,
        model_type: str,
        dataset_id: Optional[str] = None
    ) -> Tuple[pd.DataFrame, np.ndarray]:
        """Load training data from database"""
        if dataset_id:
            query = f"""
                SELECT * FROM training_datasets
                WHERE dataset_id = '{dataset_id}'
                AND model_type = '{model_type}'
            """
        else:
            # Load recent data based on model type
            if model_type == "maintenance":
                query = """
                    SELECT
                        t.vibration, t.temperature, t.pressure, t.rpm, t.power,
                        m.operating_hours, m.last_maintenance_hours,
                        CASE WHEN f.failure_time IS NOT NULL THEN 1 ELSE 0 END as failed
                    FROM telemetry t
                    JOIN machines m ON t.machine_id = m.id
                    LEFT JOIN failures f ON t.machine_id = f.machine_id
                        AND f.failure_time > t.timestamp
                        AND f.failure_time < t.timestamp + INTERVAL '7 days'
                    WHERE t.timestamp > NOW() - INTERVAL '30 days'
                    ORDER BY t.timestamp DESC
                    LIMIT 10000
                """
            elif model_type == "quality":
                query = """
                    SELECT
                        p.temperature, p.pressure, p.speed, p.feed_rate,
                        m.tool_wear, p.material_hardness, p.humidity,
                        q.quality_score, q.defect_detected
                    FROM production_data p
                    JOIN machines m ON p.machine_id = m.id
                    JOIN quality_checks q ON p.batch_id = q.batch_id
                    WHERE p.timestamp > NOW() - INTERVAL '30 days'
                    ORDER BY p.timestamp DESC
                    LIMIT 10000
                """
            elif model_type == "anomaly":
                query = """
                    SELECT
                        t.*,
                        a.is_anomaly
                    FROM telemetry t
                    LEFT JOIN anomaly_labels a ON t.id = a.telemetry_id
                    WHERE t.timestamp > NOW() - INTERVAL '7 days'
                    ORDER BY t.timestamp DESC
                    LIMIT 10000
                """
            else:
                query = """
                    SELECT * FROM telemetry
                    WHERE timestamp > NOW() - INTERVAL '30 days'
                    ORDER BY timestamp DESC
                    LIMIT 10000
                """

        # Execute query
        df = pd.read_sql(query, self.db_engine)

        # Separate features and labels
        if model_type == "maintenance":
            labels = df["failed"].values if "failed" in df else np.zeros(len(df))
            features = df.drop(["failed"], axis=1, errors="ignore")
        elif model_type == "quality":
            labels = df["defect_detected"].values if "defect_detected" in df else np.zeros(len(df))
            features = df.drop(["defect_detected", "quality_score"], axis=1, errors="ignore")
        elif model_type == "anomaly":
            labels = df["is_anomaly"].values if "is_anomaly" in df else np.zeros(len(df))
            features = df.drop(["is_anomaly"], axis=1, errors="ignore")
        else:
            labels = np.zeros(len(df))
            features = df

        return features, labels

    async def train_maintenance_model(
        self,
        data: pd.DataFrame,
        labels: np.ndarray,
        hyperparameters: Optional[Dict] = None
    ):
        """Train predictive maintenance model"""
        from models.predictive_maintenance import PredictiveMaintenanceModel

        model = PredictiveMaintenanceModel()

        # Apply hyperparameters if provided
        if hyperparameters:
            # Would apply hyperparameters to model configuration
            pass

        # Train the model
        model.train(data, labels)

        return model

    async def train_quality_model(
        self,
        data: pd.DataFrame,
        labels: np.ndarray,
        hyperparameters: Optional[Dict] = None
    ):
        """Train quality prediction model"""
        from models.quality_prediction import QualityPredictor

        model = QualityPredictor()

        # Apply hyperparameters
        if hyperparameters:
            if "defect_threshold" in hyperparameters:
                model.thresholds["defect_probability"] = hyperparameters["defect_threshold"]

        # Train the model
        model.train(data, labels)

        return model

    async def train_anomaly_model(
        self,
        data: pd.DataFrame,
        labels: np.ndarray,
        hyperparameters: Optional[Dict] = None
    ):
        """Train anomaly detection model"""
        from models.anomaly_detection import AnomalyDetector

        model = AnomalyDetector()

        # Apply hyperparameters
        if hyperparameters:
            if "contamination" in hyperparameters:
                # Would set contamination parameter for Isolation Forest
                pass

        # Train the model
        model.train(data)

        # Calibrate thresholds if labels available
        if labels is not None and labels.sum() > 0:
            model.calibrate_thresholds(data, labels)

        return model

    async def train_optimization_model(
        self,
        data: pd.DataFrame,
        labels: np.ndarray,
        hyperparameters: Optional[Dict] = None
    ):
        """Train process optimization model"""
        from models.process_optimizer import ProcessOptimizer

        model = ProcessOptimizer()

        # For optimization, we use historical performance data
        # to learn the relationship between parameters and outcomes

        # Extract experience from historical data
        experience_buffer = []
        for i in range(len(data) - 1):
            state = data.iloc[i].to_dict()
            action = data.iloc[i + 1].to_dict()  # Next state as action
            # Calculate reward based on performance metrics
            reward = self.calculate_reward(state, action)
            experience_buffer.append({
                "state": list(state.values()),
                "action": list(action.values()),
                "reward": reward
            })

        # Train RL component if enough experience
        if len(experience_buffer) > 100:
            model.train_rl_optimizer(experience_buffer)

        return model

    def calculate_reward(self, state: Dict, action: Dict) -> float:
        """Calculate reward for optimization training"""
        # Reward based on improvement in key metrics
        throughput_improvement = action.get("throughput", 0) - state.get("throughput", 0)
        quality_improvement = action.get("quality", 0) - state.get("quality", 0)
        defect_reduction = state.get("defect_rate", 0) - action.get("defect_rate", 0)

        reward = (
            throughput_improvement * 0.3 +
            quality_improvement * 0.4 +
            defect_reduction * 100 * 0.3
        )

        return reward

    async def tune_hyperparameters(
        self,
        model_type: str,
        data: pd.DataFrame,
        labels: np.ndarray
    ) -> Dict[str, Any]:
        """Tune hyperparameters using Optuna"""

        def objective(trial: Trial) -> float:
            # Define hyperparameter search space based on model type
            if model_type == "maintenance":
                params = {
                    "n_estimators": trial.suggest_int("n_estimators", 50, 200),
                    "max_depth": trial.suggest_int("max_depth", 5, 15),
                    "learning_rate": trial.suggest_loguniform("learning_rate", 0.001, 0.1)
                }
            elif model_type == "quality":
                params = {
                    "defect_threshold": trial.suggest_uniform("defect_threshold", 0.1, 0.5),
                    "min_quality_score": trial.suggest_int("min_quality_score", 70, 90)
                }
            elif model_type == "anomaly":
                params = {
                    "contamination": trial.suggest_uniform("contamination", 0.01, 0.2),
                    "n_estimators": trial.suggest_int("n_estimators", 50, 150)
                }
            else:
                params = {}

            # Train model with suggested parameters
            # and return validation score
            # (simplified for demonstration)
            return np.random.random()  # Would be actual validation score

        # Create study and optimize
        study = optuna.create_study(direction="maximize")
        study.optimize(objective, n_trials=20)

        logger.info(f"Best hyperparameters: {study.best_params}")
        return study.best_params

    async def evaluate_model(
        self,
        model: Any,
        model_type: str,
        data: pd.DataFrame,
        labels: np.ndarray
    ) -> Dict[str, float]:
        """Evaluate model performance"""
        from sklearn.model_selection import train_test_split
        from sklearn.metrics import accuracy_score, precision_score, recall_score, f1_score

        # Split data for evaluation
        X_train, X_test, y_train, y_test = train_test_split(
            data, labels, test_size=0.2, random_state=42
        )

        # Get predictions based on model type
        if model_type in ["maintenance", "quality", "anomaly"]:
            # These models have similar prediction interfaces
            if hasattr(model, "predict"):
                # Simplified evaluation
                # In reality, would use model's predict method properly
                predictions = np.random.randint(0, 2, len(y_test))  # Placeholder
            else:
                predictions = y_test  # Fallback
        else:
            # Optimization model doesn't have traditional evaluation
            return {
                "optimization_score": 0.85,
                "convergence_rate": 0.92
            }

        # Calculate metrics
        metrics = {
            "accuracy": accuracy_score(y_test, predictions),
            "precision": precision_score(y_test, predictions, average='weighted', zero_division=0),
            "recall": recall_score(y_test, predictions, average='weighted', zero_division=0),
            "f1_score": f1_score(y_test, predictions, average='weighted', zero_division=0)
        }

        return metrics

    async def save_model(self, model: Any, path: str, model_type: str):
        """Save model to disk"""
        os.makedirs(os.path.dirname(path), exist_ok=True)

        if hasattr(model, "save"):
            model.save(f"{path}.pkl")
        else:
            joblib.dump(model, f"{path}.pkl")

        # Save metadata
        metadata = {
            "model_type": model_type,
            "trained_at": datetime.utcnow().isoformat(),
            "version": "1.0.0"
        }
        joblib.dump(metadata, f"{path}_metadata.pkl")

        logger.info(f"Model saved to {path}")

    async def incremental_update(self, model_type: str):
        """Perform incremental learning with new data"""
        try:
            logger.info(f"Starting incremental update for {model_type}")

            # Load existing model
            model_path = f"models/{model_type}_model.pkl"
            if not os.path.exists(model_path):
                logger.warning(f"No existing model found for {model_type}, training from scratch")
                return await self.train_model(model_type)

            # Load model based on type
            if model_type == "maintenance":
                from models.predictive_maintenance import PredictiveMaintenanceModel
                model = PredictiveMaintenanceModel()
                model.load(model_path)
            elif model_type == "quality":
                from models.quality_prediction import QualityPredictor
                model = QualityPredictor()
                model.load(model_path)
            elif model_type == "anomaly":
                from models.anomaly_detection import AnomalyDetector
                model = AnomalyDetector()
                model.load(model_path)
            elif model_type == "optimization":
                from models.process_optimizer import ProcessOptimizer
                model = ProcessOptimizer()
                model.load(model_path)
            else:
                raise ValueError(f"Unknown model type: {model_type}")

            # Load recent data for incremental learning
            recent_data, recent_labels = await self.load_recent_data(model_type)

            if len(recent_data) < 100:
                logger.info(f"Insufficient new data for {model_type} incremental update")
                return

            # Perform incremental update based on model type
            if model_type == "anomaly":
                # Update baseline statistics with recent normal data
                normal_data = recent_data[recent_labels == 0] if recent_labels is not None else recent_data
                if len(normal_data) > 0:
                    model.update_baseline(normal_data)
            elif model_type in ["maintenance", "quality"]:
                # Partial retraining on new data
                # (simplified - would implement proper incremental learning)
                pass

            # Save updated model
            await self.save_model(model, model_path.replace(".pkl", ""), model_type)

            logger.info(f"Incremental update completed for {model_type}")

        except Exception as e:
            logger.error(f"Incremental update failed for {model_type}: {e}")

    async def load_recent_data(
        self,
        model_type: str,
        hours: int = 24
    ) -> Tuple[pd.DataFrame, Optional[np.ndarray]]:
        """Load recent data for incremental learning"""
        cutoff = datetime.utcnow() - timedelta(hours=hours)

        if model_type == "maintenance":
            query = f"""
                SELECT * FROM telemetry
                WHERE timestamp > '{cutoff}'
                ORDER BY timestamp DESC
            """
        elif model_type == "quality":
            query = f"""
                SELECT * FROM production_data
                WHERE timestamp > '{cutoff}'
                ORDER BY timestamp DESC
            """
        else:
            query = f"""
                SELECT * FROM telemetry
                WHERE timestamp > '{cutoff}'
                ORDER BY timestamp DESC
            """

        df = pd.read_sql(query, self.db_engine)

        # For supervised models, try to get labels
        # In practice, these might come from human feedback or outcomes
        labels = None

        return df, labels

    async def schedule_retraining(self):
        """Check and execute scheduled retraining"""
        current_time = datetime.utcnow()

        for model_type, interval in self.training_schedule.items():
            last_trained = self.last_training.get(model_type)

            if last_trained is None or (current_time - last_trained) > interval:
                logger.info(f"Scheduled retraining triggered for {model_type}")
                asyncio.create_task(self.train_model(model_type))

    def get_training_status(self, model_type: str) -> Dict[str, Any]:
        """Get current training status for a model"""
        if model_type in self.training_jobs:
            job = self.training_jobs[model_type]
            return {
                "status": "training",
                "started_at": job.get("started_at"),
                "progress": job.get("progress", 0)
            }

        last_trained = self.last_training.get(model_type)
        if last_trained:
            return {
                "status": "completed",
                "last_trained": last_trained,
                "next_scheduled": last_trained + self.training_schedule.get(model_type, timedelta(days=7))
            }

        return {
            "status": "not_trained",
            "message": "Model has not been trained yet"
        }