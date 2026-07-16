"""Dispatch-time credential provisioning + API-key env injection end to end."""
from server import config, credentials
from tests.conftest import wait_for


def test_dispatch_provisions_credentials(seeded, monkeypatch):
    """Every ssh/pct dispatch must push CURRENT creds before launch so an agent
    never runs on a rotated-out copy (the recurring lxc-101 401)."""
    calls = []

    async def spy(ex, target):
        calls.append(target["name"])
    monkeypatch.setattr(credentials, "provision", spy)

    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "cred dispatch",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    assert calls, "credentials.provision was not called before launch"


def test_api_key_injected_into_launch(seeded, monkeypatch):
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "sk-ant-xyz")
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "keyed",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    from server.executor import _cache
    mock = next(iter(_cache.values()))
    launch = next(cmd for cmd in mock.cmd_log if cmd.startswith("tmux new-session"))
    assert "ANTHROPIC_API_KEY=sk-ant-xyz" in launch


def test_project_env_overrides_base_auth(seeded, monkeypatch):
    """A project can point at a different endpoint; its env wins over the base."""
    monkeypatch.setattr(config, "ANTHROPIC_API_KEY", "sk-base")
    c = seeded["client"]
    tid = c.get("/api/targets").json()[0]["id"]
    p = c.post("/api/projects", json={"name": "altkey", "target_id": tid,
                                      "repo_path": "/mock/a",
                                      "env": {"ANTHROPIC_API_KEY": "sk-project"}}).json()
    t = c.post("/api/tasks", json={"project_id": p["id"], "title": "override",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    from server.executor import _cache
    mock = next(iter(_cache.values()))
    launch = next(cmd for cmd in mock.cmd_log if cmd.startswith("tmux new-session"))
    assert "ANTHROPIC_API_KEY=sk-project" in launch
    assert "sk-base" not in launch
