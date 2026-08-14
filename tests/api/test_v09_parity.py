"""v0.9 context parity: a dispatched agent gets the same knowledge, tools and
grantable permissions a local interactive session would — on every target kind.

Each test asserts against what actually reached the target (mock fs + cmd_log),
not against the config that was requested.
"""
from server import db
from server import executor as executor_pkg
from tests.conftest import wait_for


def _mock():
    return next(iter(executor_pkg._cache.values()))


def _launch_cmd():
    return next(c for c in _mock().cmd_log if c.startswith("tmux new-session"))


def _staged(suffix):
    """Contents of the one staged file whose path ends with `suffix`."""
    hits = [v for k, v in _mock().fs.items() if k.endswith(suffix)]
    assert hits, f"nothing staged at *{suffix} (have: {sorted(_mock().fs)})"
    return hits[0]


def _run_task(c, project_id, **task_kw):
    t = c.post("/api/tasks", json={"project_id": project_id, "title": "parity",
                                   "prompt": "do the thing", **task_kw}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    return t


# ---- context bundle ----------------------------------------------------------

def test_target_context_is_staged_and_announced(client, tmp_path):
    house = tmp_path / "CLAUDE.md"
    house.write_text("never hardcode IPs")
    tid = client.get("/api/targets").json()[0]["id"]
    client.patch(f"/api/targets/{tid}", json={"context_paths": [str(house)]})
    p = client.post("/api/projects", json={"name": "ctx", "target_id": tid,
                                           "repo_path": "/mock/ctx"}).json()

    _run_task(client, p["id"])

    assert _staged("/.agentdeck/context/CLAUDE.md") == b"never hardcode IPs"
    prompt = _staged("/.agentdeck/prompt.md").decode()
    assert ".agentdeck/context/CLAUDE.md" in prompt
    assert prompt.index("## Context") < prompt.index("do the thing")
    assert b"CLAUDE.md" in _staged("/.agentdeck/context/INDEX.md")


def test_project_context_stacks_on_target_context(client, tmp_path):
    (tmp_path / "host.md").write_text("host rules")
    (tmp_path / "proj.md").write_text("project rules")
    tid = client.get("/api/targets").json()[0]["id"]
    client.patch(f"/api/targets/{tid}",
                 json={"context_paths": [str(tmp_path / "host.md")]})
    p = client.post("/api/projects", json={
        "name": "stack", "target_id": tid, "repo_path": "/mock/stack",
        "context_paths": [str(tmp_path / "proj.md")]}).json()

    _run_task(client, p["id"])

    assert _staged("/context/host.md") == b"host rules"
    assert _staged("/context/proj.md") == b"project rules"


def test_missing_context_is_reported_to_the_agent(client, tmp_path):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "gone", "target_id": tid, "repo_path": "/mock/gone",
        "context_paths": [str(tmp_path / "absent.md")]}).json()

    _run_task(client, p["id"])

    prompt = _staged("/.agentdeck/prompt.md").decode()
    assert "could NOT be staged" in prompt and "absent.md" in prompt


def test_no_context_configured_leaves_prompt_clean(seeded):
    _run_task(seeded["client"], seeded["project_id"])
    prompt = _staged("/.agentdeck/prompt.md").decode()
    assert "## Context" not in prompt
    assert not [k for k in _mock().fs if "/.agentdeck/context/" in k]


# ---- MCP ---------------------------------------------------------------------

def test_project_mcp_reaches_the_agent(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "mcp", "target_id": tid, "repo_path": "/mock/mcp",
        "mcp": {"homelab": {"command": "python3", "args": ["-m", "srv"]}},
        "strict_mcp": True}).json()

    _run_task(client, p["id"])

    staged = _staged("/.agentdeck/mcp.json").decode()
    assert '"mcpServers"' in staged and '"homelab"' in staged
    cmd = _launch_cmd()
    assert "--mcp-config .agentdeck/mcp.json" in cmd
    assert "--strict-mcp-config" in cmd


def test_mcp_accepts_a_full_mcpservers_document(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "mcpfull", "target_id": tid, "repo_path": "/mock/mcpfull",
        "mcp": {"mcpServers": {"x": {"command": "x"}}}}).json()

    _run_task(client, p["id"])

    staged = db.unj(_staged("/.agentdeck/mcp.json").decode())
    assert list(staged) == ["mcpServers"]          # not double-wrapped
    assert "--strict-mcp-config" not in _launch_cmd()


