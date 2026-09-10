"""Project skills controls through the real browser and terminal surfaces."""

import json
import os
import pty
import select
import subprocess
import threading
import time
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest
from playwright.sync_api import expect

from conftest import DESKTOP, PHONE, _binary
from test_terminal_dashboard import Dashboard
from test_ui import _tab


def _skill_payload(agent):
    return {"skills": [{
        "id": f"repo/{agent}-review", "name": f"{agent.title()} Review",
        "source": "repo", "source_path": "/fixture/repo/.agents/skills/review",
        "entry_name": "review", "kind": "dir", "description": "Review changes safely",
    }, {
        "id": f"configured:0/{agent}-lint", "name": f"{agent.title()} Lint",
        "source": "configured:0", "source_path": "/fixture/skills/lint",
        "entry_name": "lint", "kind": "dir", "description": "Check style",
    }]}


class _SkillAPI(BaseHTTPRequestHandler):
    state = {"attachments": [], "next": 1, "attach_attempts": 0, "requests": []}

    def log_message(self, *_):
        pass

    def _json(self, status, body):
        raw = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def _body(self):
        return json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        q = urllib.parse.parse_qs(parsed.query)
        self.state["requests"].append(("GET", parsed.path))
        if parsed.path == "/api/health": return self._json(200, {"version": "fixture"})
        if parsed.path == "/api/projects":
            return self._json(200, [{"id": 7, "name": "Skills fixture", "target_id": 1,
                "target_name": "fixture-target", "target_kind": "local", "repo_path": "/fixture/repo",
                "default_agent": "claude", "capability_profile": "restricted", "skill_sources_json": "[]"}])
        if parsed.path == "/api/targets": return self._json(200, [{"id": 1, "name": "fixture-target", "kind": "local"}])
        if parsed.path in ("/api/agents", "/api/launch-profiles", "/api/sessions", "/api/tasks", "/api/routines", "/api/approvals"):
            return self._json(200, [])
        if parsed.path == "/api/skills": return self._json(200, _skill_payload(q.get("agent", ["claude"])[0]))
        if parsed.path == "/api/projects/7/skills":
            agent = q.get("agent", ["claude"])[0]
            return self._json(200, {"attachments": [a for a in self.state["attachments"] if a["agent"] == agent]})
        self._json(404, {"detail": "not found"})

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        body = self._body()
        self.state["requests"].append(("POST", parsed.path, body))
        if parsed.path == "/api/projects/7/skills":
            self.state["attach_attempts"] += 1
            if self.state["attach_attempts"] == 1:
                return self._json(409, {"detail": "skill is already attached"})
            row = {"id": self.state["next"], "project_id": 7, "target_id": 1,
                   "agent": body["agent"], "skill_id": body["skill_id"], "source_id": "repo",
                   "entry_name": "review", "target_rel": ".agents/skills/review"}
            self.state["next"] += 1; self.state["attachments"].append(row)
            return self._json(201, {"attachment": row, "mode": "symlink", "owned": True})
        self._json(404, {"detail": "not found"})

    def do_DELETE(self):
        parsed = urllib.parse.urlparse(self.path)
        self.state["requests"].append(("DELETE", parsed.path))
        if parsed.path.startswith("/api/projects/7/skills/"):
            aid = int(parsed.path.rsplit("/", 1)[1])
            self.state["attachments"] = [a for a in self.state["attachments"] if a["id"] != aid]
            return self._json(200, {"removed": True, "preserved": []})
        self._json(404, {"detail": "not found"})

    def do_PATCH(self):
        parsed = urllib.parse.urlparse(self.path)
        body = self._body(); self.state["requests"].append(("PATCH", parsed.path, body))
        if parsed.path == "/api/projects/7": return self._json(200, {"skill_sources_json": json.dumps(body.get("skill_sources", []))})
        self._json(404, {"detail": "not found"})


@pytest.fixture()
def skill_api():
    _SkillAPI.state = {"attachments": [], "next": 1, "attach_attempts": 0, "requests": []}
    server = ThreadingHTTPServer(("127.0.0.1", 0), _SkillAPI)
    thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}", _SkillAPI.state
    finally:
        server.shutdown(); thread.join(timeout=3); server.server_close()


