# Use AgentDeck from a terminal

Run `agentdeck` in an interactive terminal, or `agentdeck console`, for the live
dashboard. It opens on Sessions, groups by project, and refreshes automatically.
Use `agentdeck serve` to start the server explicitly. Existing systemd/container
launches with no arguments and no terminal still start the server.

| Keys | Action |
| --- | --- |
| ↑/↓ or j/k | Select a session, task, routine, project, target or approval |
| Enter | Attach; Ctrl-b then d returns to the same selection |
| S | Choose a machine and open a blank persistent shell |
| / | Fuzzy search names, projects, targets, agent names and paths |
| @ / ! / # / & at start of search | Waiting / running / idle / failed |
| 1–6, ←/→ | Switch sections |
| g / w | Group by project or target / show items needing attention |
| Tab / p, PgUp/PgDn | Focus and scroll the preview |
| n / e / m | Create / rename / all actions |
| P | Manage named launch profiles |
| Q | Manage agent runners (add custom CLIs) |
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

Use `agentdeck shell [MACHINE]` when you want to work directly in a machine's
shell. With no machine argument, an interactive client shows a searchable
picker; scripts should pass the target name or numeric ID. The shell is tracked
as a durable session and starts the target user's interactive shell in a fresh
AgentDeck scratch directory. It does not select a project, agent, model, launch
profile, memory, or worktree.

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
`-Server YOUR_SSH_ALIAS` (and `-Api http://127.0.0.1:9110` when the server uses
a non-default API address). It installs an OpenSSH launcher on your user PATH;
management runs on the server with an explicit hosted API environment.
`agentdeck upload` stages local files over SCP.
The native Windows launcher requires OpenSSH, with your existing host/key setup.

For a terminal workspace that runs entirely on the current computer, use the
[standalone local installer](local.md) instead. It installs `agentdeck` (or
`agentdeck-local` when the remote client already owns that name) and does not
need a control-plane URL or SSH server; the remote installer above continues to
install the `agentdeck` client.

This client works in your existing terminal. The
separate desktop URI installers enable opening an attachment from the web UI.

## Scripting and context files

```sh
agentdeck api GET /launch-profiles
agentdeck agent list
agentdeck agent save @agents.json
agentdeck api POST /sessions '{"name":"Work","profile_id":7,"scratch":true}'
agentdeck api GET /sessions
agentdeck api POST /sessions '{"name":"Scratch","agent":"codex","scratch":true}'
agentdeck shell AIServer
agentdeck api POST /tasks/12/takeover '{}'
agentdeck api PATCH /routines/3 '{"enabled":false}'
agentdeck api POST /sessions/4/send '{"text":"Run the tests"}'
agentdeck api POST /sessions/4/setup/cancel '{}'  # request checkout cancellation; retain files
agentdeck api POST /sessions/4/worktree/recover '{}'  # validate interrupted allocation; keep files
agentdeck upload session 4 ./requirements.pdf
agentdeck files session 4
agentdeck download session 4 reports/result.txt ./result.txt
agentdeck attach session 4
```

`agentdeck agent save` accepts the runner fields shown in Settings → Agents:
`name`, required `command`, optional `args`, `model_flag`, provider endpoint
environment, `prompt_arg`, `resume_args`, `yolo_args`, and `env`. The command
starts the runner; provider URLs and models configure that runner through its
environment contract.

Uploads print the stored remote path. They do not submit a message: mention the
path in your prompt, or paste it in the attached terminal. Upload also accepts
`task`, `attempt`, and `project`. Files/download use `session`, `attempt`, or
`project`. Downloads preserve an existing destination file.

## Project skills

Claude and Codex can discover skills on the selected target and attach them to a
project. Configure additional target-local source directories with the project
API; repository skills are discovered by walking up from the project's Git root:

```sh
agentdeck api PATCH /projects/7 '{"skill_sources":["/srv/agent-skills"]}'
agentdeck skill list 7 --agent codex
agentdeck skill attach 7 'configured:<source-hash>/lint' --agent codex
agentdeck skill attached 7 --agent codex
agentdeck skill detach 7 12
```

The web and terminal dashboards provide the same discovery and attach/detach
actions. Sources are read on the target and linked into `.claude/skills` or
`.agents/skills` in the project and any selected worktree; source files are not
copied. Detach removes only links proven to be AgentDeck-owned. Native
repository skills and changed or foreign destinations are preserved.

`api` writes JSON to stdout and failures to stderr with a nonzero exit code.
Bodies accept inline JSON, `@filename`, or `-` for stdin. Every web operation is
available through the same API; specialized menus cover frequent operations,
and Full API accepts the remainder without opening a browser.

| Resource | Operations |
| --- | --- |
| `/sessions` | GET list, POST create; GET/PATCH/DELETE `/{id}` |
| `/shells` | POST create a tracked blank shell on a target (`target_id` or `machine`) |
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

## Saved native conversations and forks

The Sessions view also has **Recently closed**. It fetches the latest ten
server records each time it opens, so ended sessions remain available after the
live list is empty. The terminal dashboard opens the same list with `C` or
Actions → Recently closed; `Esc` or Backspace returns to the live list. A record
with an exact durable native binding offers **Resume** and continues that
conversation after refreshing the session list. Released adopted terminals
offer **Restore tracking**, which resumes monitoring the existing terminal
without launching another agent. Other records offer **Choose history**, which
opens the existing explicit native history picker.

