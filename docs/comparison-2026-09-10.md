# Comparative workflow evidence — 2026-09-10

This is a bounded evidence record for the browser and TUI workflows. It does
not establish an overall web superiority claim.

## Browser workflow

Against the AgentDeck baseline `bdc4685`, web find, attach, input, reattach,
and tracked-diff behavior was observed at both 1440x900 and 390x900. The
evidence is under `/tmp/agentdeck-parity-proof-v2/current-web/`.

The corresponding AoE find, attach, input, and reattach behavior was observed
at both viewports under `/tmp/agentdeck-parity-proof-v2/aoe-web-rerun/`. The
comparison references upstream AgentDeck `61cc4d6` and AoE `5687bbd`.

The full rubric remains unverified. C3 requires resize and scroll coverage,
which was not tested; C4 requires child-isolation coverage, which was not
tested. C1 API fixtures are not UI creation. The exact rubric and bounded
matrix are `/tmp/agentdeck-parity-proof-v2/rubric.md` and
`/tmp/agentdeck-parity-proof-v2/bounded-matrix.md`. Broader comparison claims
remain unverified.

## TUI Alpha search

Before the baseline fix, the TUI Alpha search showed all four unrelated rows.
The fixed `622f` build showed only Alpha, exactly 1 of 4 matches. These are
match counts, not failure counts.

The original raw before-fix output was overwritten. The recorded observation
and this limitation are documented at
`/tmp/agentdeck-parity-proof-v2/tui-pyte/prefix-observation.md`. Post-fix
evidence is at `/tmp/agentdeck-parity-proof-v2/postfix-evidence.json` and
`/tmp/agentdeck-parity-proof-v2/postfix-search-screen.txt`; the focused suite
log is `/tmp/agentdeck-parity-proof-v2/focused-tests.log`.

Generic-agent CLI proof and full release verification are separate pending
work and are not represented as completed by this ledger.
