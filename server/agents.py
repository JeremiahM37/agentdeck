"""Agent adapter seam — everything CLI-specific per coding agent lives here.

claude  — first-class: stream-json events, hook approvals, session resume.
codex   — experimental: `codex exec --json` JSONL events; no gated mode, no resume.
gemini  — experimental: plain-text output mapped to timeline lines; no gated mode.

The run protocol is agent-agnostic (worktree + tmux + events file + exit_code),
so adapters only build the inner command and normalize output lines.
"""
import json
import shlex

from . import claude_runner

AGENTS = ("claude", "codex", "gemini")
# which agents support the hook-gated 'default' permission mode
GATED_CAPABLE = {"claude"}


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
                  env: dict | None = None) -> str:
    prefix = env_prefix(env, sandbox=sandbox)
    if agent == "claude":
        return claude_runner.launch_command(worktree, tmux_session, permission_mode,
                                            model=model, resume_session=resume_session,
                                            env_prefix=prefix)
    rt = claude_runner.runtime_dir(worktree)
    if agent == "codex":
        parts = ["codex", "exec", "--json"]
        if model:
            parts += ["-m", model]
        if permission_mode == "plan":
            parts += ["--sandbox", "read-only"]
        elif permission_mode == "bypassPermissions":
            parts += ["--dangerously-bypass-approvals-and-sandbox"]
        else:   # acceptEdits
            parts += ["--full-auto"]
        parts.append('"$(cat .agentdeck/prompt.md)"')
    elif agent == "gemini":
        parts = ["gemini", "-p", '"$(cat .agentdeck/prompt.md)"']
        if model:
            parts += ["-m", model]
        if permission_mode in ("acceptEdits", "bypassPermissions"):
            parts.append("--yolo")
    else:
        raise ValueError(f"unknown agent {agent!r}")
    inner = (f"cd {worktree} && {prefix}{' '.join(parts)} "
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
    if t == "item.completed":
        item = raw.get("item") or {}
        it = item.get("type", "")
        if it == "agent_message" and item.get("text", "").strip():
            return [{"type": "text", "payload": {"text": item["text"]}}]
        if it == "command_execution":
            return [{"type": "tool_use", "payload": {
                "id": item.get("id", ""), "name": "Bash",
                "input": {"command": item.get("command", "")}}},
                {"type": "tool_result", "payload": {
                    "tool_use_id": item.get("id", ""),
                    "content": str(item.get("aggregated_output", ""))[:2000],
                    "is_error": item.get("exit_code", 0) != 0}}]
        if it == "file_change":
            files = ", ".join(c.get("path", "?") for c in item.get("changes", []))
            return [{"type": "tool_use", "payload": {
                "id": item.get("id", ""), "name": "Edit", "input": {"file_path": files}}}]
        if it == "reasoning":
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
        if not line:
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