On a Claude or Codex session, press `H` (or Actions → Saved conversations / fork)
to choose a saved conversation from that workspace on its target. Read it in the
preview, use `O` for the preceding page, or choose **Fork** in the picker. The
confirmation names the exact conversation ID. Choose **Use the same files** or
**New isolated Git worktree** in Workspace for forks. Isolation creates a branch
from the selected committed base (HEAD by default); uncommitted changes stay in
the parent workspace. A blank branch name gets a unique name. The original
conversation is unchanged, and forking submits no new prompt.

The web offers **Saved conversations** in the session's More menu and the
attached terminal's Tools menu. Choose a conversation explicitly; messages are
grouped by role, tool activity folds away, and **Load earlier messages** pages
back without replacing the messages already on screen. Codex's saved thread
names are used when present. Long messages and discovery limits are labelled.

The web fork confirmation offers the same Workspace, branch and base controls.
The confirmation uses the dialog space; Cancel returns to the transcript.
Scripts pass an optional worktree object to POST /sessions/ID/fork, for example
{"conversation_id":"UUID","worktree":{"branch":"experiment","base":"HEAD"}}.
Omitting worktree keeps the shared-files behavior. Resume always continues in
the recorded directory; it does not allocate another worktree. A removed or
unavailable directory produces an error until the workspace is restored.

History reads native JSONL stores using the target's `CODEX_HOME` or
`CLAUDE_CONFIG_DIR` (including configured agent environment overrides), and
checks the stored working directory. It never guesses that a directory's most
recent conversation belongs to the selected terminal. Other agents retain the
terminal history/reader. Native history is currently limited to these two agent
formats; private reasoning/system/developer records are omitted.

`GET /sessions/{id}/conversations` lists candidates;
`GET /sessions/{id}/conversations/{uuid}?before=BYTE_OFFSET` reads a page;
`POST /sessions/{id}/fork` accepts `conversation_id` and an optional `name`.
Custom Claude/Codex definitions can provide `fork_args` with an `{id}` placeholder.
Without it, the picker offers reading only. A fork is a conversation branch, not
a Git worktree; interactive worktree isolation is a separate remaining feature.

Optional installed-CLI checks (no new prompt/model turn):
`python3 tests/native_codex_fork.py` verifies a distinct persisted Codex ID,
copied fixture history, and unchanged original; `python3 tests/native_claude_fork.py`
verifies Claude loads fixture history with its native fork flag and leaves the
original unchanged. Claude need not write the new transcript before a new turn.
The regular suite tests target lookup, pagination, workspace boundaries and
actual tmux launch with scripted agents, without requiring either paid CLI.

## Interactive Git worktrees

In **New session**, select a project and enable **Isolate in a new Git worktree**.
The terminal dashboard's `n` form has the same choice and also accepts an
explicit repository directory. Choose a base branch/tag/commit (blank means
committed `HEAD`) and a new branch name, or let AgentDeck allocate a unique name.
The agent starts in a separate directory beside the repository. Uncommitted
source edits are not copied; this mode starts a fresh conversation.

Both interfaces show the branch, allocation state and working directory. On
the web, expand the worktree line to see its path/base. After ending a session,
use **Include ended and untracked sessions** (web) or `z` (TUI) to find it again.
**More/Actions → Remove worktree** removes the directory only after its sessions
and tmux terminals have left, and only when Git reports no changed, untracked
or ignored files. The branch and committed work remain. There is no force-delete
option. Cleanup checks the recorded repository, branch and ownership marker.

`POST /api/sessions` accepts `"worktree":{"base":"main","branch":"feature/example"}`;
it can use a project or an absolute `workdir` on a local/SSH target. Session
responses include `workspace`. `DELETE /api/sessions/{id}/worktree` performs
checked removal; ending/dismissing a session never removes its worktree.
Allocation is recorded before Git runs; failed allocations remain in ended
sessions with their planned path. A launch failure does not erase files.

This is separate from saved-conversation forking: native conversation forks
currently share their original directory. Interactive worktrees are not yet a
multi-repository workspace, and setup hooks/profile templates remain separate
work on the parity roadmap.

## Named session groups

Groups organize sessions across projects and targets. Press `G` or choose
**Actions → Move to group**; use a path such as `Work/Client`, or clear it to
ungroup. `g` cycles project, target, none and named-group ordering. Search also
matches group paths and worktree branches/directories. New-session forms accept
a group, and conversation forks and handoff successors inherit it.

On the web, **More → Move to group** offers existing names and keeps a failed
edit open for correction. **Group by** selects named group, project, target or
none. Named groups form collapsible nested sections with session/waiting counts.
The selected grouping and collapsed sections survive reloads in that browser
tab; searching opens matching sections. The API's `group_path` is shared across
clients, while these display preferences are local to the browser.

`PATCH /api/sessions/{id}` accepts `{"group_path":"Work/Client"}` or
`{"group_path":""}`. Names are normalized by trimming each level; empty levels,
control characters and more than eight levels are rejected. Moving a group
label does not move files, change the project, or restart the agent.

When a live Claude or Codex conversation can be verified, **Saved conversations**
marks it **Current terminal** and selects it initially. The web reader opens its
saved messages immediately; the terminal dashboard defaults its conversation
choice to that entry. Refresh preserves a different conversation you selected.

Identification is read-only and currently uses Linux process information, including
on SSH/WSL targets. Claude supplies a runtime record tied to its process start;
Codex must hold its transcript open in the native `codex` process. The active tmux
pane and its identity must remain stable. Ambiguous, unavailable or stale evidence
leaves manual selection available. A new Claude conversation can be identified
before it has saved any readable messages; it becomes readable after persistence.
Older active terminals are checked through their current pane without changing
their tracking metadata. Renamed Codex binaries may require manual selection.
