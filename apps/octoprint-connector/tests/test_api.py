"""
Tests for OctoPrint API client methods and FastAPI endpoint layer.

Covers:
- OctoPrintClient initialisation and authentication header injection
- HTTP methods: get_version, get_printer_status, send_gcode, upload_file,
  start_print, pause_print, resume_print, cancel_print, get_files,
  delete_file, get_system_info, execute_system_command
- HTTP error handling (4xx, 5xx, timeouts)
- FastAPI endpoint routing and response codes
"""

import base64
import json

import httpx
import pytest
from unittest.mock import AsyncMock, Mock, patch

# Re-use the patched imports from conftest
from tests.conftest import make_httpx_response

with patch("paho.mqtt.client.Client"), patch("redis.asyncio.from_url"):
    from main import (
        OctoPrintClient,
        OctoPrintInstance,
        ConnectionManager,
        PrinterStatus,
        manager as app_manager,
    )


# ============================================================================
# OctoPrintClient -- initialisation & auth
# ============================================================================


class TestClientInitialisation:
    """Verify client construction, base URL, and header injection."""

    def test_base_url_derived_from_instance(self, octoprint_client):
        assert str(octoprint_client.client.base_url) == "http://octoprint.local:5000"

    def test_api_key_header_injected(self, octoprint_client):
        assert octoprint_client.client.headers["X-Api-Key"] == "ABCDEF1234567890"

    def test_timeout_set(self, octoprint_client):
        # httpx stores timeout as a Timeout object; default we set is 30s
        assert octoprint_client.client.timeout.connect == 30.0

    def test_instance_reference_stored(self, octoprint_client, octoprint_instance):
        assert octoprint_client.instance is octoprint_instance

    @pytest.mark.asyncio
    async def test_context_manager_closes_client(self, octoprint_instance):
        client = OctoPrintClient(octoprint_instance)
        async with client:
            pass
        assert client.client.is_closed


# ============================================================================
# OctoPrintClient -- get_version
# ============================================================================


class TestGetVersion:

    @pytest.mark.asyncio
    async def test_returns_parsed_json(self, octoprint_client):
        resp = make_httpx_response(json_body={"api": "0.1", "server": "1.9.3", "text": "OctoPrint 1.9.3"})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp):
            result = await octoprint_client.get_version()
        assert result["server"] == "1.9.3"

    @pytest.mark.asyncio
    async def test_calls_correct_endpoint(self, octoprint_client):
        resp = make_httpx_response(json_body={})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp) as mock_get:
            await octoprint_client.get_version()
        mock_get.assert_called_once_with("/api/version")

    @pytest.mark.asyncio
    async def test_raises_on_http_error(self, octoprint_client):
        resp = make_httpx_response(status_code=401, raise_on_error=True)
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.get_version()


# ============================================================================
# OctoPrintClient -- get_printer_status
# ============================================================================


