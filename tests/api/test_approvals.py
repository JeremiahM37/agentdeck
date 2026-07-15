"""Approval round-trip: mock agent calls the REAL hook endpoints via ASGI."""
from tests.conftest import wait_for


def _make_gated(c, pid, title):
    r = c.post("/api/tasks", json={
        "project_id": pid, "title": title,
        "prompt": "deploy it [mock:approval]", "permission_mode": "default"})
    t = r.json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    return t


def _pending(c):
    return c.get("/api/approvals?status=pending").json()


def test_approve_lets_agent_finish(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_gated(c, pid, "Gated approve")
    appr = wait_for(lambda: next((a for a in _pending(c) if a["task_id"] == t["id"]), None),
                    msg="pending approval")
    assert appr["tool_name"] == "Bash"
    assert "rm -rf build/" in appr["input"]["command"]
    assert appr["task_title"] == "Gated approve"

    r = c.post(f"/api/approvals/{appr['id']}/decision", json={"decision": "approved"})
    assert r.status_code == 200 and r.json()["status"] == "approved"
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="review after approval")
    evs = [e["type"] for e in c.get(f"/api/tasks/{t['id']}/events").json()]
    assert "tool_use" in evs and "result" in evs


def test_deny_stops_agent(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_gated(c, pid, "Gated deny")
    appr = wait_for(lambda: next((a for a in _pending(c) if a["task_id"] == t["id"]), None),
                    msg="pending approval")
    c.post(f"/api/approvals/{appr['id']}/decision",
           json={"decision": "denied", "note": "too risky"})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "review",
             msg="agent stopped cleanly")
    evs = c.get(f"/api/tasks/{t['id']}/events").json()
    assert any("denied" in (e["payload"].get("text") or "").lower() for e in evs
               if e["type"] == "text")
    # decision recorded
    row = c.get("/api/approvals?status=denied").json()[0]
    assert row["note"] == "too risky"


def test_hook_endpoint_rejects_bad_token(client):
    r = client.post("/api/hook/approval", json={
        "token": "nope", "tool_name": "Bash", "tool_input": {}})
    assert r.status_code == 403


def test_double_decision_conflicts(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = _make_gated(c, pid, "Gated double")
    appr = wait_for(lambda: next((a for a in _pending(c) if a["task_id"] == t["id"]), None),
                    msg="pending approval")
    assert c.post(f"/api/approvals/{appr['id']}/decision",
                  json={"decision": "approved"}).status_code == 200
    assert c.post(f"/api/approvals/{appr['id']}/decision",
                  json={"decision": "denied"}).status_code == 409
    assert c.post("/api/approvals/9999/decision",
                  json={"decision": "approved"}).status_code == 409
