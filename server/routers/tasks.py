"""Tasks: CRUD, dispatch, lifecycle actions, events, diff, SSE."""
import asyncio
from pathlib import Path

from fastapi import APIRouter, HTTPException
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from .. import config, db, state, worktree
from ..bus import bus
from ..executor import get_executor
from ..scheduler import create_attempt, scheduler

router = APIRouter(prefix="/api")


class TaskIn(BaseModel):
    project_id: int
    title: str
    prompt: str = ""
    priority: int = Field(2, ge=0, le=4)
    labels: list[str] = []
    agent: str = Field("claude", pattern="^(claude|codex|gemini)$")
    model: str = ""
    permission_mode: str = Field("acceptEdits",
                                 pattern="^(default|acceptEdits|plan|bypassPermissions)$")
    base_branch: str = ""


class TaskPatch(BaseModel):
    title: str | None = None
    prompt: str | None = None
    status: str | None = None
    priority: int | None = None
    labels: list[str] | None = None
    model: str | None = None
    permission_mode: str | None = None
    base_branch: str | None = None


def _view(task: dict) -> dict:
    att = db.one("SELECT * FROM attempts WHERE task_id=? ORDER BY n DESC LIMIT 1",
                 (task["id"],))
    proj = db.one("SELECT name, target_id FROM projects WHERE id=?", (task["project_id"],))
    tgt = db.one("SELECT name, host, user, kind FROM targets WHERE id=?",
                 (proj["target_id"],)) if proj else None
    out = dict(task)
    out["labels"] = db.unj(task["labels_json"], [])
    out["project_name"] = proj["name"] if proj else "?"
    out["target_name"] = tgt["name"] if tgt else "?"
    out["target_host"] = tgt["host"] if tgt else ""
    out["target_user"] = tgt["user"] if tgt else ""
    out["target_kind"] = tgt["kind"] if tgt else ""
    if att:
        out["attempt"] = {k: att[k] for k in
                          ("id", "n", "status", "branch", "worktree_path", "tmux_session",
                           "started_at", "finished_at", "exit_code")}
        out["attempt"]["result"] = db.unj(att["result_json"])
        out["attempt"]["diff_stat"] = db.unj(att["diff_stat_json"], [])
        out["attempt"]["verify"] = db.unj(att["verify_json"])
    out["attempts"] = [
        {"n": a["n"], "status": a["status"], "model": a["model"],
         "exit_code": a["exit_code"],
         "cost_usd": db.unj(a["result_json"]).get("cost_usd")}
        for a in db.query("SELECT * FROM attempts WHERE task_id=? ORDER BY n",
                          (task["id"],))]
    return out


@router.get("/tasks")
def list_tasks(status: str | None = None, project_id: int | None = None):
    sql, params = "SELECT * FROM tasks", []
    conds = []
    if status:
        conds.append("status=?"); params.append(status)
    if project_id:
        conds.append("project_id=?"); params.append(project_id)
    if conds:
        sql += " WHERE " + " AND ".join(conds)
    sql += " ORDER BY priority DESC, updated_at DESC"
    return [_view(t) for t in db.query(sql, tuple(params))]


@router.get("/tasks/{task_id}")
def get_task(task_id: int):
    t = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not t:
        raise HTTPException(404, "no such task")
    return _view(t)


@router.post("/tasks", status_code=201)
def create_task(t: TaskIn):
    if not db.one("SELECT id FROM projects WHERE id=?", (t.project_id,)):
        raise HTTPException(400, "no such project")
    from ..agents import GATED_CAPABLE
    if t.permission_mode == "default" and t.agent not in GATED_CAPABLE:
        raise HTTPException(400,
                            f"agent {t.agent!r} does not support gated approvals; "
                            "use acceptEdits/plan")
    data = t.model_dump()
    data["labels_json"] = db.j(data.pop("labels"))
    tid = db.insert("tasks", {**data, "status": "backlog",
                              "created_at": db.now(), "updated_at": db.now()})
    task = db.one("SELECT * FROM tasks WHERE id=?", (tid,))
    bus.publish("board", "task", task)
    return _view(task)


@router.patch("/tasks/{task_id}")
def patch_task(task_id: int, p: TaskPatch):
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    data = {k: v for k, v in p.model_dump().items() if v is not None}
    if "labels" in data:
        data["labels_json"] = db.j(data.pop("labels"))
    if "status" in data:
        try:
            state.check(task["status"], data["status"])
        except state.IllegalTransition as e:
            raise HTTPException(409, str(e))
        if data["status"] == "queued":   # moving a card into queued = dispatch
            raise HTTPException(409, "use /dispatch to queue a task")
    if data:
        data["updated_at"] = db.now()
        db.update("tasks", task_id, data)
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    bus.publish("board", "task", task)
    return _view(task)


