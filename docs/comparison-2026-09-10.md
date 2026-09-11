# Comparative workflow evidence — 2026-09-10

This is a bounded evidence record for the common terminal workflow. It does not
close the broader parity goal or establish overall web superiority. The
comparison uses the official upstream projects linked in the [terminal
experience review](terminal-experience-review.md): [Agent Deck](https://github.com/asheshgoplani/agent-deck)
and [Agent of Empires](https://github.com/agent-of-empires/agent-of-empires).

## Browser workflow

The AgentDeck candidate at `bdc468587547f9a4276e266a1a3032ea0383fbd5` (reported
version 2.2.0) passed the bounded find → attach → type input → detach/reattach →
review tracked diff workflow at both 1440×900 and 390×900. The fixture used
isolated temporary homes, Git worktrees, tmux state and a harmless shell shim.
The relevant reproduction and screenshots are retained under
`/tmp/agentdeck-common-workflow-comparison/final/`, with the browser harness
and receipts in `/tmp/agentdeck-browser-comparison*.py` and
`/tmp/agentdeck-command-search-receipt.md`. The original pre-capture for this
comparison was lost; this statement relies on the surviving run artifacts and
the recorded operator receipt, not an attempt to reconstruct an absent capture.

The corrected AoE mobile attach probe also passed at 1440×900 and 390×900. It
ran the pinned local debug binary
`/mnt/bulk/agentdeck-comparison/aoe-target/debug/aoe`, with isolated
HOME/XDG/TMPDIR/tmux state and an absolute fake Codex shim. The exact harness is
`/tmp/agentdeck-aoe-attach-probe.py`; screenshots and text are in
`/tmp/agentdeck-aoe-attach-proof/attached-{1440,390}.{png,txt}`. The first
PATH-only attempt selected a real Codex and reached sign-in, so it is discarded;
the absolute-override rerun is the accepted evidence. No account, credential or
model call was made.

These runs establish the named cells above. They do not establish overall web
superiority. AoE's broader mobile review/diff workflow remains unverified, and
the broad native-fork, resize and scroll comparative cases remain unverified
unless their proof owner supplies run-specific artifacts. The bounded matrix's
reported full C3/C4 PASS is excluded: it contained partial workflow results
labelled as the full rubric, and its owner is correcting that record.

## TUI search correction

The cross-field TUI search case was reproduced against the AgentDeck TUI at
`622f` and initially failed all four cases (`4/4`). The correction was rerun at
`622f` and reduced the failure to `1/4`; the remaining case is retained as an
open limitation rather than being counted as a full pass. Relevant surviving
artifacts include `/tmp/agentdeck-native-search-tui-*.log`,
`/tmp/agentdeck-native-search-pages-tui-e2e.log`, and the proof files under
`/tmp/agentdeck-upstream-tui-proof/`. The exact commit and harness should be
recorded by the proof owner alongside the final corrected run before this cell
is promoted to a release claim.

## Scope and pending evidence

The browser evidence covers shell input and tracked diff visibility through the
two tested surfaces. It does not cover long-running full-screen agent rendering,
all mobile review paths, or every resize/scroll state. New generic-agent CLI
proof and the full release verification are separate pending gates; neither is
implied by this comparison record.
