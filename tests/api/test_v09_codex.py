"""v0.9 codex as a first-class agent: project-level toggle, correct launch flags,
and its own credential provisioning."""
from server import db
from server import executor as executor_pkg
from tests.conftest import wait_for


def _mock():
    return next(iter(executor_pkg._cache.values()))


def _launch_cmd():
    return next(c for c in _mock().cmd_log if c.startswith("tmux new-session"))


def _project(c, name, **kw):
    tid = c.get("/api/targets").json()[0]["id"]
    return c.post("/api/projects", json={"name": name, "target_id": tid,
                                         "repo_path": f"/mock/{name}", **kw}).json()


def _run(c, project_id, **task_kw):
    t = c.post("/api/tasks", json={"project_id": project_id, "title": "codex run",
                                   "prompt": "do it", **task_kw}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] in ("review", "failed"),
             msg="finished")
    return c.get(f"/api/tasks/{t['id']}").json()


# ---- the toggle --------------------------------------------------------------

def test_project_default_agent_drives_tasks(client):
    p = _project(client, "codexproj", default_agent="codex")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "inherit"}).json()
    assert t["agent"] == "codex"


def test_task_agent_overrides_project_default(client):
    p = _project(client, "override", default_agent="codex")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "explicit",
                                        "agent": "claude"}).json()
    assert t["agent"] == "claude"


def test_default_agent_defaults_to_claude(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "plain"}).json()
    assert t["agent"] == "claude"


def test_default_agent_is_patchable(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    assert c.patch(f"/api/projects/{pid}",
                   json={"default_agent": "codex"}).json()["default_agent"] == "codex"
    assert c.patch(f"/api/projects/{pid}",
                   json={"default_agent": "cursor"}).status_code == 422


def test_gated_mode_rejected_for_codex_including_via_project_default(client):
    p = _project(client, "gatedcodex", default_agent="codex")
    r = client.post("/api/tasks", json={"project_id": p["id"], "title": "nope",
                                        "permission_mode": "default"})
    assert r.status_code == 400
    assert "gated approvals" in r.json()["detail"]


# ---- dispatch ----------------------------------------------------------------

def test_codex_dispatch_uses_codex_flags(client):
    p = _project(client, "cxdispatch", default_agent="codex")
    _run(client, p["id"])
    cmd = _launch_cmd()
    assert "codex exec --json" in cmd
    assert "--sandbox workspace-write" in cmd
    assert "< /dev/null" in cmd
    assert "claude -p" not in cmd


def test_codex_gets_staged_context_like_claude(client, tmp_path):
    house = tmp_path / "CLAUDE.md"
    house.write_text("codex should read this too")
    tid = client.get("/api/targets").json()[0]["id"]
    client.patch(f"/api/targets/{tid}", json={"context_paths": [str(house)]})
    p = _project(client, "cxctx", default_agent="codex")

    _run(client, p["id"])

    staged = [v for k, v in _mock().fs.items() if k.endswith("/context/CLAUDE.md")]
    assert staged and staged[0] == b"codex should read this too"
    prompt = [v for k, v in _mock().fs.items() if k.endswith("/prompt.md")][0].decode()
    assert ".agentdeck/context/CLAUDE.md" in prompt


def test_codex_launch_skips_claude_only_flags(client):
    p = _project(client, "cxflags", default_agent="codex",
                 mcp={"x": {"command": "x"}}, permissions={"allow": ["Bash(ls*)"]})
    _run(client, p["id"])
    cmd = _launch_cmd()
    # codex has no --settings/--mcp-config equivalent; passing them would abort it
    assert "--settings" not in cmd and "--mcp-config" not in cmd


# ---- credentials -------------------------------------------------------------

def test_codex_credentials_are_provisioned_to_remote_targets(client, tmp_path,
                                                             monkeypatch):
    from server import credentials
    codex_creds = tmp_path / "auth.json"
    codex_creds.write_text('{"tokens": {"access_token": "codex-test"}}')
    monkeypatch.setattr(credentials, "CODEX_CREDS_PATH", str(codex_creds))
    monkeypatch.setitem(credentials.AGENT_CREDS, "codex",
                        (lambda: str(codex_creds), "~/.codex", "~/.codex/auth.json"))

    tid = client.post("/api/targets", json={"name": "cx-ssh", "kind": "ssh",
                                            "host": "192.0.2.9"}).json()["id"]
    p = client.post("/api/projects", json={"name": "cxremote", "target_id": tid,
                                           "repo_path": "/mock/cxremote",
                                           "default_agent": "codex"}).json()
    _run(client, p["id"])

    pushes = [c for c in _mock().cmd_log if "auth.json" in c]
    assert pushes, "codex auth was never pushed to the remote target"
    assert "~/.codex/auth.json" in pushes[0]
    # and claude's credentials are NOT pushed for a codex run
    assert not [c for c in _mock().cmd_log if ".claude/.credentials.json" in c]


def test_claude_run_still_provisions_claude_credentials(client, tmp_path, monkeypatch):
    from server import credentials
    fake = tmp_path / "creds.json"
    fake.write_text('{"access_token": "t", "refresh_token": "t"}')
    monkeypatch.setattr(credentials, "CREDS_PATH", str(fake))
    monkeypatch.setitem(credentials.AGENT_CREDS, "claude",
                        (lambda: str(fake), "~/.claude", "~/.claude/.credentials.json"))

    tid = client.post("/api/targets", json={"name": "cl-ssh", "kind": "ssh",
                                            "host": "192.0.2.10"}).json()["id"]
    p = client.post("/api/projects", json={"name": "clremote", "target_id": tid,
                                           "repo_path": "/mock/clremote"}).json()
    _run(client, p["id"])

    assert [c for c in _mock().cmd_log if ".claude/.credentials.json" in c]


def test_gemini_has_no_credentials_to_push(client):
    tid = client.post("/api/targets", json={"name": "gm-ssh", "kind": "ssh",
                                            "host": "192.0.2.11"}).json()["id"]
    p = client.post("/api/projects", json={"name": "gmremote", "target_id": tid,
                                           "repo_path": "/mock/gmremote",
                                           "default_agent": "gemini"}).json()
    _run(client, p["id"])
    assert not [c for c in _mock().cmd_log if "auth.json" in c]


def test_agent_column_survives_followups(client):
    """A follow-up attempt must not silently fall back to claude."""
    p = _project(client, "cxfollow", default_agent="codex")
    t = _run(client, p["id"])
    r = client.post(f"/api/tasks/{t['id']}/dispatch",
                    json={"prompt": "also add a test"})
    assert r.status_code in (200, 201)
    assert db.one("SELECT agent FROM tasks WHERE id=?", (t["id"],))["agent"] == "codex"
