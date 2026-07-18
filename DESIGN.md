# agentdeck — Design Document

> **Working name.** `agentdeck` is a placeholder; final name decided before publishing.
> **One-liner:** Self-hosted, mobile-first mission control for AI coding agents running on *your own* infrastructure — Proxmox LXCs, VMs, Raspberry Pis, any box with SSH.

Status: v0.1 in development · Started 2026-07-14 · Lives at `/home/admin/projects/agentdeck/`

---

## 1. Vision

Describe a task from your phone on the couch. An AI coding agent picks it up **on a real machine you own**, works in an isolated git worktree, and your phone buzzes when it needs a decision or has a diff ready to review. From a desk, the same system is a multi-pane cockpit running a dozen agents in parallel across your cluster.

Commercial products (BridgeMind, Omnara, Conductor) do slices of this in the cloud, metered by credits. Open-source options (vibe-kanban, Happy) each cover one axis. **Nothing covers both axes we care about: mobile-first UX and real-infrastructure execution.** That's the product.

### Design principles

1. **Mobile-first, desktop-great.** Every flow must be completable on a phone. Desktop gets density (multi-pane grid), not exclusive features.
2. **Agentless targets.** An execution target needs only `ssh + git + tmux + <agent CLI>`. No daemon to install, no runtime to babysit. Works with Proxmox LXCs, VMs, bare metal, cloud VPS, WSL — anything SSH-able.
3. **Structured events, never terminal scraping.** Claude Code's `stream-json` output and hooks are the integration surface. The terminal is an *escape hatch* (attach via ttyd/mttyd), not the data source.
4. **The approval loop is the product.** Push notification → read proposed action → approve/deny from the lock screen. This single flow is what makes remote agents trustworthy.
5. **Boring, durable state.** One SQLite file. Server restart never loses a running task (tmux sessions survive; the server re-attaches to log tails).
6. **Self-contained OSP.** No SaaS dependencies. Runs from one `docker compose up` or a systemd unit. Auth is pluggable (none / reverse-proxy header / token).
7. **Claude Code first, adapters later.** Depth on one agent beats shallow support for ten.

---

## 2. Competitive analysis

| Capability | BridgeMind (closed, $16–100/mo) | vibe-kanban (OSS, sunsetting) | Happy (OSS) | Omnara ($9/mo) | **agentdeck** |
|---|---|---|---|---|---|
| Kanban dispatch board | ✅ BridgeSpace | ✅ | ❌ | ❌ | ✅ |
| Parallel agents | ✅ up to 16 panes | ✅ worktrees | ❌ 1 session/view | ❌ | ✅ worktrees × targets |
| Execution location | ☁️ ephemeral cloud sandbox | 🖥️ install machine only | 🖥️ your machine | 🖥️ your machine | 🏠 **any SSH target / LXC / VM** |
| Mobile-first UI | ⚠️ responsive web | ❌ none | ✅ native apps | ✅ native apps | ✅ PWA, designed phone-first |
| Push approvals | ⚠️ in-app | ❌ | ✅ | ✅ | ✅ web push + Discord/ntfy |
| Diff review on phone | ⚠️ | ❌ | ⚠️ chat view | ⚠️ | ✅ first-class |
| Terminal attach | ✅ | ⚠️ local | ❌ (chat UI) | ❌ | ✅ tmux + ttyd/mttyd |
| Voice dispatch | ✅ BridgeVoice | ❌ | ✅ | ❌ | 🗺️ roadmap (Web Speech + local whisper) |
| MCP connectivity | ✅ BridgeMCP | ⚠️ | ⚠️ | ❌ | ✅ inherits Claude Code MCP config |
| Memory | ✅ BridgeMemory | ❌ | ❌ | ❌ | ✅ CLAUDE.md mgmt + optional RAG hook |
| Cost | credits, runs out | free | free | $9/mo | free, your API plan |
| Maintained | ✅ | ❌ sunsetting, 382 open issues | ✅ | ✅ | us |

**What we deliberately don't chase (non-goals):** hosted multi-tenant SaaS, browser IDE (use real editors / terminal attach), supporting 10+ agent CLIs at launch, Windows *server* support (targets can be anything; control plane is Linux).

---

## 3. Core concepts & vocabulary

