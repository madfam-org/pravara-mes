"""
Database Utilities
Database connection and management utilities for ML orchestrator
"""

import os
import json
import logging
from typing import Optional, Dict, Any, List
from datetime import datetime
import pandas as pd
from sqlalchemy import create_engine, text, MetaData, Table
from sqlalchemy.orm import sessionmaker, Session
from sqlalchemy.pool import NullPool
import asyncpg

logger = logging.getLogger(__name__)

class DatabaseManager:
    """Manage database connections and operations"""

    def __init__(self):
        self.engine = None
        self.async_pool = None
        self.metadata = MetaData()
        self.tables = {}

    def init_sync_connection(self) -> None:
        """Initialize synchronous database connection"""
        db_url = os.getenv("DATABASE_URL", "postgresql://user:pass@localhost/pravara")

        # Use NullPool for better connection management
        self.engine = create_engine(
            db_url,
            poolclass=NullPool,
            echo=False
        )

        # Load table metadata
        self.metadata.reflect(bind=self.engine)

        # Cache frequently used tables
        table_names = ["telemetry", "machines", "production_data", "quality_checks"]
        for table_name in table_names:
            if table_name in self.metadata.tables:
                self.tables[table_name] = self.metadata.tables[table_name]

        logger.info("Synchronous database connection initialized")

    async def init_async_connection(self) -> None:
        """Initialize asynchronous database connection pool"""
        db_url = os.getenv("DATABASE_URL", "postgresql://user:pass@localhost/pravara")

        # Parse connection string
        if db_url.startswith("postgresql://"):
            db_url = db_url.replace("postgresql://", "")

        # Create async connection pool
        self.async_pool = await asyncpg.create_pool(
            dsn=f"postgresql://{db_url}",
            min_size=5,
            max_size=20,
            max_queries=50000,
            max_cached_statement_lifetime=300,
            command_timeout=60
        )

        logger.info("Asynchronous database connection pool initialized")

    def get_session(self) -> Session:
        """Get a database session"""
        if not self.engine:
            self.init_sync_connection()

        SessionLocal = sessionmaker(bind=self.engine)
        return SessionLocal()

    async def execute_async(self, query: str, *args) -> List[asyncpg.Record]:
        """Execute async query"""
        if not self.async_pool:
            await self.init_async_connection()

        async with self.async_pool.acquire() as conn:
            return await conn.fetch(query, *args)

    async def execute_async_one(self, query: str, *args) -> Optional[asyncpg.Record]:
        """Execute async query returning single result"""
        if not self.async_pool:
            await self.init_async_connection()

        async with self.async_pool.acquire() as conn:
            return await conn.fetchrow(query, *args)

    async def execute_async_scalar(self, query: str, *args) -> Any:
        """Execute async query returning scalar value"""
        if not self.async_pool:
            await self.init_async_connection()

        async with self.async_pool.acquire() as conn:
            return await conn.fetchval(query, *args)

    def close(self) -> None:
        """Close database connections"""
        if self.engine:
            self.engine.dispose()

    async def close_async(self) -> None:
        """Close async connection pool"""
        if self.async_pool:
            await self.async_pool.close()

# Global database manager instance
db_manager = DatabaseManager()

def get_db_connection():
    """Get database connection for sync operations"""
    return db_manager.engine

def init_db():
    """Initialize database connections"""
    db_manager.init_sync_connection()

async def init_async_db():
    """Initialize async database connections"""
    await db_manager.init_async_connection()

# Database query helpers

async def get_recent_telemetry(
    machine_id: str,
    hours: int = 24,
    metrics: Optional[List[str]] = None
) -> pd.DataFrame:
    """Get recent telemetry data for a machine"""
    if metrics:
        columns = ", ".join(["timestamp"] + metrics)
    else:
        columns = "*"

    query = f"""
        SELECT {columns}
        FROM telemetry
        WHERE machine_id = $1
        AND timestamp > NOW() - INTERVAL '{hours} hours'
        ORDER BY timestamp DESC
    """

    results = await db_manager.execute_async(query, machine_id)

    if results:
        return pd.DataFrame([dict(r) for r in results])
    return pd.DataFrame()

