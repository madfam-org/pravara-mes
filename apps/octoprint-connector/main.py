"""
OctoPrint Connector Service for PravaraMES
Manages connections to OctoPrint instances for 3D printer control
"""

import asyncio
import json
import logging
import os
from contextlib import asynccontextmanager
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Any

import httpx
import structlog
import uvicorn
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect, BackgroundTasks
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel, HttpUrl, Field
from redis import asyncio as aioredis
import paho.mqtt.client as mqtt
from tenacity import retry, stop_after_attempt, wait_exponential

# Configure structured logging
structlog.configure(
    processors=[
        structlog.stdlib.filter_by_level,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.stdlib.PositionalArgumentsFormatter(),
        structlog.processors.TimeStamper(fmt="iso"),
        structlog.processors.StackInfoRenderer(),
        structlog.processors.format_exc_info,
        structlog.processors.UnicodeDecoder(),
        structlog.processors.JSONRenderer()
    ],
    context_class=dict,
    logger_factory=structlog.stdlib.LoggerFactory(),
    cache_logger_on_first_use=True,
)

logger = structlog.get_logger()

# ============================================================================
# Data Models
# ============================================================================

class OctoPrintInstance(BaseModel):
    """OctoPrint instance configuration"""
    id: str
    name: str
    url: HttpUrl
    api_key: str
    description: Optional[str] = None
    printer_profile: Optional[str] = None
    active: bool = True
    last_seen: Optional[datetime] = None
    capabilities: List[str] = Field(default_factory=list)

class PrinterStatus(BaseModel):
    """Current printer status"""
    state: str  # Operational, Printing, Paused, Error, Offline
    temperature: Dict[str, Any]
    job: Optional[Dict[str, Any]] = None
    progress: Optional[float] = None
    time_remaining: Optional[int] = None
    time_elapsed: Optional[int] = None

class PrintJob(BaseModel):
    """Print job details"""
    file_name: str
    file_path: str
    file_size: int
    estimated_time: int
    material_length: Optional[float] = None
    material_volume: Optional[float] = None

class GCodeCommand(BaseModel):
    """G-code command to send"""
    command: str
    instance_id: str

class FileUpload(BaseModel):
    """File upload request"""
    instance_id: str
    file_name: str
    file_content: str  # Base64 encoded
    location: str = "local"  # local or sdcard
    print_after_upload: bool = False

# ============================================================================
# OctoPrint API Client
# ============================================================================

class OctoPrintClient:
    """Client for interacting with OctoPrint API"""

    def __init__(self, instance: OctoPrintInstance):
        self.instance = instance
        self.client = httpx.AsyncClient(
            base_url=str(instance.url),
            headers={"X-Api-Key": instance.api_key},
            timeout=30.0
        )

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb):
        await self.client.aclose()

    @retry(stop=stop_after_attempt(3), wait=wait_exponential(min=1, max=10))
    async def get_version(self) -> Dict:
        """Get OctoPrint version info"""
        response = await self.client.get("/api/version")
        response.raise_for_status()
        return response.json()

    @retry(stop=stop_after_attempt(3), wait=wait_exponential(min=1, max=10))
    async def get_printer_status(self) -> PrinterStatus:
        """Get current printer status"""
        response = await self.client.get("/api/printer")
        response.raise_for_status()
        data = response.json()

        # Get job info
        job_response = await self.client.get("/api/job")
        job_data = job_response.json() if job_response.status_code == 200 else {}

        return PrinterStatus(
            state=data.get("state", {}).get("text", "Unknown"),
            temperature=data.get("temperature", {}),
            job=job_data.get("job"),
            progress=job_data.get("progress", {}).get("completion"),
            time_remaining=job_data.get("progress", {}).get("printTimeLeft"),
            time_elapsed=job_data.get("progress", {}).get("printTime")
        )

    async def send_gcode(self, commands: List[str]) -> Dict:
        """Send G-code commands to printer"""
        response = await self.client.post(
            "/api/printer/command",
            json={"commands": commands}
        )
        response.raise_for_status()
        return {"status": "sent", "commands": commands}

    async def upload_file(self, file_name: str, file_content: bytes,
                         location: str = "local", print_after: bool = False) -> Dict:
        """Upload file to OctoPrint"""
        files = {"file": (file_name, file_content)}
        data = {
            "select": "true" if print_after else "false",
            "print": "true" if print_after else "false"
        }

        response = await self.client.post(
            f"/api/files/{location}",
            files=files,
            data=data
        )
        response.raise_for_status()
        return response.json()

    async def start_print(self, file_path: str) -> Dict:
        """Start printing a file"""
        response = await self.client.post(
            f"/api/files/local/{file_path}",
            json={"command": "select", "print": True}
        )
        response.raise_for_status()
        return {"status": "printing", "file": file_path}

    async def pause_print(self) -> Dict:
        """Pause current print"""
        response = await self.client.post(
            "/api/job",
            json={"command": "pause", "action": "pause"}
        )
        response.raise_for_status()
        return {"status": "paused"}

    async def resume_print(self) -> Dict:
        """Resume paused print"""
        response = await self.client.post(
            "/api/job",
            json={"command": "pause", "action": "resume"}
        )
        response.raise_for_status()
        return {"status": "resumed"}

    async def cancel_print(self) -> Dict:
        """Cancel current print"""
        response = await self.client.post(
            "/api/job",
            json={"command": "cancel"}
        )
        response.raise_for_status()
        return {"status": "cancelled"}

    async def get_files(self, location: str = "local") -> List[Dict]:
        """Get list of files"""
        response = await self.client.get(f"/api/files/{location}")
        response.raise_for_status()
        return response.json().get("files", [])

    async def delete_file(self, file_path: str, location: str = "local") -> Dict:
        """Delete a file"""
        response = await self.client.delete(f"/api/files/{location}/{file_path}")
        response.raise_for_status()
        return {"status": "deleted", "file": file_path}

    async def get_system_info(self) -> Dict:
        """Get system information"""
        response = await self.client.get("/api/system/commands")
        response.raise_for_status()
        return response.json()

    async def execute_system_command(self, source: str, action: str) -> Dict:
        """Execute system command (restart, shutdown, etc.)"""
        response = await self.client.post(
            f"/api/system/commands/{source}/{action}"
        )
        response.raise_for_status()
        return {"status": "executed", "command": f"{source}/{action}"}

