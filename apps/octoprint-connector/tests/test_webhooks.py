"""
Tests for webhook / event handling and MQTT publishing in the OctoPrint connector.

Covers:
- MQTTManager initialisation, connect, and disconnect lifecycle
- MQTT broker URL parsing (host, port, defaults)
- Event publishing: printer status, print events (started, completed, failed, paused)
- Payload serialisation and topic routing
- on_connect callback and subscription behaviour
- on_message callback invocation
- Error handling for malformed payloads and broker disconnections
"""

import json

import pytest
from unittest.mock import Mock, MagicMock, patch, call

with patch("paho.mqtt.client.Client"), patch("redis.asyncio.from_url"):
    from main import MQTTManager


# ============================================================================
# MQTTManager -- initialisation
# ============================================================================


class TestMQTTInitialisation:

    def test_stores_broker_url(self, mqtt_manager):
        assert mqtt_manager.broker_url == "mqtt://broker.local:1883"

    def test_client_object_created(self, mqtt_manager):
        assert mqtt_manager.client is not None

    def test_on_connect_callback_assigned(self, mqtt_manager):
        assert mqtt_manager.client.on_connect is not None

    def test_on_message_callback_assigned(self, mqtt_manager):
        assert mqtt_manager.client.on_message is not None


# ============================================================================
# MQTTManager -- broker URL parsing and connect
# ============================================================================


class TestMQTTConnect:

    def test_connect_parses_host_and_port(self, mqtt_manager):
        mqtt_manager.connect()
        mqtt_manager._mock_paho.connect.assert_called_once_with("broker.local", 1883, 60)
        mqtt_manager._mock_paho.loop_start.assert_called_once()

    def test_connect_default_port(self):
        """When no port is specified, default MQTT port 1883 should be used."""
        with patch("paho.mqtt.client.Client") as mock_cls:
            mock_client = MagicMock()
            mock_cls.return_value = mock_client
            mgr = MQTTManager("mqtt://broker.example.com")
            mgr.connect()
            mock_client.connect.assert_called_once_with("broker.example.com", 1883, 60)

    @pytest.mark.parametrize(
        "url, expected_host, expected_port",
        [
            ("mqtt://10.0.0.1:1884", "10.0.0.1", 1884),
            ("mqtt://localhost:9883", "localhost", 9883),
            ("mqtt://broker:1883", "broker", 1883),
        ],
    )
    def test_connect_various_urls(self, url, expected_host, expected_port):
        with patch("paho.mqtt.client.Client") as mock_cls:
            mock_client = MagicMock()
            mock_cls.return_value = mock_client
            mgr = MQTTManager(url)
            mgr.connect()
            mock_client.connect.assert_called_once_with(expected_host, expected_port, 60)


# ============================================================================
# MQTTManager -- disconnect
# ============================================================================


class TestMQTTDisconnect:

    def test_disconnect_stops_loop_and_disconnects(self, mqtt_manager):
        mqtt_manager.disconnect()
        mqtt_manager._mock_paho.loop_stop.assert_called_once()
        mqtt_manager._mock_paho.disconnect.assert_called_once()


# ============================================================================
# MQTTManager -- on_connect callback
# ============================================================================


class TestOnConnect:

    def test_subscribes_to_printer_command_topic(self, mqtt_manager):
        """When connected, the manager must subscribe to pravara/printers/+/command."""
        mock_client = MagicMock()
        # Invoke the callback the same way paho would
        mqtt_manager._on_connect(mock_client, None, {}, 0)
        mock_client.subscribe.assert_called_once_with("pravara/printers/+/command")

    def test_subscribes_on_reconnect(self, mqtt_manager):
        """Subscriptions should be re-established on reconnect (rc may differ)."""
        mock_client = MagicMock()
        mqtt_manager._on_connect(mock_client, None, {}, 0)
        mqtt_manager._on_connect(mock_client, None, {}, 0)
        assert mock_client.subscribe.call_count == 2


# ============================================================================
# MQTTManager -- on_message callback
# ============================================================================


