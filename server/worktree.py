"""Git worktree lifecycle + diff capture, all through the Executor interface."""
import re

from .executor.base import Executor, ExecutorError


def branch_name(task_id: int, attempt_n: int) -> str:
    return f"adk/task{task_id}-a{attempt_n}"


def worktree_path(workroot: str, task_id: int, attempt_n: int) -> str:
    return f"{workroot.rstrip('/')}/task{task_id}-a{attempt_n}"


def default_workroot(repo_path: str) -> str:
    parent = repo_path.rstrip("/").rsplit("/", 1)[0]
    return f"{parent}/.agentdeck-worktrees"


async def ensure_worktree(ex: Executor, repo: str, base_branch: str,
                          branch: str, path: str) -> None:
    r = await ex.run(f"git -C {_q(repo)} worktree add -b {_q(branch)} {_q(path)} {_q(base_branch)}",
                     timeout=120)
    if not r.ok:
        # follow-up attempts reuse an existing worktree/branch — that's fine
        if "already exists" in r.stderr or "already checked out" in r.stderr:
            return
        raise ExecutorError(f"worktree add failed: {r.stderr.strip() or r.stdout.strip()}")
    await add_excludes(ex, path)


async def add_excludes(ex: Executor, repo_dir: str) -> None:
    """Keep our runtime dir + verify-run artifacts out of git status/diff/commit."""
    for pattern in (".agentdeck/", "__pycache__/", "*.pyc"):
        await ex.run(
            f"ex_file=$(git -C {_q(repo_dir)} rev-parse --git-common-dir)/info/exclude; "
            f"grep -qx {_q(pattern)} \"$ex_file\" 2>/dev/null || echo {_q(pattern)} >> \"$ex_file\"",
            timeout=30)


async def capture_diff(ex: Executor, wt: str, base_branch: str) -> tuple[str, list[dict]]:
    """Full patch + per-file stats of everything the attempt changed vs base."""
    await ex.run(f"git -C {_q(wt)} add -A -N", timeout=60)
    patch = (await ex.run(f"git -C {_q(wt)} diff --no-color {_q(base_branch)}", timeout=120)).stdout
    numstat = (await ex.run(f"git -C {_q(wt)} diff --numstat {_q(base_branch)}", timeout=60)).stdout
    files = []
    for line in numstat.splitlines():
        m = re.match(r"^(\d+|-)\t(\d+|-)\t(.+)$", line)
        if m:
            files.append({"path": m.group(3),
                          "additions": 0 if m.group(1) == "-" else int(m.group(1)),
                          "deletions": 0 if m.group(2) == "-" else int(m.group(2))})
    return patch, files


def split_patch(patch: str) -> list[dict]:
    """Split a unified diff into per-file chunks for the mobile viewer."""
    out, cur = [], None
    for line in patch.splitlines(keepends=True):
        if line.startswith("diff --git "):
            if cur:
                out.append(cur)
            m = re.search(r" b/(.+?)\s*$", line)
            cur = {"path": m.group(1) if m else "?", "patch": line}
        elif cur:
            cur["patch"] += line
    if cur:
        out.append(cur)
    return out


async def remove_worktree(ex: Executor, repo: str, path: str) -> None:
    await ex.run(f"git -C {_q(repo)} worktree remove --force {_q(path)}", timeout=60)


def _q(s: str) -> str:
    return "'" + s.replace("'", "'\\''") + "'"
