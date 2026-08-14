"""Targets + projects CRUD."""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel, Field

from .. import claude_runner, db
from ..executor import get_executor, reset_cache
from ..executor.base import ExecutorError

router = APIRouter(prefix="/api")


class TargetIn(BaseModel):
    name: str
    kind: str = Field("ssh", pattern="^(local|ssh|pct|sandbox|mock)$")
    host: str = ""
    port: int = 22
    user: str = "root"
    key_path: str = ""
    workroot: str = ""
    max_concurrent: int = 4
    sandbox: bool = False
    # control-plane paths (globs ok) staged into every worktree on this target
    context_paths: list[str] = []
    # opt-in: share one Claude Code memory store across attempts. Depends on the
    # CLI's internal ~/.claude/projects layout, hence off unless you set it.
    memory_dir: str = ""


@router.get("/targets")
def list_targets():
    return db.query("SELECT * FROM targets ORDER BY name")


@router.post("/targets", status_code=201)
def create_target(t: TargetIn):
    if db.one("SELECT id FROM targets WHERE name=?", (t.name,)):
        raise HTTPException(409, "target name exists")
    data = t.model_dump()
    data["context_json"] = db.j(data.pop("context_paths"))
    tid = db.insert("targets", {**data, "sandbox": int(t.sandbox),
                                "created_at": db.now()})
    return db.one("SELECT * FROM targets WHERE id=?", (tid,))


class TargetPatch(BaseModel):
    context_paths: list[str] | None = None
    memory_dir: str | None = None
    workroot: str | None = None
    max_concurrent: int | None = None


@router.patch("/targets/{target_id}")
def patch_target(target_id: int, t: TargetPatch):
    if not db.one("SELECT id FROM targets WHERE id=?", (target_id,)):
        raise HTTPException(404, "no such target")
    data = {k: v for k, v in t.model_dump().items() if v is not None}
    if "context_paths" in data:
        data["context_json"] = db.j(data.pop("context_paths"))
    if data:
        db.update("targets", target_id, data)
        reset_cache()
    return db.one("SELECT * FROM targets WHERE id=?", (target_id,))


@router.post("/targets/{target_id}/check")
async def check_target(target_id: int, deep: bool = False):
    t = db.one("SELECT * FROM targets WHERE id=?", (target_id,))
    if not t:
        raise HTTPException(404, "no such target")
    try:
        ex = get_executor(t)
        info = await ex.check()
        status = "online" if info.get("git") and info.get("tmux") else "degraded"
        if deep and info.get("claude"):
            # provision current auth first, so the probe both TESTS and HEALS the
            # exact path an agent dispatch uses (fresh OAuth creds, or the API key)
            from .. import agents, credentials
            await credentials.provision(ex, dict(t))
            prefix = agents.env_prefix(credentials.base_agent_env())
            # real 1-token auth round-trip — catches rotated/stale OAuth creds
            # </dev/null: claude -p reads stdin to EOF — an ssh exec channel never
            # EOFs, so without the redirect the probe hangs until timeout
            r = await ex.run(f'{prefix}claude -p "Reply with exactly: ok" --model haiku '
                             '< /dev/null', timeout=120)
            if r.ok:
                info["claude_auth"] = "ok"
            else:
                info["claude_auth"] = f"FAILED: {(r.stdout + r.stderr)[-300:].strip()}"
                status = "degraded"
    except ExecutorError as e:
        info, status = {"error": str(e)}, "offline"
    db.update("targets", target_id, {"status": status, "info_json": db.j(info)})
    return db.one("SELECT * FROM targets WHERE id=?", (target_id,))


@router.delete("/targets/{target_id}", status_code=204)
def delete_target(target_id: int):
    if db.one("SELECT id FROM projects WHERE target_id=? LIMIT 1", (target_id,)):
        raise HTTPException(409, "target has projects")
    db.execute("DELETE FROM targets WHERE id=?", (target_id,))
    reset_cache()