class DispatchIn(BaseModel):
    permission_mode: str | None = None
    model: str | None = None
    model_b: str | None = None   # A/B: second parallel attempt with this model


@router.post("/tasks/{task_id}/dispatch")
def dispatch(task_id: int, body: DispatchIn | None = None):
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    try:
        state.check(task["status"], "queued")
    except state.IllegalTransition as e:
        raise HTTPException(409, str(e))
    updates = {"status": "queued", "updated_at": db.now()}
    if body and body.permission_mode:
        updates["permission_mode"] = body.permission_mode
    if body and body.model:
        updates["model"] = body.model
    db.update("tasks", task_id, updates)
    fresh = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    create_attempt(fresh)
    if body and body.model_b:
        create_attempt(fresh, model=body.model_b)
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    bus.publish("board", "task", task)
    return _view(task)


class FollowupIn(BaseModel):
    feedback: str


@router.post("/tasks/{task_id}/followup")
def followup(task_id: int, body: FollowupIn):
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    if task["status"] != "review":
        raise HTTPException(409, "follow-up only from review")
    last = db.one("SELECT * FROM attempts WHERE task_id=? ORDER BY n DESC LIMIT 1",
                  (task_id,))
    tgt = db.one("SELECT t.kind FROM targets t JOIN projects p ON p.target_id=t.id "
                 "WHERE p.id=?", (task["project_id"],))
    if tgt and tgt["kind"] == "sandbox":
        # the old container is gone — fresh sandbox, feedback carries the context
        prompt = ("A previous agent attempted this task and the operator requests "
                  f"changes:\n\nORIGINAL TASK:\n{task['prompt']}\n\n"
                  f"REQUESTED CHANGES:\n{body.feedback}")
        create_attempt(task, prompt=prompt)
    else:
        prompt = ("The previous attempt finished. The operator reviewed the diff and "
                  f"requests changes:\n\n{body.feedback}\n\nApply them in this worktree.")
        create_attempt(task, prompt=prompt,
                       resume_session=last["session_id"] if last else "",
                       worktree_path=last["worktree_path"] if last else "",
                       branch=last["branch"] if last else "")
    db.update("tasks", task_id, {"status": "queued", "updated_at": db.now()})
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    bus.publish("board", "task", task)
    return _view(task)


@router.post("/tasks/{task_id}/complete")
def complete(task_id: int):
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    try:
        state.check(task["status"], "done")
    except state.IllegalTransition as e:
        raise HTTPException(409, str(e))
    db.update("tasks", task_id, {"status": "done", "updated_at": db.now()})
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    bus.publish("board", "task", task)
    return _view(task)


@router.post("/tasks/{task_id}/cancel")
async def cancel(task_id: int):
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    if task["status"] not in ("queued", "running"):
        raise HTTPException(409, f"cannot cancel from {task['status']}")
    att = db.one("SELECT * FROM attempts WHERE task_id=? AND status IN "
                 "('queued','running') ORDER BY n DESC LIMIT 1", (task_id,))
    if att:
        await scheduler.cancel_attempt(att)
    else:
        db.update("tasks", task_id, {"status": "cancelled", "updated_at": db.now()})
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    bus.publish("board", "task", task)
    return _view(task)


def _work_ctx(task_id: int):
    """(task, project, target, latest attempt with a worktree) or 404/409."""
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    att = db.one("SELECT * FROM attempts WHERE task_id=? AND worktree_path!='' "
                 "ORDER BY n DESC LIMIT 1", (task_id,))
    if not att:
        raise HTTPException(409, "task has no worktree yet")
    project = db.one("SELECT * FROM projects WHERE id=?", (task["project_id"],))
    target = db.one("SELECT * FROM targets WHERE id=?", (project["target_id"],))
    return task, project, target, att


class CommitIn(BaseModel):
    message: str = ""
    push: bool = False
    pr: bool = False


