"""
Metrics Collector
Collects and exposes metrics for monitoring and observability
"""

import time
import logging
from datetime import datetime
from typing import Dict, Any, Optional
from collections import defaultdict
from prometheus_client import (
    Counter, Histogram, Gauge, Summary,
    CollectorRegistry, generate_latest
)

logger = logging.getLogger(__name__)

class MetricsCollector:
    """
    Collects metrics for ML orchestrator service:
    - Prediction metrics (latency, count, errors)
    - Model performance metrics (accuracy, drift)
    - Training metrics (duration, success rate)
    - System metrics (memory, CPU usage)
    """

    def __init__(self):
        self.registry = CollectorRegistry()

        # Prediction metrics
        self.prediction_counter = Counter(
            'ml_predictions_total',
            'Total number of predictions made',
            ['model_type', 'status'],
            registry=self.registry
        )

        self.prediction_latency = Histogram(
            'ml_prediction_latency_seconds',
            'Prediction latency in seconds',
            ['model_type'],
            buckets=[0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0],
            registry=self.registry
        )

        self.prediction_confidence = Summary(
            'ml_prediction_confidence',
            'Prediction confidence scores',
            ['model_type'],
            registry=self.registry
        )

        # Model performance metrics
        self.model_accuracy = Gauge(
            'ml_model_accuracy',
            'Model accuracy score',
            ['model_type'],
            registry=self.registry
        )

        self.model_drift = Gauge(
            'ml_model_drift',
            'Model drift score',
            ['model_type'],
            registry=self.registry
        )

        # Training metrics
        self.training_duration = Histogram(
            'ml_training_duration_seconds',
            'Model training duration in seconds',
            ['model_type'],
            buckets=[60, 300, 600, 1800, 3600, 7200, 14400],
            registry=self.registry
        )

        self.training_counter = Counter(
            'ml_training_jobs_total',
            'Total number of training jobs',
            ['model_type', 'status'],
            registry=self.registry
        )

        # Anomaly detection metrics
        self.anomaly_counter = Counter(
            'ml_anomalies_detected_total',
            'Total number of anomalies detected',
            ['severity', 'type'],
            registry=self.registry
        )

        self.anomaly_rate = Gauge(
            'ml_anomaly_rate',
            'Current anomaly detection rate',
            ['machine_id'],
            registry=self.registry
        )

        # Quality metrics
        self.quality_score = Gauge(
            'ml_quality_score',
            'Current quality score',
            ['machine_id', 'product_type'],
            registry=self.registry
        )

        self.defect_rate = Gauge(
            'ml_defect_rate',
            'Current defect rate',
            ['machine_id', 'product_type'],
            registry=self.registry
        )

        # Process optimization metrics
        self.optimization_improvement = Gauge(
            'ml_optimization_improvement_percent',
            'Process optimization improvement percentage',
            ['machine_id', 'metric'],
            registry=self.registry
        )

        # System metrics
        self.active_models = Gauge(
            'ml_active_models',
            'Number of active models',
            registry=self.registry
        )

        self.cache_hit_rate = Gauge(
            'ml_cache_hit_rate',
            'Inference cache hit rate',
            registry=self.registry
        )

        # Internal metrics storage
        self.internal_metrics = defaultdict(lambda: defaultdict(float))
        self.timers = {}

    def record_prediction(
        self,
        model_type: str,
        status: str = "success",
        latency: Optional[float] = None,
        confidence: Optional[float] = None
    ):
        """Record a prediction event"""
        self.prediction_counter.labels(
            model_type=model_type,
            status=status
        ).inc()

        if latency is not None:
            self.prediction_latency.labels(
                model_type=model_type
            ).observe(latency)

        if confidence is not None:
            self.prediction_confidence.labels(
                model_type=model_type
            ).observe(confidence)

        # Update internal metrics
        self.internal_metrics[model_type]["total_predictions"] += 1
        if status == "success":
            self.internal_metrics[model_type]["successful_predictions"] += 1

    def record_training(
        self,
        model_type: str,
        status: str,
        duration: Optional[float] = None,
        metrics: Optional[Dict[str, float]] = None
    ):
        """Record a training event"""
        self.training_counter.labels(
            model_type=model_type,
            status=status
        ).inc()

        if duration is not None:
            self.training_duration.labels(
                model_type=model_type
            ).observe(duration)

        if metrics:
            if "accuracy" in metrics:
                self.model_accuracy.labels(
                    model_type=model_type
                ).set(metrics["accuracy"])

            # Store other metrics internally
            for metric_name, value in metrics.items():
                self.internal_metrics[model_type][f"training_{metric_name}"] = value

    def record_anomaly(
        self,
        severity: str,
        anomaly_type: str,
        machine_id: Optional[str] = None
    ):
        """Record an anomaly detection"""
        self.anomaly_counter.labels(
            severity=severity,
            type=anomaly_type
        ).inc()

        if machine_id:
            # Update anomaly rate for machine
            current_rate = self.internal_metrics[machine_id].get("anomaly_rate", 0)
            self.internal_metrics[machine_id]["anomaly_rate"] = current_rate * 0.9 + 0.1
            self.anomaly_rate.labels(machine_id=machine_id).set(
                self.internal_metrics[machine_id]["anomaly_rate"]
            )

    def record_quality(
        self,
        machine_id: str,
        product_type: str,
        quality_score: float,
        defect_detected: bool
    ):
        """Record quality metrics"""
        self.quality_score.labels(
            machine_id=machine_id,
            product_type=product_type
        ).set(quality_score)

        # Update defect rate with exponential moving average
        current_rate = self.internal_metrics[machine_id].get("defect_rate", 0)
        new_rate = current_rate * 0.95 + (0.05 if defect_detected else 0)
        self.internal_metrics[machine_id]["defect_rate"] = new_rate

        self.defect_rate.labels(
            machine_id=machine_id,
            product_type=product_type
        ).set(new_rate)

    def record_optimization(
        self,
        machine_id: str,
        metric: str,
        improvement_percent: float
    ):
        """Record optimization results"""
        self.optimization_improvement.labels(
            machine_id=machine_id,
            metric=metric
        ).set(improvement_percent)

        # Track best improvements
        key = f"{machine_id}_{metric}_best"
        if improvement_percent > self.internal_metrics["optimization"].get(key, 0):
            self.internal_metrics["optimization"][key] = improvement_percent

    def update_model_drift(self, model_type: str, drift_score: float):
        """Update model drift score"""
        self.model_drift.labels(model_type=model_type).set(drift_score)

    def update_cache_metrics(self, hit_rate: float):
        """Update cache hit rate"""
        self.cache_hit_rate.set(hit_rate)

    def update_active_models(self, count: int):
        """Update active models count"""
        self.active_models.set(count)

    def start_timer(self, operation: str) -> str:
        """Start a timer for an operation"""
        timer_id = f"{operation}_{time.time()}"
        self.timers[timer_id] = time.time()
        return timer_id

    def stop_timer(self, timer_id: str) -> float:
        """Stop a timer and return duration"""
        if timer_id in self.timers:
            duration = time.time() - self.timers[timer_id]
            del self.timers[timer_id]
            return duration
        return 0.0

    def get_prometheus_metrics(self) -> bytes:
        """Get metrics in Prometheus format"""
        return generate_latest(self.registry)

    def get_internal_metrics(self) -> Dict[str, Any]:
        """Get internal metrics as dictionary"""
        return dict(self.internal_metrics)

    def calculate_model_health(self, model_type: str) -> Dict[str, Any]:
        """Calculate overall model health metrics"""
        metrics = self.internal_metrics.get(model_type, {})

        total = metrics.get("total_predictions", 0)
        successful = metrics.get("successful_predictions", 0)

        if total > 0:
            success_rate = successful / total
        else:
            success_rate = 0

        health = {
            "model_type": model_type,
            "success_rate": success_rate,
            "total_predictions": total,
            "accuracy": metrics.get("training_accuracy", 0),
            "last_training": metrics.get("last_training_time"),
            "health_score": self._calculate_health_score(metrics)
        }

        return health

    def _calculate_health_score(self, metrics: Dict[str, float]) -> float:
        """Calculate overall health score for a model"""
        score = 0.5  # Base score

        # Factor in success rate
        total = metrics.get("total_predictions", 0)
        successful = metrics.get("successful_predictions", 0)
        if total > 0:
            success_rate = successful / total
            score = score * 0.5 + success_rate * 0.5

        # Factor in accuracy
        accuracy = metrics.get("training_accuracy", 0)
        if accuracy > 0:
            score = score * 0.7 + accuracy * 0.3

        return min(max(score, 0), 1)  # Clamp between 0 and 1

    def generate_report(self) -> Dict[str, Any]:
        """Generate comprehensive metrics report"""
        report = {
            "timestamp": datetime.utcnow().isoformat(),
            "models": {},
            "system": {
                "cache_hit_rate": self.internal_metrics.get("cache_hit_rate", 0),
                "active_models": self.internal_metrics.get("active_models", 0)
            },
            "anomalies": {
                "total_detected": sum(
                    self.internal_metrics.get(f"anomaly_{severity}", 0)
                    for severity in ["low", "medium", "high", "critical"]
                )
            },
            "optimizations": {
                "best_improvements": {
                    k: v for k, v in self.internal_metrics.get("optimization", {}).items()
                    if "best" in k
                }
            }
        }

        # Add model-specific metrics
        model_types = ["maintenance", "quality", "anomaly", "optimization"]
        for model_type in model_types:
            report["models"][model_type] = self.calculate_model_health(model_type)

        return report

    def reset_metrics(self):
        """Reset all metrics (useful for testing)"""
        self.internal_metrics.clear()
        self.timers.clear()
        logger.info("Metrics reset")