async def get_machine_info(machine_id: str) -> Dict[str, Any]:
    """Get machine information"""
    query = """
        SELECT
            m.*,
            mt.name as machine_type_name,
            COUNT(DISTINCT t.id) as total_tasks,
            AVG(t.actual_duration) as avg_task_duration,
            MAX(tel.timestamp) as last_telemetry
        FROM machines m
        LEFT JOIN machine_types mt ON m.type_id = mt.id
        LEFT JOIN tasks t ON m.id = t.assigned_machine_id
        LEFT JOIN telemetry tel ON m.id = tel.machine_id
        WHERE m.id = $1
        GROUP BY m.id, mt.name
    """

    result = await db_manager.execute_async_one(query, machine_id)

    if result:
        return dict(result)
    return {}

async def get_production_metrics(
    start_date: datetime,
    end_date: datetime
) -> Dict[str, Any]:
    """Get production metrics for a date range"""
    query = """
        SELECT
            COUNT(DISTINCT o.id) as total_orders,
            COUNT(DISTINCT t.id) as total_tasks,
            COUNT(DISTINCT t.id) FILTER (WHERE t.status = 'completed') as completed_tasks,
            AVG(t.actual_duration) as avg_task_duration,
            AVG(q.quality_score) as avg_quality_score,
            SUM(CASE WHEN q.defect_detected THEN 1 ELSE 0 END)::float / COUNT(q.id) as defect_rate
        FROM orders o
        LEFT JOIN tasks t ON o.id = t.order_id
        LEFT JOIN quality_checks q ON t.id = q.task_id
        WHERE o.created_at BETWEEN $1 AND $2
    """

    result = await db_manager.execute_async_one(query, start_date, end_date)

    if result:
        return dict(result)
    return {}

async def store_prediction(
    machine_id: str,
    model_type: str,
    prediction: Dict[str, Any],
    confidence: float
) -> None:
    """Store model prediction in database"""
    query = """
        INSERT INTO ml_predictions
        (machine_id, model_type, prediction, confidence, created_at)
        VALUES ($1, $2, $3::jsonb, $4, $5)
    """

    await db_manager.execute_async(
        query,
        machine_id,
        model_type,
        json.dumps(prediction),
        confidence,
        datetime.utcnow()
    )

async def get_training_data(
    model_type: str,
    limit: int = 10000
) -> pd.DataFrame:
    """Get training data for a specific model type"""
    if model_type == "maintenance":
        query = """
            SELECT
                t.*,
                m.operating_hours,
                m.last_maintenance_hours,
                CASE
                    WHEN f.id IS NOT NULL THEN 1
                    ELSE 0
                END as failure_indicator
            FROM telemetry t
            JOIN machines m ON t.machine_id = m.id
            LEFT JOIN (
                SELECT DISTINCT machine_id, id
                FROM failure_logs
                WHERE failure_time > NOW() - INTERVAL '30 days'
            ) f ON t.machine_id = f.machine_id
            WHERE t.timestamp > NOW() - INTERVAL '30 days'
            ORDER BY t.timestamp DESC
            LIMIT $1
        """
    elif model_type == "quality":
        query = """
            SELECT
                pd.*,
                q.quality_score,
                q.defect_detected,
                m.tool_wear
            FROM production_data pd
            JOIN quality_checks q ON pd.batch_id = q.batch_id
            JOIN machines m ON pd.machine_id = m.id
            WHERE pd.timestamp > NOW() - INTERVAL '30 days'
            ORDER BY pd.timestamp DESC
            LIMIT $1
        """
    elif model_type == "anomaly":
        query = """
            SELECT
                t.*,
                al.is_anomaly,
                al.anomaly_type
            FROM telemetry t
            LEFT JOIN anomaly_labels al ON t.id = al.telemetry_id
            WHERE t.timestamp > NOW() - INTERVAL '7 days'
            ORDER BY t.timestamp DESC
            LIMIT $1
        """
    else:
        query = """
            SELECT *
            FROM telemetry
            WHERE timestamp > NOW() - INTERVAL '30 days'
            ORDER BY timestamp DESC
            LIMIT $1
        """

    results = await db_manager.execute_async(query, limit)

    if results:
        return pd.DataFrame([dict(r) for r in results])
    return pd.DataFrame()

