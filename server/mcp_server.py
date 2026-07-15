#!/usr/bin/env python3
"""agentdeck MCP server — lets any MCP client (Claude Code, claude.ai bridges,
the homelab Discord bot via mcpo) file and steer agentdeck tasks.

Run (stdio):  .venv/bin/python server/mcp_server.py
Env: AGENTDECK_API (default http://127.0.0.1:9110), AGENTDECK_AUTH_TOKEN (optional).
"""
import json
import os
import sys
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from mcp.server.fastmcp import FastMCP  # noqa: E402

API = os.environ.get("AGENTDECK_API", "http://127.0.0.1:9110").rstrip("/")
TOKEN = os.environ.get("AGENTDECK_AUTH_TOKEN", "")

mcp = FastMCP("agentdeck")


def api(method: str, path: str, body: dict | None = None):
    headers = {"Content-Type": "application/json"}
    if TOKEN:
        headers["Authorization"] = f"Bearer {TOKEN}"
    req = urllib.request.Request(API + "/api" + path, method=method, headers=headers,
                                 data=json.dumps(body).encode() if body else None)
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.load(r)


@mcp.tool()
def board_summary() -> dict:
    """Current board state: task counts per column and pending approval count."""
    return api("GET", "/health")


@mcp.tool()
def list_projects() -> list:
    """Projects that tasks can be filed against (name, target, repo)."""
    return [{"id": p["id"], "name": p["name"], "target": p["target_name"],
             "repo": p["repo_path"]} for p in api("GET", "/projects")]


@mcp.tool()
def list_tasks(status: str = "") -> list:
    """List tasks, optionally filtered by status
    (backlog|queued|running|review|done|failed|cancelled)."""
    q = f"?status={urllib.parse.quote(status)}" if status else ""
    return [{"id": t["id"], "title": t["title"], "status": t["status"],
             "project": t["project_name"], "target": t["target_name"],
             "created_by": t["created_by"]} for t in api("GET", f"/tasks{q}")]


@mcp.tool()
def create_task(project: str, title: str, prompt: str, dispatch: bool = True,
                model: str = "", permission_mode: str = "acceptEdits") -> dict:
    """File a coding task. project = project name (see list_projects).
    dispatch=True starts an agent immediately; False parks it in the backlog."""
    match = [p for p in api("GET", "/projects") if p["name"] == project]
    if not match:
        return {"error": f"no project named {project!r}",
                "projects": [p["name"] for p in api("GET", "/projects")]}
    t = api("POST", "/tasks", {"project_id": match[0]["id"], "title": title,
                               "prompt": prompt, "model": model,
                               "permission_mode": permission_mode})
    if dispatch:
        t = api("POST", f"/tasks/{t['id']}/dispatch", {})
    return {"task_id": t["id"], "status": t["status"],
            "url": f"{API}/#task/{t['id']}"}


@mcp.tool()
def task_status(task_id: int, include_events: bool = False) -> dict:
    """Status, attempt result, verify outcome, and diff stats for a task."""
    t = api("GET", f"/tasks/{task_id}")
    out = {"id": t["id"], "title": t["title"], "status": t["status"],
           "attempt": t.get("attempt")}
    if include_events:
        evs = api("GET", f"/tasks/{task_id}/events")
        out["events_tail"] = [{"type": e["type"], "payload": e["payload"]}
                              for e in evs[-15:]]
    return out


@mcp.tool()
def task_diff(task_id: int) -> dict:
    """The unified diff a finished task produced (per file)."""
    return api("GET", f"/tasks/{task_id}/diff")


@mcp.tool()
def pending_approvals() -> list:
    """Approvals waiting on a human — tool name, input, and owning task."""
    return api("GET", "/approvals?status=pending")


@mcp.tool()
def decide_approval(approval_id: int, decision: str, note: str = "") -> dict:
    """Approve or deny a pending approval. decision: approved|denied."""
    return api("POST", f"/approvals/{approval_id}/decision",
               {"decision": decision, "note": note})


@mcp.tool()
def complete_task(task_id: int) -> dict:
    """Mark a reviewed task as done."""
    return api("POST", f"/tasks/{task_id}/complete")


@mcp.tool()
def request_changes(task_id: int, feedback: str) -> dict:
    """Send a reviewed task back for another attempt with feedback."""
    return api("POST", f"/tasks/{task_id}/followup", {"feedback": feedback})


if __name__ == "__main__":
    mcp.run()
