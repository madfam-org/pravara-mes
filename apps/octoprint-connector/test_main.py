"""
Tests for OctoPrint Connector Service
"""

import pytest
import asyncio
import json
from unittest.mock import Mock, AsyncMock, patch
from fastapi.testclient import TestClient
import httpx

# Mock the modules before importing main
with patch('paho.mqtt.client.Client'), \
     patch('redis.asyncio.from_url'):
    from main import (
        app,
        OctoPrintInstance,
        OctoPrintClient,
        ConnectionManager,
        PrinterStatus,
        MQTTManager
    )


@pytest.fixture
def test_client():
    """Create test client"""
    return TestClient(app)


@pytest.fixture
def mock_instance():
    """Create mock OctoPrint instance"""
    return OctoPrintInstance(
        id="test-instance",
        name="Test Printer",
        url="http://localhost:5000",
        api_key="test-api-key",
        description="Test printer instance"
    )


@pytest.fixture
def mock_manager():
    """Create mock connection manager"""
    manager = ConnectionManager()
    return manager


class TestOctoPrintClient:
    """Test OctoPrint client"""

    @pytest.mark.asyncio
    async def test_client_initialization(self, mock_instance):
        """Test client initialization"""
        client = OctoPrintClient(mock_instance)
        assert client.instance == mock_instance
        assert client.client.base_url == "http://localhost:5000"
        assert client.client.headers["X-Api-Key"] == "test-api-key"

    @pytest.mark.asyncio
    async def test_get_version(self, mock_instance):
        """Test getting version"""
        client = OctoPrintClient(mock_instance)

        with patch.object(client.client, 'get', new_callable=AsyncMock) as mock_get:
            mock_response = Mock()
            mock_response.json.return_value = {"version": "1.8.0"}
            mock_response.raise_for_status = Mock()
            mock_get.return_value = mock_response

            version = await client.get_version()
            assert version["version"] == "1.8.0"
            mock_get.assert_called_once_with("/api/version")

    @pytest.mark.asyncio
    async def test_get_printer_status(self, mock_instance):
        """Test getting printer status"""
        client = OctoPrintClient(mock_instance)

        with patch.object(client.client, 'get', new_callable=AsyncMock) as mock_get:
            # Mock printer response
            printer_response = Mock()
            printer_response.json.return_value = {
                "state": {"text": "Operational"},
                "temperature": {"bed": {"actual": 60, "target": 60}}
            }
            printer_response.raise_for_status = Mock()

            # Mock job response
            job_response = Mock()
            job_response.status_code = 200
            job_response.json.return_value = {
                "job": {"file": {"name": "test.gcode"}},
                "progress": {
                    "completion": 50.0,
                    "printTimeLeft": 3600,
                    "printTime": 3600
                }
            }

            mock_get.side_effect = [printer_response, job_response]

            status = await client.get_printer_status()
            assert status.state == "Operational"
            assert status.progress == 50.0

    @pytest.mark.asyncio
    async def test_send_gcode(self, mock_instance):
        """Test sending G-code"""
        client = OctoPrintClient(mock_instance)

        with patch.object(client.client, 'post', new_callable=AsyncMock) as mock_post:
            mock_response = Mock()
            mock_response.raise_for_status = Mock()
            mock_post.return_value = mock_response

            commands = ["G28", "G1 X100 Y100"]
            result = await client.send_gcode(commands)

            assert result["status"] == "sent"
            assert result["commands"] == commands
            mock_post.assert_called_once_with(
                "/api/printer/command",
                json={"commands": commands}
            )

    @pytest.mark.asyncio
    async def test_start_print(self, mock_instance):
        """Test starting print"""
        client = OctoPrintClient(mock_instance)

        with patch.object(client.client, 'post', new_callable=AsyncMock) as mock_post:
            mock_response = Mock()
            mock_response.raise_for_status = Mock()
            mock_post.return_value = mock_response

            result = await client.start_print("test.gcode")

            assert result["status"] == "printing"
            assert result["file"] == "test.gcode"
            mock_post.assert_called_once_with(
                "/api/files/local/test.gcode",
                json={"command": "select", "print": True}
            )


