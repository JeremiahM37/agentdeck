# Contributing to agentdeck

## Dev setup

```bash
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
.venv/bin/python -m playwright install chromium    # for e2e tests
```

## Running

```bash
AGENTDECK_MOCK=1 .venv/bin/python -m server   # fake agents, no infra needed
.venv/bin/python -m server                    # real: needs ssh/git/tmux/claude on targets
```

## Tests

Everything is hermetic (mock executor — no network, no real agent):

```bash
.venv/bin/pytest              # unit + API + Playwright e2e
.venv/bin/pytest tests/unit   # fast, no browser
```

- `tests/unit/` — parser, state machine, policy, agent adapters, worktree naming
- `tests/api/` — full lifecycle against the `MockExecutor`
- `tests/integration/` — real `git worktree` via `LocalExecutor`
- `tests/e2e/` — Playwright driving the real UI against a mock-mode server

Add a test with any behavior change. New execution paths should also be exercised
against a real target at least once — the mock suite has (twice) masked bugs that
only surfaced on real infrastructure (see `DESIGN.md` §10).

## Architecture

`DESIGN.md` is the source of truth — read §4 (architecture) and §3 (vocabulary)
first. Key seams to respect:

- **`server/executor/`** — everything above it is target-kind agnostic. New target
  types (docker, k8s, …) implement the `Executor` protocol and nothing else changes.
- **`server/agents.py`** — everything CLI-specific per coding agent. New agents add
  a launch-command builder and a stream parser here.
- **`server/claude_runner.py`** — the Claude Code integration seam. CLI drift lands
  here and nowhere else.

## Conventions

- Plain SQL, no ORM. Explicit `.verify.yaml` + hermetic tests over mocking internals.
- Secrets never in the repo — config via environment (see `server/config.py`).
- Vanilla ES modules in `web/`, no build step.
