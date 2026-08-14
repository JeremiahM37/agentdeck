"""Agent adapter seam — everything CLI-specific per coding agent lives here.

claude  — first-class: stream-json events, hook approvals, session resume.
codex   — experimental: `codex exec --json` JSONL events; no gated mode, no resume.
gemini  — experimental: plain-text output mapped to timeline lines; no gated mode.

The run protocol is agent-agnostic (worktree + tmux + events file + exit_code),
so adapters only build the inner command and normalize output lines.
"""
import json
import shlex

from . import claude_runner, config

AGENTS = ("claude", "codex", "gemini")
# which agents support the hook-gated 'default' permission mode
GATED_CAPABLE = {"claude"}

# agentdeck's permission modes → codex sandbox policy (codex >= 0.140; the older
# --full-auto was removed upstream). codex has no PreToolUse hook, so the gated
# 'default' mode stays claude-only and is rejected before dispatch.
CODEX_SANDBOX = {
    "plan": ["--sandbox", "read-only"],
    "acceptEdits": ["--sandbox", "workspace-write"],
    "bypassPermissions": ["--dangerously-bypass-approvals-and-sandbox"],
}

# banner codex prints to stdout before its JSONL; not an event, not worth a
# 'raw' card on the timeline
NOISE_LINES = ("Reading additional input from stdin...",)


def env_prefix(env: dict | None, sandbox: bool = False) -> str:
    """Shell prefix of KEY=VAL pairs injected before the agent binary.

    This is the any-model door: point a project at any Anthropic-compatible
    endpoint (Ollama ≥0.20 natively, LiteLLM, llama.cpp, vLLM gateways) via
    ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN, or set OPENAI_*/GEMINI_* for
    the other agents. Values are shell-quoted; keys are validated.
    """
    pairs = dict(env or {})
    if sandbox:
        # claude refuses bypassPermissions as root; inside a disposable
        # container that refusal is the wrong default
        pairs.setdefault("IS_SANDBOX", "1")
    parts = []
    for k, v in pairs.items():
        if not k.replace("_", "").isalnum() or k[0].isdigit():
            raise ValueError(f"invalid env var name {k!r}")
        parts.append(f"{k}={shlex.quote(str(v))}")
    return (" ".join(parts) + " ") if parts else ""


def launch_command(agent: str, worktree: str, tmux_session: str,
                  permission_mode: str, model: str = "",
                  resume_session: str = "", sandbox: bool = False,
                  env: dict | None = None, settings_path: str = "",
                  mcp_config: str = "", strict_mcp: bool = False) -> str:
    prefix = env_prefix(env, sandbox=sandbox)
    if agent == "claude":
        return claude_runner.launch_command(
            worktree, tmux_session, permission_mode, model=model,
            resume_session=resume_session, env_prefix=prefix,
            settings_path=settings_path or claude_runner.SETTINGS_REL,
            mcp_config=mcp_config, strict_mcp=strict_mcp)
    # codex/gemini have no equivalent of --settings/--mcp-config; the staged
    # context bundle still reaches them through the prompt prefix
    rt = claude_runner.runtime_dir(worktree)
    if agent == "codex":
        parts = [config.CODEX_BIN, "exec", "--json"]
        if model:
            parts += ["-m", model]
        parts += CODEX_SANDBOX[permission_mode]
        parts.append('"$(cat .agentdeck/prompt.md)"')
    elif agent == "gemini":
        parts = [config.GEMINI_BIN, "-p", '"$(cat .agentdeck/prompt.md)"']
        if model:
            parts += ["-m", model]
        if permission_mode in ("acceptEdits", "bypassPermissions"):
            parts.append("--yolo")
    else:
        raise ValueError(f"unknown agent {agent!r}")
    # both read stdin even with the prompt passed as an argument, and a tmux pane's
    # stdin never EOFs — without this the agent waits forever on "Reading additional
    # input from stdin" and the attempt looks hung
    inner = (f"cd {worktree} && {prefix}{' '.join(parts)} < /dev/null "
             f"> {rt}/events.jsonl 2> {rt}/stderr.log; echo $? > {rt}/exit_code")
    return f"tmux new-session -d -s {tmux_session} {shlex.quote(inner)}"


def parse_stream_lines(agent: str, buf: str) -> tuple[list[dict], str]:
    if agent == "codex":
        return _parse_jsonl(buf, _codex_normalize)
    if agent == "gemini":
        return _parse_plaintext(buf)
    return claude_runner.parse_stream_lines(buf)   # claude + unknown default


# ---- codex: `codex exec --json` JSONL thread events --------------------------

def _codex_normalize(raw: dict) -> list[dict]:
    t = raw.get("type", "")
    if t == "thread.started":
        return [{"type": "init", "payload": {
            "session_id": raw.get("thread_id", ""), "model": "codex", "tools": []}}]
    # item.started is what makes the timeline live: codex emits it when a command
    # begins, so the card shows the work in flight instead of only after it lands
    if t in ("item.started", "item.completed"):
        item = raw.get("item") or {}
        it = item.get("type", "")
        started = t == "item.started"
        if it == "command_execution":
            if started:
                return [{"type": "tool_use", "payload": {
                    "id": item.get("id", ""), "name": "Bash",
                    "input": {"command": item.get("command", "")}}}]
            return [{"type": "tool_result", "payload": {
                "tool_use_id": item.get("id", ""),
                "content": str(item.get("aggregated_output", ""))[:2000],
                "is_error": (item.get("exit_code") or 0) != 0}}]
        if it == "file_change":
            files = ", ".join(c.get("path", "?") for c in item.get("changes", []))
            if started:
                return [{"type": "tool_use", "payload": {
                    "id": item.get("id", ""), "name": "Edit",
                    "input": {"file_path": files}}}]
            return [{"type": "tool_result", "payload": {
                "tool_use_id": item.get("id", ""), "content": f"changed: {files}",
                "is_error": item.get("status") == "failed"}}]
        if it == "agent_message" and not started:
            text = item.get("text", "")
            return [{"type": "text", "payload": {"text": text}}] if text.strip() else []
        if it in ("reasoning", "agent_message", "todo_list"):
            return []
    if t == "turn.completed":
        usage = raw.get("usage") or {}
        return [{"type": "result", "payload": {
            "subtype": "success", "cost_usd": None,
            "num_turns": None, "duration_ms": None,
            "result": "", "session_id": "",
            "tokens": usage.get("output_tokens")}}]
    if t in ("turn.started", "thread.completed"):
        return []
    return [{"type": "raw", "payload": {"data": raw}}]


def _parse_jsonl(buf: str, normalize) -> tuple[list[dict], str]:
    if "\n" not in buf:
        return [], buf
    complete, remainder = buf.rsplit("\n", 1)
    events = []
    for line in complete.split("\n"):
        line = line.strip()
        if not line or line in NOISE_LINES:
            continue
        try:
            events.extend(normalize(json.loads(line)))
        except ValueError:
            events.append({"type": "raw", "payload": {"line": line[:2000]}})
    return events, remainder


# ---- gemini: plain text — every output line becomes a timeline line ----------

def _parse_plaintext(buf: str) -> tuple[list[dict], str]:
    if "\n" not in buf:
        return [], buf
    complete, remainder = buf.rsplit("\n", 1)
    events = [{"type": "text", "payload": {"text": line.rstrip()[:2000]}}
              for line in complete.split("\n") if line.strip()]
    return events, remainder
