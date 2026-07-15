from server.executor.mock import MOCK_DIFF
from server.worktree import (branch_name, default_workroot, split_patch,
                             worktree_path)


def test_naming():
    assert branch_name(7, 2) == "adk/task7-a2"
    assert worktree_path("/data/.wt/", 7, 2) == "/data/.wt/task7-a2"
    assert default_workroot("/opt/docker/librarr-go/") == "/opt/docker/.agentdeck-worktrees"
    assert default_workroot("/srv/app") == "/srv/.agentdeck-worktrees"


def test_split_patch_single_file():
    files = split_patch(MOCK_DIFF)
    assert len(files) == 1
    assert files[0]["path"] == "app.py"
    assert "+    print(\"hello, agentdeck\")" in files[0]["patch"]


def test_split_patch_multi_file():
    patch = (
        "diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n@@ -1 +1 @@\n-x\n+y\n"
        "diff --git a/dir/b.txt b/dir/b.txt\n--- a/dir/b.txt\n+++ b/dir/b.txt\n@@ -0,0 +1 @@\n+new\n")
    files = split_patch(patch)
    assert [f["path"] for f in files] == ["a.py", "dir/b.txt"]
    assert files[1]["patch"].endswith("+new\n")


def test_split_patch_empty():
    assert split_patch("") == []
