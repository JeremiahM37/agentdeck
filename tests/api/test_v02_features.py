"""v0.2 features: auto-verify, commit/push/PR, cleanup, policy, concurrency, unicode."""
from tests.conftest import wait_for


def _project(c, name, **kw):
    tid = c.get("/api/targets").json()[0]["id"]
    return c.post("/api/projects", json={"name": name, "target_id": tid,
                                         "repo_path": f"/mock/{name}", **kw}).json()


def _run_task(c, pid, prompt="do it", **kw):
    t = c.post("/api/tasks", json={"project_id": pid, "title": kw.pop("title", "t"),
                                   "prompt": prompt, **kw}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    return t


def _task(c, tid):
    return c.get(f"/api/tasks/{tid}").json()


def test_auto_verify_pass(client):
    p = _project(client, "verpass", verify_cmd="mockverify-pass")
    t = _run_task(client, p["id"])
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review")
    v = _task(client, t["id"])["attempt"]["verify"]
    assert v["rc"] == 0 and "5 passed" in v["output"]
    evs = client.get(f"/api/tasks/{t['id']}/events").json()
    assert any(e["type"] == "verify" and e["payload"]["rc"] == 0 for e in evs)


def test_auto_verify_fail_still_reviewable(client):
    p = _project(client, "verfail", verify_cmd="mockverify-fail")
    t = _run_task(client, p["id"])
    wait_for(lambda: _task(client, t["id"])["status"] == "review", msg="review")
    v = _task(client, t["id"])["attempt"]["verify"]
    assert v["rc"] == 1 and "2 failed" in v["output"]


def test_commit_push_pr(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _run_task(c, pid, title="Commit me")
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    r = c.post(f"/api/tasks/{t['id']}/commit",
               json={"message": "feat: thing", "push": True, "pr": True})
    assert r.status_code == 200
    steps = {s["step"]: s for s in r.json()["steps"]}
    assert steps["commit"]["rc"] == 0
    assert steps["push"]["rc"] == 0
    assert steps["pr"]["url"] == "https://github.com/mock/repo/pull/7"


def test_commit_requires_worktree_and_review(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "no wt"}).json()
    assert c.post(f"/api/tasks/{t['id']}/commit", json={}).status_code == 409


def test_cleanup_after_done(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _run_task(c, pid, title="Clean me")
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    # not allowed while in review
    assert c.post(f"/api/tasks/{t['id']}/cleanup").status_code == 409
    c.post(f"/api/tasks/{t['id']}/complete")
    r = c.post(f"/api/tasks/{t['id']}/cleanup")
    assert r.status_code == 200 and r.json()["removed_attempts"] == [1]
    assert _task(c, t["id"])["attempt"]["worktree_path"] == ""


def test_policy_auto_approves(client):
    p = _project(client, "policied")
    client.patch(f"/api/projects/{p['id']}",
                 json={"policy": {"allow": [{"tool": "Bash", "prefix": "rm"}]}})
    t = _run_task(client, p["id"], prompt="deploy [mock:approval]",
                  permission_mode="default", title="policy run")
    # completes without any human decision (mock approval command starts with 'rm')
    wait_for(lambda: _task(client, t["id"])["status"] == "review",
             msg="review via policy auto-approval")
    rows = client.get("/api/approvals?status=approved").json()
    mine = [r for r in rows if r["task_id"] == t["id"]]
    assert mine and mine[0]["decided_by"] == "policy"
    assert client.get("/api/approvals?status=pending").json() == []


def test_always_allow_teaches_policy(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t1 = _run_task(c, pid, prompt="x [mock:approval]", permission_mode="default",
                   title="teach policy")
    appr = wait_for(lambda: next((a for a in c.get("/api/approvals?status=pending").json()
                                  if a["task_id"] == t1["id"]), None), msg="pending")
    c.post(f"/api/approvals/{appr['id']}/decision",
           json={"decision": "approved", "always_allow": True})
    wait_for(lambda: _task(c, t1["id"])["status"] == "review", msg="review")
    proj = [p for p in c.get("/api/projects").json() if p["id"] == pid][0]
    assert '"prefix": "rm"' in proj["policy_json"].replace("'", '"') or \
           "rm" in proj["policy_json"]
    # second identical run sails through with no human involvement
    t2 = _run_task(c, pid, prompt="y [mock:approval]", permission_mode="default",
                   title="policy reuse")
    wait_for(lambda: _task(c, t2["id"])["status"] == "review", msg="auto review")
    assert all(a["task_id"] != t2["id"]
               for a in c.get("/api/approvals?status=pending").json())


def test_concurrency_slots_respected(client):
    tid = client.post("/api/targets", json={"name": "narrow", "kind": "mock",
                                            "max_concurrent": 1}).json()["id"]
    p = client.post("/api/projects", json={"name": "narrowp", "target_id": tid,
                                           "repo_path": "/mock/n"}).json()
    t1 = _run_task(client, p["id"], prompt="a [mock:slow]", title="slot1")
    t2 = _run_task(client, p["id"], prompt="b [mock:slow]", title="slot2")
    # at some point: one running, the other still queued
    wait_for(lambda: _task(client, t1["id"])["status"] == "running"
             or _task(client, t2["id"])["status"] == "running", msg="first running")
    statuses = {_task(client, t1["id"])["status"], _task(client, t2["id"])["status"]}
    assert "queued" in statuses, f"both ran at once: {statuses}"
    wait_for(lambda: _task(client, t1["id"])["status"] == "review"
             and _task(client, t2["id"])["status"] == "review",
             timeout=30, msg="both finish eventually")


def test_unicode_and_shell_chars_survive(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    title = "héllo 🚀 'quotes' $(no-exec) `bt`"
    t = _run_task(c, pid, prompt=f"prompt with {title}", title=title)
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    got = _task(c, t["id"])
    assert got["title"] == title
    evs = c.get(f"/api/tasks/{t['id']}/events").json()
    assert any(e["type"] == "result" for e in evs)


def test_project_patch_verify_cmd(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    r = c.patch(f"/api/projects/{pid}", json={"verify_cmd": "mockverify-pass"})
    assert r.status_code == 200 and r.json()["verify_cmd"] == "mockverify-pass"
    assert c.patch("/api/projects/9999", json={}).status_code == 404
