# Standalone local AgentDeck

Use the standalone local runtime when the computer where you run the agent is
also the computer where you want the terminal workspace. It does not require a
separately hosted AgentDeck server, an SSH alias, a hosted URL, or Grimoire.
AgentDeck starts a private local helper on demand; no manual server setup is
needed. Your existing agent CLI and its provider/model environment remain the
source of truth.

## Install on Linux or macOS

The local runtime uses `tmux` for durable sessions, Git for project workspaces,
and Python 3 for its local session/task helpers. Install those first, then use
the installer from an AgentDeck checkout:

```sh
git clone https://github.com/JeremiahM37/agentdeck.git
cd agentdeck
bash tools/install-local.sh
```

The installer builds the checkout with Go and installs `agentdeck` in
`~/.local/bin`. If the remote client already owns that name, it installs
`agentdeck-local` instead, leaving the remote launcher unchanged. Add
`~/.local/bin` to `PATH` if your shell does not already include it.

To install a binary you built or received through a release process:

```sh
bash tools/install-local.sh --binary ./agentdeck --prefix "$HOME/.local/bin"
```

The installer deliberately accepts a checkout or an already-built binary. This
repository has no release asset workflow, so it does not guess at download
URLs or substitute an unrelated platform build.

Source builds require Go 1.25.x in addition to the runtime prerequisites.

## Start an agent locally

The installed command has the same local command surface as the hosted client:

```sh
agentdeck local
agentdeck local api --help
```

If the installer selected `agentdeck-local` to preserve an existing remote
launcher, substitute that name in the commands above.

With no `AGENTDECK_API`, ordinary AgentDeck commands use the local runtime by
default. `agentdeck local` explicitly selects local mode even when that
variable points at a remote control plane. Conversely, keep `AGENTDECK_API`
set when an ordinary command should use the hosted client; an unreachable
explicit remote does not silently fall back to local state.

The helper starts privately when the first local command needs it. Check or
stop it with:

```sh
agentdeck local status
agentdeck local stop
```

Stopping the helper does not discard the local database or durable tmux
sessions; later local commands can start it again and resume them. A stop is
refused while a task is still active, so inspect or finish that task first.
Local state defaults to `~/.local/state/agentdeck/local`, or to
`$XDG_STATE_HOME/agentdeck/local` when `XDG_STATE_HOME` is set.

`agentdeck local` opens the local dashboard/console, where you choose the
configured coding-agent command and its project. The local command keeps the
interactive workspace in tmux. Configure the coding-agent command and its
provider/model settings in AgentDeck; a model API endpoint alone is not an
executable coding agent.

Use the normal AgentDeck terminal dashboard and session controls exposed by the
local runtime. Project paths are local paths on this machine; no target SSH
connection is involved. For a blank room, omit a project when prompted and let
the local runtime create its workspace.

## Windows and WSL

The local runtime is Linux-based. On Windows, install WSL2, Git, tmux, Python
3, Go (for a source build), and the agent CLI inside the same WSL distribution,
then run `tools/install-local.sh` from WSL. The Windows-native CLI and Windows
paths are not automatically available inside WSL. The installer intentionally
refuses Git Bash, MSYS, and Cygwin so a partial Windows installation is not
mistaken for a working tmux runtime.

The existing PowerShell remote client installer remains available when the
control plane is on another machine. That path still requires your existing
OpenSSH host/key setup and is separate from the local command.

## Local versus remote

| | Standalone local | Remote client |
|---|---|---|
| Agent process | Same machine as the terminal | AgentDeck server or a registered target |
| Setup | Git, tmux, Python 3, agent CLI; Go for source builds | SSH alias/key and reachable control plane |
| Command | `agentdeck local` | `agentdeck` or `agentdeck console` |
| Server URL | Not required | `AGENTDECK_API` or installer `--api` |
| Grimoire | Optional/not required | Optional; configured by the control plane |

Both paths preserve the agent CLI's own provider and model settings. Choose
the remote client when one board should manage agents on several machines;
choose local when the terminal workspace should stay on this computer.

Linux local terminal mode is the tested path. macOS local terminal mode is
experimental and has not been runtime-tested. Browser and file integrations
currently rely on Linux-specific assumptions, so this guide makes no macOS
support or test claim for them; use the hosted client for those integrations
when required.
