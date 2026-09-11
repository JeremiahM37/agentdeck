# Re-running the bounded evidence

Run from a clean checkout with a local fake executable and isolated temporary
HOME/XDG/tmux state. Do not provide real provider keys or invoke a hosted model.
The original harnesses used short-lived fixture roots under `/tmp`; those roots
are intentionally not part of the repository.

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

For the broader C1–C6 comparison, reproduce each product separately with the
rubric in `rubric.md`; record the exact revision, viewport, fixture root,
observable marker/sentinel, and strict status. If a harness fails to expose a
criterion, record `UNVERIFIED` and preserve the failed attempt. A single
authorized harness retry is allowed only when the harness itself errors.

The 2026-09-10 evidence JSON records the exact fake-agent values and request
boundaries from the completed runs. Temporary screenshots/logs mentioned in
older review notes are audit material, not required inputs to this rerun.
