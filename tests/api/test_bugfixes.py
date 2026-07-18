"""Regression tests for bugs found in the v0.8 adversarial audit."""
from server import broker, db
from server.executor.pct import PctExecutor
from server.executor.ssh import SSHExecutor
from tests.conftest import wait_for


def _pending(c):
    return c.get("/api/approvals?status=pending").json()


def _gated(c, pid, title, prompt="x [mock:approval]"):
    t = c.post("/api/tasks", json={"project_id": pid, "title": title,
                                   "prompt": prompt, "permission_mode": "default"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: any(a["task_id"] == t["id"] for a in _pending(c)),
             msg="pending approval")
    return t


def test_cancel_clears_pending_approval(seeded):
    """Cancelling a task with a live approval must not leave a ghost 'pending'
    row (board badge would stick forever)."""
    c, pid = seeded["client"], seeded["project_id"]
    t = _gated(c, pid, "cancel ghost")
    assert len(_pending(c)) == 1
    c.post(f"/api/tasks/{t['id']}/cancel")
    wait_for(lambda: len(_pending(c)) == 0, msg="approval resolved")
    assert c.get("/api/health").json()["pending_approvals"] == 0
    # audit trail: it's recorded as expired, not silently dropped
    assert any(a["task_id"] == t["id"]
               for a in c.get("/api/approvals?status=expired").json())


def test_finalize_clears_pending_approval(seeded):
    """If an agent files an approval then dies without a decision, the approval
    must not hang pending after the attempt finalizes."""
    c, pid = seeded["client"], seeded["project_id"]
    t = _gated(c, pid, "finalize ghost")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    # simulate the agent process vanishing: force-finalize the attempt directly
    from server.scheduler import scheduler
    scheduler._finalize(att, rc=1, note="killed")
    assert len(_pending(c)) == 0
    assert att["id"] not in [a["attempt_id"] for a in _pending(c)]


def test_expire_for_attempt_returns_count(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _gated(c, pid, "count")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    assert broker.expire_for_attempt(att["id"]) == 1
    assert broker.expire_for_attempt(att["id"]) == 0   # idempotent


def test_diffs_isolated_per_database(seeded, tmp_path):
    """A mock/test run must NOT write into another database's diff store —
    attempt ids collide across databases and would clobber real diffs (they did:
    running the suite once overwrote production attempt-N.patch with MOCK_DIFF)."""
    from server import config
    c, pid = seeded["client"], seeded["project_id"]
    # this test's DB lives under tmp_path (conftest), so diffs must too
    assert str(config.diff_dir()).startswith(str(tmp_path))
    assert config.diff_dir() != config.ROOT / "diffs"

    t = c.post("/api/tasks", json={"project_id": pid, "title": "iso",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    # the patch landed in the isolated store, not the repo's real one
    patches = list(config.diff_dir().glob("*.patch"))
    assert patches and all(str(p).startswith(str(tmp_path)) for p in patches)


def test_delete_task_removes_all_traces(seeded):
    """Deleting a task must leave no orphaned attempts/events/approvals/memories/
    diff files (previously there was no delete at all; tasks accumulated forever)."""
    from server import config
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "delete me",
                                   "prompt": "x [mock:note]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    diff_file = config.diff_dir() / f"attempt-{att['id']}.patch"
    assert diff_file.exists()
    assert db.query("SELECT id FROM events WHERE attempt_id=?", (att["id"],))
    assert db.query("SELECT id FROM memories WHERE created_by_attempt=?", (att["id"],))

    r = c.request("DELETE", f"/api/tasks/{t['id']}")
    assert r.status_code == 200 and r.json()["deleted"] == t["id"]

    assert c.get(f"/api/tasks/{t['id']}").status_code == 404
    assert db.one("SELECT id FROM attempts WHERE id=?", (att["id"],)) is None
    assert not db.query("SELECT id FROM events WHERE attempt_id=?", (att["id"],))
    assert not db.query("SELECT id FROM approvals WHERE attempt_id=?", (att["id"],))
    assert not db.query("SELECT id FROM memories WHERE created_by_attempt=?", (att["id"],))
    assert not diff_file.exists()
    assert c.request("DELETE", "/api/tasks/99999").status_code == 404


def test_delete_running_task_cancels_first(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "delete running",
                                   "prompt": "x [mock:slow]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "running",
             msg="running")
    r = c.request("DELETE", f"/api/tasks/{t['id']}")
    assert r.status_code == 200
    assert c.get(f"/api/tasks/{t['id']}").status_code == 404


def test_delete_orphans_children_not_cascade(seeded):
    """Agent-filed follow-ups may be real work — deleting the parent orphans them
    (parent_task_id NULL), it does not delete them."""
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "parent",
                                   "prompt": "x [mock:subtask]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review")
    child = next(x for x in c.get("/api/tasks").json()
                 if x["parent_task_id"] == t["id"])
    c.request("DELETE", f"/api/tasks/{t['id']}")
    still = c.get(f"/api/tasks/{child['id']}").json()
    assert still["id"] == child["id"]
    assert still["parent_task_id"] is None


def test_store_events_survives_concurrent_delete(seeded):
    """A task deleted mid-poll must not crash the scheduler with a FOREIGN KEY
    error (which aborted the whole tick and stalled every other running task).
    _store_events must no-op for a vanished attempt."""
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "racer",
                                   "prompt": "x [mock:slow]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "running",
             msg="running")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    # simulate the delete winning the race: rows gone, scheduler still holds `att`
    db.execute("DELETE FROM events WHERE attempt_id=?", (att["id"],))
    db.execute("DELETE FROM attempts WHERE id=?", (att["id"],))
    db.execute("DELETE FROM tasks WHERE id=?", (t["id"],))
    from server.scheduler import scheduler
    # this used to raise sqlite3.IntegrityError (FK) and abort the tick
    scheduler._store_events(att, [{"type": "text", "payload": {"text": "late event"}}])
    # no orphan event was written for the deleted attempt
    assert not db.query("SELECT id FROM events WHERE attempt_id=?", (att["id"],))


def test_token_mode_blocks_agent_self_approval(tmp_path, monkeypatch):
    """Security: an agent knows the base URL and its per-attempt hook token. In
    token mode it must NOT be able to reach the human decision endpoint to
    self-approve its own gated action — while hook endpoints stay reachable.
    (In 'none' mode a LAN agent CAN self-approve; that's the documented trust
    boundary — see DESIGN §4.7.)"""
    from fastapi.testclient import TestClient

    from server import config
    from server.app import create_app
    monkeypatch.setattr(config, "DB_PATH", tmp_path / "auth.db")
    monkeypatch.setattr(config, "AUTH_TOKEN", "humanonly")
    with TestClient(create_app()) as c:
        # agent (no human bearer) is blocked from the approval surfaces
        assert c.get("/api/approvals?status=pending").status_code == 401
        assert c.post("/api/approvals/1/decision",
                      json={"decision": "approved"}).status_code == 401
        # but the hook path is exempt (agent reaches it; 403/422, never 401)
        assert c.post("/api/hook/approval", json={"token": "bad", "tool_name": "Bash",
                                                  "tool_input": {}}).status_code in (403, 422)
        # the human, with the bearer, gets through
        assert c.get("/api/approvals?status=pending",
                     headers={"Authorization": "Bearer humanonly"}).status_code == 200


def test_read_file_uses_fast_tail_not_dd():
    """dd bs=1 was O(bytes) syscalls — a long agent log re-read every poll would
    crawl. Confirm both remote executors emit a byte-accurate tail command."""
    ssh = SSHExecutor(host="h")
    pct = PctExecutor(vmid="9")
    seen = {}

    async def fake_run(cmd, cwd="", timeout=120):
        seen["cmd"] = cmd
        from server.executor.base import ExecResult
        return ExecResult(0, "data", "")

    ssh.run = fake_run
    import asyncio
    asyncio.get_event_loop().run_until_complete(ssh.read_file("/log", offset=100))
    assert "tail -c +101" in seen["cmd"] and "dd " not in seen["cmd"]

    async def fake_run2(cmd, cwd="", timeout=120):
        seen["cmd2"] = cmd
        from server.executor.base import ExecResult
        return ExecResult(0, "data", "")
    pct._local.run = fake_run2
    asyncio.get_event_loop().run_until_complete(pct.read_file("/log", offset=0))
    assert "tail -c +1" in seen["cmd2"]
