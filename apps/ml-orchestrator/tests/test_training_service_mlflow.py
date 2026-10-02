"""
Test TrainingService's MLflow 3 call contract (#46).

mlflow 3.16 changed two defaults that TrainingService depends on:
- the filesystem tracking store ("mlruns") is refused unless
  MLFLOW_ALLOW_FILE_STORE=true, so the local default is a SQLite store;
- mlflow.sklearn.log_model() defaults to the skops format, which cannot
  serialize this service's own wrapper classes, so the call pins cloudpickle
  and passes the model name as `name=` (the positional `artifact_path` is
  deprecated).

These tests pin that contract with an in-memory stand-in for the `mlflow`
module, so they run without an MLflow install, a tracking store or a model.
The module is loaded from its file path rather than through the `services`
package, whose __init__ also imports the inference and telemetry services
(and through them TensorFlow, Prophet, Redis and cachetools).
"""

import importlib.util
import sys
import types
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock

import numpy as np
import pandas as pd
import pytest

TRAINING_SERVICE_PATH = (
    Path(__file__).resolve().parent.parent / "services" / "training_service.py"
)


def _fake_mlflow() -> types.ModuleType:
    """A stand-in for the parts of `mlflow` TrainingService calls."""
    mlflow = types.ModuleType("mlflow")
    mlflow.set_tracking_uri = MagicMock()
    mlflow.start_run = MagicMock()  # MagicMock supports `with` out of the box
    mlflow.log_param = MagicMock()
    mlflow.log_params = MagicMock()
    mlflow.log_metric = MagicMock()

    sklearn = types.ModuleType("mlflow.sklearn")
    # Same value as mlflow.sklearn.SERIALIZATION_FORMAT_CLOUDPICKLE.
    sklearn.SERIALIZATION_FORMAT_CLOUDPICKLE = "cloudpickle"
    sklearn.log_model = MagicMock()
    mlflow.sklearn = sklearn
    return mlflow


def _fake_optuna() -> types.ModuleType:
    optuna = types.ModuleType("optuna")
    optuna.Trial = type("Trial", (), {})
    return optuna


@pytest.fixture
def mlflow_stub(monkeypatch):
    mlflow = _fake_mlflow()
    monkeypatch.setitem(sys.modules, "mlflow", mlflow)
    monkeypatch.setitem(sys.modules, "mlflow.sklearn", mlflow.sklearn)
    monkeypatch.setitem(sys.modules, "optuna", _fake_optuna())
    return mlflow


@pytest.fixture
def training_module(mlflow_stub):
    spec = importlib.util.spec_from_file_location(
        "training_service_under_test", TRAINING_SERVICE_PATH
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_default_tracking_uri_is_sqlite_store(monkeypatch, mlflow_stub, training_module):
    monkeypatch.delenv("MLFLOW_TRACKING_URI", raising=False)

    service = training_module.TrainingService()

    assert service.mlflow_tracking_uri == "sqlite:///mlflow.db"
    mlflow_stub.set_tracking_uri.assert_called_once_with("sqlite:///mlflow.db")


def test_tracking_uri_env_override_wins(monkeypatch, mlflow_stub, training_module):
    monkeypatch.setenv("MLFLOW_TRACKING_URI", "http://tracking.invalid:5000")

    service = training_module.TrainingService()

    assert service.mlflow_tracking_uri == "http://tracking.invalid:5000"
    mlflow_stub.set_tracking_uri.assert_called_once_with("http://tracking.invalid:5000")


@pytest.mark.asyncio
async def test_train_model_registers_with_name_and_cloudpickle(
    monkeypatch, mlflow_stub, training_module
):
    monkeypatch.delenv("MLFLOW_TRACKING_URI", raising=False)
    service = training_module.TrainingService()
    trained = object()
    service.load_training_data = AsyncMock(
        return_value=(pd.DataFrame({"temperature": [80.0, 81.0]}), np.array([0, 1]))
    )
    service.train_quality_model = AsyncMock(return_value=trained)
    service.evaluate_model = AsyncMock(return_value={"accuracy": 0.9})
    service.save_model = AsyncMock()

    result = await service.train_model("quality")

    assert result["status"] == "completed"
    assert result["metrics"] == {"accuracy": 0.9}
    mlflow_stub.sklearn.log_model.assert_called_once()
    args, kwargs = mlflow_stub.sklearn.log_model.call_args
    # Only the model is positional: the deprecated positional artifact_path is gone.
    assert args == (trained,)
    assert "artifact_path" not in kwargs
    assert kwargs["name"] == "quality"
    assert kwargs["serialization_format"] == "cloudpickle"
    assert kwargs["registered_model_name"] == "pravara_quality_model"
    mlflow_stub.log_metric.assert_called_once_with("accuracy", 0.9)


@pytest.mark.asyncio
async def test_unknown_model_type_fails_visibly(monkeypatch, mlflow_stub, training_module):
    monkeypatch.delenv("MLFLOW_TRACKING_URI", raising=False)
    service = training_module.TrainingService()
    service.load_training_data = AsyncMock(
        return_value=(pd.DataFrame({"temperature": [80.0]}), np.array([0]))
    )

    with pytest.raises(ValueError, match="Unknown model type"):
        await service.train_model("not-a-model")

    mlflow_stub.sklearn.log_model.assert_not_called()
    mlflow_stub.log_param.assert_any_call("error", "Unknown model type: not-a-model")