class TestConnectionManager:
    """Test connection manager"""

    @pytest.mark.asyncio
    async def test_add_instance(self, mock_manager, mock_instance):
        """Test adding instance"""
        with patch.object(OctoPrintClient, 'get_version', new_callable=AsyncMock) as mock_version:
            mock_version.return_value = {
                "capabilities": {"plugin": True, "api": True}
            }

            await mock_manager.add_instance(mock_instance)

            assert mock_instance.id in mock_manager.instances
            assert mock_instance.id in mock_manager.clients
            assert mock_instance.id in mock_manager.websockets
            assert mock_instance.capabilities == ["plugin", "api"]

    @pytest.mark.asyncio
    async def test_remove_instance(self, mock_manager, mock_instance):
        """Test removing instance"""
        mock_manager.instances[mock_instance.id] = mock_instance
        mock_manager.clients[mock_instance.id] = Mock()
        mock_manager.clients[mock_instance.id].client = AsyncMock()
        mock_manager.clients[mock_instance.id].client.aclose = AsyncMock()
        mock_manager.websockets[mock_instance.id] = []

        await mock_manager.remove_instance(mock_instance.id)

        assert mock_instance.id not in mock_manager.instances
        assert mock_instance.id not in mock_manager.clients
        assert mock_instance.id not in mock_manager.websockets

    @pytest.mark.asyncio
    async def test_get_client(self, mock_manager, mock_instance):
        """Test getting client"""
        mock_client = Mock()
        mock_manager.clients[mock_instance.id] = mock_client

        client = await mock_manager.get_client(mock_instance.id)
        assert client == mock_client

        with pytest.raises(ValueError):
            await mock_manager.get_client("non-existent")


class TestMQTTManager:
    """Test MQTT manager"""

    def test_mqtt_initialization(self):
        """Test MQTT manager initialization"""
        with patch('paho.mqtt.client.Client') as mock_client:
            manager = MQTTManager("mqtt://localhost:1883")
            assert manager.broker_url == "mqtt://localhost:1883"
            assert manager.client is not None

    def test_mqtt_connect(self):
        """Test MQTT connection"""
        with patch('paho.mqtt.client.Client') as mock_client_class:
            mock_client = Mock()
            mock_client_class.return_value = mock_client

            manager = MQTTManager("mqtt://localhost:1883")
            manager.connect()

            mock_client.connect.assert_called_once_with("localhost", 1883, 60)
            mock_client.loop_start.assert_called_once()

    def test_publish_status(self):
        """Test publishing status"""
        with patch('paho.mqtt.client.Client') as mock_client_class:
            mock_client = Mock()
            mock_client_class.return_value = mock_client

            manager = MQTTManager("mqtt://localhost:1883")
            status = {"state": "Operational", "temperature": 60}
            manager.publish_status("test-instance", status)

            mock_client.publish.assert_called_once_with(
                "pravara/printers/test-instance/status",
                json.dumps(status)
            )


class TestAPIEndpoints:
    """Test API endpoints"""

    def test_health_check(self, test_client):
        """Test health check endpoint"""
        response = test_client.get("/health")
        assert response.status_code == 200
        data = response.json()
        assert data["status"] == "healthy"
        assert data["service"] == "octoprint-connector"

    @patch('main.manager')
    def test_list_instances(self, mock_manager, test_client, mock_instance):
        """Test listing instances"""
        mock_manager.instances = {"test": mock_instance}

        response = test_client.get("/instances")
        assert response.status_code == 200
        data = response.json()
        assert len(data["instances"]) == 1

    @patch('main.manager')
    def test_get_instance(self, mock_manager, test_client, mock_instance):
        """Test getting specific instance"""
        mock_manager.instances = {"test-instance": mock_instance}

        response = test_client.get("/instances/test-instance")
        assert response.status_code == 200
        data = response.json()
        assert data["id"] == "test-instance"
        assert data["name"] == "Test Printer"

    @patch('main.manager')
    def test_get_instance_not_found(self, mock_manager, test_client):
        """Test getting non-existent instance"""
        mock_manager.instances = {}

        response = test_client.get("/instances/non-existent")
        assert response.status_code == 404
        assert "Instance not found" in response.json()["detail"]


if __name__ == "__main__":
    pytest.main([__file__, "-v"])