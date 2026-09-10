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
| Branch a conversation / isolated parallel work | Native Claude/Codex workspace conversation picker, paginated reader, and exact-ID fork implemented in TUI/web. API, real tmux/PTY and mobile browser tests cover boundaries, unchanged original history and explicit confirmation. Installed Codex fork persisted a distinct ID; installed Claude loaded saved history with its native fork flag (no new turn). Interactive worktree creation and automatic terminal-to-native identity remain gaps. |
| Organize large fleets | Project/target grouping and search exist. Saved profiles, archive/recovery, custom groups, global conversation search need audit/implementation. |
| Agent setup | Custom commands exist; named agent settings, MCP/skills setup, installed-agent discovery and lifecycle need comparison with current upstream. |
| Workspace setup | Task worktrees and target executors exist; multi-repository interactive workspaces and repo setup hooks need audit/implementation. |
| Sandbox choices | Existing Proxmox sandbox path is not equivalent to portable Docker/Podman sandboxing; portability gap remains. |
| Web everyday management | Internal terminal tabs, structured chat, PDFs, routines/takeover and approvals exist. Benchmark the same create/find/attach/review/send/recover workflows against AoE desktop and phone; improve discoverability and consistency before claiming superiority. |
| Installation and keyboard UX | Linux and Windows SSH clients exist; installation portability, help consistency, terminal compatibility and first-run flows need further audit. |

Current upstream evidence: the Agent Deck README lists session forks, archive,
MCP/skills managers, global search and settings; the AoE README lists profiles,
repo hooks, multi-repo workspaces, portable containers and structured mobile
views. These remain part of the comparison, not exclusions added to declare the
current implementation sufficient.