- **Target** — a machine that runs agents. Kinds: `local` (the control-plane host), `ssh` (anything reachable via SSH), later `pct` (Proxmox LXC via `pct exec`, no SSH needed) and `docker`. A target advertises capabilities discovered at registration: git/tmux/claude versions, disk free.
- **Project** — a git repo path *on a target* (e.g. `/opt/docker/librarr-go` on LXC 200, `~/projects/pocketlab` on AIServer). One repo may exist on several targets; a project binds repo↔target.
- **Task** — a card. Title + prompt (description) + config (agent, model, permission mode, base branch). Moves through the board.
- **Attempt** — one execution of a task (task : attempts is 1:N — retries and parallel A/B attempts create new attempts). An attempt owns a worktree, a branch, a tmux session, an event log, and a result (diff, summary, cost).
- **Event** — one structured record from the agent stream (assistant text, tool_use, tool_result, status change, error, cost).
- **Approval** — a blocked tool call waiting for a human decision, created by a PreToolUse hook.

### Task lifecycle (state machine)

```
backlog → queued → running → review → done
                     ↓   ↘ failed (→ queued on retry)
                  cancelled
```

- `backlog`: card exists, not dispatched. `queued`: dispatch requested, waiting for target slot.
- `running`: attempt live on target. `review`: agent finished, diff captured, human decision pending.
- `review → done`: user merges/commits/accepts. `review → queued`: user requests changes (spawns follow-up attempt with feedback prompt).
- Guardrail: `max_concurrent` per target (default 4) gates queued→running.

---

## 4. Architecture

```
┌─ phone / desktop PWA ────────────────────────────────┐
│  board · task detail · diff review · approvals · att │
└──────────────┬───────────────────────────────────────┘
        HTTPS / SSE / Web Push
┌──────────────┴───────────────────────────────────────┐
│  control plane (FastAPI, AIServer)                    │
│  REST API · SSE bus · scheduler · approval broker     │
│  SQLite (tasks/attempts/events/approvals/targets)     │
│  executor layer: local | ssh (asyncssh) | mock        │
└───────┬───────────────┬───────────────┬──────────────┘
     ssh/exec        ssh/exec        subprocess
┌───────┴──────┐ ┌──────┴───────┐ ┌──────┴───────┐
│ LXC 101      │ │ LXC 104      │ │ AIServer      │
│ git worktree │ │ git worktree │ │ git worktree  │
│ tmux: claude │ │ tmux: claude │ │ tmux: claude  │
│ stream-json→ │ │ stream-json→ │ │ stream-json→  │
│ task log     │ │ task log     │ │ task log      │
└──────────────┘ └──────────────┘ └───────────────┘
```

### 4.1 Control plane

- **FastAPI + uvicorn**, single process, asyncio. SQLite (WAL) via `aiosqlite`-style thin wrapper. No ORM — explicit SQL, few tables.
- **SSE event bus**: in-process pub/sub; clients subscribe to `/api/stream` (global board updates) and `/api/tasks/{id}/stream` (task timeline). SSE over WebSockets because it survives proxies/PWA background better. Events emitted while a client is disconnected are **not** replayed (no server-side buffer); instead the client resyncs on every (re)connect — the board refetches tasks+approvals in `onopen`, the task sheet refetches its event list. A 30s poll is the backstop. (A `Last-Event-ID` replay buffer is a possible future optimization; today resync-on-reconnect is the contract.)
- **Scheduler loop** (asyncio task, 2s tick): promotes `queued` attempts when their target has a free slot; drives log tailing for running attempts; detects finished/dead sessions; captures result diff; transitions state; emits push notifications.
- **Server restarts are safe**: on boot, scan `running` attempts, re-attach tailers at the stored byte offset, reconcile tmux session existence (session gone + result file present → finalize; gone + no result → failed).

### 4.2 Executor layer

```python
class Executor(Protocol):
    async def run(cmd: list[str] | str, cwd, timeout) -> ExecResult   # rc, stdout, stderr
    async def read_file(path, offset) -> bytes                        # log tailing
    async def write_file(path, data)                                  # hook settings, prompt files
    async def check() -> TargetInfo                                   # git/tmux/agent versions, disk
```

