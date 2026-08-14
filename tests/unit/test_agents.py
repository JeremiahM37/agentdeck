import json

import pytest

from server.agents import launch_command, parse_stream_lines
from server.executor.pct import wrap


def test_claude_launch_unchanged():
    cmd = launch_command("claude", "/wt", "adk-1", "acceptEdits", model="opus")
    assert "claude -p" in cmd and "--model opus" in cmd and "stream-json" in cmd


def test_codex_launch_flags():
    cmd = launch_command("codex", "/wt", "adk-2", "acceptEdits", model="o4-mini")
    assert "codex exec --json" in cmd and "-m o4-mini" in cmd
    # --full-auto was removed upstream; workspace-write is the sandbox that lets
    # codex edit the worktree without the dangerous full-access escape hatch
    assert "--sandbox workspace-write" in cmd and "exit_code" in cmd
    assert "--full-auto" not in cmd
    assert "--sandbox read-only" in launch_command("codex", "/wt", "s", "plan")
    assert "--dangerously-bypass-approvals-and-sandbox" in \
        launch_command("codex", "/wt", "s", "bypassPermissions")


def test_non_claude_agents_get_stdin_redirect():
    # codex/gemini read stdin even with the prompt as an argument, and a tmux
    # pane never EOFs — without this the attempt hangs forever, looking alive
    assert "< /dev/null" in launch_command("codex", "/wt", "s", "acceptEdits")
    assert "< /dev/null" in launch_command("gemini", "/wt", "s", "acceptEdits")


def test_agent_binaries_are_overridable(monkeypatch):
    from server import config
    monkeypatch.setattr(config, "CODEX_BIN", "/home/me/.local/bin/codex")
    assert "/home/me/.local/bin/codex exec --json" in \
        launch_command("codex", "/wt", "s", "acceptEdits")


def test_gemini_launch_flags():
    cmd = launch_command("gemini", "/wt", "adk-3", "acceptEdits")
    assert "gemini -p" in cmd and "--yolo" in cmd
    assert "--yolo" not in launch_command("gemini", "/wt", "s", "plan")


def test_unknown_agent_raises():
    with pytest.raises(ValueError):
        launch_command("cursor", "/wt", "s", "acceptEdits")


# Captured from a REAL `codex exec --json` run (codex-cli 0.147.0). Synthetic
# fixtures hid two things: item.started exists, and codex prints a plain-text
# banner before its JSONL.
CODEX_STREAM = "\n".join([
    "Reading additional input from stdin...",
    json.dumps({"type": "thread.started", "thread_id": "th_1"}),
    json.dumps({"type": "turn.started"}),
    json.dumps({"type": "item.completed", "item": {
        "type": "agent_message", "id": "m1", "text": "Planning the change."}}),
    json.dumps({"type": "item.started", "item": {
        "type": "command_execution", "id": "c1", "command": "pytest -q",
        "aggregated_output": "", "exit_code": None, "status": "in_progress"}}),
    json.dumps({"type": "item.completed", "item": {
        "type": "command_execution", "id": "c1", "command": "pytest -q",
        "aggregated_output": "3 passed", "exit_code": 0, "status": "completed"}}),
    json.dumps({"type": "item.started", "item": {
        "type": "file_change", "id": "f1", "status": "in_progress",
        "changes": [{"path": "app.py", "kind": "update"}]}}),
    json.dumps({"type": "item.completed", "item": {
        "type": "file_change", "id": "f1", "status": "completed",
        "changes": [{"path": "app.py", "kind": "update"}]}}),
    json.dumps({"type": "turn.completed", "usage": {"output_tokens": 420}}),
]) + "\n"


def test_codex_parser_maps_real_thread_events():
    events, rem = parse_stream_lines("codex", CODEX_STREAM)
    assert rem == ""
    types = [e["type"] for e in events]
    # the stdin banner is dropped, not surfaced as a junk 'raw' card
    assert types == ["init", "text", "tool_use", "tool_result",
                     "tool_use", "tool_result", "result"]
    assert events[0]["payload"]["session_id"] == "th_1"
    # tool_use comes from item.started, so the command shows while it runs
    assert events[2]["payload"]["input"]["command"] == "pytest -q"
    assert events[3]["payload"]["content"] == "3 passed"
    assert events[3]["payload"]["is_error"] is False
    assert "app.py" in events[4]["payload"]["input"]["file_path"]
    assert events[6]["payload"]["tokens"] == 420


def test_codex_failed_command_marks_error():
    line = json.dumps({"type": "item.completed", "item": {
        "type": "command_execution", "id": "c9", "command": "pytest",
        "aggregated_output": "1 failed", "exit_code": 1, "status": "completed"}})
    events, _ = parse_stream_lines("codex", line + "\n")
    assert events[0]["payload"]["is_error"] is True


def test_codex_inflight_command_is_not_an_error():
    # exit_code is null while running — treating that as failure marked every
    # in-flight command red
    line = json.dumps({"type": "item.started", "item": {
        "type": "command_execution", "id": "c9", "command": "sleep 1",
        "exit_code": None, "status": "in_progress"}})
    events, _ = parse_stream_lines("codex", line + "\n")
    assert [e["type"] for e in events] == ["tool_use"]


def test_codex_parser_tolerates_garbage():
    events, _ = parse_stream_lines("codex", "not json\n" +
                                   json.dumps({"type": "mystery"}) + "\n")
    assert [e["type"] for e in events] == ["raw", "raw"]


def test_gemini_plaintext_lines_become_timeline():
    events, rem = parse_stream_lines("gemini", "Reading files...\n\nDone, updated app.py\npartial")
    assert [e["payload"]["text"] for e in events] == \
        ["Reading files...", "Done, updated app.py"]
    assert rem == "partial"


def test_pct_wrap_quoting():
    cmd = wrap("101", "git -C '/root/adk demo' diff", cwd="/root")
    assert cmd.startswith("sudo pct exec 101 -- bash -c ")
    assert "cd /root" in cmd
    # single-quoted payload survives shell quoting round-trip
    assert "adk demo" in cmd
