"""
Shared fixtures for OctoPrint Connector tests.

Provides reusable mock objects for OctoPrintInstance, OctoPrintClient,
ConnectionManager, MQTTManager, and the FastAPI test client.
"""

import json
import pytest
from unittest.mock import Mock, AsyncMock, MagicMock, patch

# Patch external dependencies before importing application code so module-level
# side effects (MQTT client creation, Redis connections) do not reach real services.
with patch("paho.mqtt.client.Client"), patch("redis.asyncio.from_url"):
    from main import (
        app,
        OctoPrintInstance,
        OctoPrintClient,
        ConnectionManager,
        MQTTManager,
        PrinterStatus,
    )

from fastapi.testclient import TestClient


# ---------------------------------------------------------------------------
# OctoPrint instance fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def octoprint_instance() -> OctoPrintInstance:
    """A minimal valid OctoPrintInstance for unit tests."""
    return OctoPrintInstance(
        id="printer-001",
        name="Prusa MK4",
        url="http://octoprint.local:5000",
        api_key="ABCDEF1234567890",
        description="Lab printer #1",
    )


@pytest.fixture
def second_instance() -> OctoPrintInstance:
    """A second instance for multi-printer scenarios."""
    return OctoPrintInstance(
        id="printer-002",
        name="Ender 3 V3",
        url="http://192.168.1.50:5000",
        api_key="ZYXWVU0987654321",
        description="Lab printer #2",
    )


# ---------------------------------------------------------------------------
# OctoPrintClient fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def octoprint_client(octoprint_instance) -> OctoPrintClient:
    """An OctoPrintClient backed by the default instance."""
    return OctoPrintClient(octoprint_instance)


# ---------------------------------------------------------------------------
# Connection manager fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def connection_manager() -> ConnectionManager:
    """A fresh, empty ConnectionManager."""
    return ConnectionManager()


# ---------------------------------------------------------------------------
# MQTT fixtures
# ---------------------------------------------------------------------------


@pytest.fixture
def mqtt_manager() -> MQTTManager:
    """An MQTTManager wired to a mock paho client."""
    with patch("paho.mqtt.client.Client") as mock_cls:
        mock_client = MagicMock()
        mock_cls.return_value = mock_client
        mgr = MQTTManager("mqtt://broker.local:1883")
        # Expose the underlying mock for assertions in tests
        mgr._mock_paho = mock_client
        return mgr


# ---------------------------------------------------------------------------
# FastAPI test client
# ---------------------------------------------------------------------------


@pytest.fixture
def api_client() -> TestClient:
    """A synchronous FastAPI TestClient (no lifespan)."""
    return TestClient(app, raise_server_exceptions=False)


# ---------------------------------------------------------------------------
# httpx response factory
# ---------------------------------------------------------------------------


def make_httpx_response(
    status_code: int = 200,
    json_body: dict | list | None = None,
    text_body: str = "",
    raise_on_error: bool = False,
) -> Mock:
    """Create a mock httpx.Response with configurable status and body.

    Parameters
    ----------
    status_code : int
        HTTP status code.
    json_body : dict | list | None
        If provided, ``response.json()`` returns this value.
    text_body : str
        Raw text body returned by ``response.text``.
    raise_on_error : bool
        When True, ``raise_for_status()`` raises ``httpx.HTTPStatusError``
        for 4xx/5xx codes.
    """
    import httpx as _httpx

    resp = Mock()
    resp.status_code = status_code
    resp.text = text_body

    if json_body is not None:
        resp.json.return_value = json_body
    else:
        resp.json.side_effect = json.JSONDecodeError("No JSON", "", 0)

    if raise_on_error and status_code >= 400:
        request = _httpx.Request("GET", "http://mock")
        error = _httpx.HTTPStatusError(
            f"{status_code}", request=request, response=Mock(status_code=status_code)
        )
        resp.raise_for_status.side_effect = error
    else:
        resp.raise_for_status = Mock()

    return resp
