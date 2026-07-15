import json

from server.claude_runner import hook_settings, launch_command, parse_stream_lines

INIT = json.dumps({"type": "system", "subtype": "init", "session_id": "s-42",
                   "model": "claude-opus", "tools": ["Bash", "Edit"]})
TEXT = json.dumps({"type": "assistant", "message": {"content": [
    {"type": "text", "text": "Working on it."}]}})
TOOL = json.dumps({"type": "assistant", "message": {"content": [
    {"type": "tool_use", "id": "tu1", "name": "Bash", "input": {"command": "ls"}}]}})
TOOL_RES = json.dumps({"type": "user", "message": {"content": [
    {"type": "tool_result", "tool_use_id": "tu1",
     "content": [{"type": "text", "text": "file1\nfile2"}], "is_error": False}]}})
RESULT = json.dumps({"type": "result", "subtype": "success", "total_cost_usd": 0.5,
                     "duration_ms": 1000, "num_turns": 4, "result": "done",
                     "session_id": "s-42"})


def test_full_stream():
    buf = "\n".join([INIT, TEXT, TOOL, TOOL_RES, RESULT]) + "\n"
    events, rem = parse_stream_lines(buf)
    assert rem == ""
    types = [e["type"] for e in events]
    assert types == ["init", "text", "tool_use", "tool_result", "result"]
    assert events[0]["payload"]["session_id"] == "s-42"
    assert events[2]["payload"]["input"]["command"] == "ls"
    assert "file1" in events[3]["payload"]["content"]
    assert events[4]["payload"]["cost_usd"] == 0.5


def test_partial_line_buffered():
    events, rem = parse_stream_lines(INIT + "\n" + TEXT[:20])
    assert [e["type"] for e in events] == ["init"]
    assert rem == TEXT[:20]


def test_no_newline_all_buffered():
    events, rem = parse_stream_lines(INIT[:30])
    assert events == [] and rem == INIT[:30]


def test_malformed_json_survives():
    events, _ = parse_stream_lines("this is not json\n" + INIT + "\n")
    assert events[0]["type"] == "raw"
    assert events[1]["type"] == "init"


def test_housekeeping_noise_dropped():
    lines = [
        json.dumps({"type": "rate_limit_event", "rate_limit_info": {}}),
        json.dumps({"data": {"type": "rate_limit_event"}, "uuid": "u1"}),
        json.dumps({"type": "system", "subtype": "compact_boundary"}),
    ]
    events, _ = parse_stream_lines("\n".join(lines) + "\n")
    assert events == []


def test_unknown_type_kept_raw():
    events, _ = parse_stream_lines(json.dumps({"type": "hologram", "x": 1}) + "\n")
    assert events[0]["type"] == "raw"


def test_empty_text_blocks_skipped():
    line = json.dumps({"type": "assistant", "message": {"content": [
        {"type": "text", "text": "  "}]}})
    events, _ = parse_stream_lines(line + "\n")
    assert events == []


def test_launch_command_shapes():
    cmd = launch_command("/wt/task1-a1", "adk-1", "acceptEdits", model="opus")
    assert cmd.startswith("tmux new-session -d -s adk-1 ")
    assert "--permission-mode acceptEdits" in cmd
    assert "--model opus" in cmd
    assert "--settings" not in cmd          # hooks only in gated mode
    assert "stream-json" in cmd and "exit_code" in cmd

    gated = launch_command("/wt/x", "adk-2", "default", resume_session="s-9")
    assert "--settings .agentdeck/settings.json" in gated
    assert "--resume s-9" in gated


def test_hook_settings_carry_url_and_token():
    s = hook_settings("http://cp:9110", "tok123")
    cmd = s["hooks"]["PreToolUse"][0]["hooks"][0]["command"]
    assert "AGENTDECK_URL=http://cp:9110" in cmd
    assert "AGENTDECK_TOKEN=tok123" in cmd
    assert s["hooks"]["PreToolUse"][0]["matcher"].startswith("Bash")
