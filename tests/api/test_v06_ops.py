"""v0.6 ops hardening: worktree janitor + deep creds probe."""
from server import db
from tests.conftest import wait_for


def _task(c, tid):
    return c.get(f"/api/tasks/{tid}").json()


def _finished_task(c, pid, title):
    t = c.post("/api/tasks", json={"project_id": pid, "title": title,
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    c.post(f"/api/tasks/{t['id']}/complete")
    return t


def test_janitor_sweeps_old_done_worktrees(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _finished_task(c, pid, "old done")
    fresh = _finished_task(c, pid, "fresh done")
    # backdate only the first
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    db.update("attempts", att["id"], {"finished_at": db.now() - 30 * 86400})

    r = c.post("/api/admin/janitor", json={"days": 7})
    assert r.status_code == 200
    assert att["id"] in r.json()["removed_attempts"]
    assert _task(c, t["id"])["attempt"]["worktree_path"] == ""
    assert _task(c, fresh["id"])["attempt"]["worktree_path"] != ""


def test_janitor_respects_keep_worktrees(client):
    tid = client.get("/api/targets").json()[0]["id"]
    p = client.post("/api/projects", json={"name": "keeper", "target_id": tid,
                                           "repo_path": "/mock/keeper",
                                           "keep_worktrees": True}).json()
    t = _finished_task(client, p["id"], "kept")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    db.update("attempts", att["id"], {"finished_at": db.now() - 30 * 86400})
    r = client.post("/api/admin/janitor", json={"days": 7})
    assert att["id"] not in r.json()["removed_attempts"]
    assert _task(client, t["id"])["attempt"]["worktree_path"] != ""


def test_janitor_ignores_review_tasks(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "in review",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    att = db.one("SELECT * FROM attempts WHERE task_id=?", (t["id"],))
    db.update("attempts", att["id"], {"finished_at": db.now() - 30 * 86400})
    r = c.post("/api/admin/janitor", json={"days": 7})
    assert att["id"] not in r.json()["removed_attempts"]


def test_deep_probe_reports_claude_auth(client):
    tid = client.post("/api/targets", json={"name": "deep", "kind": "mock"}).json()["id"]
    r = client.post(f"/api/targets/{tid}/check?deep=true").json()
    assert '"claude_auth": "ok"' in r["info_json"]
    # shallow probe doesn't spend tokens
    r = client.post(f"/api/targets/{tid}/check").json()
    assert "claude_auth" not in r["info_json"] or r["info_json"].count("claude_auth") <= 1
