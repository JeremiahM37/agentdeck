"""Context bundle — what a dispatched agent should read before it starts.

A dispatched agent opens a FRESH session: no conversation history, and on an
ssh/pct/sandbox target none of the control plane user's Claude config either.
The same task therefore runs with far less knowledge remotely than it does
locally, and nothing in the timeline says so — the output is just quietly worse.

`context_paths` closes that gap. The control plane reads the listed files off
its OWN filesystem (one place to curate) and stages them into the worktree at
.agentdeck/context/ through the Executor, so every target kind — local, ssh,
pct, sandbox — gets byte-identical context.

Caps are deliberate and LOUD: an oversized or missing file is reported in the
staged index and in the prompt, never dropped silently.
"""
import glob
from pathlib import Path

SUBDIR = "context"
INDEX_NAME = "INDEX.md"
MAX_FILE_BYTES = 256 * 1024
MAX_TOTAL_BYTES = 1024 * 1024


def resolve(patterns) -> tuple[list[Path], list[str]]:
    """Expand globs to existing files. Returns (files, notes-about-what-missed)."""
    files: list[Path] = []
    notes: list[str] = []
    seen: set[str] = set()
    for pattern in patterns or []:
        pattern = str(pattern).strip()
        if not pattern:
            continue
        matches = sorted(glob.glob(pattern, recursive=True))
        hits = [Path(m) for m in matches if Path(m).is_file()]
        if not hits:
            notes.append(f"{pattern} — no such file (skipped)")
            continue
        for p in hits:
            key = str(p.resolve())
            if key not in seen:
                seen.add(key)
                files.append(p)
    return files, notes


def _stage_names(paths: list[Path]) -> list[str]:
    """Flat, collision-free filenames for the staging dir."""
    names: list[str] = []
    used: set[str] = set()
    for p in paths:
        name = p.name
        parts = list(p.parts[:-1])
        while name in used and parts:
            name = f"{parts.pop()}_{name}"
        n, base = 2, name
        while name in used:
            name, n = f"{base}.{n}", n + 1
        used.add(name)
        names.append(name)
    return names


def collect(patterns) -> tuple[list[tuple[str, str, bytes]], list[str]]:
    """Read the configured paths into (staged_name, source_path, bytes) + notes.

    Files are truncated at MAX_FILE_BYTES and the bundle stops at
    MAX_TOTAL_BYTES; both cases append a note rather than dropping quietly.
    """
    paths, notes = resolve(patterns)
    names = _stage_names(paths)
    out: list[tuple[str, str, bytes]] = []
    total = 0
    for path, name in zip(paths, names):
        try:
            data = path.read_bytes()
        except OSError as e:
            notes.append(f"{path} — unreadable ({e.strerror or e}) (skipped)")
            continue
        if len(data) > MAX_FILE_BYTES:
            data = (data[:MAX_FILE_BYTES]
                    + f"\n\n[truncated by agentdeck at {MAX_FILE_BYTES} bytes]\n".encode())
            notes.append(f"{path} — truncated to {MAX_FILE_BYTES} bytes")
        if total + len(data) > MAX_TOTAL_BYTES:
            notes.append(f"{path} — bundle hit the {MAX_TOTAL_BYTES}-byte cap (skipped)")
            continue
        total += len(data)
        out.append((name, str(path), data))
    return out, notes


def index_markdown(files: list[tuple[str, str, bytes]], notes: list[str]) -> str:
    lines = ["# Staged context",
             "",
             "Files copied here by agentdeck from the control plane at dispatch.",
             ""]
    for name, src, data in files:
        lines.append(f"- `{name}` — from `{src}` ({len(data)} bytes)")
    if notes:
        lines += ["", "## Not staged", ""] + [f"- {n}" for n in notes]
    return "\n".join(lines) + "\n"


def prompt_prefix(files: list[tuple[str, str, bytes]], notes: list[str]) -> str:
    """Header prepended to the task prompt so the agent cannot miss the bundle."""
    if not files and not notes:
        return ""
    lines = ["## Context",
             "",
             "Read these staged files before doing anything else — they carry "
             "conventions and constraints this task depends on:"]
    lines += [f"- .agentdeck/{SUBDIR}/{name}" for name, _src, _data in files]
    if notes:
        lines.append("")
        lines.append("Context that could NOT be staged (work without it, and say so "
                     "if it blocks you):")
        lines += [f"- {n}" for n in notes]
    return "\n".join(lines) + "\n\n---\n\n"
