"""v0.8: per-project env injection (any-model support), templates, stats."""
import pytest

from server.agents import env_prefix
from tests.conftest import wait_for


def test_env_prefix_builder():
    assert env_prefix(None) == ""
    assert env_prefix({}) == ""
    p = env_prefix({"ANTHROPIC_BASE_URL": "http://ollama:11434",
                    "ANTHROPIC_AUTH_TOKEN": "it's local"})
    assert "ANTHROPIC_BASE_URL=http://ollama:11434" in p
    assert "ANTHROPIC_AUTH_TOKEN='it'\"'\"'s local'" in p   # shell-quoted
    assert env_prefix({}, sandbox=True) == "IS_SANDBOX=1 "
    # explicit value beats the sandbox default
    assert env_prefix({"IS_SANDBOX": "0"}, sandbox=True) == "IS_SANDBOX=0 "
    with pytest.raises(ValueError):
        env_prefix({"BAD-NAME": "x"})
    with pytest.raises(ValueError):
        env_prefix({"$(evil)": "x"})


def test_project_env_reaches_launch_command(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "localmodel", "target_id": tid, "repo_path": "/mock/lm",
        "env": {"ANTHROPIC_BASE_URL": "http://100.127.85.58:11434",
                "ANTHROPIC_AUTH_TOKEN": "ollama"}}).json()
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "local run",
                                        "prompt": "x", "model": "qwen3.5:4b"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: client.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    from server.executor import _cache
    mock = next(iter(_cache.values()))
    launch = next(c for c in mock.cmd_log if c.startswith("tmux new-session"))
    assert "ANTHROPIC_BASE_URL=http://100.127.85.58:11434" in launch
    assert "ANTHROPIC_AUTH_TOKEN=ollama" in launch
    assert "--model qwen3.5:4b" in launch


def test_project_env_patchable(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    r = c.patch(f"/api/projects/{pid}", json={"env": {"OPENAI_BASE_URL": "http://x:8000/v1"}})
    assert '"OPENAI_BASE_URL"' in r.json()["env_json"]


def test_templates_roundtrip(client):
    assert client.get("/api/templates").json() == []
    tpls = [{"name": "bugfix", "title": "Fix: ", "prompt": "Reproduce, fix, add a test.",
             "permission_mode": "acceptEdits"},
            {"name": "local-quick", "model": "qwen3.5:4b", "prompt": "small change"}]
    r = client.put("/api/templates", json=tpls)
    assert r.status_code == 200
    assert client.get("/api/templates").json()[0]["name"] == "bugfix"
    assert client.put("/api/templates", json=[{"prompt": "no name"}]).status_code == 400


def test_stats_aggregates_costs(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "cost me",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    c.post(f"/api/tasks/{t['id']}/complete")
    s = c.get("/api/stats").json()
    assert s["total_cost_usd"] >= 0.0123          # mock result cost
    assert s["last_7d_usd"] >= 0.0123
    assert s["tasks_done"] >= 1
    assert any(p["cost_usd"] > 0 for p in s["by_project"])
