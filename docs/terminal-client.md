# Use AgentDeck from a terminal

Run `agentdeck` in an interactive terminal, or `agentdeck console`, for the live
dashboard. It opens on Sessions, groups by project, and refreshes automatically.
Use `agentdeck serve` to start the server explicitly. Existing systemd/container
launches with no arguments and no terminal still start the server.

| Keys | Action |
| --- | --- |
| ↑/↓ or j/k | Select a session, task, routine, project, target or approval |
| Enter | Attach; Ctrl-b then d returns to the same selection |
| / | Fuzzy search names, projects, targets, agent names and paths |
| @ / ! / # / & at start of search | Waiting / running / idle / failed |
| 1–6, ←/→ | Switch sections |
| g / w | Group by project or target / show items needing attention |
| Tab / p, PgUp/PgDn | Focus and scroll the preview |
| n / e / m | Create / rename / all actions |
| h / v / u | Read retained history / review a task diff / upload context |
| f | Find running agents and add one to tracking by name |
| 7 / 8 / 9 | Settings / usage / full API |
| ? / q | Help / quit without stopping agents |

The wide layout shows a live preview beside the list. Narrow terminals keep one
focused pane visible; Tab switches between the list and preview. Forms use named
project/target choices, accept multiline prompts, and keep your draft after an
API error. Tab changes fields, arrows choose options, Ctrl-s submits, Esc cancels.
Task creation can dispatch into an isolated worktree; Tasks → Actions also offers
routine takeover, follow-up, diff review, completion and cancellation.

`agentdeck console --plain` retains the line-oriented menu. Redirected input or
output selects it automatically, so scripts keep working. The JSON API commands
below are unchanged. The TUI polls the existing API; it does not run another
agent collector or maintain a second session database.

## Install on another Linux machine

Use an existing SSH alias for the AgentDeck server. Download
`/desktop/install-agentdeck-cli.sh` from your AgentDeck instance, then run:

```sh
bash install-agentdeck-cli.sh --server agentdeck --api https://YOUR_SERVER:8443
agentdeck
```

The installer copies the client from your server over SSH, checks the machine
architecture, preserves any previous client, and installs into `~/.local/bin`.
The installed launcher opens the console by default. Re-run it to update.
The server and client must use the same Linux architecture for this installer;
other architectures can build the Go binary from source.

For Windows, download `/desktop/install-agentdeck-cli.ps1` and run it with
`-Server YOUR_SSH_ALIAS`. It installs an OpenSSH launcher on your user PATH;
management runs on the server. `agentdeck upload` stages local files over SCP.
The native Windows launcher requires OpenSSH, with your existing host/key setup.

This client works in your existing terminal. The
separate desktop URI installers enable opening an attachment from the web UI.

## Scripting and context files

```sh
agentdeck api GET /sessions
agentdeck api POST /sessions '{"name":"Scratch","agent":"codex","scratch":true}'
agentdeck api POST /tasks/12/takeover '{}'
agentdeck api PATCH /routines/3 '{"enabled":false}'
agentdeck api POST /sessions/4/send '{"text":"Run the tests"}'
agentdeck upload session 4 ./requirements.pdf
agentdeck files session 4
agentdeck download session 4 reports/result.txt ./result.txt
agentdeck attach session 4
```

Uploads print the stored remote path. They do not submit a message: mention the
path in your prompt, or paste it in the attached terminal. Upload also accepts
`task`, `attempt`, and `project`. Files/download use `session`, `attempt`, or
`project`. Downloads preserve an existing destination file.

`api` writes JSON to stdout and failures to stderr with a nonzero exit code.
Bodies accept inline JSON, `@filename`, or `-` for stdin. Every web operation is
available through the same API; specialized menus cover frequent operations,
and Full API accepts the remainder without opening a browser.

| Resource | Operations |
| --- | --- |
| `/sessions` | GET list, POST create; GET/PATCH/DELETE `/{id}` |
| `/sessions/discover`, `/sessions/adopt` | GET running agents, POST track |
| `/sessions/{id}/send`, `/handoff`, `/promote` | POST message/key, handoff, associate project |
| `/sessions/{id}/reader`, `/wraps` | GET conversation, handoff records |
| `/tasks` | GET list, POST create; GET/PATCH/DELETE `/{id}` |
| `/tasks/{id}/dispatch`, `/takeover`, `/followup`, `/complete`, `/cancel`, `/commit`, `/cleanup` | POST actions |
| `/tasks/{id}/messages`, `/events`, `/diff` | GET; messages also POST |
| `/tasks/clear` | POST completed-task cleanup |
| `/routines`, `/projects`, `/targets` | GET list, POST create; PATCH/DELETE `/{id}` |
| `/routines/{id}/run`, `/targets/{id}/check` | POST run/probe |
| `/projects/import/scan`, `/projects/import` | GET scan with target_id/root; POST import |
| `/projects/{id}/brief`, `/notes`, `/wraps`, `/capability` | GET project context; DELETE `/notes/{noteID}` |
| `/approvals`, `/approvals/{id}/decision` | GET queue, POST approved/denied decision |
| `/agents`, `/templates`, `/settings` | GET or PUT configuration |
| `/models`, `/stats`, `/health`, `/projects/usage` | GET available models, usage and health |
| `/settings/test-notification`, `/admin/janitor` | POST test or maintenance |

Configure `AGENTDECK_API` and `AGENTDECK_AUTH_TOKEN` for HTTP. Set
`AGENTDECK_ATTACH_HOST` to the server's SSH alias on a remote Linux client;
attachment is resolved on the server, where its tmux sessions and SSH targets
exist. The Linux installer sets the URL and alias in its launcher.

Inside an existing tmux workspace, native attachment opens a full-size popup
(tmux 3.2 or newer). The attachment owns its keyboard input; Ctrl-b d closes it
and returns to the same dashboard selection without detaching the outer workspace.

## Review live code changes

On a session or project, press `v` for live Git review. Left/right changes files,
`s` switches working-tree versus staged changes, PgUp/PgDn scrolls, `r` refreshes
the current file, and Esc returns. Tasks retain their captured diff on `v`; their
actions menu also offers **Review live changes** for an existing attempt.

The same review is available on the web through a session's **More → Review
changes** or an attached terminal's **Tools → Review changes**. It includes file
search, line numbers, mobile wrapping, and separate staged/working counts. It
reads a snapshot when opened/refreshed; the agent can continue editing. Large
patches are explicitly truncated at 512 KiB. Git and Python 3 run on the target,
including SSH targets; there is no local-checkout assumption and no staging or
checkout mutation.
