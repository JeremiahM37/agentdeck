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
feature-for-feature replacement of every tool: native conversation branching,
tmuxp-compatible declarative layouts, and Zellij's pane/plugin system remain
separate capabilities. Existing tmux/Zellij workspaces can host the client;
AgentDeck continues using tmux for the underlying agent sessions.