# ============================================================================
# Connection Manager
# ============================================================================

class ConnectionManager:
    """Manages OctoPrint instance connections"""

    def __init__(self):
        self.instances: Dict[str, OctoPrintInstance] = {}
        self.clients: Dict[str, OctoPrintClient] = {}
        self.websockets: Dict[str, List[WebSocket]] = {}

    async def add_instance(self, instance: OctoPrintInstance) -> None:
        """Add new OctoPrint instance"""
        self.instances[instance.id] = instance
        self.clients[instance.id] = OctoPrintClient(instance)
        self.websockets[instance.id] = []

        # Test connection
        try:
            async with self.clients[instance.id] as client:
                version = await client.get_version()
                instance.capabilities = list(version.get("capabilities", {}).keys())
                instance.last_seen = datetime.utcnow()
                logger.info(f"Connected to OctoPrint instance",
                           instance_id=instance.id, version=version)
        except Exception as e:
            logger.error(f"Failed to connect to OctoPrint",
                        instance_id=instance.id, error=str(e))
            raise

    async def remove_instance(self, instance_id: str) -> None:
        """Remove OctoPrint instance"""
        if instance_id in self.instances:
            del self.instances[instance_id]
            if instance_id in self.clients:
                await self.clients[instance_id].client.aclose()
                del self.clients[instance_id]
            if instance_id in self.websockets:
                for ws in self.websockets[instance_id]:
                    await ws.close()
                del self.websockets[instance_id]

    async def get_client(self, instance_id: str) -> OctoPrintClient:
        """Get client for instance"""
        if instance_id not in self.clients:
            raise ValueError(f"Instance {instance_id} not found")
        return self.clients[instance_id]

    async def connect_websocket(self, instance_id: str, websocket: WebSocket) -> None:
        """Connect WebSocket for instance"""
        if instance_id not in self.websockets:
            self.websockets[instance_id] = []
        self.websockets[instance_id].append(websocket)

    async def disconnect_websocket(self, instance_id: str, websocket: WebSocket) -> None:
        """Disconnect WebSocket for instance"""
        if instance_id in self.websockets:
            self.websockets[instance_id].remove(websocket)

    async def broadcast_to_instance(self, instance_id: str, message: Dict) -> None:
        """Broadcast message to all WebSockets for instance"""
        if instance_id in self.websockets:
            disconnected = []
            for websocket in self.websockets[instance_id]:
                try:
                    await websocket.send_json(message)
                except:
                    disconnected.append(websocket)

            # Clean up disconnected websockets
            for ws in disconnected:
                self.websockets[instance_id].remove(ws)