# Decorators for automatic metric collection

def track_prediction_metrics(model_type: str):
    """Decorator to track prediction metrics"""
    def decorator(func):
        async def wrapper(self, *args, **kwargs):
            collector = getattr(self, 'metrics_collector', None)
            if not collector:
                return await func(self, *args, **kwargs)

            timer_id = collector.start_timer(f"prediction_{model_type}")
            try:
                result = await func(self, *args, **kwargs)
                latency = collector.stop_timer(timer_id)

                confidence = result.get("confidence", 0.5) if isinstance(result, dict) else 0.5
                collector.record_prediction(
                    model_type=model_type,
                    status="success",
                    latency=latency,
                    confidence=confidence
                )
                return result
            except Exception as e:
                collector.stop_timer(timer_id)
                collector.record_prediction(
                    model_type=model_type,
                    status="error"
                )
                raise

        return wrapper
    return decorator

def track_training_metrics(model_type: str):
    """Decorator to track training metrics"""
    def decorator(func):
        async def wrapper(self, *args, **kwargs):
            collector = getattr(self, 'metrics_collector', None)
            if not collector:
                return await func(self, *args, **kwargs)

            timer_id = collector.start_timer(f"training_{model_type}")
            try:
                result = await func(self, *args, **kwargs)
                duration = collector.stop_timer(timer_id)

                metrics = result.get("metrics", {}) if isinstance(result, dict) else {}
                collector.record_training(
                    model_type=model_type,
                    status="success",
                    duration=duration,
                    metrics=metrics
                )
                return result
            except Exception as e:
                collector.stop_timer(timer_id)
                collector.record_training(
                    model_type=model_type,
                    status="error"
                )
                raise

        return wrapper
    return decorator