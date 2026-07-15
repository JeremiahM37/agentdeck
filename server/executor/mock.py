"""MockExecutor — scripted fake target. Powers the whole hermetic test suite and
AGENTDECK_MOCK=1 demo mode. Emits REAL claude stream-json shapes so the parser
is exercised end to end.

Scenario markers in the prompt:
  [mock:approval]  agent requests a hook approval mid-run (real HTTP to the server)
  [mock:fail]      agent exits non-zero
  [mock:slow]      agent takes ~3x longer
"""
import asyncio
import json
import os
import re

from .base import ExecResult, Executor

MOCK_DIFF = """diff --git a/app.py b/app.py
index 83db48f..bf2f3f4 100644
--- a/app.py
+++ b/app.py
@@ -1,4 +1,7 @@
 def main():
-    print("hello")
+    print("hello, agentdeck")
+
+def health():
+    return {"ok": True}
"""
MOCK_NUMSTAT = "4\t1\tapp.py\n"


class MockExecutor(Executor):
    def __init__(self, http_client_factory=None):
        self.fs: dict[str, bytes] = {}
        self.agents: dict[str, asyncio.Task] = {}   # session name -> fake agent
        self.finished: set[str] = set()
        self.cmd_log: list[str] = []                # every run() call, for assertions
        self.delay = float(os.environ.get("AGENTDECK_MOCK_DELAY", "0.4"))
        # tests inject an httpx client bound to the ASGI app; demo mode uses real HTTP
        self.http_client_factory = http_client_factory

    # ---- Executor interface -------------------------------------------------
    async def run(self, cmd: str, cwd: str = "", timeout: float = 120) -> ExecResult:
        self.cmd_log.append(cmd)
        if cmd.startswith("sudo pvesh get /cluster/nextid"):
            return ExecResult(0, "9001\n", "")
        if cmd.startswith(("sudo pct clone", "sudo pct start", "sudo pct stop",
                           "sudo pct destroy", "sudo pct push", "sudo pct exec")):
            return ExecResult(0, "", "")
        if cmd.startswith("git clone"):
            return ExecResult(0, "", "")
        if cmd.startswith("rm -f "):
            for tok in cmd.split()[2:]:
                self.fs.pop(tok, None)
            return ExecResult(0, "", "")
        if cmd.startswith("tmux new-session"):
            m = re.search(r"-s (\S+)", cmd)
            sess = m.group(1)
            wt = _extract_cd(cmd)
            self.agents[sess] = asyncio.create_task(self._fake_agent(sess, wt))
            return ExecResult(0, "", "")
        if cmd.startswith("tmux has-session"):
            m = re.search(r"-t (\S+)", cmd)
            alive = m and m.group(1) in self.agents and not self.agents[m.group(1)].done()
            return ExecResult(0 if alive else 1, "", "")
        if cmd.startswith("tmux kill-session"):
            m = re.search(r"-t (\S+)", cmd)
            if m and m.group(1) in self.agents:
                self.agents[m.group(1)].cancel()
            return ExecResult(0, "", "")
        if "diff --numstat" in cmd:
            return ExecResult(0, MOCK_NUMSTAT, "")
        if "diff --no-color" in cmd or re.search(r"\bgit\b.*\bdiff\b", cmd):
            return ExecResult(0, MOCK_DIFF, "")
        if "status --porcelain" in cmd:
            return ExecResult(0, " M app.py\n", "")
        if "--version" in cmd or cmd.startswith("df "):
            return ExecResult(0, "mock 1.0", "")
        if cmd.startswith('claude -p "Reply with exactly: ok"'):
            assert "< /dev/null" in cmd, "deep probe must redirect stdin (ssh hang)"
            return ExecResult(0, "ok", "")
        if "mockverify-fail" in cmd:
            return ExecResult(1, "", "2 failed, 3 passed")
        if "mockverify-pass" in cmd:
            return ExecResult(0, "5 passed in 0.1s", "")
        if "git add -A && git commit" in cmd:
            return ExecResult(0, "[adk 1a2b3c4] mock commit", "")
        if cmd.startswith("git") and " push " in cmd:
            return ExecResult(0, "branch pushed (mock)", "")
        if cmd.startswith("gh pr create"):
            return ExecResult(0, "https://github.com/mock/repo/pull/7", "")
        return ExecResult(0, "", "")   # git worktree add, mkdir, exclude appends, …

    async def read_file(self, path: str, offset: int = 0) -> bytes:
        return self.fs.get(path, b"")[offset:]

    async def write_file(self, path: str, data: bytes) -> None:
        self.fs[path] = data

    async def check(self) -> dict:
        return {"git": "git version 2.43 (mock)", "tmux": "tmux 3.4 (mock)",
                "claude": "2.0.0 (mock)", "python3": "Python 3.12 (mock)",
                "disk_free": "42G"}

    # ---- fake agent ----------------------------------------------------------
    def _append(self, path: str, line: dict) -> None:
        self.fs[path] = self.fs.get(path, b"") + (json.dumps(line) + "\n").encode()

    async def _fake_agent(self, sess: str, wt: str) -> None:
        rt = f"{wt}/.agentdeck"
        events, prompt = f"{rt}/events.jsonl", self.fs.get(f"{rt}/prompt.md", b"").decode()
        slow = 3.0 if "[mock:slow]" in prompt else 1.0
        sid = f"mock-sess-{sess}"
        try:
            self._append(events, {"type": "system", "subtype": "init", "session_id": sid,
                                  "model": "claude-mock", "tools": ["Bash", "Edit", "Write"]})
            await asyncio.sleep(self.delay * slow)
            self._append(events, {"type": "assistant", "message": {"content": [
                {"type": "text", "text": "Reading the codebase and planning the change."}]}})
            await asyncio.sleep(self.delay * slow)

            if "[mock:subtask]" in prompt:
                await self._hook_post(rt, "/api/hook/tasks", {
                    "title": "Agent follow-up: add tests",
                    "prompt": "Write tests for the new health() endpoint.",
                    "dispatch": False})
            if "[mock:note]" in prompt:
                await self._hook_post(rt, "/api/hook/notes", {
                    "note": "Auth uses bcrypt; integration tests live in tests/."})

            if "[mock:approval]" in prompt:
                ok = await self._request_approval(rt)
                if not ok:
                    self._append(events, {"type": "assistant", "message": {"content": [
                        {"type": "text", "text": "Action denied by operator — stopping."}]}})
                    self._finish(rt, sid, rc=0, result="Stopped: operator denied the action.")
                    return

            self._append(events, {"type": "assistant", "message": {"content": [
                {"type": "tool_use", "id": "tu_1", "name": "Edit",
                 "input": {"file_path": "app.py", "old_string": "hello", "new_string": "hello, agentdeck"}}]}})
            await asyncio.sleep(self.delay * slow)
            self._append(events, {"type": "user", "message": {"content": [
                {"type": "tool_result", "tool_use_id": "tu_1", "content": "Edit applied"}]}})
            await asyncio.sleep(self.delay * slow)

            if "[mock:fail]" in prompt:
                self._append(events, {"type": "assistant", "message": {"content": [
                    {"type": "text", "text": "Hit an unrecoverable error."}]}})
                self._finish(rt, sid, rc=1, result="error")
                return
            if "[mock:reject-verdict]" in prompt:
                result = "Reviewed the diff. VERDICT: REQUEST_CHANGES — rename health() and add a test."
            elif "[mock:approve-verdict]" in prompt:
                result = "Reviewed the diff. VERDICT: APPROVE — clean, focused change."
            else:
                result = "Done: updated app.py and added health()."
            self._finish(rt, sid, rc=0, result=result)
        except asyncio.CancelledError:
            pass
        finally:
            self.finished.add(sess)

    def _finish(self, rt: str, sid: str, rc: int, result: str) -> None:
        self._append(f"{rt}/events.jsonl", {
            "type": "result", "subtype": "success" if rc == 0 else "error",
            "total_cost_usd": 0.0123, "duration_ms": 3456, "num_turns": 3,
            "result": result, "session_id": sid})
        self.fs[f"{rt}/exit_code"] = f"{rc}\n".encode()

    async def _hook_post(self, rt: str, path: str, payload: dict) -> None:
        """Simulate the agent using .agentdeck/adk.py (tasks/notes hooks)."""
        import httpx
        env = dict(line.split("=", 1) for line in
                   self.fs.get(f"{rt}/env", b"").decode().splitlines() if "=" in line)
        url, token = env.get("ADK_URL", ""), env.get("ADK_TOKEN", "")
        if not token:
            return
        client = self.http_client_factory() if self.http_client_factory \
            else httpx.AsyncClient(base_url=url)
        try:
            await client.post(path, json={"token": token, **payload})
        finally:
            await client.aclose()

    async def _request_approval(self, rt: str) -> bool:
        """Exercise the REAL hook flow: read generated settings, POST, long-poll."""
        import httpx
        try:
            hook_cmd = json.loads(self.fs.get(f"{rt}/settings.json", b"{}").decode())[
                "hooks"]["PreToolUse"][0]["hooks"][0]["command"]
            url = re.search(r"AGENTDECK_URL=(\S+)", hook_cmd).group(1)
            token = re.search(r"AGENTDECK_TOKEN=(\S+)", hook_cmd).group(1)
        except (KeyError, AttributeError, ValueError):
            return True   # no hooks configured (permission mode not 'default')
        client = self.http_client_factory() if self.http_client_factory else httpx.AsyncClient(base_url=url)
        try:
            r = await client.post("/api/hook/approval", json={
                "token": token, "tool_name": "Bash",
                "tool_input": {"command": "rm -rf build/ && make deploy"}})
            aid = r.json()["id"]
            for _ in range(600):
                r = await client.get(f"/api/hook/approval/{aid}/decision")
                d = r.json()
                if d["status"] == "approved":
                    return True
                if d["status"] in ("denied", "expired"):
                    return False
        finally:
            await client.aclose()
        return False


def _extract_cd(cmd: str) -> str:
    m = re.search(r"cd (\S+) &&", cmd)
    return m.group(1) if m else "/mock"
