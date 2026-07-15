"""Shared fixtures. Env is pinned BEFORE server.config is imported."""
import os

os.environ["AGENTDECK_MOCK"] = "1"
os.environ["AGENTDECK_TICK"] = "0.05"
os.environ["AGENTDECK_MOCK_DELAY"] = "0.05"
os.environ["AGENTDECK_APPROVAL_POLL"] = "0.5"

import time

import httpx
import pytest
from fastapi.testclient import TestClient

from server import config, db
from server import executor as executor_pkg
from server.app import create_app


@pytest.fixture()
def client(tmp_path, monkeypatch):
    monkeypatch.setattr(config, "DB_PATH", tmp_path / "test.db")
    executor_pkg.reset_cache()
    app = create_app()
    # mock agents exercise the REAL hook endpoints through the app itself
    executor_pkg.mock_http_client_factory = lambda: httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app), base_url="http://adk.test")
    with TestClient(app) as c:
        yield c
    executor_pkg.mock_http_client_factory = None
    executor_pkg.reset_cache()
    db.close()


@pytest.fixture()
def seeded(client):
    """A mock target + project ready for task tests (seed_demo_data ran in mock mode)."""
    projects = client.get("/api/projects").json()
    return {"client": client, "project_id": projects[0]["id"]}


def wait_for(fn, timeout=15.0, interval=0.05, msg="condition"):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = fn()
        if v:
            return v
        time.sleep(interval)
    raise AssertionError(f"timed out waiting for {msg}")
