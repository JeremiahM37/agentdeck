"""Context bundle + the settings/memory plumbing that gives a dispatched agent
the same knowledge and permissions a local session has."""
import shlex

import pytest

from server import context
from server.claude_runner import (
    DEFAULT_GATE_MATCHER,
    build_settings,
    memory_link_command,
    project_slug,
)


def test_collect_reads_files_and_globs(tmp_path):
    (tmp_path / "CLAUDE.md").write_text("house rules")
    (tmp_path / "docs").mkdir()
    (tmp_path / "docs" / "a.md").write_text("aaa")
    (tmp_path / "docs" / "b.md").write_text("bbb")

    files, notes = context.collect([str(tmp_path / "CLAUDE.md"),
                                    str(tmp_path / "docs" / "*.md")])
    assert [n for n, _s, _d in files] == ["CLAUDE.md", "a.md", "b.md"]
    assert dict((n, d) for n, _s, d in files)["CLAUDE.md"] == b"house rules"
    assert notes == []


def test_missing_paths_are_reported_not_swallowed(tmp_path):
    files, notes = context.collect([str(tmp_path / "nope.md")])
    assert files == []
    assert any("no such file" in n for n in notes)
    # and the agent is told, rather than quietly running with less
    assert "nope.md" in context.prompt_prefix(files, notes)


def test_duplicate_basenames_do_not_collide(tmp_path):
    for d in ("one", "two"):
        (tmp_path / d).mkdir()
        (tmp_path / d / "CLAUDE.md").write_text(d)
    files, _ = context.collect([str(tmp_path / "one" / "CLAUDE.md"),
                                str(tmp_path / "two" / "CLAUDE.md")])
    names = [n for n, _s, _d in files]
    assert len(set(names)) == 2
    assert names[0] == "CLAUDE.md" and names[1] == "two_CLAUDE.md"


def test_same_path_twice_is_staged_once(tmp_path):
    p = tmp_path / "CLAUDE.md"
    p.write_text("x")
    files, _ = context.collect([str(p), str(p)])
    assert len(files) == 1


def test_oversized_file_truncates_loudly(tmp_path, monkeypatch):
    monkeypatch.setattr(context, "MAX_FILE_BYTES", 100)
    (tmp_path / "big.md").write_bytes(b"x" * 500)
    files, notes = context.collect([str(tmp_path / "big.md")])
    assert b"truncated by agentdeck" in files[0][2]
    assert any("truncated" in n for n in notes)


def test_total_cap_stops_the_bundle_with_a_note(tmp_path, monkeypatch):
    monkeypatch.setattr(context, "MAX_TOTAL_BYTES", 10)
    (tmp_path / "a.md").write_bytes(b"12345")
    (tmp_path / "b.md").write_bytes(b"67890123")
    files, notes = context.collect([str(tmp_path / "a.md"), str(tmp_path / "b.md")])
    assert [n for n, _s, _d in files] == ["a.md"]
    assert any("cap" in n for n in notes)


def test_prompt_prefix_empty_when_nothing_configured():
    assert context.prompt_prefix([], []) == ""


def test_index_lists_source_paths(tmp_path):
    (tmp_path / "CLAUDE.md").write_text("hi")
    files, notes = context.collect([str(tmp_path / "CLAUDE.md")])
    idx = context.index_markdown(files, notes)
    assert str(tmp_path / "CLAUDE.md") in idx


# ---- settings / permissions --------------------------------------------------

def test_settings_carry_permissions_without_hooks():
    s = build_settings(permissions={"allow": ["Bash(pytest*)"]})
    assert s == {"permissions": {"allow": ["Bash(pytest*)"]}}
    assert "hooks" not in s        # rules apply to ungated runs too


def test_settings_merge_permissions_and_gate():
    s = build_settings("http://cp:9110", "tok", gated=True,
                       permissions={"deny": ["Bash(rm *)"]})
    assert s["permissions"] == {"deny": ["Bash(rm *)"]}
    assert s["hooks"]["PreToolUse"][0]["matcher"] == DEFAULT_GATE_MATCHER


def test_settings_reject_typo_permission_keys():
    with pytest.raises(ValueError):
        build_settings(permissions={"allowed": ["Bash"]})


def test_empty_settings_is_empty():
    assert build_settings() == {}


# ---- memory linking ----------------------------------------------------------

def test_project_slug_matches_claude_layout():
    # verified against the running CLI: every non-alphanumeric collapses to '-'
    assert project_slug("/srv/repos/x/.wt/task1-a1") == "-srv-repos-x--wt-task1-a1"
    assert project_slug("/tmp/slug_test.d/a_b") == "-tmp-slug-test-d-a-b"


def test_memory_link_command_keys_on_the_git_main_worktree():
    """Memory follows the main worktree, not cwd — the cwd slug is never linked.

    Verified against the CLI: a session started inside a linked worktree reports
    the PARENT repo's slug as its memory dir, so linking the worktree slug (what
    this used to do) produced a symlink nothing ever opened.
    """
    cmd = memory_link_command("/wt/task1-a1", "/store/memory")
    assert "--git-common-dir" in cmd, "main worktree must be resolved, not guessed"
    assert "-wt-task1-a1/memory" not in cmd, "cwd slug is the wrong key"
    assert "ln -sfn /store/memory" in cmd
    assert "$HOME/.claude/projects/$slug" in cmd


def test_memory_link_command_never_destroys_real_memories():
    """The link path can be a repo the operator also uses interactively."""
    cmd = memory_link_command("/wt/task1-a1", "/store/memory")
    assert "refusing to replace non-empty memory dir" in cmd
    # deletion is reachable only through the emptiness check
    assert cmd.count("rm -rf") == 1
    assert 'ls -A "$link"' in cmd


def test_memory_link_command_quotes_hostile_paths():
    evil = "/store/'; rm -rf /; '"
    cmd = memory_link_command("/wt/x", evil)
    # the payload survives as ONE literal argument — never as shell syntax
    tokens = shlex.split(cmd)
    assert evil in tokens
    assert tokens.count("rm") == 1        # only the guarded rm -rf we wrote
