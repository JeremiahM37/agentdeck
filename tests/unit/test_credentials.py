"""The rotation-proof auth fix: API-key precedence + OAuth provisioning."""
import asyncio

from server import config, credentials
from server.executor.base import ExecResult


class FakeExec:
    def __init__(self):
        self.cmds = []
        self.files = {}

    async def run(self, cmd, cwd="", timeout=120):
        self.cmds.append(cmd)
        return ExecResult(0, "", "")

    async def write_file(self, path, data):
        self.files[path] = data


def _run(coro):
    # run in a worker thread so we never collide with an ambient event loop left
    # by the API tests' TestClient portals (same pattern as test_real_git.py)
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(1) as pool:
        return pool.submit(asyncio.run, coro).result()


def test_api_key_wins_and_skips_push(monkeypatch, tmp_path):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "sk-ant-test")
    assert credentials.base_agent_env() == {"ANTHROPIC_API_KEY": "sk-ant-test"}
    ex = FakeExec()
    _run(credentials.provision(ex, {"kind": "ssh", "name": "t"}))
    assert ex.cmds == []   # nothing pushed — the key is injected via env instead


def test_no_key_means_no_env(monkeypatch):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "")
    assert credentials.base_agent_env() == {}


def test_oauth_provision_pushes_current_creds(monkeypatch, tmp_path):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "")
    creds = tmp_path / "creds.json"
    creds.write_text('{"claudeAiOauth": {"refreshToken": "rt-current"}}')
    monkeypatch.setattr(credentials, "CREDS_PATH", str(creds))
    ex = FakeExec()
    _run(credentials.provision(ex, {"kind": "ssh", "name": "lxc-101"}))
    # it writes the CURRENT creds into the target user's ~/.claude via base64
    assert any("base64 -d > ~/.claude/.credentials.json" in c for c in ex.cmds)
    assert any("chmod 600 ~/.claude/.credentials.json" in c for c in ex.cmds)


def test_local_and_mock_targets_are_noop(monkeypatch, tmp_path):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "")
    creds = tmp_path / "c.json"; creds.write_text("{}")
    monkeypatch.setattr(credentials, "CREDS_PATH", str(creds))
    for kind in ("local", "mock"):
        ex = FakeExec()
        _run(credentials.provision(ex, {"kind": kind, "name": kind}))
        assert ex.cmds == []   # local uses the control plane's own creds


def test_missing_creds_is_graceful(monkeypatch):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "")
    monkeypatch.setattr(credentials, "CREDS_PATH", "/nonexistent/creds.json")
    ex = FakeExec()
    _run(credentials.provision(ex, {"kind": "ssh", "name": "t"}))  # no raise
    assert ex.cmds == []
