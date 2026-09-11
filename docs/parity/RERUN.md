# Re-running the bounded evidence

Run from a clean checkout with a local fake executable and isolated temporary
HOME/XDG/tmux state. Do not provide real provider keys or invoke a hosted model.
The original harnesses used short-lived fixture roots under `/tmp`; those roots
are intentionally not part of the repository.

## Minimal isolated fixture

The following creates the same kind of local repository and fake runner used by
the comparison. Each product must be run in its own `RUN` directory:

```bash
RUN=$(mktemp -d /tmp/agentdeck-parity-XXXXXX)
export HOME="$RUN/home" XDG_CONFIG_HOME="$RUN/config" XDG_DATA_HOME="$RUN/data"
export XDG_STATE_HOME="$RUN/state" TMUX_TMPDIR="$RUN/tmux"
mkdir -p "$HOME" "$XDG_CONFIG_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME" "$TMUX_TMPDIR"
REPO="$RUN/repo"; mkdir "$REPO"; cd "$REPO"
git init -q; git config user.email fixture@example.invalid; git config user.name Fixture
printf 'before\n' > tracked.txt; git add tracked.txt; git commit -qm baseline
cat > "$RUN/fake-agent" <<'SH'
#!/bin/sh
printf 'FIXTURE_AGENT_MARKER\n'
printf 'C3_PID:%s\n' "$$"
exec "${SHELL:-/bin/bash}"
SH
chmod 755 "$RUN/fake-agent"
```

Pin and record the binary/source revision before launching. The successful C3
launch forms used in this comparison were:

```bash
# upstream Agent Deck 61cc4d6
agent-deck launch "$REPO" -c "$RUN/fake-agent" -t "Alpha synthetic" \
  -g Work/Backend --no-wait --quiet

# Agent of Empires 5687bbd (repeat with distinct titles for four sessions)
aoe add "$REPO" --title "Alpha synthetic" --tool codex \
  --cmd-override "$RUN/fake-agent" --trust-hooks --launch --yolo
```

For AgentDeck, create the fixture project/target through its Settings wizard,
choose the fake command, and create four sessions through the New session form;
the checked-in mobile test is the executable local example. When exposing a
browser terminal through ttyd, the worker used:

```bash
ttyd --writable --port "$PORT" "$RUN/fake-agent"
```

Attach the selected row and capture the live terminal. Send a marker plus 70
known lines, then issue PageUp and record a visible `C3_KNOWN_*` line. Resize
to 110 columns × 30 rows and expect `stty size` to report `30:110`. Detach
with tmux `Ctrl-b d` (AoE and upstream) or the product’s detach action; after
reattaching, expect the same pane/session identity and a fresh
`C3_REATTACHED_UNIQUE_<token>` from the same shell PID. Upstream’s copy-mode
wheel path enters tmux copy mode; press Escape before sending shell input and
record `pane_in_mode=0`. Any missing observation is `UNVERIFIED`.

For the C4 child-isolation check, the pinned upstream CLI workflow was:

```bash
UPSTREAM=/path/to/agent-deck-61cc4d6
"$UPSTREAM" launch "$REPO" -c "$RUN/fake-agent" -t 'C4 upstream parent' --no-wait --quiet
"$UPSTREAM" launch "$REPO" -c "$RUN/fake-agent" -t 'C4 upstream child' \
  -w c4-upstream-child -b --no-wait --quiet
CHILD="$REPO/.worktrees/feature/c4-upstream-child"
printf 'after\n' > "$CHILD/tracked.txt"
git -C "$CHILD" diff --no-color main
"$UPSTREAM" session send 'C4 upstream child' 'git diff --no-color main'
```

The equivalent AgentDeck/AoE web path creates the child from the product’s
workspace/fork control, edits `tracked.txt` in the displayed child directory,
and captures the rendered Review/Diff surface. Expected observations are a
distinct child identity, unchanged parent `before\n`, and the exact two-line
patch `-before` / `+after`. Do not count a filesystem diff without the
user-facing product surface as full C4 proof.

For the checked-in AgentDeck mobile behavior, run the real PTY/browser tests:

```bash
python -m pytest -q \
  e2e/test_terminal_swipe_tabs.py \
  e2e/test_mobile_terminal_focus.py
```

The swipe test creates two local tmux sessions, uses CDP touch events at
390×900, sends shell markers, checks xterm selection and mouse-reporting mode,
and cleans its fixture sessions. The focus test checks the compact Tools →
Files path, keybar behavior, rotation/resizing, offline recovery, and that
Tools/status do not overlap the xterm viewport.

For the broader C1–C6 comparison, use the fixture above separately per product
and the rubric in `rubric.md`; record the exact revision, viewport, fixture
root, observable marker/sentinel, and strict status. If a harness fails to
expose a criterion, record `UNVERIFIED` and preserve the failed attempt. A
single authorized retry is allowed only when the harness itself errors. The
2026-09-10 historical harness exceeded that retry budget while recovering
selector, xterm-focus, socket-path, and clipboard issues; those attempts are
disclosed in the comparison ledger and were not counted as product passes.

The 2026-09-10 evidence JSON records the exact fake-agent values and request
boundaries from the completed runs. Temporary screenshots/logs mentioned in
older review notes are audit material, not required inputs to this rerun.
