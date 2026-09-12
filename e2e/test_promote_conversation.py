"""Real PTY proof for promoting the native conversation in a tracked shell.

The agent fixture is deliberately a small native identity writer: it owns a
real transcript FD and process receipt, but makes no paid Claude/Codex call.
"""
import json
import os
import fcntl
import pty
import select
import struct
import subprocess
import termios
import time
import uuid
import urllib.request
from pathlib import Path

from conftest import _binary
from test_terminal_workspace import real_terminal


def _pty(binary, env, *args):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))

    def tty():
        os.setsid()
        fcntl.ioctl(slave, termios.TIOCSCTTY, 0)

    child = subprocess.Popen([str(binary), *args], stdin=slave, stdout=slave,
                             stderr=slave, env={**env, "TERM": "xterm-256color"},
                             preexec_fn=tty)
    os.close(slave)
    return master, child


def _read(master, token, timeout=20, output=b""):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if token in output:
            return output
        if select.select([master], [], [], .2)[0]:
            try:
                output += os.read(master, 65536)
            except OSError:
                break
    raise AssertionError("missing %r in fresh PTY output: %r" % (token, output[-4000:]))


def _native_agent(t, agent="claude"):
    root = t["root"]
    # Use the product's quick-shell API, then cd into the tracked repository as
    # an operator would before starting Claude/Codex.
    shell = t["api"]("/shells", {"machine": "terminal-local"})
    t["id"] = shell["id"]
    row = shell
    home = root / "native-home"
    slug = "".join(c if c.isalnum() else "-" for c in str(root))
    folder = home / "projects" / slug
    folder.mkdir(parents=True)
    cid = "11111111-1111-4111-8111-111111111111"
    transcript = folder / (cid + ".jsonl")
    transcript.write_text("\n".join([
        json.dumps({"type": "user", "sessionId": cid, "cwd": str(root),
                    "message": {"role": "user", "content": "PROMOTION HISTORY PROOF"}}),
        json.dumps({"type": "assistant", "sessionId": cid, "cwd": str(root),
                    "message": {"role": "assistant", "content": "history retained"}}),
        "",
    ]))
    record_dir = home / "sessions"
    record_dir.mkdir()
    script = root / "native-agent.py"
    script.write_text("""import json, os, time
from pathlib import Path
home, cwd, cid = os.environ['NATIVE_HOME'], os.environ['NATIVE_CWD'], os.environ['NATIVE_CID']
pid = os.getpid()
start = Path('/proc', str(pid), 'stat').read_text().rsplit(')', 1)[1].split()[19]
Path(home, 'sessions', str(pid) + '.json').write_text(json.dumps({'pid': pid, 'procStart': start, 'sessionId': cid, 'cwd': cwd, 'kind': 'interactive', 'entrypoint': 'cli'}))
Path(cwd, 'native-ready').write_text(str(pid))
while True: time.sleep(.05)
""")
    row = t["api"]("/sessions/" + str(t["id"]))
    # Start the native CLI as a child of the existing tracked blank shell.
    # Replacing the pane would change its lifecycle identity and is not the
    # operator flow this feature promises to preserve.
    command = ("cd %s && env CLAUDE_CONFIG_DIR=%s NATIVE_HOME=%s NATIVE_CWD=%s NATIVE_CID=%s "
               "python3 %s >/dev/null 2>&1 &" %
               (str(root), str(home), str(home), str(root), cid, str(script)))
    subprocess.run(["tmux", "send-keys", "-t", "=" + row["tmux_session"] + ":", command, "Enter"],
                   env=t["env"], check=True)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline and not (root / "native-ready").exists():
        time.sleep(.05)
    assert (root / "native-ready").exists()
    t["native_home"] = home
    t["native_cid"] = cid
    t["native_pid"] = int((root / "native-ready").read_text())
    assert Path("/proc/%d/cwd" % t["native_pid"]).resolve() == root.resolve()
    assert (home / "sessions" / (str(t["native_pid"]) + ".json")).exists()
    # Native history lookup uses the configured agent profile, while the
    # tracked row remains the real shell's session record.
    req = urllib.request.Request(t["url"] + "/api/agents", method="PUT",
        data=json.dumps([{"name": "claude", "command": "claude",
                          "env": {"CLAUDE_CONFIG_DIR": str(home)}}]).encode(),
        headers={"Content-Type": "application/json"})
    urllib.request.urlopen(req, timeout=20).close()
    return row


