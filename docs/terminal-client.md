# Use AgentDeck from a terminal

The server binary includes a terminal client. On the server, run:

```sh
agentdeck console
```

Choose Sessions, Tasks, Routines, Projects, Targets, Approvals, Notifications,
or Usage. Select an ID for actions. Attach enters the real tmux session;
Ctrl-b followed by d detaches and returns to the menu. A routine's running task
has a `takeover` action that creates an interactive session from that attempt.

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

Kitty and WezTerm are optional; this client works in a normal terminal. The
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