@pytest.mark.parametrize("page", [PHONE, DESKTOP], indirect=True, ids=["phone390", "desktop1440"])
def test_project_skills_browser_catalog_search_retry_and_provider_race(page, server):
    target = page.request.get(f"{server}/api/targets").json()[0]
    project = page.request.post(f"{server}/api/projects", data={
        "name": "Skills browser fixture", "target_id": target["id"], "repo_path": "/mock/skills-ui",
        "default_agent": "claude"}).json()
    state = {"attachments": [], "attempts": 0}
    project_id = project["id"]

    def route_skill(route):
        request = route.request; parsed = urllib.parse.urlparse(request.url)
        query = urllib.parse.parse_qs(parsed.query); agent = query.get("agent", ["claude"])[0]
        if request.method == "GET" and parsed.path == "/api/skills":
            # Delay the reload that is immediately superseded by Codex. The
            # generation guard must prevent this stale Claude result repainting.
            if agent == "claude" and state.get("delay_claude"):
                time.sleep(.35)
            return route.fulfill(status=200, content_type="application/json", body=json.dumps(_skill_payload(agent)))
        if request.method == "GET" and parsed.path == f"/api/projects/{project_id}/skills":
            return route.fulfill(status=200, content_type="application/json", body=json.dumps({"attachments": [a for a in state["attachments"] if a["agent"] == agent]}))
        if request.method == "POST" and parsed.path == f"/api/projects/{project_id}/skills":
            state["attempts"] += 1
            if state["attempts"] == 1:
                return route.fulfill(status=409, content_type="application/json", body=json.dumps({"detail": "skill is already attached"}))
            body = request.post_data_json
            row = {"id": 1, "project_id": project_id, "target_id": target["id"], "agent": body["agent"], "skill_id": body["skill_id"], "source_id": "repo", "entry_name": "review"}
            state["attachments"].append(row)
            return route.fulfill(status=201, content_type="application/json", body=json.dumps({"attachment": row, "mode": "symlink", "owned": True}))
        if request.method == "DELETE" and parsed.path.startswith(f"/api/projects/{project_id}/skills/"):
            state["attachments"] = []
            return route.fulfill(status=200, content_type="application/json", body=json.dumps({"removed": True, "preserved": []}))
        return route.continue_()

    page.route("**/api/skills**", route_skill)
    page.route(f"**/api/projects/{project_id}/skills**", route_skill)
    try:
        page.goto(server); _tab(page, "targets")
        page.locator('[data-settings="projects"]').click()
        page.fill("#pj-search", "Skills browser fixture")
        page.locator(".pjrow", has_text="Skills browser fixture").click()
        card = page.locator("#sheet .project-skills")
        expect(card.locator(".skills-status")).to_contain_text("available", timeout=10000)
        card.locator(".skills-search").fill("review")
        expect(card.locator(".catalog-skill")).to_have_count(1)
        assert "repo" in card.locator(".catalog-skill").inner_text()

        state["delay_claude"] = True
        card.locator(".skills-reload").click()
        card.locator(".skills-agent").select_option("codex")
        expect(card.locator(".skills-status")).to_contain_text("available", timeout=10000)
        expect(card.locator(".catalog-skill").first.locator("b")).to_contain_text("Codex Review")

        attach = card.locator(".catalog-skill").first.locator(".skill-attach")
        attach.click(); expect(card.locator(".skills-status")).to_contain_text("already attached", timeout=10000)
        attach.click(); expect(card.locator(".skills-status")).to_have_text("Skill attached.", timeout=10000)
        card.locator(".attached-skill .skill-detach").click()
        expect(card.locator(".skills-status")).to_have_text("Skill detached.", timeout=10000)
        assert page.evaluate("document.body.scrollWidth <= window.innerWidth")
        page.screenshot(path=f"/tmp/agentdeck-skills-{page.viewport_size['width']}.png", full_page=True)
    finally:
        page.request.delete(f"{server}/api/projects/{project_id}")


def _pty_console(base, project_id=7):
    master, slave = pty.openpty()
    def controlling_terminal():
        os.setsid(); import fcntl, termios
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)
    proc = subprocess.Popen([_binary(), "console", "--plain"], stdin=slave, stdout=slave, stderr=slave,
                            env={**os.environ, "AGENTDECK_API": base}, preexec_fn=controlling_terminal)
    os.close(slave); output = b""
    def wait(token, timeout=12):
        nonlocal output
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if token in output: return
            if select.select([master], [], [], .1)[0]:
                try: output += os.read(master, 65536)
                except OSError: break
        raise AssertionError(f"missing {token!r}: {output.decode(errors='replace')}")
    def wait_count(token, count, timeout=12):
        nonlocal output
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            if output.count(token) >= count: return
            if select.select([master], [], [], .1)[0]:
                try: output += os.read(master, 65536)
                except OSError: break
        raise AssertionError(f"missing {count} occurrences of {token!r}: {output.decode(errors='replace')}")
    def send(s): os.write(master, s if isinstance(s, bytes) else s.encode())
    try:
        wait(b"Open:"); send(b"4\n"); wait(b"Choose:"); send(f"{project_id}\n".encode()); wait(b"Action:"); send(b"skills\n")
        wait(b"Available:"); send(b"a\n"); wait(b"Skill ID:"); send(b"repo/claude-review\n"); wait(b"Attach failed")
        # A transient/duplicate response stays in the editor loop; retrying is
        # an explicit second operation and does not restart any terminal.
        send(b"a\n"); wait(b"Skill ID:"); send(b"repo/claude-review\n"); wait_count(b"PROJECT SKILLS", 2)
        send(b"d\n"); wait(b"Attachment ID:"); send(b"1\n"); wait_count(b"PROJECT SKILLS", 3)
        send(b"b\n"); wait_count(b"Action:", 2); send(b"b\n"); wait_count(b"Choose:", 2); send(b"b\n"); wait_count(b"Open:", 2); send(b"q\n"); proc.wait(timeout=10)
        assert proc.returncode == 0
    finally:
        if proc.poll() is None: proc.terminate(); proc.wait(timeout=10)
        os.close(master)


def test_project_skills_plain_console_real_pty_crud_and_conflict(skill_api):
    base, state = skill_api
    _pty_console(base)
    assert state["attach_attempts"] == 2
    assert any(r[0] == "DELETE" for r in state["requests"])


def test_project_skills_dashboard_real_pty_has_native_controls(skill_api, tmp_path):
    base, state = skill_api
    env = {**os.environ, "AGENTDECK_API": base, "AGENTDECK_ATTACH_HOST": ""}
    t = {"url": base, "env": env, "root": Path(tmp_path)}
    d = Dashboard(t)
    try:
        d.wait("AgentDeck"); d.send("4"); d.wait("Skills fixture"); d.send("m")
        for _ in range(9): d.send("j")
        d.send("\r"); d.wait("Project skills"); d.wait("Attach discovered skill")
        d.send("\x13"); d.wait("skill is already attached")
        # The form remains open after the conflict, so retrying the same
        # selected skill is an explicit second action with no process restart.
        d.send("\x13"); d.wait("Attach skill completed")
        assert any(r[0] == "POST" and r[1] == "/api/projects/7/skills" for r in state["requests"])
    finally:
        d.close()
