"""Scheduler: promotes queued attempts, tails running ones, finalizes results.
Restart-safe: all progress (log_offset, state) lives in SQLite; tmux sessions on
targets survive a control-plane restart and are re-attached on the next tick.
"""
import asyncio
import logging
import secrets
import sqlite3
from pathlib import Path

from . import (
    agents,
    broker,
    claude_runner,
    config,
    credentials,
    db,
    sandbox,
    sinks,
    state,
    worktree,
)
from .bus import bus
from .executor import get_executor
from .executor.base import Executor, ExecutorError
from .executor.pct import PctExecutor

log = logging.getLogger("agentdeck.scheduler")

AGENT_TASK_FOOTER = """

---
agentdeck: if you discover out-of-scope work (bugs, refactors, follow-ups), do NOT
expand this task. File a card on the board instead:
  python3 .agentdeck/adk.py add-task "short title" "detailed prompt"            # → backlog
  python3 .agentdeck/adk.py add-task "short title" "detailed prompt" --dispatch # runs now
Stay focused on the task above."""


class Scheduler:
    def __init__(self) -> None:
        self._task: asyncio.Task | None = None
        self._stop: asyncio.Event | None = None
        self._poll_errors: dict[int, int] = {}
        self._ghost_strikes: dict[int, int] = {}
        self._last_janitor: float = 0.0

    def start(self) -> None:
        self._stop = asyncio.Event()   # fresh per loop (tests restart the app)
        self._task = asyncio.create_task(self._loop(), name="agentdeck-scheduler")

    async def stop(self) -> None:
        self._stop.set()
        if self._task:
            await self._task

    async def _loop(self) -> None:
        while not self._stop.is_set():
            try:
                await self.tick()
            except Exception:
                log.exception("tick failed")
            try:
                await asyncio.wait_for(self._stop.wait(), timeout=config.TICK_SECONDS)
            except TimeoutError:
                pass

    async def tick(self) -> None:
        if config.JANITOR_DAYS > 0 and db.now() - self._last_janitor > 3600:
            self._last_janitor = db.now()
            try:
                await self.janitor()
            except Exception:
                log.exception("janitor sweep failed")
        await self._promote_queued()
        for att in db.query("SELECT * FROM attempts WHERE status='running'"):
            try:
                await self._poll(att)
                self._poll_errors.pop(att["id"], None)
            except sqlite3.IntegrityError:
                # the task/attempt was deleted concurrently (DELETE /tasks/{id})
                # mid-poll — nothing to do; it's gone from the next query. Must
                # not abort the tick and stall every other running attempt.
                log.info("attempt %s vanished mid-poll (concurrent delete)", att["id"])
                self._poll_errors.pop(att["id"], None)
            except ExecutorError as e:
                n = self._poll_errors.get(att["id"], 0) + 1
                self._poll_errors[att["id"]] = n
                log.warning("poll attempt %s error %s/30: %s", att["id"], n, e)
                if n >= 30:
                    self._finalize(att, rc=-1, note=f"target unreachable: {e}")
                    ctx = _context(att)
                    if ctx:
                        await self._destroy_if_sandbox(att, ctx)

    # ---- queued → running ----------------------------------------------------
    async def _promote_queued(self) -> None:
        queued = db.query(
            "SELECT a.* FROM attempts a JOIN tasks t ON t.id=a.task_id "
            "WHERE a.status='queued' ORDER BY t.priority DESC, a.id")
        for att in queued:
            ctx = _context(att)
            if not ctx:
                continue
            running = db.one(
                "SELECT COUNT(*) c FROM attempts a JOIN tasks t ON t.id=a.task_id "
                "JOIN projects p ON p.id=t.project_id "
                "WHERE a.status='running' AND p.target_id=?", (ctx["target"]["id"],))["c"]
            if running >= (ctx["target"]["max_concurrent"] or 4):
                continue
            try:
                await self._launch(att, ctx)
            except (ExecutorError, Exception) as e:
                log.exception("launch attempt %s failed", att["id"])
                db.update("attempts", att["id"], {"status": "failed",
                                                  "result_json": db.j({"error": str(e)})})
                _set_task_status(att["task_id"], "failed")

    async def _launch(self, att: dict, ctx: dict) -> None:
        task, project, target = ctx["task"], ctx["project"], ctx["target"]
        if target["kind"] == "sandbox":
            return await self._launch_sandbox(att, ctx)
        ex = get_executor(target)
        wt = att["worktree_path"]
        branch = att["branch"] or worktree.branch_name(task["id"], att["n"])
        if not wt:
            workroot = (project["workroot_override"] or target["workroot"]
                        or worktree.default_workroot(project["repo_path"]))
            wt = worktree.worktree_path(workroot, task["id"], att["n"])
            base = task["base_branch"] or project["default_base_branch"] or "main"
            await worktree.ensure_worktree(ex, project["repo_path"], base, branch, wt)

        rt = claude_runner.runtime_dir(wt)
        prompt = att["prompt"] or task["prompt"] or task["title"]
        notes = db.query("SELECT note FROM memories WHERE project_id=? "
                         "ORDER BY id DESC LIMIT 12", (project["id"],))
        if notes and task["created_by"] != "reviewer-gate":
            prompt = build_notes_prefix([n["note"] for n in notes]) + prompt
        if task["created_by"] != "reviewer-gate":
            prompt += AGENT_TASK_FOOTER
        await ex.write_file(f"{rt}/prompt.md", prompt.encode())
        # task-filing kit: lets the agent put follow-up cards on the board
        await ex.write_file(f"{rt}/adk.py", (config.HOOKS_DIR / "adk.py").read_bytes())
        await ex.write_file(f"{rt}/env",
                            f"ADK_URL={config.BASE_URL}\nADK_TOKEN={att['token']}\n".encode())
        if task["permission_mode"] == "default":
            hook_src = (config.HOOKS_DIR / "hook.py").read_bytes()
            await ex.write_file(f"{rt}/hook.py", hook_src)
            await ex.write_file(f"{rt}/settings.json", db.j(
                claude_runner.hook_settings(config.BASE_URL, att["token"])).encode())

        # reused worktrees (follow-ups, reviewer gate) carry the PREVIOUS attempt's
        # runtime files — a stale exit_code would finalize this attempt instantly
        await ex.run(f"rm -f {rt}/exit_code {rt}/events.jsonl {rt}/stderr.log",
                     timeout=20)

        # push CURRENT auth so the agent never runs on a rotated-out credential copy
        await credentials.provision(ex, target)

        sess = f"adk-{att['id']}"
        cmd = agents.launch_command(
            task["agent"] or "claude", wt, sess, task["permission_mode"],
            model=att["model"] or task["model"],
            resume_session=att["resume_session"],
            env={**credentials.base_agent_env(), **db.unj(project["env_json"])})
        r = await ex.run(cmd, timeout=60)
        if not r.ok:
            raise ExecutorError(f"tmux launch failed: {r.stderr.strip()}")
        db.update("attempts", att["id"], {
            "status": "running", "worktree_path": wt, "branch": branch,
            "tmux_session": sess, "started_at": db.now(), "log_offset": 0})
        _set_task_status(att["task_id"], "running")
        log.info("attempt %s launched on target %s (%s)", att["id"], target["name"], wt)

    async def _launch_sandbox(self, att: dict, ctx: dict) -> None:
        """Ephemeral flow: clone template → repo inside container → agent → (destroy at finalize)."""
        target = ctx["target"]
        host = get_executor(target)
        vmid = await sandbox.provision(host, target["host"], att["id"])
        db.update("attempts", att["id"], {"sandbox_vmid": vmid})
        att = {**att, "sandbox_vmid": vmid}
        try:
            await self._launch_sandbox_inner(att, ctx, host, vmid)
        except Exception:
            # ANY failure after the container exists must not leak it — the
            # generic handler in _promote_queued only marks the attempt failed
            await sandbox.destroy(host, vmid)
            db.update("attempts", att["id"], {"worktree_path": ""})
            raise

    async def _launch_sandbox_inner(self, att, ctx, host, vmid) -> None:
        task, project, target = ctx["task"], ctx["project"], ctx["target"]
        inside = _attempt_executor(att, target)

        base = task["base_branch"] or project["default_base_branch"] or "main"
        if sandbox.is_repo_url(project["repo_path"]):
            workdir = f"/root/work/{project['name']}"
            r = await inside.run(f"git clone --branch {base} {project['repo_path']} "
                                 f"{workdir}", timeout=600)
            if not r.ok:
                await sandbox.destroy(host, vmid)
                raise ExecutorError(f"repo clone failed: {r.stderr.strip()[-400:]}")
        else:
            workdir = project["repo_path"]   # repo baked into the template
        branch = worktree.branch_name(task["id"], att["n"])
        await inside.run(f"git checkout -b {branch}", cwd=workdir, timeout=60)
        await worktree.add_excludes(inside, workdir)

        rt = claude_runner.runtime_dir(workdir)
        prompt = att["prompt"] or task["prompt"] or task["title"]
        if task["created_by"] != "reviewer-gate":
            prompt += AGENT_TASK_FOOTER
        await inside.write_file(f"{rt}/prompt.md", prompt.encode())
        await inside.write_file(f"{rt}/adk.py", (config.HOOKS_DIR / "adk.py").read_bytes())
        await inside.write_file(f"{rt}/env",
                                f"ADK_URL={config.BASE_URL}\nADK_TOKEN={att['token']}\n".encode())
        if task["permission_mode"] == "default":
            await inside.write_file(f"{rt}/hook.py",
                                    (config.HOOKS_DIR / "hook.py").read_bytes())
            await inside.write_file(f"{rt}/settings.json", db.j(
                claude_runner.hook_settings(config.BASE_URL, att["token"])).encode())
        await inside.run(f"rm -f {rt}/exit_code {rt}/events.jsonl {rt}/stderr.log",
                         timeout=20)
        # provision current auth into the container via its own executor (mock-safe)
        await credentials.provision(inside, {"kind": "pct", "name": f"sandbox-{vmid}"})
        sess = f"adk-{att['id']}"
        cmd = agents.launch_command(task["agent"] or "claude", workdir, sess,
                                    task["permission_mode"],
                                    model=att["model"] or task["model"], sandbox=True,
                                    env={**credentials.base_agent_env(),
                                         **db.unj(project["env_json"])})
        r = await inside.run(cmd, timeout=60)
        if not r.ok:
            await sandbox.destroy(host, vmid)
            raise ExecutorError(f"tmux launch failed in sandbox: {r.stderr.strip()}")
        db.update("attempts", att["id"], {
            "status": "running", "worktree_path": workdir, "branch": branch,
            "tmux_session": sess, "started_at": db.now(), "log_offset": 0})
        _set_task_status(att["task_id"], "running")
        log.info("attempt %s launched in sandbox %s", att["id"], vmid)

    # ---- running: tail + finalize ---------------------------------------------
    async def _poll(self, att: dict) -> None:
        ctx = _context(att)
        if not ctx:
            return
        ex = _attempt_executor(att, ctx["target"])
        rt = claude_runner.runtime_dir(att["worktree_path"])

        chunk = await ex.read_file(f"{rt}/events.jsonl", offset=att["log_offset"])
        if chunk:
            nl = chunk.rfind(b"\n")
            if nl >= 0:
                events, _ = agents.parse_stream_lines(
                    ctx["task"]["agent"] or "claude",
                    chunk[:nl + 1].decode(errors="replace"))
                self._store_events(att, events)
                db.update("attempts", att["id"],
                          {"log_offset": att["log_offset"] + nl + 1})

        exit_raw = await ex.read_file(f"{rt}/exit_code")
        if exit_raw.strip():
            try:
                rc = int(exit_raw.strip())
            except ValueError:
                rc = -1
            await self._capture_and_finalize(att, ctx, rc)
            return

        if not chunk:   # no output and no exit code — is the session even alive?
            alive = await ex.run(f"tmux has-session -t adk-{att['id']} 2>/dev/null", timeout=20)
            if alive.ok:
                self._ghost_strikes.pop(att["id"], None)
                return
            # two consecutive strikes: a single miss can be a transient read race
            # (session ended but exit_code not yet visible through the executor)
            strikes = self._ghost_strikes.get(att["id"], 0) + 1
            self._ghost_strikes[att["id"]] = strikes
            if strikes >= 2:
                self._ghost_strikes.pop(att["id"], None)
                self._finalize(att, rc=-1, note="tmux session disappeared without exit code")
                await self._destroy_if_sandbox(att, ctx)

    def _store_events(self, att: dict, events: list[dict]) -> None:
        if not events:
            return
        # fast-path: skip if the attempt was deleted concurrently (avoids the
        # common case of an FK error; the tick loop still catches the race).
        if not db.one("SELECT 1 FROM attempts WHERE id=?", (att["id"],)):
            return
        seq = (db.one("SELECT MAX(seq) m FROM events WHERE attempt_id=?",
                      (att["id"],))["m"] or 0)
        for ev in events:
            seq += 1
            db.insert("events", {"attempt_id": att["id"], "seq": seq, "ts": db.now(),
                                 "type": ev["type"], "payload_json": db.j(ev["payload"])})
            if ev["type"] == "init" and ev["payload"].get("session_id"):
                db.update("attempts", att["id"], {"session_id": ev["payload"]["session_id"]})
            if ev["type"] == "result":
                db.update("attempts", att["id"], {"result_json": db.j(ev["payload"])})
            bus.publish(f"task:{att['task_id']}", "agent_event",
                        {"attempt_id": att["id"], "seq": seq, "type": ev["type"],
                         "payload": ev["payload"]})

    async def _capture_and_finalize(self, att: dict, ctx: dict, rc: int) -> None:
        task, project = ctx["task"], ctx["project"]
        ex = _attempt_executor(att, ctx["target"])
        base = task["base_branch"] or project["default_base_branch"] or "main"
        try:
            patch, files = await worktree.capture_diff(ex, att["worktree_path"], base)
        except ExecutorError as e:
            patch, files = "", [{"path": f"(diff capture failed: {e})",
                                 "additions": 0, "deletions": 0}]
        diff_dir = config.diff_dir()
        diff_dir.mkdir(parents=True, exist_ok=True)
        Path(diff_dir / f"attempt-{att['id']}.patch").write_text(patch)
        db.update("attempts", att["id"], {"diff_stat_json": db.j(files)})

        # auto-verify: run the project's test command in the worktree and badge the result
        if rc == 0 and (project["verify_cmd"] or "").strip():
            try:
                vr = await ex.run(project["verify_cmd"], cwd=att["worktree_path"],
                                  timeout=900)
                verify = {"cmd": project["verify_cmd"], "rc": vr.rc,
                          "output": (vr.stdout + vr.stderr)[-4000:]}
            except ExecutorError as e:
                verify = {"cmd": project["verify_cmd"], "rc": -1, "output": str(e)}
            db.update("attempts", att["id"], {"verify_json": db.j(verify)})
            self._store_events(att, [{"type": "verify", "payload": {
                "cmd": verify["cmd"], "rc": verify["rc"],
                "output": verify["output"][-1200:]}}])
        self._finalize(att, rc=rc)
        # ephemeral sandbox: everything worth keeping (events, diff, verify) is
        # already on the control plane — the container has served its purpose
        await self._destroy_if_sandbox(att, ctx)

    async def _destroy_if_sandbox(self, att: dict, ctx: dict) -> None:
        vmid = att.get("sandbox_vmid") or db.one(
            "SELECT sandbox_vmid FROM attempts WHERE id=?", (att["id"],))["sandbox_vmid"]
        if ctx["target"]["kind"] == "sandbox" and vmid:
            await sandbox.destroy(get_executor(ctx["target"]), vmid)
            db.update("attempts", att["id"], {"worktree_path": ""})

    def _finalize(self, att: dict, rc: int, note: str = "") -> None:
        broker.expire_for_attempt(att["id"])   # no ghost approvals on a dead attempt
        ok = rc == 0
        result = db.unj(db.one("SELECT result_json FROM attempts WHERE id=?",
                               (att["id"],))["result_json"])
        if note:
            result["error"] = note
        db.update("attempts", att["id"], {
            "status": "done" if ok else "failed", "finished_at": db.now(),
            "exit_code": rc, "result_json": db.j(result)})
        self._poll_errors.pop(att["id"], None)
        task = db.one("SELECT * FROM tasks WHERE id=?", (att["task_id"],))
        # A/B: the task stays running until its last active attempt lands
        others = db.one("SELECT COUNT(*) c FROM attempts WHERE task_id=? AND "
                        "status IN ('queued','running') AND id!=?",
                        (att["task_id"], att["id"]))["c"]
        if task["status"] != "running" or others:
            return
        any_ok = db.one("SELECT COUNT(*) c FROM attempts WHERE task_id=? AND "
                        "status='done'", (att["task_id"],))["c"] > 0
        _set_task_status(task["id"], "review" if any_ok else "failed")
        sinks.notify("Ready for review" if any_ok else "Task failed",
                     task["title"][:80], url=f"/#task/{task['id']}")
        if any_ok and task["created_by"] == "reviewer-gate":
            self._apply_review_verdict(task, result)
        elif any_ok:
            self._maybe_spawn_reviewer(task, att)

    # ---- reviewer gate ---------------------------------------------------------
    def _maybe_spawn_reviewer(self, task: dict, att: dict) -> None:
        project = db.one("SELECT * FROM projects WHERE id=?", (task["project_id"],))
        if not project or not project["review_gate"]:
            return
        target = db.one("SELECT kind FROM targets WHERE id=?", (project["target_id"],))
        if target and target["kind"] == "sandbox":
            log.info("review gate skipped for sandbox task %s (worktree is destroyed)",
                     task["id"])
            return
        if db.one("SELECT id FROM tasks WHERE created_by='reviewer-gate' AND "
                  "created_by_attempt=? LIMIT 1", (att["id"],)):
            return
        patch = ""
        pf = config.diff_dir() / f"attempt-{att['id']}.patch"
        if pf.exists():
            patch = pf.read_text()[:12000]
        prompt = (
            "You are a strict code reviewer. Another agent completed this task in "
            "the current worktree (you may read files and run read-only checks):\n\n"
            f"TASK: {task['title']}\n{task['prompt']}\n\nDIFF:\n```diff\n{patch}\n```\n\n"
            "Review for correctness, edge cases, and scope creep. Your FINAL line "
            "must be exactly one of:\nVERDICT: APPROVE — <one-line reason>\n"
            "VERDICT: REQUEST_CHANGES — <specific required changes>")
        rid = db.insert("tasks", {
            "project_id": task["project_id"], "title": f"Review: {task['title']}"[:90],
            "prompt": prompt, "status": "queued", "priority": task["priority"],
            "permission_mode": "plan", "created_by": "reviewer-gate",
            "parent_task_id": task["id"], "created_by_attempt": att["id"],
            "created_at": db.now(), "updated_at": db.now()})
        rtask = db.one("SELECT * FROM tasks WHERE id=?", (rid,))
        # reviewer works in the SAME worktree, read-only via plan mode
        create_attempt(rtask, worktree_path=att["worktree_path"], branch=att["branch"])
        bus.publish("board", "task", rtask)
        log.info("reviewer gate: spawned task %s for task %s", rid, task["id"])

    def _apply_review_verdict(self, rtask: dict, result: dict) -> None:
        import re
        text = str(result.get("result", ""))
        matches = re.findall(r"VERDICT:\s*(APPROVE|REQUEST_CHANGES)", text)
        verdict = matches[-1] if matches else "UNCLEAR"   # last verdict wins
        parent_att = db.one(
            "SELECT * FROM attempts WHERE task_id=? ORDER BY n DESC LIMIT 1",
            (rtask["parent_task_id"],))
        if parent_att:
            pres = db.unj(parent_att["result_json"])
            pres["review"] = {"verdict": verdict, "notes": text[-1500:],
                              "reviewer_task_id": rtask["id"]}
            db.update("attempts", parent_att["id"], {"result_json": db.j(pres)})
            self._store_events(parent_att, [{"type": "review_verdict", "payload": {
                "verdict": verdict, "notes": text[-800:]}}])
        parent = db.one("SELECT * FROM tasks WHERE id=?", (rtask["parent_task_id"],))
        if parent:
            bus.publish("board", "task", parent)
            sinks.notify(f"Review: {verdict.lower().replace('_', ' ')}",
                         parent["title"][:80], url=f"/#task/{parent['id']}")
        # reviewer card served its purpose — off the board
        _set_task_status(rtask["id"], "done")

    async def janitor(self, days: float | None = None) -> dict:
        """Sweep worktrees of finished tasks (vibe-kanban's #1 complaint)."""
        days = config.JANITOR_DAYS if days is None else days
        cutoff = db.now() - days * 86400
        removed = []
        rows = db.query(
            "SELECT a.* FROM attempts a JOIN tasks t ON t.id=a.task_id "
            "JOIN projects p ON p.id=t.project_id "
            "WHERE t.status IN ('done','cancelled') AND a.worktree_path!='' "
            "AND a.finished_at IS NOT NULL AND a.finished_at<? AND p.keep_worktrees=0",
            (cutoff,))
        for att in rows:
            ctx = _context(att)
            if not ctx:
                continue
            try:
                await worktree.remove_worktree(
                    get_executor(ctx["target"]), ctx["project"]["repo_path"],
                    att["worktree_path"])
            except ExecutorError as e:
                log.warning("janitor: attempt %s worktree removal failed: %s",
                            att["id"], e)
                continue
            db.update("attempts", att["id"], {"worktree_path": ""})
            removed.append(att["id"])
        if removed:
            log.info("janitor removed %d worktree(s)", len(removed))
        return {"removed_attempts": removed}

    async def cancel_attempt(self, att: dict) -> None:
        broker.expire_for_attempt(att["id"])
        ctx = _context(att)
        if ctx:
            try:
                ex = _attempt_executor(att, ctx["target"])
                await ex.run(f"tmux kill-session -t adk-{att['id']} 2>/dev/null || true",
                             timeout=20)
            except ExecutorError:
                pass
            if ctx["target"]["kind"] == "sandbox" and att.get("sandbox_vmid"):
                await sandbox.destroy(get_executor(ctx["target"]), att["sandbox_vmid"])
                db.update("attempts", att["id"], {"worktree_path": ""})
        db.update("attempts", att["id"], {"status": "cancelled", "finished_at": db.now()})
        _set_task_status(att["task_id"], "cancelled")