class TestOnMessage:

    def test_on_message_does_not_raise(self, mqtt_manager):
        """The default on_message handler should not raise for any payload."""
        mock_msg = Mock()
        mock_msg.topic = "pravara/printers/printer-001/command"
        mock_msg.payload = json.dumps({"command": "pause"}).encode()
        # Should not raise
        mqtt_manager._on_message(None, None, mock_msg)

    def test_on_message_handles_binary_payload(self, mqtt_manager):
        """Non-JSON payloads should not crash the handler."""
        mock_msg = Mock()
        mock_msg.topic = "pravara/printers/printer-001/command"
        mock_msg.payload = b"\x00\x01\x02"
        mqtt_manager._on_message(None, None, mock_msg)


# ============================================================================
# MQTTManager -- publish_status
# ============================================================================


class TestPublishStatus:

    def test_publishes_to_correct_topic(self, mqtt_manager):
        status = {"state": "Printing", "progress": 55.0}
        mqtt_manager.publish_status("printer-001", status)
        mqtt_manager._mock_paho.publish.assert_called_once_with(
            "pravara/printers/printer-001/status",
            json.dumps(status),
        )

    def test_topic_includes_instance_id(self, mqtt_manager):
        mqtt_manager.publish_status("my-ender3", {"state": "Operational"})
        topic = mqtt_manager._mock_paho.publish.call_args[0][0]
        assert "my-ender3" in topic

    def test_payload_is_valid_json(self, mqtt_manager):
        data = {"state": "Paused", "temperature": {"bed": 60, "tool0": 210}}
        mqtt_manager.publish_status("printer-001", data)
        raw_payload = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw_payload)
        assert parsed == data

    @pytest.mark.parametrize(
        "instance_id",
        ["printer-001", "printer-002", "lab-west-fdm-7"],
    )
    def test_publish_multiple_instances(self, mqtt_manager, instance_id):
        mqtt_manager.publish_status(instance_id, {"state": "Idle"})
        topic = mqtt_manager._mock_paho.publish.call_args[0][0]
        assert topic == f"pravara/printers/{instance_id}/status"


# ============================================================================
# Webhook event parsing -- simulated OctoPrint events
# ============================================================================


class TestWebhookEventParsing:
    """
    OctoPrint fires webhook-style events (PrintStarted, PrintDone, PrintFailed,
    PrintPaused, etc.). These tests validate that the MQTT manager can route
    properly constructed event payloads to the correct topics.

    Note: The current codebase uses publish_status for event routing.
    These tests verify that arbitrary event payloads survive serialisation
    and are published with correct topic structure.
    """

    @pytest.fixture
    def event_payloads(self):
        """Standard OctoPrint event payloads for common print lifecycle events."""
        return {
            "PrintStarted": {
                "event": "PrintStarted",
                "name": "benchy.gcode",
                "path": "benchy.gcode",
                "origin": "local",
            },
            "PrintDone": {
                "event": "PrintDone",
                "name": "benchy.gcode",
                "path": "benchy.gcode",
                "origin": "local",
                "time": 7200,
            },
            "PrintFailed": {
                "event": "PrintFailed",
                "name": "benchy.gcode",
                "path": "benchy.gcode",
                "origin": "local",
                "reason": "thermal_runaway",
            },
            "PrintPaused": {
                "event": "PrintPaused",
                "name": "benchy.gcode",
                "path": "benchy.gcode",
                "origin": "local",
            },
        }

    @pytest.mark.parametrize(
        "event_type",
        ["PrintStarted", "PrintDone", "PrintFailed", "PrintPaused"],
    )
    def test_event_payload_serialises_correctly(
        self, mqtt_manager, event_payloads, event_type
    ):
        payload = event_payloads[event_type]
        mqtt_manager.publish_status("printer-001", payload)

        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["event"] == event_type

    def test_print_started_contains_file_info(self, mqtt_manager, event_payloads):
        payload = event_payloads["PrintStarted"]
        mqtt_manager.publish_status("printer-001", payload)

        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["name"] == "benchy.gcode"
        assert parsed["origin"] == "local"

    def test_print_done_contains_elapsed_time(self, mqtt_manager, event_payloads):
        payload = event_payloads["PrintDone"]
        mqtt_manager.publish_status("printer-001", payload)

        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["time"] == 7200

    def test_print_failed_contains_reason(self, mqtt_manager, event_payloads):
        payload = event_payloads["PrintFailed"]
        mqtt_manager.publish_status("printer-001", payload)

        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["reason"] == "thermal_runaway"

    def test_print_paused_roundtrip(self, mqtt_manager, event_payloads):
        payload = event_payloads["PrintPaused"]
        mqtt_manager.publish_status("printer-001", payload)

        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed == payload


