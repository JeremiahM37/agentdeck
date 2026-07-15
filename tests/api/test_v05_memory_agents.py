"""v0.5: shared project memory + agent adapter validation."""
from server import db
from server.scheduler import build_notes_prefix
from tests.conftest import wait_for


def _task(c, tid):
    return c.get(f"/api/tasks/{tid}").json()


def test_agent_leaves_note_and_next_prompt_includes_it(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    t = c.post("/api/tasks", json={"project_id": pid, "title": "noter",
                                   "prompt": "do work [mock:note]"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: _task(c, t["id"])["status"] == "review", msg="review")
    notes = c.get(f"/api/projects/{pid}/notes").json()
    assert len(notes) == 1 and "bcrypt" in notes[0]["note"]

    # a second task's prompt file must carry the memory prefix
    t2 = c.post("/api/tasks", json={"project_id": pid, "title": "reader",
                                    "prompt": "second job"}).json()
    c.post(f"/api/tasks/{t2['id']}/dispatch", json={})
    wait_for(lambda: _task(c, t2["id"])["status"] == "review", msg="review 2")
    from server.executor import _cache
    mock = next(iter(_cache.values()))
    prompts = [v.decode() for k, v in mock.fs.items()
               if k.endswith("/prompt.md") and "second job" in v.decode()]
    assert prompts and "Project memory" in prompts[0] and "bcrypt" in prompts[0]

    # delete removes it from the API
    c.delete(f"/api/projects/{pid}/notes/{notes[0]['id']}")
    assert c.get(f"/api/projects/{pid}/notes").json() == []


def test_note_hook_auth_and_validation(client):
    assert client.post("/api/hook/notes",
                       json={"token": "bogus", "note": "x"}).status_code == 403


def test_notes_prefix_builder():
    p = build_notes_prefix(["newest", "older"])
    assert p.index("older") < p.index("newest")   # chronological order
    assert p.startswith("## Project memory")


def test_gated_mode_rejected_for_non_claude(seeded):
    c, pid = seeded["client"], seeded["project_id"]
    r = c.post("/api/tasks", json={"project_id": pid, "title": "x",
                                   "agent": "gemini", "permission_mode": "default"})
    assert r.status_code == 400 and "gated" in r.json()["detail"]
    r = c.post("/api/tasks", json={"project_id": pid, "title": "x",
                                   "agent": "cursor"})
    assert r.status_code == 422   # unknown agent rejected by schema


def test_pct_target_kind_accepted(client):
    r = client.post("/api/targets", json={"name": "lxc-105", "kind": "pct",
                                          "host": "105"})
    assert r.status_code == 201
    assert client.post("/api/targets", json={"name": "bad", "kind": "warp"}).status_code == 422
