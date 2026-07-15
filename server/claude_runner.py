"""Claude Code integration seam: launch command construction + stream-json parsing.
Everything CLI-format-specific lives here so agent drift touches one file.
"""
import json
import shlex

from . import config


def runtime_dir(worktree: str) -> str:
    return f"{worktree}/.agentdeck"


def hook_settings(base_url: str, token: str) -> dict:
    """PreToolUse gate for mutating tools; only used when permission_mode='default'."""
    cmd = (f"AGENTDECK_URL={base_url} AGENTDECK_TOKEN={token} "
           f"python3 .agentdeck/hook.py")
    return {"hooks": {"PreToolUse": [{
        "matcher": "Bash|Write|Edit|MultiEdit|NotebookEdit",
        "hooks": [{"type": "command", "command": cmd,
                   "timeout": int(config.APPROVAL_EXPIRE_SECONDS) + 30}],
    }]}}


def launch_command(worktree: str, tmux_session: str, permission_mode: str,
                   model: str = "", resume_session: str = "",
                   env_prefix: str = "") -> str:
    rt = runtime_dir(worktree)
    parts = [config.CLAUDE_BIN, "-p", '"$(cat .agentdeck/prompt.md)"',
             "--output-format", "stream-json", "--verbose",
             "--permission-mode", permission_mode]
    if permission_mode == "default":
        parts += ["--settings", ".agentdeck/settings.json"]
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
