"""Claude Code integration seam: launch command construction + stream-json parsing.
Everything CLI-format-specific lives here so agent drift touches one file.
"""
import json
import re
import shlex

from . import config

SETTINGS_REL = ".agentdeck/settings.json"
MCP_REL = ".agentdeck/mcp.json"

# Which tools the approval hook intercepts in gated mode. '*' matches every tool,
# which is the only value that makes gated mode usable: anything NOT matched falls
# through to the normal permission system, and headless `claude -p` has no way to
# ask — so an unmatched tool is silently DENIED. The old narrow matcher meant MCP
# tools, WebFetch and Task could never run in gated mode and never said why.
DEFAULT_GATE_MATCHER = "*"

# permission keys we pass through to settings.json; anything else is rejected so a
# typo fails loudly at config time instead of silently widening or narrowing access
PERMISSION_KEYS = ("allow", "deny", "ask", "defaultMode", "additionalDirectories")


def runtime_dir(worktree: str) -> str:
    return f"{worktree}/.agentdeck"


def hook_settings(base_url: str, token: str,
                  matcher: str = DEFAULT_GATE_MATCHER) -> dict:
    """PreToolUse gate; only used when permission_mode='default'."""
    cmd = (f"AGENTDECK_URL={base_url} AGENTDECK_TOKEN={token} "
           f"python3 .agentdeck/hook.py")
    return {"hooks": {"PreToolUse": [{
        "matcher": matcher or DEFAULT_GATE_MATCHER,
        "hooks": [{"type": "command", "command": cmd,
                   "timeout": int(config.APPROVAL_EXPIRE_SECONDS) + 30}],
    }]}}


def build_settings(base_url: str = "", token: str = "", gated: bool = False,
                   permissions: dict | None = None,
                   matcher: str = DEFAULT_GATE_MATCHER) -> dict:
    """The settings.json every attempt gets — permission rules always, hook when gated.

    Written unconditionally (it used to be gated-mode only) because without it an
    acceptEdits run can't be granted Bash at all: headless has no prompt, so any
    tool needing permission is denied with no signal to the operator.
    """
    settings: dict = {}
    perms = {k: v for k, v in (permissions or {}).items() if v}
    unknown = set(perms) - set(PERMISSION_KEYS)
    if unknown:
        raise ValueError(f"unknown permission keys: {sorted(unknown)}")
    if perms:
        settings["permissions"] = perms
    if gated:
        settings.update(hook_settings(base_url, token, matcher))
    return settings


def project_slug(path: str) -> str:
    """Claude Code's per-cwd session directory name under ~/.claude/projects.

    Every non-alphanumeric character collapses to '-' (verified empirically against
    the running CLI, 2.1.232). This mirrors an internal layout, not a public API —
    which is why memory sharing is opt-in per target.
    """
    return re.sub(r"[^A-Za-z0-9-]", "-", path)


def memory_link_command(worktree: str, memory_dir: str) -> str:
    """Point this attempt's session memory at a shared store.

    Claude Code keys memory by cwd, so a fresh worktree per attempt starts
    memory-blind and everything an agent learns dies with the worktree. There is
    no CLI flag for this, so the only mechanism is symlinking the session's
    memory dir at a store the operator nominates.
    """
    proj = f'"$HOME/.claude/projects/{project_slug(worktree)}"'
    link = f'"$HOME/.claude/projects/{project_slug(worktree)}/memory"'
    tgt = shlex.quote(memory_dir)
    # a real directory already sitting there (a previous non-shared run) has to go,
    # but only ever that path, and only when it is not already our symlink
    return (f"mkdir -p {proj} {tgt} && "
            f"{{ [ -L {link} ] || rm -rf {link}; }} && ln -sfn {tgt} {link}")


def launch_command(worktree: str, tmux_session: str, permission_mode: str,
                   model: str = "", resume_session: str = "",
                   env_prefix: str = "", settings_path: str = SETTINGS_REL,
                   mcp_config: str = "", strict_mcp: bool = False) -> str:
    rt = runtime_dir(worktree)
    parts = [config.CLAUDE_BIN, "-p", '"$(cat .agentdeck/prompt.md)"',
             "--output-format", "stream-json", "--verbose",
             "--permission-mode", permission_mode]
    if settings_path:
        parts += ["--settings", settings_path]
    if mcp_config:
        parts += ["--mcp-config", mcp_config]
        if strict_mcp:
            parts.append("--strict-mcp-config")
    if model:
        parts += ["--model", model]
    if resume_session:
        parts += ["--resume", resume_session]
    inner = (f"cd {worktree} && {env_prefix}{' '.join(parts)} "
             f"> {rt}/events.jsonl 2> {rt}/stderr.log; echo $? > {rt}/exit_code")
    return f"tmux new-session -d -s {tmux_session} {shlex.quote(inner)}"


# ---- stream-json → normalized events ----------------------------------------

def parse_stream_lines(buf: str) -> tuple[list[dict], str]:
    """Parse complete lines from a text buffer; return (events, remainder)."""
    events, remainder = [], ""
    if "\n" not in buf:
        return [], buf
    complete, remainder = buf.rsplit("\n", 1)
    for line in complete.split("\n"):
        line = line.strip()
        if not line:
            continue
        try:
            raw = json.loads(line)
        except ValueError:
            events.append({"type": "raw", "payload": {"line": line[:2000]}})
            continue
        events.extend(normalize(raw))
    return events, remainder


def normalize(raw: dict) -> list[dict]:
    t = raw.get("type")
    # housekeeping noise (rate-limit ticks, non-init system chatter) — not timeline-worthy
    inner = raw.get("data") if isinstance(raw.get("data"), dict) else {}
    if t == "rate_limit_event" or inner.get("type") == "rate_limit_event":
        return []
    if t == "system" and raw.get("subtype") != "init":
        return []
    if t == "system" and raw.get("subtype") == "init":
        return [{"type": "init", "payload": {
            "session_id": raw.get("session_id", ""), "model": raw.get("model", ""),
            "tools": raw.get("tools", [])[:40]}}]
    if t == "assistant":
        out = []
        for block in (raw.get("message") or {}).get("content", []):
            if block.get("type") == "text" and block.get("text", "").strip():
                out.append({"type": "text", "payload": {"text": block["text"]}})
            elif block.get("type") == "tool_use":
                out.append({"type": "tool_use", "payload": {
                    "id": block.get("id", ""), "name": block.get("name", ""),
                    "input": _truncate(block.get("input", {}))}})
        return out
    if t == "user":
        out = []
        for block in (raw.get("message") or {}).get("content", []):
            if isinstance(block, dict) and block.get("type") == "tool_result":
                content = block.get("content", "")
                if isinstance(content, list):
                    content = " ".join(c.get("text", "") for c in content
                                       if isinstance(c, dict))
                out.append({"type": "tool_result", "payload": {
                    "tool_use_id": block.get("tool_use_id", ""),
                    "content": str(content)[:2000],
                    "is_error": bool(block.get("is_error"))}})
        return out
    if t == "result":
        return [{"type": "result", "payload": {
            "subtype": raw.get("subtype", ""),
            "cost_usd": raw.get("total_cost_usd"),
            "duration_ms": raw.get("duration_ms"),
            "num_turns": raw.get("num_turns"),
            "result": str(raw.get("result", ""))[:4000],
            "session_id": raw.get("session_id", "")}}]
    return [{"type": "raw", "payload": {"data": _truncate(raw)}}]


def _truncate(obj, limit: int = 2000):
    s = json.dumps(obj, ensure_ascii=False)
    if len(s) <= limit:
        return obj
    return {"_truncated": s[:limit]}
