"""
ML Utils Package
"""

from .database import (
    get_db_connection,
    init_db,
    init_async_db,
    get_recent_telemetry,
    get_machine_info,
    get_production_metrics,
    store_prediction,
    get_training_data,
    create_ml_tables,
    optimize_dataframe_dtypes,
)

from .metrics import (
    MetricsCollector,
    track_prediction_metrics,
    track_training_metrics,
)

__all__ = [
    # Database utilities
    "get_db_connection",
    "init_db",
    "init_async_db",
    "get_recent_telemetry",
    "get_machine_info",
    "get_production_metrics",
    "store_prediction",
    "get_training_data",
    "create_ml_tables",
    "optimize_dataframe_dtypes",
    # Metrics utilities
    "MetricsCollector",
    "track_prediction_metrics",
    "track_training_metrics",
]