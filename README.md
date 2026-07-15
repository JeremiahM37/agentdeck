# agentdeck

> Self-hosted, mobile-first mission control for AI coding agents on **your own**
> infrastructure — Proxmox LXCs, VMs, or any box with SSH. Working name; see
> `DESIGN.md` for the full design document, feature catalog, and roadmap.

Describe a task from your phone. A Claude Code agent picks it up on a machine you
own, works in an isolated git worktree inside tmux, streams every step live to the
board, pings your phone when it needs an approval, and hands you a reviewable diff.

## Status — v0.8 (deployed)

Runs as a systemd service behind nginx+Authelia at `deck.homelab.internal`.
123 hermetic tests (unit + API + Playwright e2e), a live `.verify.yaml`, and a
nightly smoke timer that dispatches a real task and reports to Discord.

Working today:
- **Board**: kanban (mobile PWA + desktop), quick-dispatch bar, drag-to-dispatch,
  live SSE timeline, mobile diff review, board filter, desktop **Deck** multi-pane view.
- **Targets**: `local`, `ssh`, `pct` (Proxmox-native, no SSH), and **`sandbox`**
  (ephemeral LXC: clone template → run → capture → destroy). Deep creds probe.
- **Agents**: Claude Code (first-class), Codex/Gemini (experimental) via an adapter
  seam. **Any model, including fully local** — point a project's `env` at any
  Anthropic-compatible endpoint (Ollama ≥0.20, LiteLLM, vLLM/llama.cpp gateways)
  with `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN`.
- **Control loop**: hook-gated approvals with web-push + Discord/ntfy sinks
  (ntfy carries approve/deny buttons), always-allow policy engine, follow-ups,
  auto-verify, reviewer gates, A/B parallel attempts, agents that file their own
  task cards, shared project memory.
- **Ops**: worktree janitor, cost stats, task templates, one-click ttyd terminal
  attach, **MCP server** so any MCP client (Claude Code, the Discord bot) can file
  and steer tasks.

## Using local / alternative models

Set a project's `env` to route its agent at any Anthropic-compatible API:

```bash
curl -X POST .../api/projects -d '{
  "name":"myrepo","target_id":1,"repo_path":"/srv/myrepo",
  "env":{"ANTHROPIC_BASE_URL":"http://ollama-host:11434",
         "ANTHROPIC_AUTH_TOKEN":"ollama"}}'
# then dispatch with "model":"qwen3.6:35b-a3b" (or any served model)
```

Note: driving *agentic* coding (tool calls, edits) needs a capable model —
small local models often reply conversationally instead of acting. The transport
works with any model; the results depend on the model.

## Run

```bash
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
.venv/bin/python -m server            # http://<host>:9110
```

Demo mode with fake agents (no git/tmux/claude needed): `AGENTDECK_MOCK=1 .venv/bin/python -m server`

Targets need only: `ssh` reachability (key auth), `git`, `tmux`, `python3`, and the
`claude` CLI. Register a target + project in the Targets tab or via `POST /api/targets`
/ `POST /api/projects`, then create and dispatch tasks from the board.

## Tests

```bash
.venv/bin/pytest                      # unit + API + Playwright e2e (all hermetic, mock executor)
```

## Layout

```
server/          FastAPI control plane (SQLite, SSE, scheduler, executors)
server/executor/ local | ssh | mock target executors
hooks/hook.py    PreToolUse approval hook (stdlib-only, copied to worktrees)
web/             PWA frontend (vanilla ES modules, no build step)
tests/           unit / api / e2e (Playwright)
DESIGN.md        full design doc — architecture, feature catalog, roadmap
```

Config via env: `AGENTDECK_PORT` (9110), `AGENTDECK_DB`, `AGENTDECK_BASE_URL`
(URL targets use to reach this server for approval callbacks), `AGENTDECK_AUTH_TOKEN`
(optional bearer), `AGENTDECK_VAPID_PUBLIC`/`_PRIVATE` (web push), `AGENTDECK_MOCK`.
