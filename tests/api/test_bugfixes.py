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