def _promote(t, session_id, input_text):
    env = {**t["env"], "AGENTDECK_API": t["url"], "TMUX": ""}
    master, child = _pty(_binary(), env, "promote", str(session_id))
    try:
        output = _read(master, b"Detected session", output=b"")
        os.write(master, input_text.encode())
        output = _read(master, b"Conversation promoted", 20, output)
        child.wait(timeout=15)
        return output, child.returncode
    finally:
        if child.poll() is None:
            child.terminate(); child.wait(timeout=10)
        os.close(master)


def test_cli_promotes_live_native_conversation_in_place(real_terminal):
    t = real_terminal
    before_projects = {p["id"] for p in t["api"]("/projects")}
    source = _native_agent(t)
    old_pid = t["native_pid"]
    output, code = _promote(t, source["id"], "n\rConversation proof\ryes\r")
    assert code == 0, output
    session = t["api"]("/sessions/" + str(source["id"]))
    projects = [p for p in t["api"]("/projects") if p["id"] not in before_projects]
    assert len(projects) == 1
    assert session["project_id"] == projects[0]["id"]
    assert projects[0]["repo_path"] == str(t["root"])
    assert projects[0]["default_agent"] == "claude"
    assert Path("/proc/%d" % old_pid).exists(), "promotion restarted native process"
    assert session["tmux_session"] == source["tmux_session"]
    assert session["id"] == source["id"]
    history = t["api"]("/sessions/%d/conversations/%s" % (source["id"], t["native_cid"]))
    assert any("PROMOTION HISTORY PROOF" in json.dumps(m) for m in history["messages"])
    # The tracked tmux terminal remains attachable after the CLI returns.
    subprocess.run(["tmux", "has-session", "-t", "=" + source["tmux_session"]],
                   env=t["env"], check=True)


def test_cli_refuses_missing_native_process_without_partial_project(real_terminal):
    t = real_terminal
    source = _native_agent(t)
    before_projects = {p["id"] for p in t["api"]("/projects")}
    subprocess.run(["tmux", "kill-session", "-t", "=" + source["tmux_session"]],
                   env=t["env"], check=True)
    env = {**t["env"], "AGENTDECK_API": t["url"], "TMUX": ""}
    master, child = _pty(_binary(), env, "promote", str(source["id"]))
    try:
        out = _read(master, b"conversation", 20).decode(errors="replace")
        child.wait(timeout=15)
        assert child.returncode != 0
        assert "native" in out.lower() or "process" in out.lower()
    finally:
        if child.poll() is None: child.terminate(); child.wait(timeout=10)
        os.close(master)
    assert {p["id"] for p in t["api"]("/projects")} == before_projects


def test_cli_cancellation_leaves_running_conversation_unmodified(real_terminal):
    t = real_terminal
    source = _native_agent(t)
    before_projects = {p["id"] for p in t["api"]("/projects")}
    env = {**t["env"], "AGENTDECK_API": t["url"], "TMUX": ""}
    master, child = _pty(_binary(), env, "promote", str(source["id"]))
    try:
        _read(master, b"Project", 20)
        os.write(master, b"\x03")
        child.wait(timeout=15)
        assert child.returncode in (0, 1, 130, -2)
    finally:
        if child.poll() is None: child.terminate(); child.wait(timeout=10)
        os.close(master)
    row = t["api"]("/sessions/" + str(source["id"]))
    assert row.get("project_id") is None
    assert {p["id"] for p in t["api"]("/projects")} == before_projects
    assert Path("/proc/%d" % t["native_pid"]).exists()
