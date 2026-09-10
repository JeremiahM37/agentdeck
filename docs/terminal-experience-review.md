# Terminal experience review

Reviewed upstream documentation on 2026-09-09. This is a feature/workflow review,
not a performance benchmark or a claim that every upstream feature was tested.

| Project | Where it sets a useful bar |
| --- | --- |
| [Agent Deck](https://github.com/asheshgoplani/agent-deck) | Live session list, project groups, fuzzy/status search, quick attachment, native conversation forks, worktrees, and a keyboard help system. It also has a web surface and agent configuration tools. |
| [Agent of Empires](https://github.com/agent-of-empires/agent-of-empires) | Persistent tmux sessions with TUI, browser and API access; worktrees, diff review, multi-repository workspaces and optional containers. |
| [Claude Squad](https://github.com/smtg-ai/claude-squad) | A focused terminal workflow for independent agent workspaces, previews, diff review and attachment. |
| [tmuxp](https://github.com/tmux-python/tmuxp) | Declarative, repeatable tmux window/pane layouts and startup commands. It is a workspace loader rather than an agent task/approval system. |
| [Zellij](https://github.com/zellij-org/zellij) | A general terminal multiplexer with strong keyboard UX, layouts, floating/stacked panes, collaboration, plugins and a browser client. |

None is established as better in every dimension. Our previous numbered terminal
menu was a clear usability gap relative to the agent dashboards. Our existing
remote-target, task/routine, takeover, approval and context-file workflows remain
useful, but breadth alone does not make a terminal interface pleasant.

## Implemented acceptance criteria

- A live dashboard, with stable selection while rows refresh or arrive.
- Search by name/project/target/agent/path; status filters and project/target groups.
- Readable status, preview and keyboard help without manually looking up IDs.
- One-key native attachment, proper terminal ownership while attached, and return
  to the same dashboard selection after detaching.
- Named project and target choices, multiline prompts, draft preservation after
  an error, context upload and common edit forms.
- Task dispatch into existing worktree infrastructure, captured diff review,
  routine actions/takeover and approval decisions from the terminal.
- Clear deletion semantics for adopted versus owned sessions; quitting a
  dashboard never ends its agents.
- Wide and narrow layouts, Unicode-safe sizing, real PTY tests and shell-mode
  restoration after exit. A plain menu remains for pipes and accessibility.

These changes target daily agent-management workflow quality. They do not imply
feature-for-feature replacement of every tool: tmuxp-compatible declarative layouts, and Zellij's pane/plugin system remain
separate capabilities. Existing tmux/Zellij workspaces can host the client;
AgentDeck continues using tmux for the underlying agent sessions.

## Full parity goal: evidence ledger

The active goal is terminal quality at least matching Agent Deck and Agent of
Empires, and web quality exceeding Agent of Empires. The acceptance list above
is an initial milestone, **not completion of that goal**. Compare the common
workflows on real Git/tmux and rendered desktop/mobile surfaces. A checked box
in this document is not a substitute for that evidence.

| Workflow | Current evidence and remaining work |
| --- | --- |
| Find, group, monitor, attach, detach, reconnect | Live dashboard + PTY tests; browser real tmux resize/reconnect/dual-client tests. Broader multi-session and saved-view UX comparison remains. |
| Review ongoing work | Live staged/working review implemented for TUI and web; real Git API tests cover renames, binary/untracked files, path boundaries and unchanged index; Playwright covers desktop/mobile, stale responses and retained attachment; actual SSH target proof passed. Full rollout verification is recorded in shared memory. |
| Branch a conversation / isolated parallel work | Native Claude/Codex workspace conversation picker, paginated reader, and exact-ID fork implemented in TUI/web. API, real tmux/PTY and mobile browser tests cover boundaries, unchanged original history and explicit confirmation. Installed Codex fork persisted a distinct ID; installed Claude loaded saved history with its native fork flag (no new turn). Fresh interactive worktree creation/removal now exists in both interfaces, with durable allocation, ownership checks, retained branches, local/SSH Git proof and mobile/desktop/PTy tests. Native forks still share their original workspace; automatic terminal-to-native identity and combined fork/isolation remain gaps. |
| Organize large fleets | Named group paths now persist across projects/targets and inherit on forks/handoffs. TUI and web have nested collapse/counts; terminal search reveals folded children and selection/collapse survive refresh. Web keeps per-tab display state. TUI grouping is remembered per section/server and folded named groups survive restarts, with real PTY restart coverage. Adopted sessions can restore their original record after stopping tracking, gated by a persistent tmux identity marker; real API, PTY and mobile/desktop tests cover retained metadata, concurrent requests and reused-name rejection. Unmarked live records capture identity when released; already-released records without identity still require explicit discovery. Archive now retains terminal snapshots and metadata, with explicit stop confirmation and unarchive without restart; exact native continuation is available separately. Profile configuration, automatic native identity and global conversation search remain. |
| Agent setup | Custom commands exist; named agent settings, MCP/skills setup, installed-agent discovery and lifecycle need comparison with current upstream. |
| Workspace setup | Task and single-repository interactive worktrees run on local/SSH targets. Multi-repository interactive workspaces and repo setup hooks need audit/implementation. |
| Sandbox choices | Existing Proxmox sandbox path is not equivalent to portable Docker/Podman sandboxing; portability gap remains. |
| Web everyday management | Global command search now reaches sessions, tasks, projects, settings and common actions from desktop/mobile; real browser/tmux tests cover attachment, retained terminal identity, keyboard selection, draft/focus restoration, and refresh failures. Mobile terminals now default to focused navigation with a one-button return, retained frames, visible file/tool controls, and Esc/Tab/arrows/Ctrl-C keys in focused, expanded and standalone phone terminals. Real tmux tests cover height gained, application cursor keys, offline recovery, rotation, and restored preferences. Internal terminal tabs, structured chat, PDFs, routines/takeover and approvals exist. Native conversation content is not yet part of global search. Benchmark the same create/find/attach/review/send/recover workflows against AoE desktop and phone; improve discoverability and consistency before claiming superiority. |
| Installation and keyboard UX | Linux and Windows SSH clients exist; installation portability, help consistency, terminal compatibility and first-run flows need further audit. |

Current upstream evidence: the Agent Deck README lists session forks, archive,
MCP/skills managers, global search and settings; the AoE README lists profiles,
repo hooks, multi-repo workspaces, portable containers and structured mobile
views. These remain part of the comparison, not exclusions added to declare the
current implementation sufficient.

## Local terminal layout preferences

The console remembers grouping separately for Sessions, Tasks and other sections,
and remembers folded named session groups. Preferences live under the operating
system user config directory at `agentdeck/console/<server-hash>.json` (on Linux,
`$XDG_CONFIG_HOME/agentdeck/console`, or `~/.config/agentdeck/console`). Each server
has a separate file. Remove its file while the dashboard is closed to reset the
layout. Queries, selected sessions, attention filters and credentials are not
saved. Invalid or newer-version files are preserved; the dashboard displays a
notice and remains usable without saving over them.

## Restore tracking

After stopping tracking, enable **Include ended and untracked sessions** in the
Sessions view and choose **Track again**. In the console press `z`, select the
record, and press Enter (or `m`) to choose **Track again**. For scripts use
`agentdeck api POST /sessions/ID/restore '{}'`.

Restoration retains the session ID, name, project, group, workdir and handoff
links. It does not start, restart or interrupt an agent. A random tmux session
marker captured during adoption (or while releasing an older live record) must still match. Reused names and sessions
already tracked through another record are rejected. Records already released without
identity capture cannot be restored automatically; use **Find running sessions**
(`f` in the console) to adopt explicitly. This operation restores monitoring of
a running process; recovery of a stopped native agent conversation is separate.

SSH time limits cover connection setup, handshake, channel opening and command
execution. A timed-out pooled connection is discarded, and later requests
reconnect. Other in-flight operations on that connection may need retrying; the
persistent tmux agents remain separate from these control connections. Real SSH
protocol tests cover stalled handshake/channel/command, a stalled cached
connection, reconnection and concurrent commands under the race detector.

Capturing identity while releasing an older live record is best effort and
limited to three seconds. An unreachable target still leaves tracking normally;
an already-released record never receives a guessed identity later.

Polling requires a complete framed response and a successful control command.
Blank panes remain live; capture errors and truncated replies preserve the last
known session state. Only explicit tmux absence marks a session dead. Pane text
is encoded so delimiter-shaped output cannot impersonate another session.
Real-command regression tests reproduce the previous false deaths and exercise
blank, long Unicode and missing panes. Adoption and Track again refresh only
the affected target; an integration test verifies they never contact an
unrelated target with a stalled SSH handshake.

## Continue a stopped conversation

In Sessions, include ended/untracked records, open **Saved conversations**, select
an exact history, and choose **Resume conversation**. The web interface opens the
new terminal inside AgentDeck. In the terminal dashboard use `z`, select the old
record, press `H`, choose the conversation and Resume action, then confirm.
Scripts can POST `/api/sessions/ID/resume` with `conversation_id` and optional
`name` (or `agentdeck api POST /sessions/ID/resume '{...}'`).

This continues the selected Claude/Codex history in its original workspace;
**Fork** creates an independent conversation. The old record is retained. The
original terminal must have stopped: merely stopping tracking does not suffice.
Failed or incomplete remote checks refuse the launch. The selected ID persists
on the new record, allowing later requests to check previous resumed terminals,
including released records, before launching again. Concurrent continuation
requests for the same target/agent/conversation are serialized by rejection.
There is no fallback to `--last`, `--continue`, or fresh history on an error.

Selection is explicit because a workspace can contain multiple conversations.
This does not automatically infer IDs for agents launched outside AgentDeck,
or detect a writer on an unrelated machine. Automatic authoritative identity
capture, profiles and broader comparison workflows remain.


## Archive and return later

Choose **Stop and archive** from a live session's actions. Confirmation explicitly
ends its terminal process. Captured output is saved before stopping it; tmux's
session-local identity is checked and absence is verified before the record moves
to Archive. Failed captures, changed identity and refused stops leave it visible.
Worktree files, native transcripts and handoff records remain in place.

The web **Show** selector switches between Active, Include ended/untracked and
Archived. In the terminal dashboard press `A` for Archive, `z` for ended records.
Archived records offer **Archived terminal output** and **Unarchive record**.
Unarchiving restores the ended record without starting a process. Use Saved
conversations afterward to continue an exact history when supported.

Already-stopped records use **Archive stopped record**. An untracked terminal
that is still running must be tracked again before AgentDeck can stop/archive it.
Archive is separate from Stop tracking, which continues to leave processes alone.
Snapshots contain up to 10,000 scrollback lines plus the visible screen, capped
at 2 MiB; if the terminal had already stopped, its last recorded preview is
clearly labeled. An existing archive snapshot survives unarchive/rearchive.

Scripts use POST `/api/sessions/ID/archive` with `{"stop":true}` for a live
terminal or `{"stop":false}` for a stopped record, DELETE the same route to
unarchive, GET `/api/sessions?archived=true` to list archived records, and GET
`/api/sessions/ID/archive/history` to read captured output. Ordinary session lists,
including `?all=true`, exclude archived records. This is an organizational filter,
not an access-control boundary.

Action menus now calculate their available space around the desktop sidebar,
header and mobile bottom navigation. The archive browser test reproduced a
visible-but-unclickable desktop action underneath the sidebar; the corrected
menu remains within the usable area on desktop and phone, including after resize.


## Configuration continuity

Native history now honors the project's environment overrides, including
`CLAUDE_CONFIG_DIR` and `CODEX_HOME`. New interactive launches retain their
resolved agent command, fixed arguments, declared environment and permission
mode privately in the database. Reading, forking and resuming that session use
those settings even if the agent or project settings later change. A new session
uses current settings: agent defaults, then project overrides, then explicit
launch overrides. Trust commands receive the same declared environment.

This records declared launch settings, not a copy of the target's ambient
environment or the contents of its configuration and credential files. Changes
inside those files still apply, and literal credentials explicitly set as
environment overrides remain the saved values. Existing/adopted records without
a snapshot use current agent and project settings; their original process
environment cannot be reconstructed. Invalid snapshots produce an error rather
than silently selecting another configuration.

The snapshot is excluded from session JSON and event payloads. SQLite database
and journal files are restricted to the service account. Regression tests use
real tmux processes and native Claude/Codex transcript files: conflicting project
and agent configuration directories, settings edits between fork and resume,
unchanged history files, private API responses, database migration and reopen.
Named configuration profiles and profile selection remain separate work.


## Verified terminal stops

End/Kill now confirms that the exact tmux session has disappeared before closing
its record. A refused command, timeout, incomplete remote check or no-op stop
returns an error and leaves the session available for retry. Session-local
identity guards against a replacement process with the same name; a learned
identity is retained even if the stop fails. An already-stopped record keeps its
original end time.

Real tmux tests cover failures, identity changes during the operation, neighboring
session names and repeated stops. Desktop and phone browser tests confirm that
a failed stop leaves the card visible and the terminal running, and that a later
successful retry closes it. Stop tracking remains non-destructive.
