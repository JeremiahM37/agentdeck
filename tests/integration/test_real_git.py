"""Integration: worktree module against REAL git via LocalExecutor, and
ghost-run reconciliation with the real scheduler (no mock executor)."""
import asyncio
import subprocess

import pytest

from server import config, db
from server import executor as executor_pkg
from server.executor.base import ExecutorError
from server.executor.local import LocalExecutor
from server.worktree import (
    branch_name,
    capture_diff,
    ensure_worktree,
    remove_worktree,
    split_patch,
    worktree_path,
)
from tests.conftest import wait_for


def _mkrepo(path):
    def git(*args):
        subprocess.run(["git", "-C", str(path), *args], check=True,
                       capture_output=True,
                       env={"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@t",
                            "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@t",
                            "PATH": "/usr/bin:/bin"})
    path.mkdir(parents=True, exist_ok=True)
    subprocess.run(["git", "init", "-q", "-b", "main", str(path)], check=True)
    (path / "app.py").write_text('print("hello")\n')
    git("add", "app.py")
    git("commit", "-qm", "initial")
    return path


def _run(coro):
    # a worker thread, because the session-scoped Playwright sync fixture keeps a
    # running asyncio loop in the main thread during full-suite runs
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(1) as pool:
        return pool.submit(asyncio.run, coro).result()


def test_worktree_lifecycle_real_git(tmp_path):
    repo = _mkrepo(tmp_path / "repo")
    ex = LocalExecutor()
    wt = worktree_path(str(tmp_path / "wts"), 1, 1)
    br = branch_name(1, 1)

    _run(ensure_worktree(ex, str(repo), "main", br, wt))
    # runtime dir is excluded from git status
    (tmp_path / "wts" / "task1-a1" / ".agentdeck").mkdir()
    (tmp_path / "wts" / "task1-a1" / ".agentdeck" / "junk.txt").write_text("x")
    # verify-run artifacts must never leak into diffs/commits (found in real run)
    (tmp_path / "wts" / "task1-a1" / "__pycache__").mkdir()
    (tmp_path / "wts" / "task1-a1" / "__pycache__" / "app.cpython-311.pyc").write_bytes(b"\x00")
    (tmp_path / "wts" / "task1-a1" / "app.py").write_text('print("changed")\nNEW = 1\n')
    (tmp_path / "wts" / "task1-a1" / "extra.py").write_text("added = True\n")

    patch, files = _run(capture_diff(ex, wt, "main"))
    paths = {f["path"] for f in files}
    assert paths == {"app.py", "extra.py"}, paths        # .agentdeck/ excluded
    split = split_patch(patch)
    assert {s["path"] for s in split} == {"app.py", "extra.py"}
    assert any("+NEW = 1" in s["patch"] for s in split)

    # idempotent re-ensure (follow-up attempt reuses)
    _run(ensure_worktree(ex, str(repo), "main", br, wt))

    _run(remove_worktree(ex, str(repo), wt))
    r = subprocess.run(["git", "-C", str(repo), "worktree", "list"],
                       capture_output=True, text=True)
    assert "task1-a1" not in r.stdout


def test_worktree_bad_base_branch_raises(tmp_path):
    repo = _mkrepo(tmp_path / "repo2")
    ex = LocalExecutor()
    with pytest.raises(ExecutorError):
        _run(ensure_worktree(ex, str(repo), "no-such-branch",
                             "adk/task9-a1", str(tmp_path / "wt9")))


def test_dispatch_to_missing_repo_fails_task(client, monkeypatch):
    """Launch failure (repo path doesn't exist) surfaces as a failed task, not a hang.
    Uses a real LocalExecutor by overriding the mock-mode switch."""
    monkeypatch.setattr(config, "MOCK", False)
    executor_pkg.reset_cache()
    tid = client.post("/api/targets", json={"name": "real-local", "kind": "local"}).json()["id"]
    p = client.post("/api/projects", json={"name": "ghostp", "target_id": tid,
                                           "repo_path": "/nonexistent/repo"}).json()
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "doomed"}).json()
    client.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: client.get(f"/api/tasks/{t['id']}").json()["status"] == "failed",
             msg="failed launch")
    att = client.get(f"/api/tasks/{t['id']}").json()["attempt"]
    assert "worktree" in att["result"].get("error", "").lower() or att["result"]


def test_ghost_running_attempt_reconciled(client, monkeypatch):
    """Attempt marked running whose tmux session/worktree vanished → failed.
    Mirrors vibe-kanban issue #1571 (ghost runs stuck 'running' forever)."""
    monkeypatch.setattr(config, "MOCK", False)
    executor_pkg.reset_cache()
    tid = client.post("/api/targets", json={"name": "real-local2", "kind": "local"}).json()["id"]
    p = client.post("/api/projects", json={"name": "ghostq", "target_id": tid,
                                           "repo_path": "/tmp"}).json()
    t = client.post("/api/tasks", json={"project_id": p["id"], "title": "ghost"}).json()
    # forge a running attempt pointing at nothing (simulates crash / manual rm)
    aid = db.insert("attempts", {
        "task_id": t["id"], "n": 1, "status": "running", "token": "tok-ghost",
        "worktree_path": "/nonexistent/wt", "branch": "adk/ghost",
        "tmux_session": "adk-ghost-none", "log_offset": 0, "started_at": db.now()})
    db.update("tasks", t["id"], {"status": "running"})
    wait_for(lambda: client.get(f"/api/tasks/{t['id']}").json()["status"] == "failed",
             msg="ghost reconciled to failed")
    att = db.one("SELECT * FROM attempts WHERE id=?", (aid,))
    assert att["status"] == "failed"
    assert "disappeared" in db.unj(att["result_json"]).get("error", "")
