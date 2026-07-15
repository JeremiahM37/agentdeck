"""Approvals: operator-facing list/decision + agent-facing hook endpoints."""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from .. import broker, config, db, policy

router = APIRouter(prefix="/api")


@router.get("/approvals")
def list_approvals(status: str = "pending"):
    rows = db.query(
        "SELECT a.*, at.task_id, at.n AS attempt_n, t.title AS task_title "
        "FROM approvals a JOIN attempts at ON at.id=a.attempt_id "
        "JOIN tasks t ON t.id=at.task_id "
        "WHERE a.status=? ORDER BY a.created_at DESC LIMIT 200", (status,))
    return [{**r, "input": db.unj(r.pop("input_json"))} for r in rows]


class DecisionIn(BaseModel):
    decision: str   # approved | denied
    note: str = ""
    always_allow: bool = False


@router.post("/approvals/{approval_id}/decision")
def decide(approval_id: int, body: DecisionIn):
    if body.decision not in ("approved", "denied"):
        raise HTTPException(400, "decision must be approved|denied")
    row = broker.decide(approval_id, body.decision, note=body.note)
    if row is None:
        raise HTTPException(409, "approval not pending")
    if body.always_allow and body.decision == "approved":
        proj = db.one(
            "SELECT p.* FROM projects p JOIN tasks t ON t.project_id=p.id "
            "JOIN attempts a ON a.task_id=t.id WHERE a.id=?", (row["attempt_id"],))
        if proj:
            rule = policy.pattern_for(row["tool_name"], db.unj(row["input_json"]))
            updated = policy.add_rule(db.unj(proj["policy_json"]), rule)
            db.update("projects", proj["id"], {"policy_json": db.j(updated)})
    return {**row, "input": db.unj(row.pop("input_json"))}


# ---- agent-facing (per-attempt token auth, exempt from API bearer auth) ------

class HookIn(BaseModel):
    token: str
    tool_name: str
    tool_input: dict = {}


@router.post("/hook/approval", status_code=201)
def hook_create(body: HookIn):
    att = db.one("SELECT * FROM attempts WHERE token=?", (body.token,))
    if not att or att["status"] != "running":
        raise HTTPException(403, "invalid attempt token")
    proj = db.one(
        "SELECT p.* FROM projects p JOIN tasks t ON t.project_id=p.id "
        "WHERE t.id=?", (att["task_id"],))
    if proj and policy.matches(db.unj(proj["policy_json"]), body.tool_name, body.tool_input):
        aid = broker.create(att["id"], body.tool_name, body.tool_input, quiet=True)
        broker.decide(aid, "approved", note="matched always-allow rule",
                      decided_by="policy")
        return {"id": aid}
    aid = broker.create(att["id"], body.tool_name, body.tool_input)
    return {"id": aid}


@router.get("/hook/approval/{approval_id}/decision")
async def hook_decision(approval_id: int):
    row = await broker.wait(approval_id, timeout=config.APPROVAL_POLL_SECONDS)
    return {"status": row.get("status", "unknown"), "note": row.get("note", "")}


AGENT_TASK_CAP = 10


class HookTaskIn(BaseModel):
    token: str
    title: str
    prompt: str = ""
    dispatch: bool = False
    priority: int = 2


@router.post("/hook/tasks", status_code=201)
def hook_file_task(body: HookTaskIn):
    """An agent files a follow-up card. Capped per attempt to stop runaway loops."""
    from ..bus import bus
    from ..scheduler import create_attempt
    att = db.one("SELECT * FROM attempts WHERE token=?", (body.token,))
    if not att or att["status"] != "running":
        raise HTTPException(403, "invalid attempt token")
    filed = db.one("SELECT COUNT(*) c FROM tasks WHERE created_by_attempt=?",
                   (att["id"],))["c"]
    if filed >= AGENT_TASK_CAP:
        raise HTTPException(429, f"attempt already filed {AGENT_TASK_CAP} tasks")
    parent = db.one("SELECT * FROM tasks WHERE id=?", (att["task_id"],))
    tid = db.insert("tasks", {
        "project_id": parent["project_id"], "title": body.title[:120],
        "prompt": body.prompt or body.title,
        "status": "queued" if body.dispatch else "backlog",
        "priority": max(0, min(4, body.priority)),
        "permission_mode": parent["permission_mode"],
        "created_by": "agent", "parent_task_id": parent["id"],
        "created_by_attempt": att["id"],
        "created_at": db.now(), "updated_at": db.now()})
    task = db.one("SELECT * FROM tasks WHERE id=?", (tid,))
    if body.dispatch:
        create_attempt(task)
    bus.publish("board", "task", task)
    return {"task_id": tid}


NOTE_CAP = 20


class HookNoteIn(BaseModel):
    token: str
    note: str


@router.post("/hook/notes", status_code=201)
def hook_add_note(body: HookNoteIn):
    """Agent leaves a durable project note — injected into future dispatch prompts."""
    att = db.one("SELECT * FROM attempts WHERE token=?", (body.token,))
    if not att or att["status"] != "running":
        raise HTTPException(403, "invalid attempt token")
    if not body.note.strip():
        raise HTTPException(400, "empty note")
    written = db.one("SELECT COUNT(*) c FROM memories WHERE created_by_attempt=?",
                     (att["id"],))["c"]
    if written >= NOTE_CAP:
        raise HTTPException(429, f"attempt already wrote {NOTE_CAP} notes")
    proj = db.one("SELECT p.id FROM projects p JOIN tasks t ON t.project_id=p.id "
                  "WHERE t.id=?", (att["task_id"],))
    nid = db.insert("memories", {"project_id": proj["id"], "note": body.note[:1000],
                                 "created_by_attempt": att["id"],
                                 "created_at": db.now()})
    return {"note_id": nid}
