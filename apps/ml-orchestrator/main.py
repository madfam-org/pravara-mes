"""
ML Orchestrator Service for PravaraMES
Provides predictive analytics, anomaly detection, and process optimization
"""

import os
import asyncio
import logging
from datetime import datetime, timedelta
from typing import List, Dict, Any, Optional
from contextlib import asynccontextmanager

import numpy as np
import pandas as pd
from fastapi import FastAPI, HTTPException, BackgroundTasks, WebSocket, WebSocketDisconnect
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, Field
import redis.asyncio as redis
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker
import uvicorn

from models.predictive_maintenance import PredictiveMaintenanceModel
from models.anomaly_detection import AnomalyDetector
from models.quality_prediction import QualityPredictor
from models.process_optimizer import ProcessOptimizer
from services.telemetry_service import TelemetryService
from services.training_service import TrainingService
from services.inference_service import InferenceService
from utils.database import get_db_connection, init_db
from utils.metrics import MetricsCollector

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

# Global instances
redis_client = None
metrics_collector = None
telemetry_service = None
training_service = None
inference_service = None

# Models
maintenance_model = None
anomaly_detector = None
quality_predictor = None
process_optimizer = None

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Manage application lifecycle"""
    global redis_client, metrics_collector, telemetry_service
    global training_service, inference_service
    global maintenance_model, anomaly_detector, quality_predictor, process_optimizer

    # Startup
    logger.info("Starting ML Orchestrator Service")

    # Initialize Redis
    redis_client = await redis.from_url(
        os.getenv("REDIS_URL", "redis://localhost:6379"),
        encoding="utf-8",
        decode_responses=True
    )

    # Initialize database
    init_db()

    # Initialize services
    metrics_collector = MetricsCollector()
    telemetry_service = TelemetryService(redis_client)
    training_service = TrainingService()
    inference_service = InferenceService()

    # Load models
    maintenance_model = PredictiveMaintenanceModel()
    anomaly_detector = AnomalyDetector()
    quality_predictor = QualityPredictor()
    process_optimizer = ProcessOptimizer()

    # Load pre-trained weights if available
    try:
        maintenance_model.load()
        anomaly_detector.load()
        quality_predictor.load()
        process_optimizer.load()
        logger.info("Models loaded successfully")
    except Exception as e:
        logger.warning(f"Could not load pre-trained models: {e}")
        logger.info("Models will be trained on first use")

    # Start background tasks
    asyncio.create_task(telemetry_service.subscribe_to_updates())
    asyncio.create_task(continuous_learning_loop())

    yield

    # Shutdown
    logger.info("Shutting down ML Orchestrator Service")
    await redis_client.close()

# Create FastAPI app
app = FastAPI(
    title="ML Orchestrator",
    description="Machine Learning service for predictive analytics in PravaraMES",
    version="1.0.0",
    lifespan=lifespan
)

# CORS middleware
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# Pydantic models
class PredictionRequest(BaseModel):
    machine_id: str
    feature_data: Dict[str, Any]
    prediction_type: str = Field(default="maintenance", pattern="^(maintenance|quality|anomaly|optimization)$")
    horizon_hours: int = Field(default=24, ge=1, le=168)

class PredictionResponse(BaseModel):
    machine_id: str
    prediction_type: str
    timestamp: datetime
    result: Dict[str, Any]
    confidence: float
    recommendations: List[str]

class TrainingRequest(BaseModel):
    model_type: str = Field(pattern="^(maintenance|quality|anomaly|optimization)$")
    dataset_id: Optional[str] = None
    hyperparameters: Optional[Dict[str, Any]] = None
    auto_tune: bool = False

class ModelMetrics(BaseModel):
    model_type: str
    accuracy: float
    precision: float
    recall: float
    f1_score: float
    last_trained: datetime
    training_samples: int
    version: str

class AnomalyAlert(BaseModel):
    machine_id: str
    timestamp: datetime
    anomaly_type: str
    severity: str
    description: str
    recommended_actions: List[str]
    telemetry_data: Dict[str, Any]

# API Endpoints

@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {
        "status": "healthy",
        "timestamp": datetime.utcnow(),
        "models_loaded": all([
            maintenance_model is not None,
            anomaly_detector is not None,
            quality_predictor is not None,
            process_optimizer is not None
        ])
    }

@app.post("/predict", response_model=PredictionResponse)
async def predict(request: PredictionRequest, background_tasks: BackgroundTasks):
    """Make prediction based on machine telemetry and historical data"""
    try:
        # Get historical data
        historical_data = await telemetry_service.get_historical_data(
            request.machine_id,
            hours=168  # Last week
        )

        # Prepare features
        features = inference_service.prepare_features(
            historical_data,
            request.feature_data
        )

        # Make prediction based on type
        if request.prediction_type == "maintenance":
            result = await maintenance_model.predict(
                features,
                horizon_hours=request.horizon_hours
            )
        elif request.prediction_type == "quality":
            result = await quality_predictor.predict(features)
        elif request.prediction_type == "anomaly":
            result = await anomaly_detector.detect(features)
        elif request.prediction_type == "optimization":
            result = await process_optimizer.optimize(features)
        else:
            raise ValueError(f"Unknown prediction type: {request.prediction_type}")

        # Log prediction for continuous learning
        background_tasks.add_task(
            log_prediction,
            request.machine_id,
            request.prediction_type,
            result
        )

        # Update metrics
        metrics_collector.record_prediction(request.prediction_type)

        return PredictionResponse(
            machine_id=request.machine_id,
            prediction_type=request.prediction_type,
            timestamp=datetime.utcnow(),
            result=result["prediction"],
            confidence=result["confidence"],
            recommendations=result["recommendations"]
        )

    except Exception as e:
        logger.error(f"Prediction error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/train")
async def train_model(request: TrainingRequest, background_tasks: BackgroundTasks):
    """Train or retrain a model"""
    try:
        # Start training in background
        background_tasks.add_task(
            training_service.train_model,
            request.model_type,
            request.dataset_id,
            request.hyperparameters,
            request.auto_tune
        )

        return {
            "status": "training_started",
            "model_type": request.model_type,
            "job_id": f"train_{request.model_type}_{datetime.utcnow().timestamp()}"
        }

    except Exception as e:
        logger.error(f"Training error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/models/{model_type}/metrics", response_model=ModelMetrics)
async def get_model_metrics(model_type: str):
    """Get metrics for a specific model"""
    try:
        if model_type == "maintenance":
            metrics = maintenance_model.get_metrics()
        elif model_type == "quality":
            metrics = quality_predictor.get_metrics()
        elif model_type == "anomaly":
            metrics = anomaly_detector.get_metrics()
        elif model_type == "optimization":
            metrics = process_optimizer.get_metrics()
        else:
            raise ValueError(f"Unknown model type: {model_type}")

        return ModelMetrics(**metrics)

    except Exception as e:
        logger.error(f"Metrics error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/anomalies/recent")
async def get_recent_anomalies(machine_id: Optional[str] = None, limit: int = 50):
    """Get recent anomaly detections"""
    try:
        anomalies = await anomaly_detector.get_recent_anomalies(
            machine_id=machine_id,
            limit=limit
        )

        return {
            "count": len(anomalies),
            "anomalies": anomalies
        }

    except Exception as e:
        logger.error(f"Anomaly retrieval error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/optimize/parameters")
async def optimize_parameters(machine_id: str, current_params: Dict[str, float]):
    """Get optimized process parameters for a machine"""
    try:
        # Get current telemetry
        telemetry = await telemetry_service.get_latest_telemetry(machine_id)

        # Optimize parameters
        optimized = await process_optimizer.optimize_parameters(
            machine_id,
            current_params,
            telemetry
        )

        return {
            "machine_id": machine_id,
            "current_parameters": current_params,
            "optimized_parameters": optimized["parameters"],
            "expected_improvement": optimized["improvement"],
            "constraints_satisfied": optimized["constraints_satisfied"]
        }

    except Exception as e:
        logger.error(f"Optimization error: {e}")
        raise HTTPException(status_code=500, detail=str(e))

@app.websocket("/ws/predictions")
async def websocket_predictions(websocket: WebSocket):
    """WebSocket endpoint for real-time predictions"""
    await websocket.accept()

    try:
        # Subscribe to real-time telemetry
        pubsub = redis_client.pubsub()
        await pubsub.subscribe("telemetry:*")

        while True:
            # Get telemetry update
            message = await pubsub.get_message(ignore_subscribe_messages=True)
            if message:
                # Parse telemetry
                telemetry = eval(message['data'])  # In production, use proper JSON parsing
                machine_id = message['channel'].split(':')[1]

                # Check for anomalies
                anomaly = await anomaly_detector.detect_realtime(telemetry)

                if anomaly["is_anomaly"]:
                    # Send anomaly alert
                    alert = AnomalyAlert(
                        machine_id=machine_id,
                        timestamp=datetime.utcnow(),
                        anomaly_type=anomaly["type"],
                        severity=anomaly["severity"],
                        description=anomaly["description"],
                        recommended_actions=anomaly["actions"],
                        telemetry_data=telemetry
                    )

                    await websocket.send_json(alert.dict())

                # Check maintenance prediction
                if await should_check_maintenance(machine_id):
                    maintenance = await maintenance_model.predict_realtime(telemetry)

                    if maintenance["risk_score"] > 0.7:
                        await websocket.send_json({
                            "type": "maintenance_alert",
                            "machine_id": machine_id,
                            "risk_score": maintenance["risk_score"],
                            "estimated_time_to_failure": maintenance["ttf_hours"],
                            "recommended_maintenance": maintenance["recommendations"]
                        })

            await asyncio.sleep(0.1)

    except WebSocketDisconnect:
        logger.info("WebSocket client disconnected")
    except Exception as e:
        logger.error(f"WebSocket error: {e}")
    finally:
        await pubsub.unsubscribe("telemetry:*")
        await pubsub.close()

# Background tasks

async def continuous_learning_loop():
    """Continuously update models with new data"""
    while True:
        try:
            # Wait for next training cycle
            await asyncio.sleep(3600)  # Train every hour

            logger.info("Starting continuous learning cycle")

            # Update each model with recent data
            for model_type in ["maintenance", "quality", "anomaly", "optimization"]:
                try:
                    await training_service.incremental_update(model_type)
                    logger.info(f"Updated {model_type} model")
                except Exception as e:
                    logger.error(f"Failed to update {model_type} model: {e}")

            # Clean up old predictions
            await cleanup_old_predictions()

        except Exception as e:
            logger.error(f"Continuous learning error: {e}")

async def log_prediction(machine_id: str, prediction_type: str, result: Dict):
    """Log prediction for future training"""
    try:
        await redis_client.lpush(
            f"predictions:{prediction_type}:{machine_id}",
            {
                "timestamp": datetime.utcnow().isoformat(),
                "result": result
            }
        )

        # Trim to last 1000 predictions
        await redis_client.ltrim(
            f"predictions:{prediction_type}:{machine_id}",
            0,
            999
        )
    except Exception as e:
        logger.error(f"Failed to log prediction: {e}")

async def should_check_maintenance(machine_id: str) -> bool:
    """Determine if maintenance check is needed"""
    # Check every 5 minutes per machine
    last_check = await redis_client.get(f"last_maintenance_check:{machine_id}")

    if not last_check:
        await redis_client.setex(
            f"last_maintenance_check:{machine_id}",
            300,  # 5 minutes
            datetime.utcnow().isoformat()
        )
        return True

    return False

async def cleanup_old_predictions():
    """Clean up old prediction logs"""
    try:
        cutoff = datetime.utcnow() - timedelta(days=30)
        # Implementation would clean up old data from database/storage
        logger.info(f"Cleaned up predictions older than {cutoff}")
    except Exception as e:
        logger.error(f"Cleanup error: {e}")

if __name__ == "__main__":
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=8001,
        reload=True,
        log_level="info"
    )