@router.post("/tasks/{task_id}/commit")
async def commit(task_id: int, body: CommitIn):
    task, project, target, att = _work_ctx(task_id)
    if task["status"] not in ("review", "done"):
        raise HTTPException(409, f"cannot commit from {task['status']}")
    ex = get_executor(target)
    msg = (body.message or task["title"]).replace('"', "'")
    steps = []
    r = await ex.run(f'git add -A && git commit -m "{msg}"',
                     cwd=att["worktree_path"], timeout=60)
    steps.append({"step": "commit", "rc": r.rc, "output": (r.stdout + r.stderr)[-800:]})
    if not r.ok:
        detail = "nothing to commit" if "nothing to commit" in r.stdout + r.stderr \
            else steps[-1]["output"]
        raise HTTPException(409, f"commit failed: {detail}")
    if body.push:
        r = await ex.run(f"git push -u origin {att['branch']}",
                         cwd=att["worktree_path"], timeout=120)
        steps.append({"step": "push", "rc": r.rc, "output": (r.stdout + r.stderr)[-800:]})
    if body.pr and (not body.push or steps[-1]["rc"] == 0):
        title = task["title"].replace('"', "'")
        r = await ex.run(
            f'gh pr create --head {att["branch"]} --title "{title}" '
            f'--body "Created by agentdeck task #{task_id}."',
            cwd=att["worktree_path"], timeout=120)
        steps.append({"step": "pr", "rc": r.rc,
                      "output": (r.stdout + r.stderr)[-800:],
                      "url": r.stdout.strip().splitlines()[-1] if r.ok and r.stdout.strip() else ""})
    bus.publish(f"task:{task_id}", "git", {"steps": steps})
    return {"steps": steps}


@router.post("/tasks/{task_id}/cleanup")
async def cleanup(task_id: int):
    task, project, target, att = _work_ctx(task_id)
    if task["status"] not in ("done", "failed", "cancelled"):
        raise HTTPException(409, "cleanup only after done/failed/cancelled")
    ex = get_executor(target)
    removed = []
    for a in db.query("SELECT * FROM attempts WHERE task_id=? AND worktree_path!=''",
                      (task_id,)):
        await worktree.remove_worktree(ex, project["repo_path"], a["worktree_path"])
        db.update("attempts", a["id"], {"worktree_path": ""})
        removed.append(a["n"])
    return {"removed_attempts": removed}


@router.post("/tasks/{task_id}/terminal")
async def attach_terminal(task_id: int):
    from ..terminal import TerminalError, terminals
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    if not task:
        raise HTTPException(404, "no such task")
    att = db.one("SELECT * FROM attempts WHERE task_id=? AND status='running' "
                 "ORDER BY n DESC LIMIT 1", (task_id,))
    if not att or not att["tmux_session"]:
        raise HTTPException(409, "no running tmux session to attach")
    project = db.one("SELECT * FROM projects WHERE id=?", (task["project_id"],))
    target = db.one("SELECT * FROM targets WHERE id=?", (project["target_id"],))
    try:
        port = await terminals.spawn(att, target)
    except TerminalError as e:
        raise HTTPException(503, str(e))
    return {"port": port}


@router.get("/tasks/{task_id}/events")
def task_events(task_id: int, after_seq: int = 0, attempt_n: int | None = None):
    sql = ("SELECT e.*, a.n AS attempt_n FROM events e "
           "JOIN attempts a ON a.id=e.attempt_id WHERE a.task_id=?")
    params: list = [task_id]
    if attempt_n is not None:
        sql += " AND a.n=?"; params.append(attempt_n)
    if after_seq:
        sql += " AND e.seq>?"; params.append(after_seq)
    sql += " ORDER BY a.n, e.seq"
    return [{**e, "payload": db.unj(e.pop("payload_json"))}
            for e in db.query(sql, tuple(params))]


@router.get("/tasks/{task_id}/diff")
def task_diff(task_id: int, attempt_n: int | None = None):
    if attempt_n is not None:
        att = db.one("SELECT * FROM attempts WHERE task_id=? AND n=? AND "
                     "diff_stat_json!='{}'", (task_id, attempt_n))
    else:
        att = db.one("SELECT * FROM attempts WHERE task_id=? AND diff_stat_json!='{}' "
                     "ORDER BY n DESC LIMIT 1", (task_id,))
    if not att:
        raise HTTPException(404, "no diff captured yet")
    patch_file = config.diff_dir() / f"attempt-{att['id']}.patch"
    patch = patch_file.read_text() if patch_file.exists() else ""
    return {"attempt_n": att["n"], "stats": db.unj(att["diff_stat_json"], []),
            "files": worktree.split_patch(patch)}


async def _sse(channel: str):
    q = bus.subscribe(channel)
    try:
        yield ": connected\n\n"
        while True:
            try:
                payload = await asyncio.wait_for(q.get(), timeout=15)
                yield bus.sse_format(payload)
            except asyncio.TimeoutError:
                yield ": keepalive\n\n"
    finally:
        bus.unsubscribe(channel, q)


@router.get("/stream")
async def board_stream():
    return StreamingResponse(_sse("board"), media_type="text/event-stream",
                             headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"})


@router.get("/tasks/{task_id}/stream")
async def task_stream(task_id: int):
    return StreamingResponse(_sse(f"task:{task_id}"), media_type="text/event-stream",
                             headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"})