class TestGetPrinterStatus:

    @staticmethod
    def _build_printer_response(state_text="Operational", temp=None):
        temp = temp or {"tool0": {"actual": 210.0, "target": 210.0}}
        return make_httpx_response(
            json_body={"state": {"text": state_text}, "temperature": temp}
        )

    @staticmethod
    def _build_job_response(completion=75.0, time_left=600, time_elapsed=1800):
        return make_httpx_response(
            json_body={
                "job": {"file": {"name": "benchy.gcode"}},
                "progress": {
                    "completion": completion,
                    "printTimeLeft": time_left,
                    "printTime": time_elapsed,
                },
            }
        )

    @pytest.mark.asyncio
    async def test_parses_printer_and_job(self, octoprint_client):
        printer_resp = self._build_printer_response()
        job_resp = self._build_job_response(completion=42.5, time_left=120, time_elapsed=900)

        with patch.object(
            octoprint_client.client, "get", new_callable=AsyncMock, side_effect=[printer_resp, job_resp]
        ):
            status = await octoprint_client.get_printer_status()

        assert isinstance(status, PrinterStatus)
        assert status.state == "Operational"
        assert status.progress == 42.5
        assert status.time_remaining == 120
        assert status.time_elapsed == 900

    @pytest.mark.asyncio
    async def test_handles_missing_job(self, octoprint_client):
        """When the job endpoint returns a non-200, job fields should be None."""
        printer_resp = self._build_printer_response(state_text="Idle")
        job_resp = Mock()
        job_resp.status_code = 409  # Conflict -- printer not ready
        job_resp.json.return_value = {}

        with patch.object(
            octoprint_client.client, "get", new_callable=AsyncMock, side_effect=[printer_resp, job_resp]
        ):
            status = await octoprint_client.get_printer_status()

        assert status.state == "Idle"
        assert status.job is None
        assert status.progress is None

    @pytest.mark.asyncio
    async def test_unknown_state_fallback(self, octoprint_client):
        printer_resp = make_httpx_response(json_body={"temperature": {}})
        job_resp = make_httpx_response(json_body={})

        with patch.object(
            octoprint_client.client, "get", new_callable=AsyncMock, side_effect=[printer_resp, job_resp]
        ):
            status = await octoprint_client.get_printer_status()

        assert status.state == "Unknown"

    @pytest.mark.asyncio
    async def test_raises_on_printer_endpoint_error(self, octoprint_client):
        resp = make_httpx_response(status_code=500, raise_on_error=True)
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.get_printer_status()

    @pytest.mark.parametrize(
        "state_text",
        ["Operational", "Printing", "Paused", "Error", "Offline"],
    )
    @pytest.mark.asyncio
    async def test_all_documented_states(self, octoprint_client, state_text):
        printer_resp = self._build_printer_response(state_text=state_text)
        job_resp = self._build_job_response()

        with patch.object(
            octoprint_client.client, "get", new_callable=AsyncMock, side_effect=[printer_resp, job_resp]
        ):
            status = await octoprint_client.get_printer_status()

        assert status.state == state_text


# ============================================================================
# OctoPrintClient -- send_gcode
# ============================================================================


