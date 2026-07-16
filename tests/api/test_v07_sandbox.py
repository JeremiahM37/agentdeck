"""v0.7 ephemeral sandbox lifecycle (mock pct commands, real orchestration)."""
from server import db
from server import executor as executor_pkg
from tests.conftest import wait_for


def _task(c, tid):
    return c.get(f"/api/tasks/{tid}").json()


def _mock():
    return next(iter(executor_pkg._cache.values()))


def _sandbox_project(c, name, repo="https://github.com/user/demo.git"):
    tid = c.post("/api/targets", json={"name": f"sb-{name}", "kind": "sandbox",
                                       "host": "110", "sandbox": True}).json()["id"]
    return c.post("/api/projects", json={"name": name, "target_id": tid,
                                         "repo_path": repo}).json()


def test_sandbox_full_lifecycle(client):
    p = _sandbox_project(client, "sbdemo")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "sb run",
                                        "prompt": "do it",
                                        "permission_mode": "bypassPermissions"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review")

    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    assert att["sandbox_vmid"] == "9001"
    log = _mock().cmd_log
    assert any(c.startswith("sudo pct clone 110 9001") for c in log)
    assert any(c.startswith("sudo pct start 9001") for c in log)
    # auth is provisioned into the container before launch (writes ~/.claude creds)
    assert any(".claude/.credentials.json" in c for c in log)
    assert any(c.startswith("git clone --branch main https://github.com/user/demo.git "
                            "/root/work/sbdemo") for c in log)
    # destroyed right after finalize — diff/events already captured
    assert any(c.startswith("sudo pct destroy 9001") for c in log)
    assert _task(client, t["id"])["attempt"]["worktree_path"] == ""
    # diff still reviewable from the control plane copy
    d = client.get(f"/api/tasks/{t['id']}/diff")
    assert d.status_code == 200 and d.json()["files"]


def test_sandbox_cancel_destroys_container(client):
    p = _sandbox_project(client, "sbcancel")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "sb cancel",
                                        "prompt": "x [mock:slow]"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "running", msg="running")
    client.post(f"/api/tasks/{t['id']}/cancel")
    wait_for(lambda: any(c.startswith("sudo pct destroy 9001")
                         for c in _mock().cmd_log), msg="container destroyed")
    assert _task(client, t["id"])["status"] == "cancelled"


def test_sandbox_followup_gets_fresh_container_no_resume(client):
    p = _sandbox_project(client, "sbfollow")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "sb follow",
                                        "prompt": "first pass"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review 1")
    r = client.post(f"/api/tasks/{t['id']}/followup", json={"feedback": "also add tests"})
    assert r.status_code == 200
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review 2")
    a2 = db.one("SELECT * FROM attempts WHERE task_id=? AND n=2", (t["id"],))
    assert a2["resume_session"] == ""             # old container is gone
    assert "REQUESTED CHANGES" in a2["prompt"]
    assert "first pass" in a2["prompt"]           # context carried in the prompt


def test_sandbox_skips_reviewer_gate(client):
    tid = client.post("/api/targets", json={"name": "sb-gated", "kind": "sandbox",
                                            "host": "110"}).json()["id"]
    p = client.post("/api/projects", json={"name": "sbgated", "target_id": tid,
                                           "repo_path": "https://github.com/u/r.git",
                                           "review_gate": True}).json()
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "gated sb",
                                        "prompt": "x [mock:approve-verdict]"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review")
    assert not [x for x in client.get("/api/tasks").json()
                if x["created_by"] == "reviewer-gate" and x["parent_task_id"] == t["id"]]


def test_sandbox_destroyed_on_unexpected_launch_error(client, monkeypatch):
    """A failure AFTER the container exists (not a clean ExecutorError) must still
    destroy it — otherwise the generic handler marks the attempt failed and leaks
    the LXC."""
    p = _sandbox_project(client, "sbleak")
    from server.scheduler import scheduler
    orig = scheduler._launch_sandbox_inner

    async def boom(att, ctx, host, vmid):
        raise RuntimeError("unexpected explosion mid-launch")
    monkeypatch.setattr(scheduler, "_launch_sandbox_inner", boom)

    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "leaky",
                                        "prompt": "x"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "failed", msg="failed")
    # the container that provision created was destroyed despite the crash
    assert any(c.startswith("sudo pct destroy 9001") for c in _mock().cmd_log)
    monkeypatch.setattr(scheduler, "_launch_sandbox_inner", orig)


def test_template_path_repo_skips_clone(client):
    p = _sandbox_project(client, "sbbaked", repo="/root/adk-demo")
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "baked repo",
                                        "prompt": "y"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review")
    log = _mock().cmd_log
    assert not any(c.startswith("git clone") for c in log)
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    assert att["branch"].startswith("adk/task")
