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
    assert "--full-auto" in cmd and "exit_code" in cmd
    assert "--sandbox read-only" in launch_command("codex", "/wt", "s", "plan")
    assert "--dangerously-bypass-approvals-and-sandbox" in \
        launch_command("codex", "/wt", "s", "bypassPermissions")


def test_gemini_launch_flags():
    cmd = launch_command("gemini", "/wt", "adk-3", "acceptEdits")
    assert "gemini -p" in cmd and "--yolo" in cmd
    assert "--yolo" not in launch_command("gemini", "/wt", "s", "plan")


def test_unknown_agent_raises():
    with pytest.raises(ValueError):
        launch_command("cursor", "/wt", "s", "acceptEdits")


CODEX_STREAM = "\n".join([
    json.dumps({"type": "thread.started", "thread_id": "th_1"}),
    json.dumps({"type": "turn.started"}),
    json.dumps({"type": "item.completed", "item": {
        "type": "agent_message", "id": "m1", "text": "Planning the change."}}),
    json.dumps({"type": "item.completed", "item": {
        "type": "command_execution", "id": "c1", "command": "pytest -q",
        "aggregated_output": "3 passed", "exit_code": 0}}),
    json.dumps({"type": "item.completed", "item": {
        "type": "file_change", "id": "f1",
        "changes": [{"path": "app.py"}, {"path": "util.py"}]}}),
    json.dumps({"type": "turn.completed", "usage": {"output_tokens": 420}}),
]) + "\n"


def test_codex_parser_maps_thread_events():
    events, rem = parse_stream_lines("codex", CODEX_STREAM)
    assert rem == ""
    types = [e["type"] for e in events]
    assert types == ["init", "text", "tool_use", "tool_result", "tool_use", "result"]
    assert events[0]["payload"]["session_id"] == "th_1"
    assert events[2]["payload"]["input"]["command"] == "pytest -q"
    assert "app.py" in events[4]["payload"]["input"]["file_path"]
    assert events[5]["payload"]["tokens"] == 420


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