# ============================================================================
# Background Tasks
# ============================================================================

async def monitor_instances(manager: ConnectionManager, redis_client: aioredis.Redis):
    """Monitor all OctoPrint instances for status updates"""
    while True:
        for instance_id, instance in manager.instances.items():
            if not instance.active:
                continue

            try:
                async with manager.clients[instance_id] as client:
                    status = await client.get_printer_status()

                    # Store in Redis
                    await redis_client.setex(
                        f"octoprint:status:{instance_id}",
                        60,  # TTL 60 seconds
                        status.json()
                    )

                    # Broadcast to WebSockets
                    await manager.broadcast_to_instance(instance_id, {
                        "type": "status_update",
                        "data": status.dict()
                    })

                    # Update last seen
                    instance.last_seen = datetime.utcnow()

            except Exception as e:
                logger.error(f"Failed to monitor instance",
                           instance_id=instance_id, error=str(e))

        await asyncio.sleep(5)  # Poll every 5 seconds

# ============================================================================
# MQTT Integration
# ============================================================================

class MQTTManager:
    """Manages MQTT communication for printer events"""

    def __init__(self, broker_url: str):
        self.broker_url = broker_url
        self.client = mqtt.Client()
        self.client.on_connect = self._on_connect
        self.client.on_message = self._on_message

    def _on_connect(self, client, userdata, flags, rc):
        logger.info(f"Connected to MQTT broker", rc=rc)
        # Subscribe to printer control topics
        client.subscribe("pravara/printers/+/command")

    def _on_message(self, client, userdata, msg):
        logger.info(f"MQTT message received", topic=msg.topic, payload=msg.payload)
        # Handle commands from MQTT
        # This would be integrated with the ConnectionManager

    def connect(self):
        """Connect to MQTT broker"""
        parts = self.broker_url.replace("mqtt://", "").split(":")
        host = parts[0]
        port = int(parts[1]) if len(parts) > 1 else 1883
        self.client.connect(host, port, 60)
        self.client.loop_start()

    def publish_status(self, instance_id: str, status: Dict):
        """Publish printer status to MQTT"""
        topic = f"pravara/printers/{instance_id}/status"
        self.client.publish(topic, json.dumps(status))

    def disconnect(self):
        """Disconnect from MQTT broker"""
        self.client.loop_stop()
        self.client.disconnect()

# ============================================================================
# FastAPI Application
# ============================================================================

manager = ConnectionManager()
redis_client: Optional[aioredis.Redis] = None
mqtt_manager: Optional[MQTTManager] = None

@asynccontextmanager
async def lifespan(app: FastAPI):
    """Application lifespan manager"""
    global redis_client, mqtt_manager

    # Startup
    logger.info("Starting OctoPrint Connector Service")

    # Connect to Redis
    redis_url = os.getenv("REDIS_URL", "redis://localhost:6379")
    redis_client = await aioredis.from_url(redis_url)

    # Connect to MQTT
    mqtt_url = os.getenv("MQTT_URL", "mqtt://localhost:1883")
    mqtt_manager = MQTTManager(mqtt_url)
    mqtt_manager.connect()

    # Start background tasks
    monitor_task = asyncio.create_task(monitor_instances(manager, redis_client))

    yield

    # Shutdown
    logger.info("Shutting down OctoPrint Connector Service")
    monitor_task.cancel()

    if redis_client:
        await redis_client.close()

    if mqtt_manager:
        mqtt_manager.disconnect()

    # Clean up all instances
    for instance_id in list(manager.instances.keys()):
        await manager.remove_instance(instance_id)

app = FastAPI(
    title="OctoPrint Connector Service",
    description="Manages connections to OctoPrint instances for 3D printer control",
    version="1.0.0",
    lifespan=lifespan
)

# CORS configuration
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# ============================================================================
# API Endpoints
# ============================================================================

@app.get("/health")
async def health_check():
    """Health check endpoint"""
    return {
        "status": "healthy",
        "service": "octoprint-connector",
        "instances": len(manager.instances),
        "timestamp": datetime.utcnow().isoformat()
    }

@app.post("/instances")
async def add_instance(instance: OctoPrintInstance):
    """Add new OctoPrint instance"""
    try:
        await manager.add_instance(instance)
        return {"status": "connected", "instance_id": instance.id}
    except Exception as e:
        raise HTTPException(status_code=400, detail=str(e))

