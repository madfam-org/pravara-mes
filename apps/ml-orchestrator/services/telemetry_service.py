"""
Telemetry Service
Manages real-time telemetry data ingestion and processing
"""

import asyncio
import json
import logging
from datetime import datetime, timedelta
from typing import Dict, List, Any, Optional
import pandas as pd
import numpy as np
import redis.asyncio as redis

logger = logging.getLogger(__name__)

class TelemetryService:
    """
    Service for telemetry management:
    - Real-time data ingestion from Redis pub/sub
    - Historical data retrieval
    - Data aggregation and windowing
    - Telemetry stream processing
    """

    def __init__(self, redis_client: redis.Redis):
        self.redis = redis_client
        self.subscribers = {}
        self.telemetry_buffer = {}
        self.aggregation_windows = {
            "1min": timedelta(minutes=1),
            "5min": timedelta(minutes=5),
            "15min": timedelta(minutes=15),
            "1hour": timedelta(hours=1),
            "1day": timedelta(days=1)
        }
        self.running = False

    async def subscribe_to_updates(self):
        """Subscribe to telemetry updates from Redis"""
        self.running = True
        pubsub = self.redis.pubsub()

        try:
            # Subscribe to telemetry channels
            await pubsub.subscribe(
                "telemetry:*",
                "machine:status:*",
                "production:metrics:*"
            )

            logger.info("Subscribed to telemetry channels")

            # Process messages
            async for message in pubsub.listen():
                if not self.running:
                    break

                if message["type"] == "message":
                    await self.process_telemetry_message(message)

        except Exception as e:
            logger.error(f"Telemetry subscription error: {e}")
        finally:
            await pubsub.unsubscribe()
            await pubsub.close()

    async def process_telemetry_message(self, message: Dict):
        """Process incoming telemetry message"""
        try:
            channel = message["channel"]
            data = json.loads(message["data"])

            # Extract machine ID from channel
            if ":" in channel:
                parts = channel.split(":")
                if len(parts) >= 2:
                    machine_id = parts[-1]
                else:
                    machine_id = "unknown"
            else:
                machine_id = "unknown"

            # Add timestamp if not present
            if "timestamp" not in data:
                data["timestamp"] = datetime.utcnow().isoformat()

            # Buffer telemetry data
            if machine_id not in self.telemetry_buffer:
                self.telemetry_buffer[machine_id] = []

            self.telemetry_buffer[machine_id].append(data)

            # Limit buffer size (keep last 1000 entries per machine)
            if len(self.telemetry_buffer[machine_id]) > 1000:
                self.telemetry_buffer[machine_id] = self.telemetry_buffer[machine_id][-1000:]

            # Store in Redis for persistence
            await self.store_telemetry(machine_id, data)

            # Trigger real-time processing if needed
            await self.trigger_realtime_processing(machine_id, data)

        except Exception as e:
            logger.error(f"Error processing telemetry message: {e}")

    async def store_telemetry(self, machine_id: str, data: Dict):
        """Store telemetry data in Redis"""
        # Store in time-series structure
        key = f"telemetry:{machine_id}:{datetime.utcnow().strftime('%Y%m%d')}"

        # Add to sorted set with timestamp as score
        timestamp = datetime.fromisoformat(data["timestamp"]).timestamp()
        await self.redis.zadd(key, {json.dumps(data): timestamp})

        # Set expiration (7 days)
        await self.redis.expire(key, 604800)

        # Update latest telemetry
        await self.redis.set(
            f"telemetry:latest:{machine_id}",
            json.dumps(data),
            ex=3600  # 1 hour expiration
        )

    async def trigger_realtime_processing(self, machine_id: str, data: Dict):
        """Trigger real-time processing for critical telemetry"""
        # Check for critical conditions
        critical = False

        if "vibration" in data and data["vibration"] > 20:
            critical = True
        elif "temperature" in data and data["temperature"] > 95:
            critical = True
        elif "pressure" in data and (data["pressure"] < 5 or data["pressure"] > 120):
            critical = True

        if critical:
            # Publish alert
            await self.redis.publish(
                f"alert:critical:{machine_id}",
                json.dumps({
                    "machine_id": machine_id,
                    "timestamp": data["timestamp"],
                    "telemetry": data,
                    "alert_type": "critical_telemetry"
                })
            )

    async def get_latest_telemetry(self, machine_id: str) -> Dict[str, Any]:
        """Get latest telemetry for a machine"""
        # Try Redis first
        cached = await self.redis.get(f"telemetry:latest:{machine_id}")
        if cached:
            return json.loads(cached)

        # Check buffer
        if machine_id in self.telemetry_buffer and self.telemetry_buffer[machine_id]:
            return self.telemetry_buffer[machine_id][-1]

        return {}

    async def get_historical_data(
        self,
        machine_id: str,
        hours: int = 24,
        resolution: str = "raw"
    ) -> pd.DataFrame:
        """Get historical telemetry data"""
        end_time = datetime.utcnow()
        start_time = end_time - timedelta(hours=hours)

        all_data = []

        # Iterate through daily keys
        current = start_time
        while current <= end_time:
            key = f"telemetry:{machine_id}:{current.strftime('%Y%m%d')}"

            # Get data from Redis sorted set
            start_score = start_time.timestamp()
            end_score = end_time.timestamp()

            data = await self.redis.zrangebyscore(
                key,
                start_score,
                end_score,
                withscores=False
            )

            for item in data:
                try:
                    all_data.append(json.loads(item))
                except:
                    continue

            current += timedelta(days=1)

        # Convert to DataFrame
        if all_data:
            df = pd.DataFrame(all_data)

            # Parse timestamp
            if "timestamp" in df.columns:
                df["timestamp"] = pd.to_datetime(df["timestamp"])
                df = df.sort_values("timestamp")

            # Apply resolution if needed
            if resolution != "raw":
                df = self.aggregate_telemetry(df, resolution)

            return df
        else:
            return pd.DataFrame()

    def aggregate_telemetry(
        self,
        df: pd.DataFrame,
        resolution: str
    ) -> pd.DataFrame:
        """Aggregate telemetry data to specified resolution"""
        if resolution not in self.aggregation_windows:
            return df

        window = self.aggregation_windows[resolution]

        # Set timestamp as index
        if "timestamp" in df.columns:
            df = df.set_index("timestamp")

        # Define aggregation rules
        numeric_columns = df.select_dtypes(include=[np.number]).columns
        agg_rules = {}

        for col in numeric_columns:
            if "count" in col.lower() or "total" in col.lower():
                agg_rules[col] = "sum"
            elif "max" in col.lower():
                agg_rules[col] = "max"
            elif "min" in col.lower():
                agg_rules[col] = "min"
            else:
                agg_rules[col] = "mean"

        # Resample and aggregate
        df_agg = df.resample(window).agg(agg_rules)

        # Reset index
        df_agg = df_agg.reset_index()

        return df_agg

    async def get_telemetry_statistics(
        self,
        machine_id: str,
        metrics: List[str],
        hours: int = 24
    ) -> Dict[str, Dict[str, float]]:
        """Calculate statistics for telemetry metrics"""
        df = await self.get_historical_data(machine_id, hours)

        if df.empty:
            return {}

        stats = {}

        for metric in metrics:
            if metric in df.columns:
                stats[metric] = {
                    "mean": float(df[metric].mean()),
                    "std": float(df[metric].std()),
                    "min": float(df[metric].min()),
                    "max": float(df[metric].max()),
                    "median": float(df[metric].median()),
                    "q25": float(df[metric].quantile(0.25)),
                    "q75": float(df[metric].quantile(0.75)),
                    "count": int(df[metric].count())
                }

        return stats

    async def detect_telemetry_patterns(
        self,
        machine_id: str,
        metric: str,
        hours: int = 24
    ) -> Dict[str, Any]:
        """Detect patterns in telemetry data"""
        df = await self.get_historical_data(machine_id, hours)

        if df.empty or metric not in df.columns:
            return {"patterns": [], "insights": []}

        patterns = {
            "trends": [],
            "cycles": [],
            "anomalies": [],
            "insights": []
        }

        values = df[metric].values

        # Trend detection
        if len(values) > 10:
            x = np.arange(len(values))
            slope, intercept = np.polyfit(x, values, 1)

            if abs(slope) > 0.1 * np.std(values):
                if slope > 0:
                    patterns["trends"].append("increasing")
                    patterns["insights"].append(f"{metric} showing upward trend")
                else:
                    patterns["trends"].append("decreasing")
                    patterns["insights"].append(f"{metric} showing downward trend")

        # Cycle detection (simplified)
        if len(values) > 20:
            # Use FFT for frequency analysis
            fft = np.fft.fft(values - np.mean(values))
            frequencies = np.fft.fftfreq(len(values))

            # Find dominant frequency
            dominant_idx = np.argmax(np.abs(fft[1:len(fft)//2])) + 1
            dominant_freq = frequencies[dominant_idx]

            if abs(dominant_freq) > 0.05:
                period = 1 / abs(dominant_freq)
                patterns["cycles"].append({
                    "frequency": float(dominant_freq),
                    "period": float(period)
                })
                patterns["insights"].append(f"{metric} shows cyclic behavior with period {period:.1f}")

        # Anomaly detection (simple threshold)
        mean = np.mean(values)
        std = np.std(values)
        anomalies = np.where(np.abs(values - mean) > 3 * std)[0]

        if len(anomalies) > 0:
            patterns["anomalies"] = anomalies.tolist()
            patterns["insights"].append(f"{metric} has {len(anomalies)} anomalous readings")

        return patterns

    async def calculate_correlations(
        self,
        machine_id: str,
        metrics: List[str],
        hours: int = 24
    ) -> pd.DataFrame:
        """Calculate correlations between metrics"""
        df = await self.get_historical_data(machine_id, hours)

        if df.empty:
            return pd.DataFrame()

        # Select only specified metrics
        available_metrics = [m for m in metrics if m in df.columns]

        if len(available_metrics) < 2:
            return pd.DataFrame()

        # Calculate correlation matrix
        corr_matrix = df[available_metrics].corr()

        return corr_matrix

    async def get_machine_state_history(
        self,
        machine_id: str,
        hours: int = 24
    ) -> List[Dict[str, Any]]:
        """Get machine state change history"""
        states = []

        # Get state changes from Redis
        key = f"machine:state:history:{machine_id}"
        history = await self.redis.lrange(key, 0, -1)

        cutoff = datetime.utcnow() - timedelta(hours=hours)

        for item in history:
            try:
                state = json.loads(item)
                if "timestamp" in state:
                    timestamp = datetime.fromisoformat(state["timestamp"])
                    if timestamp >= cutoff:
                        states.append(state)
            except:
                continue

        return states

    async def stream_telemetry(
        self,
        machine_id: str,
        callback: callable,
        metrics: Optional[List[str]] = None
    ):
        """Stream telemetry data to a callback function"""
        pubsub = self.redis.pubsub()

        try:
            # Subscribe to machine-specific channel
            await pubsub.subscribe(f"telemetry:{machine_id}")

            async for message in pubsub.listen():
                if message["type"] == "message":
                    try:
                        data = json.loads(message["data"])

                        # Filter metrics if specified
                        if metrics:
                            filtered_data = {
                                k: v for k, v in data.items()
                                if k in metrics or k == "timestamp"
                            }
                        else:
                            filtered_data = data

                        # Call callback
                        await callback(filtered_data)

                    except Exception as e:
                        logger.error(f"Stream processing error: {e}")

        finally:
            await pubsub.unsubscribe()
            await pubsub.close()

    async def cleanup_old_telemetry(self, days: int = 30):
        """Clean up old telemetry data"""
        cutoff = datetime.utcnow() - timedelta(days=days)

        # Get all telemetry keys
        pattern = "telemetry:*"
        cursor = 0
        deleted = 0

        while True:
            cursor, keys = await self.redis.scan(
                cursor,
                match=pattern,
                count=100
            )

            for key in keys:
                # Check if key contains date
                parts = key.split(":")
                if len(parts) >= 3:
                    try:
                        date_str = parts[-1]
                        key_date = datetime.strptime(date_str, "%Y%m%d")
                        if key_date < cutoff:
                            await self.redis.delete(key)
                            deleted += 1
                    except:
                        continue

            if cursor == 0:
                break

        logger.info(f"Cleaned up {deleted} old telemetry keys")

    async def stop(self):
        """Stop telemetry service"""
        self.running = False
        logger.info("Telemetry service stopped")