"""Full task lifecycle through the REST API with the mock target."""
from tests.conftest import wait_for


def _make_task(c, project_id, prompt="Do the thing.", **kw):
    body = {"project_id": project_id, "title": kw.pop("title", "Test task"),
            "prompt": prompt, **kw}
    r = c.post("/api/tasks", json=body)
    assert r.status_code == 201, r.text
    return r.json()


def _status(c, tid):
    return c.get(f"/api/tasks/{tid}").json()["status"]


def test_health(client):
    r = client.get("/api/health")
    assert r.status_code == 200 and r.json()["ok"] and r.json()["mock"]
    # flat, always-present counts for dashboard widgets (Homepage customapi tile)
    for f in ("running", "queued", "review", "pending_approvals"):
        assert f in r.json() and isinstance(r.json()[f], int), f


def test_crud_and_board(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_task(c, pid, title="CRUD check", priority=3)
    assert t["status"] == "backlog" and t["priority"] == 3
    assert t["project_name"] and t["target_name"]

    r = c.patch(f"/api/tasks/{t['id']}", json={"title": "renamed", "priority": 1})
    assert r.json()["title"] == "renamed"

    # illegal board move rejected
    r = c.patch(f"/api/tasks/{t['id']}", json={"status": "review"})
    assert r.status_code == 409
    # queueing must go through /dispatch
    r = c.patch(f"/api/tasks/{t['id']}", json={"status": "queued"})
    assert r.status_code == 409

    assert any(x["id"] == t["id"] for x in c.get("/api/tasks?status=backlog").json())


def test_happy_path_to_done(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_task(c, pid, title="Happy path")
    r = c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    assert r.status_code == 200 and r.json()["status"] == "queued"

    wait_for(lambda: _status(c, t["id"]) == "review", msg="review")
    task = c.get(f"/api/tasks/{t['id']}").json()
    assert task["attempt"]["n"] == 1
    assert task["attempt"]["exit_code"] == 0
    assert task["attempt"]["result"]["cost_usd"] == 0.0123
    assert task["attempt"]["branch"].startswith("adk/task")
    assert task["attempt"]["worktree_path"]

    evs = c.get(f"/api/tasks/{t['id']}/events").json()
    types = [e["type"] for e in evs]
    for expected in ("init", "text", "tool_use", "tool_result", "result"):
        assert expected in types, types
    assert all(e["attempt_n"] == 1 for e in evs)

    d = c.get(f"/api/tasks/{t['id']}/diff").json()
    assert d["files"][0]["path"] == "app.py"
    assert d["stats"][0]["additions"] == 4 and d["stats"][0]["deletions"] == 1
    assert "+    print(\"hello, agentdeck\")" in d["files"][0]["patch"]

    assert c.post(f"/api/tasks/{t['id']}/complete").json()["status"] == "done"
    # done is terminal
    assert c.post(f"/api/tasks/{t['id']}/dispatch", json={}).status_code == 409


def test_failure_and_retry(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_task(c, pid, prompt="break [mock:fail]", title="Fails")
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _status(c, t["id"]) == "failed", msg="failed")
    task = c.get(f"/api/tasks/{t['id']}").json()
    assert task["attempt"]["exit_code"] == 1

    # retry spawns attempt 2
    assert c.post(f"/api/tasks/{t['id']}/dispatch", json={}).status_code == 200
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["attempt"]["n"] == 2,
             msg="attempt 2")


def test_cancel_running(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_task(c, pid, prompt="take a while [mock:slow]", title="Cancel me")
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _status(c, t["id"]) == "running", msg="running")
    r = c.post(f"/api/tasks/{t['id']}/cancel")
    assert r.status_code == 200 and r.json()["status"] == "cancelled"


def test_followup_resumes_in_same_worktree(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_task(c, pid, title="Follow-up flow")
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _status(c, t["id"]) == "review", msg="review")
    first = c.get(f"/api/tasks/{t['id']}").json()["attempt"]

    r = c.post(f"/api/tasks/{t['id']}/followup", json={"feedback": "Also rename health to status"})
    assert r.status_code == 200 and r.json()["status"] == "queued"
    wait_for(lambda: _status(c, t["id"]) == "review", msg="review after follow-up")
    second = c.get(f"/api/tasks/{t['id']}").json()["attempt"]
    assert second["n"] == 2
    assert second["worktree_path"] == first["worktree_path"]
    assert second["branch"] == first["branch"]
    # attempt 2 must have run its OWN session in the reused worktree — a stale
    # exit_code/events.jsonl from attempt 1 once caused instant bogus finalize
    evs2 = c.get(f"/api/tasks/{t['id']}/events?attempt_n=2").json()
    types2 = [e["type"] for e in evs2]
    assert "init" in types2 and "result" in types2, types2
    assert second["exit_code"] == 0


def test_validation_errors(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    assert c.post("/api/tasks", json={"project_id": 999, "title": "x"}).status_code == 400
    assert c.get("/api/tasks/9999").status_code == 404
    assert c.post("/api/tasks/9999/dispatch", json={}).status_code == 404
    t = _make_task(c, pid, title="No diff yet")
    assert c.get(f"/api/tasks/{t['id']}/diff").status_code == 404
    assert c.post(f"/api/tasks/{t['id']}/followup",
                  json={"feedback": "x"}).status_code == 409