class TestSendGcode:

    @pytest.mark.asyncio
    async def test_sends_commands_list(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.send_gcode(["G28", "G1 X50 Y50 F3000"])

        assert result == {"status": "sent", "commands": ["G28", "G1 X50 Y50 F3000"]}
        mock_post.assert_called_once_with(
            "/api/printer/command", json={"commands": ["G28", "G1 X50 Y50 F3000"]}
        )

    @pytest.mark.asyncio
    async def test_single_command(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp):
            result = await octoprint_client.send_gcode(["M104 S200"])

        assert result["commands"] == ["M104 S200"]

    @pytest.mark.asyncio
    async def test_raises_on_conflict(self, octoprint_client):
        resp = make_httpx_response(status_code=409, raise_on_error=True)
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.send_gcode(["G28"])


# ============================================================================
# OctoPrintClient -- upload_file
# ============================================================================


class TestUploadFile:

    @pytest.mark.asyncio
    async def test_uploads_to_local(self, octoprint_client):
        resp = make_httpx_response(json_body={"done": True, "files": {"local": {"name": "model.gcode"}}})
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.upload_file("model.gcode", b"G28\nG1 X10")

        assert result["done"] is True
        # Verify endpoint
        call_args = mock_post.call_args
        assert call_args[0][0] == "/api/files/local"

    @pytest.mark.asyncio
    async def test_upload_to_sdcard(self, octoprint_client):
        resp = make_httpx_response(json_body={"done": True})
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            await octoprint_client.upload_file("model.gcode", b"G28", location="sdcard")

        assert mock_post.call_args[0][0] == "/api/files/sdcard"

    @pytest.mark.asyncio
    async def test_print_after_upload_flags(self, octoprint_client):
        resp = make_httpx_response(json_body={"done": True})
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            await octoprint_client.upload_file("model.gcode", b"G28", print_after=True)

        call_kwargs = mock_post.call_args[1]
        assert call_kwargs["data"]["print"] == "true"
        assert call_kwargs["data"]["select"] == "true"

    @pytest.mark.asyncio
    async def test_no_print_flags_by_default(self, octoprint_client):
        resp = make_httpx_response(json_body={"done": True})
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            await octoprint_client.upload_file("model.gcode", b"G28")

        call_kwargs = mock_post.call_args[1]
        assert call_kwargs["data"]["print"] == "false"

    @pytest.mark.asyncio
    async def test_raises_on_payload_too_large(self, octoprint_client):
        resp = make_httpx_response(status_code=413, raise_on_error=True)
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.upload_file("huge.gcode", b"x" * 1024)


# ============================================================================
# OctoPrintClient -- print control (start / pause / resume / cancel)
# ============================================================================


class TestPrintControl:

    @pytest.mark.asyncio
    async def test_start_print(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.start_print("benchy.gcode")

        assert result == {"status": "printing", "file": "benchy.gcode"}
        mock_post.assert_called_once_with(
            "/api/files/local/benchy.gcode",
            json={"command": "select", "print": True},
        )

    @pytest.mark.asyncio
    async def test_pause_print(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.pause_print()

        assert result == {"status": "paused"}
        mock_post.assert_called_once_with(
            "/api/job", json={"command": "pause", "action": "pause"}
        )

    @pytest.mark.asyncio
    async def test_resume_print(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.resume_print()

        assert result == {"status": "resumed"}
        mock_post.assert_called_once_with(
            "/api/job", json={"command": "pause", "action": "resume"}
        )

    @pytest.mark.asyncio
    async def test_cancel_print(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.cancel_print()

        assert result == {"status": "cancelled"}
        mock_post.assert_called_once_with("/api/job", json={"command": "cancel"})

    @pytest.mark.asyncio
    async def test_cancel_raises_when_not_printing(self, octoprint_client):
        resp = make_httpx_response(status_code=409, raise_on_error=True)
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.cancel_print()


# ============================================================================
# OctoPrintClient -- file management
# ============================================================================


class TestFileManagement:

    @pytest.mark.asyncio
    async def test_get_files_default_location(self, octoprint_client):
        resp = make_httpx_response(json_body={"files": [{"name": "a.gcode"}, {"name": "b.gcode"}]})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp) as mock_get:
            files = await octoprint_client.get_files()

        assert len(files) == 2
        mock_get.assert_called_once_with("/api/files/local")

    @pytest.mark.asyncio
    async def test_get_files_sdcard(self, octoprint_client):
        resp = make_httpx_response(json_body={"files": []})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp) as mock_get:
            await octoprint_client.get_files(location="sdcard")

        mock_get.assert_called_once_with("/api/files/sdcard")

    @pytest.mark.asyncio
    async def test_get_files_empty_list(self, octoprint_client):
        resp = make_httpx_response(json_body={"files": []})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp):
            files = await octoprint_client.get_files()

        assert files == []

    @pytest.mark.asyncio
    async def test_delete_file(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "delete", new_callable=AsyncMock, return_value=resp) as mock_del:
            result = await octoprint_client.delete_file("old.gcode")

        assert result == {"status": "deleted", "file": "old.gcode"}
        mock_del.assert_called_once_with("/api/files/local/old.gcode")

    @pytest.mark.asyncio
    async def test_delete_file_not_found(self, octoprint_client):
        resp = make_httpx_response(status_code=404, raise_on_error=True)
        with patch.object(octoprint_client.client, "delete", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.delete_file("missing.gcode")


# ============================================================================
# OctoPrintClient -- system commands
# ============================================================================


class TestSystemCommands:

    @pytest.mark.asyncio
    async def test_get_system_info(self, octoprint_client):
        resp = make_httpx_response(json_body={"core": [{"action": "restart"}]})
        with patch.object(octoprint_client.client, "get", new_callable=AsyncMock, return_value=resp) as mock_get:
            info = await octoprint_client.get_system_info()

        assert "core" in info
        mock_get.assert_called_once_with("/api/system/commands")

    @pytest.mark.asyncio
    async def test_execute_system_command(self, octoprint_client):
        resp = make_httpx_response()
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp) as mock_post:
            result = await octoprint_client.execute_system_command("core", "restart")

        assert result == {"status": "executed", "command": "core/restart"}
        mock_post.assert_called_once_with("/api/system/commands/core/restart")

    @pytest.mark.asyncio
    async def test_execute_system_command_forbidden(self, octoprint_client):
        resp = make_httpx_response(status_code=403, raise_on_error=True)
        with patch.object(octoprint_client.client, "post", new_callable=AsyncMock, return_value=resp):
            with pytest.raises(httpx.HTTPStatusError):
                await octoprint_client.execute_system_command("core", "shutdown")


# ============================================================================
# OctoPrintClient -- timeout and connectivity edge cases
# ============================================================================


class TestConnectivityEdgeCases:

    @pytest.mark.asyncio
    async def test_timeout_error_propagated(self, octoprint_client):
        with patch.object(
            octoprint_client.client,
            "get",
            new_callable=AsyncMock,
            side_effect=httpx.ReadTimeout("timed out"),
        ):
            with pytest.raises(httpx.ReadTimeout):
                await octoprint_client.get_version()

    @pytest.mark.asyncio
    async def test_connect_error_propagated(self, octoprint_client):
        with patch.object(
            octoprint_client.client,
            "get",
            new_callable=AsyncMock,
            side_effect=httpx.ConnectError("refused"),
        ):
            with pytest.raises(httpx.ConnectError):
                await octoprint_client.get_version()


# ============================================================================
# FastAPI endpoints
# ============================================================================


class TestHealthEndpoint:

    def test_returns_200_with_service_info(self, api_client):
        response = api_client.get("/health")
        assert response.status_code == 200
        body = response.json()
        assert body["status"] == "healthy"
        assert body["service"] == "octoprint-connector"
        assert "timestamp" in body
        assert "instances" in body


class TestInstanceEndpoints:

    @patch("main.manager")
    def test_list_instances_empty(self, mock_mgr, api_client):
        mock_mgr.instances = {}
        response = api_client.get("/instances")
        assert response.status_code == 200
        assert response.json()["instances"] == []

    @patch("main.manager")
    def test_list_instances_populated(self, mock_mgr, api_client, octoprint_instance):
        mock_mgr.instances = {octoprint_instance.id: octoprint_instance}
        response = api_client.get("/instances")
        data = response.json()
        assert len(data["instances"]) == 1
        assert data["instances"][0]["id"] == "printer-001"

    @patch("main.manager")
    def test_get_instance_found(self, mock_mgr, api_client, octoprint_instance):
        mock_mgr.instances = {octoprint_instance.id: octoprint_instance}
        response = api_client.get(f"/instances/{octoprint_instance.id}")
        assert response.status_code == 200
        assert response.json()["name"] == "Prusa MK4"

    @patch("main.manager")
    def test_get_instance_not_found(self, mock_mgr, api_client):
        mock_mgr.instances = {}
        response = api_client.get("/instances/nonexistent")
        assert response.status_code == 404
        assert "Instance not found" in response.json()["detail"]

    @patch("main.manager")
    def test_remove_instance(self, mock_mgr, api_client):
        mock_mgr.remove_instance = AsyncMock()
        response = api_client.delete("/instances/printer-001")
        assert response.status_code == 200
        assert response.json()["status"] == "removed"


# ============================================================================
# ConnectionManager unit tests
# ============================================================================


class TestConnectionManager:

    @pytest.mark.asyncio
    async def test_get_client_raises_for_unknown_id(self, connection_manager):
        with pytest.raises(ValueError, match="not found"):
            await connection_manager.get_client("ghost-printer")

    @pytest.mark.asyncio
    async def test_get_client_returns_registered_client(self, connection_manager, octoprint_instance):
        client = OctoPrintClient(octoprint_instance)
        connection_manager.clients[octoprint_instance.id] = client
        returned = await connection_manager.get_client(octoprint_instance.id)
        assert returned is client

    @pytest.mark.asyncio
    async def test_remove_cleans_all_state(self, connection_manager, octoprint_instance):
        # Manually populate state
        connection_manager.instances[octoprint_instance.id] = octoprint_instance
        mock_client = Mock()
        mock_client.client = AsyncMock()
        mock_client.client.aclose = AsyncMock()
        connection_manager.clients[octoprint_instance.id] = mock_client
        connection_manager.websockets[octoprint_instance.id] = []

        await connection_manager.remove_instance(octoprint_instance.id)

        assert octoprint_instance.id not in connection_manager.instances
        assert octoprint_instance.id not in connection_manager.clients
        assert octoprint_instance.id not in connection_manager.websockets

    @pytest.mark.asyncio
    async def test_remove_nonexistent_is_noop(self, connection_manager):
        # Should not raise
        await connection_manager.remove_instance("does-not-exist")

    @pytest.mark.asyncio
    async def test_broadcast_to_instance_handles_disconnect(self, connection_manager):
        ws_ok = AsyncMock()
        ws_broken = AsyncMock()
        ws_broken.send_json.side_effect = Exception("closed")

        instance_id = "test-printer"
        connection_manager.websockets[instance_id] = [ws_ok, ws_broken]

        await connection_manager.broadcast_to_instance(instance_id, {"type": "test"})

        ws_ok.send_json.assert_called_once_with({"type": "test"})
        # Broken websocket should be removed
        assert ws_broken not in connection_manager.websockets[instance_id]