class ProjectIn(BaseModel):
    name: str
    target_id: int
    repo_path: str
    default_base_branch: str = "main"
    workroot_override: str = ""
    verify_cmd: str = ""
    keep_worktrees: bool = False
    review_gate: bool = False
    env: dict = {}   # extra env for the agent, e.g. ANTHROPIC_BASE_URL for local models
    context_paths: list[str] = []      # staged on top of the target's bundle
    mcp: dict = {}                     # MCP servers for this project's agents
    strict_mcp: bool = False           # ignore the host's own MCP config
    permissions: dict = {}             # allow/deny/ask rules, e.g. {"allow": ["Bash(pytest*)"]}
    gate_matcher: str = ""             # PreToolUse matcher in gated mode ('' = all tools)
    default_agent: str = Field("claude", pattern="^(claude|codex|gemini)$")


def _project_columns(data: dict) -> dict:
    """Map the API shape onto columns, validating what would otherwise fail silently."""
    for flag in ("keep_worktrees", "review_gate", "strict_mcp"):
        if flag in data:
            data[flag] = int(data[flag])
    for api_key, column in (("env", "env_json"), ("policy", "policy_json"),
                            ("context_paths", "context_json"), ("mcp", "mcp_json"),
                            ("permissions", "permissions_json")):
        if api_key in data:
            data[column] = db.j(data.pop(api_key))
    if "permissions_json" in data:
        try:   # reject typo'd keys now, not as a mystery denial mid-run
            claude_runner.build_settings(
                permissions=db.unj(data["permissions_json"], {}))
        except ValueError as e:
            raise HTTPException(400, str(e))
    return data


@router.get("/projects")
def list_projects():
    return db.query(
        "SELECT p.*, t.name AS target_name, t.kind AS target_kind "
        "FROM projects p JOIN targets t ON t.id=p.target_id ORDER BY p.name")


@router.post("/projects", status_code=201)
def create_project(p: ProjectIn):
    if not db.one("SELECT id FROM targets WHERE id=?", (p.target_id,)):
        raise HTTPException(400, "no such target")
    data = _project_columns(p.model_dump())
    pid = db.insert("projects", {**data, "created_at": db.now()})
    return db.one("SELECT * FROM projects WHERE id=?", (pid,))


class ProjectPatch(BaseModel):
    verify_cmd: str | None = None
    default_base_branch: str | None = None
    keep_worktrees: bool | None = None
    review_gate: bool | None = None
    policy: dict | None = None
    env: dict | None = None
    context_paths: list[str] | None = None
    mcp: dict | None = None
    strict_mcp: bool | None = None
    permissions: dict | None = None
    gate_matcher: str | None = None
    default_agent: str | None = Field(None, pattern="^(claude|codex|gemini)$")


@router.patch("/projects/{project_id}")
def patch_project(project_id: int, p: ProjectPatch):
    proj = db.one("SELECT * FROM projects WHERE id=?", (project_id,))
    if not proj:
        raise HTTPException(404, "no such project")
    data = _project_columns({k: v for k, v in p.model_dump().items() if v is not None})
    if data:
        db.update("projects", project_id, data)
    return db.one("SELECT * FROM projects WHERE id=?", (project_id,))


@router.get("/projects/{project_id}/notes")
def project_notes(project_id: int):
    return db.query("SELECT * FROM memories WHERE project_id=? ORDER BY id DESC "
                    "LIMIT 100", (project_id,))


@router.delete("/projects/{project_id}/notes/{note_id}", status_code=204)
def delete_note(project_id: int, note_id: int):
    db.execute("DELETE FROM memories WHERE id=? AND project_id=?",
               (note_id, project_id))


@router.delete("/projects/{project_id}", status_code=204)
def delete_project(project_id: int):
    if db.one("SELECT id FROM tasks WHERE project_id=? LIMIT 1", (project_id,)):
        raise HTTPException(409, "project has tasks")
    db.execute("DELETE FROM projects WHERE id=?", (project_id,))