def test_no_mcp_configured_passes_no_flag(seeded):
    _run_task(seeded["client"], seeded["project_id"])
    assert "--mcp-config" not in _launch_cmd()


# ---- permissions -------------------------------------------------------------

def test_permission_rules_ship_in_ungated_mode(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "perms", "target_id": tid, "repo_path": "/mock/perms",
        "permissions": {"allow": ["Bash(pytest*)"], "deny": ["Bash(rm *)"]}}).json()

    _run_task(client, p["id"], permission_mode="acceptEdits")

    settings = db.unj(_staged("/.agentdeck/settings.json").decode())
    assert settings["permissions"]["allow"] == ["Bash(pytest*)"]
    assert "hooks" not in settings                 # rules without the approval gate
    assert "--settings .agentdeck/settings.json" in _launch_cmd()


def test_gated_mode_intercepts_every_tool_by_default(seeded):
    _run_task(seeded["client"], seeded["project_id"], permission_mode="default")
    settings = db.unj(_staged("/.agentdeck/settings.json").decode())
    # anything the matcher misses is denied with no prompt and no explanation
    assert settings["hooks"]["PreToolUse"][0]["matcher"] == "*"


def test_gate_matcher_is_narrowable_per_project(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={
        "name": "narrow", "target_id": tid, "repo_path": "/mock/narrow",
        "gate_matcher": "Bash|Edit"}).json()

    _run_task(client, p["id"], permission_mode="default")

    settings = db.unj(_staged("/.agentdeck/settings.json").decode())
    assert settings["hooks"]["PreToolUse"][0]["matcher"] == "Bash|Edit"


def test_typo_in_permission_keys_is_rejected_at_config_time(client):
    tid = client.get("/api/targets").json()[0]["id"]
    r = client.post("/api/projects", json={
        "name": "typo", "target_id": tid, "repo_path": "/mock/typo",
        "permissions": {"allowed": ["Bash"]}})
    assert r.status_code == 400
    assert "allowed" in r.json()["detail"]


# ---- memory ------------------------------------------------------------------

def test_memory_dir_links_the_attempt_session(client):
    tid = client.get("/api/targets").json()[0]["id"]
    client.patch(f"/api/targets/{tid}", json={"memory_dir": "/store/memory"})
    p = client.post("/api/projects", json={"name": "mem", "target_id": tid,
                                           "repo_path": "/mock/mem"}).json()

    _run_task(client, p["id"])

    link = next(c for c in _mock().cmd_log if "ln -sfn" in c)
    assert "/store/memory" in link
    assert ".claude/projects/" in link and link.endswith('/memory"')


def test_no_memory_dir_touches_nothing(seeded):
    _run_task(seeded["client"], seeded["project_id"])
    assert not [c for c in _mock().cmd_log if "ln -sfn" in c]


# ---- both launch paths -------------------------------------------------------

def test_sandbox_dispatch_gets_the_same_context_and_memory(client, tmp_path,
                                                           monkeypatch):
    from server import credentials
    fake = tmp_path / "creds.json"
    fake.write_text('{"access_token": "t", "refresh_token": "t"}')
    monkeypatch.setattr(credentials, "CREDS_PATH", str(fake))

    house = tmp_path / "CLAUDE.md"
    house.write_text("sandbox needs this too")
    tid = client.post("/api/targets", json={"name": "sb-parity", "kind": "sandbox",
                                            "host": "110", "sandbox": True,
                                            "context_paths": [str(house)]}).json()["id"]
    p = client.post("/api/projects", json={
        "name": "sbctx", "target_id": tid, "repo_path": "/root/demo",
        "permissions": {"allow": ["Bash(ls*)"]}}).json()
    db.execute("INSERT INTO memories(project_id, note, created_at) VALUES(?,?,?)",
               (p["id"], "remember the sandbox", db.now()))

    _run_task(client, p["id"], permission_mode="bypassPermissions")

    # the sandbox path used to skip project memory entirely
    prompt = _staged("/.agentdeck/prompt.md").decode()
    assert "remember the sandbox" in prompt
    assert _staged("/.agentdeck/context/CLAUDE.md") == b"sandbox needs this too"
    settings = db.unj(_staged("/.agentdeck/settings.json").decode())
    assert settings["permissions"]["allow"] == ["Bash(ls*)"]