- `LocalExecutor`: `asyncio.create_subprocess_exec`.
- `SSHExecutor`: asyncssh with connection reuse + keepalive; key auth only (no passwords in DB — key path or agent).
- `MockExecutor`: scripted responses for the whole test suite; also powers `AGENTDECK_MOCK=1` demo mode so the UI is testable with zero infra.
- Later: `PctExecutor` (`pct exec <vmid> --`) for LXCs on the same Proxmox node without per-container SSH; `DockerExecutor` (`docker exec`).
- All higher layers (worktree, runner, diff) speak only to this interface — one code path for every target kind.

### 4.3 Run protocol (dispatch → events → result)

1. **Worktree**: `git -C <repo> worktree add -b agentdeck/task-{id}-attempt-{n} <workroot>/task-{id}-{n} <base_branch>`. Workroot default: `<repo>/../.agentdeck-worktrees/` (configurable per project).
2. **Runtime dir**: `<worktree>/.agentdeck/` (git-ignored via `info/exclude`): `prompt.md`, `settings.json` (generated hooks), `events.jsonl`, `result.json`, `exit_code`.
3. **Launch** inside tmux so it survives the control plane and stays attachable:
   ```
   tmux new-session -d -s adk-{attempt} 'cd <worktree> && claude -p "$(cat .agentdeck/prompt.md)" \
     --output-format stream-json --verbose --permission-mode <mode> \
     --settings .agentdeck/settings.json > .agentdeck/events.jsonl 2> .agentdeck/stderr.log; \
     echo $? > .agentdeck/exit_code'
   ```
4. **Tail**: scheduler polls `read_file(events.jsonl, offset)` (2s), parses stream-json lines → normalized events → SQLite + SSE fan-out. Parser is tolerant: unknown event types stored raw, never crash on a partial line (buffer to newline).
5. **Finish**: `exit_code` file appears → capture `git add -A -N && git diff <base>` + `git status --porcelain` + final `result` event (cost, duration, turns) → state `review`.
6. **Interactive escape hatch**: `tmux attach -t adk-{attempt}` via ttyd/mttyd link from the task page. Print-mode runs show the log; a task can also be dispatched in `interactive` mode (full Claude TUI in tmux, no stream-json — events limited, for pairing sessions).
7. **Follow-up / steering**: "request changes" creates attempt N+1 in the *same worktree* with `claude -p --resume <session_id>` (session id harvested from stream-json `init` event) so context carries.

### 4.4 Approval protocol (the killer feature)

Generated `settings.json` in each worktree registers a PreToolUse hook (matcher: configurable, default `Bash|Write|Edit` when permission mode is `default`):

```
hook → POST {server}/api/hook/approval {attempt_token, tool_name, tool_input}
     ← blocks, long-polls GET /api/hook/approval/{id}/decision (timeout 15m → deny)
server → creates approval row, SSE to UI, web-push "Claude wants to run: rm -rf …" 
user   → taps Approve / Deny (+ optional "always allow this pattern for this project")
hook   ← exits 0 (allow) or 2 with stderr reason (deny, fed back to Claude)
```

- `attempt_token`: per-attempt bearer secret injected into hook env — hook calls can't cross attempts.
- Decisions can carry a note ("yes but use --dry-run first") → returned as hook stderr on deny / appended context on allow.
- **Policy engine (later)**: per-project allow/deny globs evaluated server-side before bothering the human; "always allow" answers append to it.
- Permission modes surfaced per dispatch: `default` (hook-gated), `acceptEdits`, `plan`, `bypassPermissions` (only on `sandbox=true` targets).

### 4.5 Frontend

- **PWA, no build step**: vanilla ES modules + `lit-html`-style tagged templates or plain DOM — same conventions as pocketlab/mttyd. Installable (manifest + SW), offline shell, icon.
- **Phone layout**: bottom tab bar — *Board · Activity · Approvals (badge) · Targets*. Board columns horizontally swipable; card tap → task sheet (timeline, live). Diff viewer: per-file accordion, unified diff, word-wrap, font-size control, swipe between files. Approve/deny as full-width thumb buttons.
- **Desktop layout** (≥1024px): board as columns side-by-side; task detail as right drawer; **Grid view**: N live task panes (timeline or embedded terminal iframe via ttyd) — the "16 terminals" BridgeSpace view, but each pane is a *different machine* if you want.
- **Live everywhere**: one SSE connection drives board moves, timeline appends, approval badges. EventSource auto-reconnects; on each reconnect the client resyncs (refetch) rather than relying on replay.
- Voice dispatch (roadmap): mic button on New Task → Web Speech API (or POST audio to local whisper) → prompt field.

