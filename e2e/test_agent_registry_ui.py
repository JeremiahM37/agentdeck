"""Real browser coverage for a custom runner configured against a local endpoint.

The executable is a tiny fixture, not a paid provider call. It proves the
registry is usable from Settings, a session launch carries model/provider/env,
masked secrets remain masked in the editor, and the same session is reachable
through the native PTY client.
"""
import json
import os
import pty
import select
import subprocess
import time

import pytest
from playwright.sync_api import expect

from conftest import _binary
from test_terminal_workspace import real_terminal


def _runner(tmp_path):
    record = tmp_path / "custom-runner.json"
    path = tmp_path / "custom-runner.py"
    path.write_text(
        "import json,os,sys,time\n"
        "from pathlib import Path\n"
        "Path(os.environ['RUNNER_RECORD']).write_text(json.dumps({'argv':sys.argv[1:], 'provider':os.environ.get('RUNNER_PROVIDER'), 'secret':os.environ.get('RUNNER_SECRET')}))\n"
        "print('CUSTOM RUNNER READY', flush=True)\n"
        "for line in sys.stdin:\n"
        "  print('CUSTOM ECHO '+line.rstrip(), flush=True)\n"
    )
    return path, record


@pytest.mark.parametrize("width", [390, 1440])
def test_custom_agent_settings_session_and_native_pty(page, real_terminal, tmp_path, width):
    t = real_terminal
    command, record = _runner(tmp_path)
    page.set_viewport_size({"width": width, "height": 844})
    page.goto(t["url"] + "/#targets")
    page.locator('[data-settings="agents"]').click()
    page.get_by_role("button", name="Add agent", exact=True).click()
    dialog = page.get_by_role("dialog", name="Add agent", exact=True)
    dialog.get_by_label("Name", exact=True).fill("local-proof")
    dialog.get_by_label("Command", exact=True).fill(str(command))
    dialog.get_by_label("Model flag", exact=True).fill("--model")
    dialog.get_by_label("Provider endpoint URL (optional)", exact=True).fill("http://127.0.0.1:11434/v1")
    dialog.get_by_label("Enable background tasks for this agent", exact=True).check()
    dialog.get_by_label("Task command", exact=True).fill(str(command))
    dialog.get_by_label("Task arguments (one per line; JSON array accepted)", exact=True).fill("[]")
    dialog.get_by_label("Prompt template", exact=True).fill("{prompt}")
    dialog.get_by_label("Environment (KEY=value lines; existing values are masked and retained)", exact=True).fill(
        f"RUNNER_RECORD={record}\nRUNNER_PROVIDER=http://127.0.0.1:11434/v1\nRUNNER_SECRET=synthetic-secret"
    )
    def reject(route):
        if route.request.method == "PUT":
            route.fulfill(status=503, content_type="application/json", body='{"detail":"Synthetic registry failure"}')
        else:
            route.continue_()
    page.route("**/api/agents", reject)
    dialog.get_by_role("button", name="Save runner", exact=True).click()
    expect(dialog.locator(".agent-dialog-status")).to_contain_text("Synthetic registry failure")
    expect(dialog.get_by_label("Name", exact=True)).to_have_value("local-proof")
    page.unroute("**/api/agents", reject)
    dialog.get_by_role("button", name="Save runner", exact=True).click()
    expect(page.locator(".agent-card", has_text="local-proof")).to_be_visible()

    # Reopen to prove the secret is not rendered into the browser form.
    page.locator(".agent-card").filter(has_text="local-proof").get_by_role("button", name="Edit", exact=True).click()
    edit = page.get_by_role("dialog", name="Edit agent local-proof", exact=True)
    expect(edit.get_by_label("Environment (KEY=value lines; existing values are masked and retained)", exact=True)).to_have_value(
        f"OPENAI_BASE_URL=••••\nRUNNER_PROVIDER=••••\nRUNNER_RECORD=••••\nRUNNER_SECRET=••••"
    )
    edit.get_by_role("button", name="Close", exact=True).click()

    page.goto(t["url"] + "/#sessions")
    page.locator("#sess-new").click()
    sheet = page.get_by_role("dialog", name="New session", exact=True)
    sheet.get_by_label("Name", exact=True).fill("Local proof session")
    sheet.get_by_label("Agent", exact=True).select_option("local-proof")
    sheet.get_by_label("Model", exact=True).fill("local-model")
    sheet.get_by_label("First message (optional)", exact=True).fill("hello custom runner")
    sheet.locator("#ns-go").click()
    for _ in range(100):
        if record.exists():
            break
        time.sleep(0.1)
    assert record.exists(), "custom runner did not launch"
    launched = json.loads(record.read_text())
    assert "--model" in launched["argv"] and "local-model" in launched["argv"]
    assert launched["provider"] == "http://127.0.0.1:11434/v1"
    assert launched["secret"] == "synthetic-secret"

    # A task uses the declared one-shot command and reaches the same custom
    # runner selector. It is not inferred from the interactive session args.
    project = t["api"]("/projects", {"name": "Local proof project", "target_id": t["target_id"],
                                     "repo_path": str(t["root"]), "default_agent": "local-proof"})
    page.goto(t["url"] + "/#board")
    page.locator("#fab").click()
    task_sheet = page.get_by_role("dialog", name="New task", exact=True)
    task_sheet.get_by_label("Project", exact=True).select_option(str(project["id"]))
    task_sheet.get_by_label("Title", exact=True).fill("Local proof task")
    task_sheet.get_by_label("Prompt — what should the agent do?", exact=False).fill("task proof")
    task_sheet.locator("#f-agent button[data-agent='local-proof']").click()
    task_sheet.get_by_role("button", name="Dispatch to board", exact=True).click()
    task_card = page.locator(".card", has_text="Local proof task")
    expect(task_card).to_be_visible(timeout=15000)
    expect(page.locator(".col.s-review .card", has_text="Local proof task")).to_be_visible(timeout=30000)
    task_launch = json.loads(record.read_text())
    assert "task proof" in " ".join(task_launch["argv"])
    assert task_launch["provider"] == "http://127.0.0.1:11434/v1"
    assert task_launch["secret"] == "synthetic-secret"

    # Native PTY attachment remains available for a custom runner session.
    session = next(row for row in t["api"]("/sessions") if row["name"] == "Local proof session")
    master, slave = pty.openpty()
    child = subprocess.Popen([_binary(), "attach", "session", str(session["id"])], stdin=slave, stdout=slave, stderr=slave,
                             env={**t["env"], "TERM": "xterm-256color"})
    os.close(slave)
    try:
        output = b""
        deadline = time.time() + 15
        while time.time() < deadline and b"CUSTOM RUNNER READY" not in output:
            if select.select([master], [], [], 0.2)[0]:
                output += os.read(master, 65536)
        assert b"CUSTOM RUNNER READY" in output
        os.write(master, b"pty-proof\r")
        deadline = time.time() + 10
        while time.time() < deadline and b"CUSTOM ECHO pty-proof" not in output:
            if select.select([master], [], [], 0.2)[0]:
                output += os.read(master, 65536)
        assert b"CUSTOM ECHO pty-proof" in output
    finally:
        os.write(master, b"\x02d")
        child.wait(timeout=10)
        os.close(master)
