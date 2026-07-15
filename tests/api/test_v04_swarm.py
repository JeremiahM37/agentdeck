"""v0.4 swarm-lite: agents filing tasks, reviewer gates, A/B attempts."""
from server import db
from tests.conftest import wait_for


def _task(c, tid):
    return c.get(f"/api/tasks/{tid}").json()


def _mk(c, pid, prompt, **kw):
    t = c.post("/api/tasks", json={"project_id": pid, "title": kw.pop("title", "t"),
                                   "prompt": prompt, **kw}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json=kw.pop("dispatch_body", {}))
    return t


def test_agent_files_followup_card(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _mk(c, pid, "build it [mock:subtask]", title="spawner")
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    filed = [x for x in c.get("/api/tasks").json()
             if x["created_by"] == "agent" and x["parent_task_id"] == t["id"]]
    assert len(filed) == 1
    assert filed[0]["title"] == "Agent follow-up: add tests"
    assert filed[0]["status"] == "backlog"       # dispatch=False → parked for human


def test_hook_tasks_auth_and_cap(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    assert c.post("/api/hook/tasks", json={"token": "bogus", "title": "x"}).status_code == 403
    t = _mk(c, pid, "long one [mock:slow]", title="cap host")
    wait_for(lambda: _task(c, t["id"])["status"] == "running", msg="running")
    token = db.one("SELECT token FROM attempts WHERE task_id=?", (t["id"],))["token"]
    for i in range(10):
        r = c.post("/api/hook/tasks", json={"token": token, "title": f"sub {i}",
                                            "dispatch": i == 0})
        assert r.status_code == 201
    assert c.post("/api/hook/tasks",
                  json={"token": token, "title": "over cap"}).status_code == 429
    subs = [x for x in c.get("/api/tasks").json() if x["parent_task_id"] == t["id"]]
    assert len(subs) == 10
    dispatched = [s for s in subs if s["title"] == "sub 0"][0]
    assert dispatched["status"] in ("queued", "running", "review")


def _gated_project(c, name):
    tid = c.get("/api/targets").json()[0]["id"]
    return c.post("/api/projects", json={"name": name, "target_id": tid,
                                         "repo_path": f"/mock/{name}",
                                         "review_gate": True}).json()


def test_reviewer_gate_approve(client):
    p = _gated_project(client, "gated-ok")
    t = _mk(client, p["id"], "change stuff [mock:approve-verdict]", title="gated work")
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="parent review")
    reviewer = wait_for(lambda: next(
        (x for x in client.get("/api/tasks").json()
         if x["created_by"] == "reviewer-gate" and x["parent_task_id"] == t["id"]), None),
        msg="reviewer spawned")
    assert reviewer["permission_mode"] == "plan"
    wait_for(lambda: _task(client, reviewer["id"])["status"] == "done",
             msg="reviewer auto-done")
    parent = _task(client, t["id"])
    assert parent["attempt"]["result"]["review"]["verdict"] == "APPROVE"
    # reviewer shared the parent's worktree
    r_att = db.one("SELECT * FROM attempts WHERE task_id=?", (reviewer["id"],))
    assert r_att["worktree_path"] == parent["attempt"]["worktree_path"]
    evs = client.get(f"/api/tasks/{t['id']}/events").json()
    assert any(e["type"] == "review_verdict" and e["payload"]["verdict"] == "APPROVE"
               for e in evs)


def test_reviewer_gate_request_changes(client):
    p = _gated_project(client, "gated-no")
    t = _mk(client, p["id"], "sloppy change [mock:reject-verdict]", title="rejected work")
    wait_for(lambda: (_task(client, t["id"]).get("attempt") or {}).get("result", {})
             .get("review", {}).get("verdict") == "REQUEST_CHANGES",
             msg="REQUEST_CHANGES verdict on parent")
    # no reviewer-of-reviewer recursion
    reviewers = [x for x in client.get("/api/tasks").json()
                 if x["created_by"] == "reviewer-gate" and x["project_id"] == p["id"]]
    assert len(reviewers) == 1


def test_ab_parallel_attempts(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "AB race",
                                   "prompt": "try it", "model": "sonnet"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={"model_b": "opus"})
    wait_for(lambda: _task(c, t["id"])["status"] == "review",
             msg="review after both attempts")
    got = _task(c, t["id"])
    assert [a["n"] for a in got["attempts"]] == [1, 2]
    assert got["attempts"][1]["model"] == "opus"
    assert all(a["status"] == "done" for a in got["attempts"])
    # per-attempt diff + events
    for n in (1, 2):
        d = c.get(f"/api/tasks/{t['id']}/diff?attempt_n={n}")
        assert d.status_code == 200 and d.json()["attempt_n"] == n
        evs = c.get(f"/api/tasks/{t['id']}/events?attempt_n={n}").json()
        assert evs and all(e["attempt_n"] == n for e in evs)


def test_ab_stays_running_until_both_land(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "AB slow",
                                   "prompt": "x [mock:slow]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={"model_b": "haiku"})
    wait_for(lambda: _task(c, t["id"])["status"] == "running", msg="running")
    # while ANY attempt is unfinished the task must not leave running
    got = _task(c, t["id"])
    unfinished = [a for a in got["attempts"] if a["status"] in ("queued", "running")]
    if unfinished:
        assert got["status"] == "running"
    wait_for(lambda: _task(c, t["id"])["status"] == "review", timeout=30, msg="review")
