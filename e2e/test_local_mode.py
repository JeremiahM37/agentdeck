"""Standalone local command: source/binary install and durable local sessions."""

import json
import os
import pty
import select
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from conftest import ROOT, _binary


@pytest.fixture(autouse=True)
def clean_board():
    """Local tests own their state and must not start the hosted fixture."""
    yield


def _run(binary, env, *args, input=None, check=True, timeout=30):
    return subprocess.run(
        [str(binary), "local", *args],
        env=env,
        input=input,
        text=True,
        capture_output=True,
        check=check,
        timeout=timeout,
    )


@pytest.fixture(scope="module")
def local_binary(tmp_path_factory):
    supplied = os.environ.get("AGENTDECK_LOCAL_TEST_BINARY") or os.environ.get("AGENTDECK_BIN")
    candidate = Path(supplied) if supplied else Path(_binary())
    probe = subprocess.run([candidate, "local", "--help"], text=True, capture_output=True)
    if probe.returncode != 0:
        pytest.skip("backend local runtime is not in the selected binary")

    root = tmp_path_factory.mktemp("local-install")
    source_prefix = root / "source-bin"
    binary_prefix = root / "binary-bin"
    env = {
        **os.environ,
        "HOME": str(root / "home"),
        "XDG_BIN_HOME": str(root / "home/.local/bin"),
        "XDG_STATE_HOME": str(root / "state"),
        "XDG_CONFIG_HOME": str(root / "config"),
        "XDG_CACHE_HOME": str(root / "cache"),
        "PATH": os.environ.get("PATH", ""),
    }
    (root / "home").mkdir()
    source_install = subprocess.run(
        ["bash", str(ROOT / "tools/install-local.sh"), "--source", str(ROOT), "--prefix", str(source_prefix)],
        cwd=root,
        env=env,
        text=True,
        capture_output=True,
        check=False,
        timeout=120,
    )
    assert source_install.returncode == 0, source_install.stderr
    source_binary = source_prefix / "agentdeck"
    binary_install = subprocess.run(
        ["bash", str(ROOT / "tools/install-local.sh"), "--binary", str(source_binary), "--prefix", str(binary_prefix)],
        cwd=root,
        env=env,
        text=True,
        capture_output=True,
        check=False,
        timeout=30,
    )
    assert binary_install.returncode == 0, binary_install.stderr
    installed = binary_prefix / "agentdeck"
    assert installed.is_file() and os.access(installed, os.X_OK)
    return installed, root


def _local_env(root, fake_agent):
    tmux_tmp = root / "tmux"
    tmux_tmp.mkdir(exist_ok=True)
    env = {
        **os.environ,
        "HOME": str(root / "home"),
        "XDG_STATE_HOME": str(root / "state"),
        "XDG_CONFIG_HOME": str(root / "config"),
        "XDG_CACHE_HOME": str(root / "cache"),
        "TMUX_TMPDIR": str(tmux_tmp),
        "AGENTDECK_CLAUDE_BIN": str(fake_agent),
        "AGENTDECK_MOCK": "0",
        "AGENTDECK_SESSION_POLL": "3600",
    }
    env.pop("AGENTDECK_API", None)
    env.pop("AGENTDECK_ATTACH_HOST", None)
    return env


def _json_command(binary, env, *args, **kwargs):
    result = _run(binary, env, *args, **kwargs)
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"expected JSON from {' '.join(args)}: {result.stdout!r} {result.stderr!r}") from exc


def _read_until(master, token, timeout=20):
    output = b""
    deadline = time.time() + timeout
    while time.time() < deadline:
        if token in output:
            return output
        ready, _, _ = select.select([master], [], [], 0.2)
        if ready:
            try:
                output += os.read(master, 65536)
            except OSError:
                break
    raise AssertionError(f"did not read {token!r}; got {output[-2000:]!r}")


def test_source_and_binary_local_install_create_persist_and_reattach(local_binary, tmp_path):
    binary, root = local_binary
    fake_agent = tmp_path / "fake-claude"
    fake_agent.write_text("#!/bin/sh\nprintf 'LOCAL_AGENT_READY\\n'\nexec /bin/bash --noprofile --norc\n")
    fake_agent.chmod(0o755)
    env = _local_env(root, fake_agent)
    workdir = tmp_path / "project"
    workdir.mkdir()
    subprocess.run(["git", "init", "-q", str(workdir)], check=True)

    before = _json_command(binary, env, "status")
    assert before["running"] is False
    targets = _json_command(binary, env, "api", "GET", "/targets")
    target = next(row for row in targets if row["kind"] == "local")
    project = _json_command(
        binary,
        env,
        "api",
        "POST",
        "/projects",
        json.dumps({"name": "Local project", "target_id": target["id"], "repo_path": str(workdir)}),
    )
    session = _json_command(
        binary,
        env,
        "api",
        "POST",
        "/sessions",
        json.dumps({"name": "Persistent local session", "project_id": project["id"], "agent": "claude"}),
    )

    master, slave = pty.openpty()
    attached = subprocess.Popen(
        [str(binary), "local", "attach", "session", str(session["id"])],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env={**env, "TERM": "xterm-256color"},
        start_new_session=True,
    )
    os.close(slave)
    try:
        _read_until(master, b"LOCAL_AGENT_READY")
        os.write(master, b"printf LOCAL_PTY_SENTINEL\\n\r")
        _read_until(master, b"LOCAL_PTY_SENTINEL")
        os.write(master, b"\x02d")
        attached.wait(timeout=15)
        assert attached.returncode == 0
    finally:
        if attached.poll() is None:
            attached.terminate()
            attached.wait(timeout=10)
        os.close(master)

    assert _json_command(binary, env, "api", "GET", f"/sessions/{session['id']}")["name"] == "Persistent local session"
    stopped = _run(binary, env, "stop")
    assert stopped.returncode == 0, stopped.stderr
    assert _json_command(binary, env, "status")["running"] is False

    sessions = _json_command(binary, env, "api", "GET", "/sessions")
    projects = _json_command(binary, env, "api", "GET", "/projects")
    assert any(row["id"] == session["id"] for row in sessions)
    assert any(row["id"] == project["id"] for row in projects)
    _run(binary, env, "stop", check=False)


def test_explicit_api_stays_remote(local_binary, tmp_path):
    binary, root = local_binary
    seen = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            seen.append(self.path)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(b"[]")

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    env = {**os.environ, "AGENTDECK_API": f"http://127.0.0.1:{server.server_port}", "HOME": str(root / "remote-home")}
    try:
        result = subprocess.run([str(binary), "api", "GET", "/sessions"], env=env, text=True, capture_output=True, check=True)
        assert json.loads(result.stdout) == []
        assert seen == ["/api/sessions"]
    finally:
        server.shutdown()
