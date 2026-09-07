"""Playwright end-to-end: a real browser against a real agentdeck binary.

The server runs in mock mode, so every flow here is the genuine one — real HTTP,
real SSE, real approval round trips — with only the target scripted.
"""
import os
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from pathlib import Path

import pytest
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
PORT = 9199
AUTH_PORT = 9198
BASE = f"http://127.0.0.1:{PORT}"
AUTH_BASE = f"http://127.0.0.1:{AUTH_PORT}"

PHONE = {"width": 390, "height": 844}
DESKTOP = {"width": 1440, "height": 900}


def _port_open(port: int) -> bool:
    with socket.socket() as s:
        return s.connect_ex(("127.0.0.1", port)) == 0


def _free_port(port: int) -> None:
    """Kill anything still listening on `port`.

    A server leaked by a run that was killed mid-way answers the readiness check,
    and then every test silently runs against its accumulated state — the exact
    failure that once made the deck tests flaky.
    """
    try:
        out = subprocess.run(["ss", "-tlnp"], capture_output=True, text=True).stdout
        for line in out.splitlines():
            if f":{port} " in line and "pid=" in line:
                pid = line.split("pid=")[1].split(",")[0]
                subprocess.run(["kill", "-9", pid], capture_output=True)
    except Exception:
        pass


def _binary() -> str:
    """The agentdeck binary under test — prebuilt via AGENTDECK_BIN, or built now."""
    if env := os.environ.get("AGENTDECK_BIN"):
        return env
    out = Path(tempfile.gettempdir()) / "agentdeck-e2e-bin"
    go = shutil.which("go") or "/usr/local/go/bin/go"
    subprocess.run([go, "build", "-o", str(out), "./cmd/agentdeck"],
                   cwd=ROOT, check=True)
    return str(out)


def _start(port: int, extra_env: dict):
    _free_port(port)
    tmp = tempfile.mkdtemp(prefix="adk-e2e-")
    env = {**os.environ,
           "AGENTDECK_MOCK": "1", "AGENTDECK_TICK": "0.1",
           "AGENTDECK_MOCK_DELAY": "0.25", "AGENTDECK_PORT": str(port),
           "AGENTDECK_DB": str(Path(tmp) / "e2e.db"),
           "AGENTDECK_BASE_URL": f"http://127.0.0.1:{port}",
           **extra_env}
    proc = subprocess.Popen([_binary()], cwd=ROOT, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    for _ in range(100):
        if _port_open(port):
            break
        time.sleep(0.1)
    else:
        proc.kill()
        raise RuntimeError(f"server did not start on {port}")
    return proc


def _stop(proc, port: int):
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
    _free_port(port)   # belt and braces: never leak past teardown


@pytest.fixture(scope="session")
def server():
    proc = _start(PORT, {})
    yield BASE
    _stop(proc, PORT)


@pytest.fixture(scope="session")
def auth_server():
    """A second server with a bearer token set, to prove the PWA works in token
    mode — fetch AND EventSource both have to thread the token through."""
    proc = _start(AUTH_PORT, {"AGENTDECK_AUTH_TOKEN": "secret123"})
    yield AUTH_BASE
    _stop(proc, AUTH_PORT)


@pytest.fixture(scope="session")
def browser():
    with sync_playwright() as p:
        b = p.chromium.launch()
        yield b
        b.close()


@pytest.fixture(autouse=True)
def clean_board(server):
    """Clear the shared board before each test.

    Without this the session-scoped server accumulates tasks and every
    board-state-dependent test (deck panes past the 16 cap, filters, counts) gets
    slower and flakier as more tests are added.

    Sessions are RELEASED, never killed: the same asymmetry the product enforces,
    so the fixture cannot quietly destroy the scripted terminals other tests then
    expect to discover.
    """
    import json
    import urllib.request

    def wipe(kind):
        try:
            rows = json.load(urllib.request.urlopen(f"{server}/api/{kind}", timeout=10))
        except Exception:
            return
        for row in rows:
            try:
                urllib.request.urlopen(urllib.request.Request(
                    f"{server}/api/{kind}/{row['id']}", method="DELETE"), timeout=10)
            except Exception:
                pass

    wipe("tasks")
    wipe("sessions")
    yield


@pytest.fixture()
def page(browser, server, request):
    viewport = getattr(request, "param", DESKTOP)
    ctx = browser.new_context(viewport=viewport)
    pg = ctx.new_page()
    yield pg
    ctx.close()