@app.delete("/instances/{instance_id}")
async def remove_instance(instance_id: str):
    """Remove OctoPrint instance"""
    await manager.remove_instance(instance_id)
    return {"status": "removed", "instance_id": instance_id}

@app.get("/instances")
async def list_instances():
    """List all OctoPrint instances"""
    return {
        "instances": [instance.dict() for instance in manager.instances.values()]
    }

@app.get("/instances/{instance_id}")
async def get_instance(instance_id: str):
    """Get specific OctoPrint instance details"""
    if instance_id not in manager.instances:
        raise HTTPException(status_code=404, detail="Instance not found")
    return manager.instances[instance_id].dict()

@app.get("/instances/{instance_id}/status")
async def get_printer_status(instance_id: str):
    """Get current printer status"""
    try:
        async with await manager.get_client(instance_id) as client:
            status = await client.get_printer_status()
            return status.dict()
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/gcode")
async def send_gcode(instance_id: str, commands: List[str]):
    """Send G-code commands to printer"""
    try:
        async with await manager.get_client(instance_id) as client:
            result = await client.send_gcode(commands)
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/files")
async def upload_file(upload: FileUpload):
    """Upload file to OctoPrint"""
    try:
        import base64
        file_content = base64.b64decode(upload.file_content)

        async with await manager.get_client(upload.instance_id) as client:
            result = await client.upload_file(
                upload.file_name,
                file_content,
                upload.location,
                upload.print_after_upload
            )
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/instances/{instance_id}/files")
async def list_files(instance_id: str, location: str = "local"):
    """List files on OctoPrint"""
    try:
        async with await manager.get_client(instance_id) as client:
            files = await client.get_files(location)
            return {"files": files}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/print/start")
async def start_print(instance_id: str, file_path: str):
    """Start printing a file"""
    try:
        async with await manager.get_client(instance_id) as client:
            result = await client.start_print(file_path)
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/print/pause")
async def pause_print(instance_id: str):
    """Pause current print"""
    try:
        async with await manager.get_client(instance_id) as client:
            result = await client.pause_print()
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/print/resume")
async def resume_print(instance_id: str):
    """Resume paused print"""
    try:
        async with await manager.get_client(instance_id) as client:
            result = await client.resume_print()
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.post("/instances/{instance_id}/print/cancel")
async def cancel_print(instance_id: str):
    """Cancel current print"""
    try:
        async with await manager.get_client(instance_id) as client:
            result = await client.cancel_print()
            return result
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

# ============================================================================
# WebSocket Endpoints
# ============================================================================

@app.websocket("/ws/{instance_id}")
async def websocket_endpoint(websocket: WebSocket, instance_id: str):
    """WebSocket endpoint for real-time updates"""
    await websocket.accept()

    if instance_id not in manager.instances:
        await websocket.send_json({"error": "Instance not found"})
        await websocket.close()
        return

    await manager.connect_websocket(instance_id, websocket)

    try:
        while True:
            # Keep connection alive and handle incoming messages
            data = await websocket.receive_text()
            message = json.loads(data)

            # Handle different message types
            if message.get("type") == "ping":
                await websocket.send_json({"type": "pong"})
            elif message.get("type") == "command":
                # Execute command
                commands = message.get("commands", [])
                async with await manager.get_client(instance_id) as client:
                    result = await client.send_gcode(commands)
                    await websocket.send_json({
                        "type": "command_result",
                        "data": result
                    })

    except WebSocketDisconnect:
        await manager.disconnect_websocket(instance_id, websocket)
    except Exception as e:
        logger.error(f"WebSocket error", error=str(e))
        await manager.disconnect_websocket(instance_id, websocket)

# ============================================================================
# Main Entry Point
# ============================================================================

if __name__ == "__main__":
    uvicorn.run(
        "main:app",
        host="0.0.0.0",
        port=int(os.getenv("PORT", 4508)),
        reload=os.getenv("ENV", "production") == "development",
        log_config={
            "version": 1,
            "disable_existing_loggers": False,
            "formatters": {
                "default": {
                    "format": "%(asctime)s - %(name)s - %(levelname)s - %(message)s",
                },
            },
            "handlers": {
                "default": {
                    "formatter": "default",
                    "class": "logging.StreamHandler",
                    "stream": "ext://sys.stdout",
                },
            },
            "root": {
                "level": os.getenv("LOG_LEVEL", "INFO"),
                "handlers": ["default"],
            },
        }
    )