# ============================================================================
# Malformed payload handling
# ============================================================================


class TestMalformedPayloads:

    def test_empty_dict_publishes_without_error(self, mqtt_manager):
        mqtt_manager.publish_status("printer-001", {})
        mqtt_manager._mock_paho.publish.assert_called_once()
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        assert json.loads(raw) == {}

    def test_nested_payload_serialises(self, mqtt_manager):
        deep = {"a": {"b": {"c": {"d": [1, 2, 3]}}}}
        mqtt_manager.publish_status("printer-001", deep)
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        assert json.loads(raw) == deep

    def test_payload_with_special_characters(self, mqtt_manager):
        payload = {"message": "Error: Can't heat nozzle -- check wiring!"}
        mqtt_manager.publish_status("printer-001", payload)
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["message"] == "Error: Can't heat nozzle -- check wiring!"

    def test_payload_with_unicode(self, mqtt_manager):
        payload = {"printer": "Drucker-Raum-A", "note": "Temperatur zu hoch"}
        mqtt_manager.publish_status("printer-001", payload)
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["note"] == "Temperatur zu hoch"

    def test_payload_with_none_values(self, mqtt_manager):
        payload = {"state": "Error", "job": None, "progress": None}
        mqtt_manager.publish_status("printer-001", payload)
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["job"] is None

    def test_payload_with_numeric_values(self, mqtt_manager):
        payload = {"temperature": 210.5, "flow_rate": 100, "layer": 42}
        mqtt_manager.publish_status("printer-001", payload)
        raw = mqtt_manager._mock_paho.publish.call_args[0][1]
        parsed = json.loads(raw)
        assert parsed["temperature"] == 210.5
        assert parsed["layer"] == 42


# ============================================================================
# MQTT topic structure validation
# ============================================================================


class TestTopicStructure:

    def test_topic_prefix(self, mqtt_manager):
        mqtt_manager.publish_status("x", {})
        topic = mqtt_manager._mock_paho.publish.call_args[0][0]
        assert topic.startswith("pravara/printers/")

    def test_topic_suffix(self, mqtt_manager):
        mqtt_manager.publish_status("x", {})
        topic = mqtt_manager._mock_paho.publish.call_args[0][0]
        assert topic.endswith("/status")

    def test_topic_three_segments(self, mqtt_manager):
        mqtt_manager.publish_status("printer-001", {})
        topic = mqtt_manager._mock_paho.publish.call_args[0][0]
        segments = topic.split("/")
        assert len(segments) == 4  # pravara / printers / printer-001 / status

    @pytest.mark.parametrize(
        "bad_id",
        ["", "a/b", "printer with spaces"],
    )
    def test_edge_case_instance_ids_in_topic(self, mqtt_manager, bad_id):
        """
        The current implementation does not sanitise instance IDs.
        This test documents the behaviour -- publish still fires.
        """
        mqtt_manager.publish_status(bad_id, {"state": "test"})
        mqtt_manager._mock_paho.publish.assert_called_once()


# ============================================================================
# Multiple publish calls -- sequencing
# ============================================================================


class TestPublishSequencing:

    def test_multiple_publishes_are_independent(self, mqtt_manager):
        mqtt_manager.publish_status("p1", {"state": "Printing"})
        mqtt_manager.publish_status("p2", {"state": "Idle"})

        assert mqtt_manager._mock_paho.publish.call_count == 2

        first_topic = mqtt_manager._mock_paho.publish.call_args_list[0][0][0]
        second_topic = mqtt_manager._mock_paho.publish.call_args_list[1][0][0]
        assert "p1" in first_topic
        assert "p2" in second_topic

    def test_rapid_status_updates(self, mqtt_manager):
        """Simulate rapid status polling that pushes many updates."""
        for i in range(50):
            mqtt_manager.publish_status("printer-001", {"progress": i * 2.0})

        assert mqtt_manager._mock_paho.publish.call_count == 50
        last_payload = json.loads(mqtt_manager._mock_paho.publish.call_args_list[-1][0][1])
        assert last_payload["progress"] == 98.0