async def create_ml_tables() -> None:
    """Create ML-specific tables if they don't exist"""
    queries = [
        """
        CREATE TABLE IF NOT EXISTS ml_predictions (
            id SERIAL PRIMARY KEY,
            machine_id UUID NOT NULL,
            model_type VARCHAR(50) NOT NULL,
            prediction JSONB NOT NULL,
            confidence FLOAT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_predictions_machine_time (machine_id, created_at DESC),
            INDEX idx_predictions_model_type (model_type)
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS ml_model_versions (
            id SERIAL PRIMARY KEY,
            model_type VARCHAR(50) NOT NULL,
            version VARCHAR(50) NOT NULL,
            metrics JSONB,
            hyperparameters JSONB,
            trained_at TIMESTAMP,
            deployed_at TIMESTAMP,
            is_active BOOLEAN DEFAULT FALSE,
            UNIQUE(model_type, version)
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS ml_training_jobs (
            id SERIAL PRIMARY KEY,
            job_id VARCHAR(100) UNIQUE NOT NULL,
            model_type VARCHAR(50) NOT NULL,
            status VARCHAR(50) NOT NULL,
            started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            completed_at TIMESTAMP,
            metrics JSONB,
            error_message TEXT,
            INDEX idx_training_jobs_status (status)
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS anomaly_labels (
            id SERIAL PRIMARY KEY,
            telemetry_id BIGINT REFERENCES telemetry(id),
            is_anomaly BOOLEAN NOT NULL,
            anomaly_type VARCHAR(50),
            confidence FLOAT,
            labeled_by VARCHAR(100),
            labeled_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_anomaly_labels_telemetry (telemetry_id)
        )
        """,
        """
        CREATE TABLE IF NOT EXISTS failure_logs (
            id SERIAL PRIMARY KEY,
            machine_id UUID NOT NULL,
            failure_time TIMESTAMP NOT NULL,
            failure_type VARCHAR(100),
            severity VARCHAR(50),
            repair_duration_hours FLOAT,
            root_cause TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            INDEX idx_failure_logs_machine_time (machine_id, failure_time DESC)
        )
        """
    ]

    for query in queries:
        try:
            await db_manager.execute_async(query)
        except Exception as e:
            logger.warning(f"Table creation warning (may already exist): {e}")

    logger.info("ML tables initialized")

# Pandas helpers

def optimize_dataframe_dtypes(df: pd.DataFrame) -> pd.DataFrame:
    """Optimize DataFrame memory usage by converting dtypes"""
    for col in df.columns:
        col_type = df[col].dtype

        if col_type != 'object':
            c_min = df[col].min()
            c_max = df[col].max()

            if str(col_type)[:3] == 'int':
                if c_min > np.iinfo(np.int8).min and c_max < np.iinfo(np.int8).max:
                    df[col] = df[col].astype(np.int8)
                elif c_min > np.iinfo(np.int16).min and c_max < np.iinfo(np.int16).max:
                    df[col] = df[col].astype(np.int16)
                elif c_min > np.iinfo(np.int32).min and c_max < np.iinfo(np.int32).max:
                    df[col] = df[col].astype(np.int32)
            else:
                if c_min > np.finfo(np.float16).min and c_max < np.finfo(np.float16).max:
                    df[col] = df[col].astype(np.float16)
                elif c_min > np.finfo(np.float32).min and c_max < np.finfo(np.float32).max:
                    df[col] = df[col].astype(np.float32)

    return df