### 4.6 Notifications

- **Web Push (VAPID)** — reuse the proven pipeline from homelab-api `routers/push.py`. Events: approval needed, task → review, task failed, target offline.
- **Sinks abstraction**: web-push, Discord webhook, ntfy, generic webhook. Per-event-type toggles.

### 4.7 Security

- Control plane binds LAN/Tailscale; intended behind existing reverse proxy (Tier 2 Authelia gate on the homelab).
- Auth modes: `none` (trusted LAN) and `token` (single bearer, `AGENTDECK_AUTH_TOKEN`; the PWA stores it in localStorage and threads it through both fetch and the SSE query param since EventSource can't set headers). Header-trust auth (`Remote-User` from Authelia) is handled *at the reverse proxy* — the deployed setup gates at nginx+Authelia and runs agentdeck in `none` mode behind it, rather than a distinct in-app mode. Hook endpoints always use per-attempt tokens regardless.
- No passwords stored; SSH by key only. Secrets never rendered into events (hook input is shown verbatim — documented caveat).
- `sandbox` flag per target: only sandboxed targets allow `bypassPermissions`; recommend disposable LXC/VM targets for it. Roadmap: one-tap **ephemeral LXC target** via Proxmox API (clone template → run task → destroy).
- **Approval-gate integrity vs. the agent itself (important):** a running agent knows the control-plane base URL (it's in `.agentdeck/env` for the task-filing kit) and is on the network. The approval gate's guarantee therefore depends on the auth mode:
  - **`token` mode** — agents hold only a per-attempt *hook* token, which works on `/api/hook/*` but not the human-facing `/api/approvals/*/decision` (that requires the `AGENTDECK_AUTH_TOKEN` bearer, which agents never receive). An adversarial agent **cannot** self-approve. Verified by `test_token_mode_blocks_agent_self_approval`.
  - **`none` mode** — the LAN is fully trusted, *including the agent*. A misbehaving agent can enumerate its own pending approval and POST a decision to self-approve, defeating the gate. In `none` mode the gate protects against *accidental* dangerous tool calls, not an *adversarial* agent. **Run `token` mode (and/or network-isolate agents to the hook path) whenever the agent is not fully trusted.** This is called out because it's a non-obvious consequence of "none = trust the network."

---

## 5. Data model (SQLite)

```sql
targets(id, name, kind, host, port, user, key_path, workroot, max_concurrent,
        sandbox, status, info_json, created_at)
projects(id, name, target_id→targets, repo_path, default_base_branch,
         workroot_override, policy_json, created_at)
tasks(id, project_id→projects, title, prompt, status, priority, labels_json,
      agent, model, permission_mode, base_branch, created_at, updated_at)
attempts(id, task_id→tasks, n, status, worktree_path, branch, tmux_session,
         session_id, log_offset, started_at, finished_at, exit_code,
         result_json /*cost, turns, duration, summary*/, diff_stat_json)
events(id, attempt_id→attempts, seq, ts, type, payload_json)
approvals(id, attempt_id→attempts, tool_name, input_json, status
          /*pending|approved|denied|expired*/, decided_by, note, created_at, decided_at)
push_subscriptions(id, endpoint, keys_json, created_at)
settings(key, value)
```

Indexes on `events(attempt_id, seq)`, `tasks(status)`, `approvals(status)`. Diffs stored on disk in the runtime dir, served on demand (not in DB).

## 6. API surface (v0.1)

```
GET  /api/health
CRUD /api/targets            POST /api/targets/{id}/check
CRUD /api/projects
CRUD /api/tasks              # PATCH moves cards (status, priority)
POST /api/tasks/{id}/dispatch    {target_id?, permission_mode?, model?}
POST /api/tasks/{id}/cancel      # tmux kill-session + state
POST /api/tasks/{id}/followup    {feedback}          # review → new attempt, resume
GET  /api/tasks/{id}/events?after_seq=
GET  /api/tasks/{id}/diff        # {files:[{path, patch, additions, deletions}]}
POST /api/tasks/{id}/complete    # review → done (optional: commit/push/PR later)
GET  /api/approvals?status=pending
POST /api/approvals/{id}/decision {decision, note?, always_allow?}
POST /api/hook/approval          # called by hook (attempt token auth)
GET  /api/hook/approval/{id}/decision   # long-poll
GET  /api/stream                 # SSE: board + approvals
GET  /api/tasks/{id}/stream      # SSE: timeline
POST /api/push/subscribe
```

## 7. Full feature catalog (the everything list)

Legend: ✅ v0.1 (this build) · 🔜 v0.2–0.3 · 🗺️ v1.0+

**Board & tasks**: kanban columns ✅ · drag-to-dispatch / drag-to-done (desktop DnD) ✅ · **quick-dispatch bar** (type ⏎ → task created + dispatched; kills the "card overhead" friction users report in vibe-kanban) ✅ · priorities & labels ✅(data)/🔜(UI filters) · project swimlanes 🔜 · WIP limits per target ✅ (`max_concurrent`) · task templates 🔜 · bulk actions 🔜 · full-text task search 🔜 · recurring/scheduled tasks 🗺️ · dependencies between tasks 🗺️
**Dispatch**: target picker ✅ · model picker ✅ · permission mode ✅ · base branch ✅ · voice dispatch (Web Speech mic on quick bar + new-task sheet) ✅ · A/B parallel attempts (same task, 2 targets/models, pick winner) 🔜 · prompt templates w/ variables 🔜 · attach files/URLs as context 🔜 · RAG context hook (query ecosystem-RAG, inject) 🗺️ · local-whisper voice fallback 🗺️
**Execution**: worktree isolation ✅ · tmux survivability ✅ · resume/follow-up with session context ✅ · retry ✅ · cancel ✅ · timeouts ✅ · token/cost per attempt ✅ · target capability discovery ✅ · **auto-verify** (project `verify_cmd` runs in the worktree after the agent finishes; ✓/✗ badge on card + output in timeline — no competitor has this) ✅ · ghost-run reconciliation (running attempt whose tmux/worktree vanished → failed, mirrors vibe-kanban #1571) ✅ · verify artifacts excluded from diffs/commits (`__pycache__`, `*.pyc`) ✅ · pct/docker executors 🗺️ · ephemeral-LXC sandbox targets 🗺️ · non-Claude agents (codex/gemini/opencode adapters) 🗺️
**Monitoring**: live timeline (tool calls, text, results, verify) ✅ · board SSE ✅ · terminal attach command (copy `ssh … tmux attach`) ✅ · ttyd one-click attach 🔜 · desktop multi-pane grid 🔜 · session replay (step through events) 🔜 · cluster activity feed 🔜 · cost dashboard 🗺️
**Approvals**: hook-gated approvals ✅ · web push ✅ · deny-with-reason feedback to agent ✅ · always-allow rules ("∞ Always" button teaches the project policy) ✅ · policy engine (server-side auto-approve: Bash first-token prefix rules, whole-tool rules; audit row records `decided_by: policy`) ✅ · approval history/audit ✅ · approve/deny action buttons ON the push notification (SW notification actions) ✅ · richer glob policies 🔜
**Review & git**: diff capture ✅ · mobile diff viewer ✅ · request-changes follow-up ✅ · commit / push / PR from UI (`/tasks/{id}/commit`, `gh pr create` on the target) ✅ · worktree cleanup button + keep_worktrees per project (vibe-kanban #1764) ✅ · PR status sync 🗺️ · inline comments → feedback prompt 🗺️
**Platform**: PWA installable ✅ · desktop layout ✅ · dark/light ✅ · auth none/token ✅ · header auth 🔜 · Discord/ntfy sinks 🔜 · metrics endpoint 🗺️ · backup = copy one sqlite + docs ✅ · docker-compose + systemd unit 🔜 · docs site 🗺️

## 8. Testing strategy

Three layers, all hermetic by default (MockExecutor; no network, no real agent):

1. **Unit** (`tests/unit/`): stream-json parser (real captured samples + malformed/partial lines), task state machine (legal/illegal transitions), worktree/branch naming, approval token auth, policy matching, diff parsing.
2. **API** (`tests/api/`): FastAPI TestClient + MockExecutor scripted scenarios — full lifecycle (create→dispatch→events flow→review→done), follow-up resume, cancel, approval round-trip incl. long-poll, restart-reconciliation, SSE delivery.
3. **E2E Playwright** (`tests/e2e/`): real browser against a real server in `AGENTDECK_MOCK=1` mode — board renders, create task on phone viewport (390×844) and desktop (1440×900), dispatch, watch card move columns live, timeline streams, approval appears → approve → agent continues, diff renders, mark done. Screenshots on failure.
4. **Real-dispatch smoke** (`tests/smoke/`, opt-in `AGENTDECK_SMOKE=1`): dispatches one trivial task via LocalExecutor against a scratch repo with the real `claude` CLI — proves the integration seam. Run manually / nightly, not in CI.

`.verify.yaml` wires: unit+API+e2e suites, health endpoint, and the Playwright UI flow.

## 9. Roadmap

- **v0.1 (2026-07-14)**: board, dispatch to local/SSH targets, live timeline, approvals + push, diff review, follow-ups, PWA both layouts, full test suite. SHIPPED.
- **v0.2 (2026-07-14)**: auto-verify, commit/push/PR from UI, always-allow policy engine, quick-dispatch bar, voice input, drag-and-drop, worktree cleanup, ghost-run reconciliation, notification action buttons, real cross-LXC SSH dispatch verified. SHIPPED.
- **v0.3 (2026-07-14)**: notification sinks (Discord webhook + ntfy with real ✅/⛔ HTTP action buttons — remote approve/deny with zero VAPID setup), settings API + UI, desktop **Deck view** (multi-pane live cockpit, up to 16 streaming panes — BridgeSpace parity), board text filter, one-click ttyd terminal attach (control plane spawns `ttyd --once` wrapping local/ssh `tmux attach`), Dockerfile + docker-compose + systemd unit. SHIPPED.
- **v0.4 (2026-07-14) — swarm-lite**: agents file their own task cards (`.agentdeck/adk.py` + per-attempt token, capped at 10, 🤖 chip + parent link), **reviewer gate** (per-project: finished work auto-spawns a plan-mode reviewer IN the parent worktree; `VERDICT: APPROVE|REQUEST_CHANGES` parsed onto the parent card + sinks notification; reviewer card auto-completes; no reviewer-of-reviewer recursion), **A/B parallel attempts** (dispatch `model_b` → two worktrees race; task holds `running` until all attempts land; per-attempt events/diff/cost with attempt chips in the sheet), glob policy rules, session replay button. Verified REAL on LXC 101: reviewer approved a live change with verify PASS. Notable bug found by real run: reused worktrees carried stale `exit_code` → instant bogus finalize; fixed + regression-tested. SHIPPED.

### Swarm & memory (v0.4 design notes, informed by BridgeSwarm/BridgeMemory)
BridgeSwarm = one prompt → Coordinator/Builders/Scout/Reviewer roles, exclusive file
ownership per task, reviewer gates merges. BridgeMemory = markdown graph in
`.bridgememory/` shared over MCP. Our angle, leveraging what we already have:
1. **agentdeck-as-MCP/tool for its own agents**: give dispatched agents a scoped
   token + tiny CLI/MCP tool so an agent can FILE follow-up task cards
   (`adk task add …`) instead of doing everything in one context. The board
   becomes the coordination fabric — swarm behavior without a bespoke
   coordinator protocol, and every hand-off is visible/auditable as a card.
2. **Roles as task templates** (coordinator/builder/reviewer prompts) + a
   "review gate" option: a completed builder task auto-spawns a reviewer task
   whose verdict drives review→done vs review→queued.
3. **Shared memory**: per-project `AGENTS.md`/`CLAUDE.md` management + optional
   `.agentdeck/memory/` markdown dir symlinked into every worktree; RAG hook to
   ecosystem-RAG for homelab installs.
- **v0.3**: A/B attempts, templates, session replay, header auth, target dashboards.
- **v0.5 (2026-07-14)**: **agent adapters** (`server/agents.py` seam: claude first-class; codex `exec --json` JSONL mapping and gemini plaintext mapping, both experimental; gated mode validated claude-only at task creation), **shared project memory** (agents leave notes via `adk.py add-note` → injected as a "Project memory" prefix into future dispatch prompts; REST list/delete; caps per attempt), **PctExecutor** (`sudo pct exec <vmid>` — Proxmox-native targets, zero SSH; verified with a real dispatch on LXC 101), capability probe now detects claude/codex/gemini per target, agent picker in UI. SHIPPED.
  - ~~Ops note: subscription OAuth creds copied to targets go stale when the source refreshes (401)~~ **FIXED in v0.8.1** — agentdeck now provisions current auth at dispatch time (`server/credentials.py`): an `AGENTDECK_ANTHROPIC_API_KEY` is injected as `ANTHROPIC_API_KEY` (rotation-proof), or the control plane's *current* OAuth creds are pushed to the target before launch so an agent never runs on a rotated-out copy. Root cause was OAuth *refresh-token* rotation invalidating stale copies. The deep probe provisions-then-tests, so it self-heals.
- **v0.6 (2026-07-15) — ops**: systemd deploy (behind nginx+Authelia), worktree janitor (hourly + admin endpoint), deep target probe (`?deep=true` = real 1-token claude auth round-trip; `< /dev/null` because `claude -p` hangs on ssh exec channels), nightly smoke timer 04:30 with Discord report, dogfood projects (mttyd, pocketlab, gamarr). SHIPPED.
- **v0.7 (2026-07-15) — ephemeral sandboxes**: target kind `sandbox` (host = Proxmox template vmid, template 110 cloned from LXC 101). Every attempt: linked-clone → start → fresh creds pushed → repo (git URL cloned, or path baked in template) → branch → agent with `IS_SANDBOX=1` (claude refuses bypassPermissions as root otherwise) → diff/verify captured to control plane → container destroyed (also on cancel/ghost/unreachable — no leaks). Follow-ups get fresh sandboxes with context in the prompt; reviewer gate skipped (worktree is gone). Ghost detection needs 2 consecutive strikes (transient read races). Verified live: clone→run→review→destroy, $0.29, zero leftovers. SHIPPED.
- **v0.8 (2026-07-15) — any-model + integrations**: per-project `env` injected into the agent launch (KEY=val shell-quoted, validated) — the any-model door: `ANTHROPIC_BASE_URL`/`ANTHROPIC_AUTH_TOKEN` route Claude Code at any Anthropic-compatible endpoint (Ollama ≥0.20 native, LiteLLM, vLLM/llama.cpp gateways); `OPENAI_*`/`GEMINI_*` for the other agents. **MCP server** (`server/mcp_server.py`, FastMCP, 11 tools: board/projects/tasks/create/status/diff/approvals/decide/complete/request_changes) — registered with Claude Code, connects; exposable to the Discord bot via mcpo. Task **templates** (settings-backed, prefill new-task form) + **cost stats** (`/api/stats`, Spend card). Verified: real dispatch against local qwen3.6:35b-a3b on LXC 102 Ollama — transport/events/cost all worked; model too weak for agentic tool-use (empty diff, matches prior finding — a model limit, not a transport bug). SHIPPED.

  **Steering (honest status):** true mid-run message injection into a live agent isn't a stable Claude Code CLI primitive yet. What agentdeck offers instead: (1) approve/deny/deny-with-reason mid-run via the hook loop, (2) cancel + re-dispatch, (3) request-changes follow-up that resumes session context, (4) one-click terminal attach to type directly into the tmux session. Real injection lands when the CLI exposes it.
- **v1.0 (OSP-ready)**: docs site, repo hygiene (git init, LICENSE, CI), rename + publish. Feature set is essentially complete; remaining work is packaging.

## 10. Risks & mitigations

- **Claude CLI flag/schema drift** → parser tolerant of unknown events; integration seam isolated in `claude_runner.py`; smoke test catches breakage.
- **Worktree edge cases** (dirty repos, submodules, LFS, force-deleted branches) → worktree ops are their own module with aggressive error surfacing; cleanup is explicit + a janitor sweep; vibe-kanban's issue tracker mined for known mines.
- **SSH flakiness** → connection reuse, keepalives, exponential backoff; target `status` degrades visibly instead of silently.
- **Phone browsers killing SSE** → resync-on-reconnect (refetch, not replay) + 30s poll backstop; push notifications carry state changes so backgrounded ≠ blind.
- **Hook long-poll ties up agent** → 15m timeout → deny with "human unavailable, proceed read-only or stop"; configurable.