def build_notes_prefix(notes: list[str]) -> str:
    lines = "\n".join(f"- {n[:400]}" for n in reversed(notes))
    return ("## Project memory (notes left by previous agents)\n"
            f"{lines}\n\n---\n\n")


def create_attempt(task: dict, prompt: str = "", resume_session: str = "",
                   worktree_path: str = "", branch: str = "", model: str = "") -> dict:
    n = (db.one("SELECT MAX(n) m FROM attempts WHERE task_id=?", (task["id"],))["m"] or 0) + 1
    aid = db.insert("attempts", {
        "task_id": task["id"], "n": n, "status": "queued",
        "token": secrets.token_urlsafe(24), "prompt": prompt,
        "resume_session": resume_session, "worktree_path": worktree_path,
        "branch": branch, "model": model})
    return db.one("SELECT * FROM attempts WHERE id=?", (aid,))


def _attempt_executor(att: dict, target: dict) -> Executor:
    """Sandbox attempts own a container; everyone else shares the target executor."""
    if target["kind"] == "sandbox" and att.get("sandbox_vmid") and not config.MOCK:
        return PctExecutor(att["sandbox_vmid"])
    return get_executor(target)


def _context(att: dict) -> dict | None:
    task = db.one("SELECT * FROM tasks WHERE id=?", (att["task_id"],))
    if not task:
        return None
    project = db.one("SELECT * FROM projects WHERE id=?", (task["project_id"],))
    if not project:
        return None
    target = db.one("SELECT * FROM targets WHERE id=?", (project["target_id"],))
    if not target:
        return None
    return {"task": task, "project": project, "target": target}


def _set_task_status(task_id: int, new: str) -> None:
    task = db.one("SELECT * FROM tasks WHERE id=?", (task_id,))
    state.check(task["status"], new)
    db.update("tasks", task_id, {"status": new, "updated_at": db.now()})
    bus.publish("board", "task", db.one("SELECT * FROM tasks WHERE id=?", (task_id,)))


scheduler = Scheduler()
