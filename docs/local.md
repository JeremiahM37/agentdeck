# Standalone local AgentDeck

Use the standalone local runtime when the computer where you run the agent is
also the computer where you want the terminal workspace. It does not need an
AgentDeck server, an SSH alias, a hosted URL, or Grimoire. Your existing agent
CLI and its provider/model environment remain the source of truth.

## Install on Linux or macOS

The local runtime uses `tmux` for durable sessions and Git for project
workspaces. Install those first, then use the installer from an AgentDeck
checkout:

```sh
git clone https://github.com/JeremiahM37/agentdeck.git
cd agentdeck
bash tools/install-local.sh
```

The installer builds the checkout with Go and installs `agentdeck-local` in
`~/.local/bin`. It does not replace an existing `agentdeck` command from the
remote client installer. Add `~/.local/bin` to `PATH` if your shell does not
already include it.

To install a binary you built or received through a release process:

```sh
bash tools/install-local.sh --binary ./agentdeck --prefix "$HOME/.local"
```

The installer accepts only an exact architecture-matched release asset when it
fetches a release. If no such asset exists, it stops and tells you to provide a
checkout or binary; it never substitutes an unrelated platform build.

## Start an agent locally

Pass the existing CLI command after `local`:

```sh
agentdeck-local local claude
agentdeck-local local codex --model gpt-5-codex
```

The local command keeps the interactive workspace in tmux and uses the CLI's
own configuration for authentication, provider endpoints, and model selection.
Install and configure that CLI separately first. A model API endpoint alone is
not an executable coding agent.

Use the normal AgentDeck terminal dashboard and session controls exposed by the
local runtime. Project paths are local paths on this machine; no target SSH
connection is involved. For a blank room, omit a project when prompted and let
the local runtime create its workspace.

## Windows and WSL

The local runtime is Linux-based. On Windows, install WSL2, install Git, tmux,
Go (for a source build), and the agent CLI inside the same WSL distribution,
then run `tools/install-local.sh` from WSL. The Windows-native CLI and Windows
paths are not automatically available inside WSL. The installer intentionally
refuses Git Bash, MSYS, and Cygwin so a partial Windows installation is not
mistaken for a working tmux runtime.

The existing PowerShell remote client installer remains available when the
control plane is on another machine. That path still requires your existing
OpenSSH host/key setup and is separate from `agentdeck-local`.

## Local versus remote

| | Standalone local | Remote client |
|---|---|---|
| Agent process | Same machine as the terminal | AgentDeck server or a registered target |
| Setup | Git, tmux, agent CLI; Go for source builds | SSH alias/key and reachable control plane |
| Command | `agentdeck-local local claude` | `agentdeck` or `agentdeck console` |
| Server URL | Not required | `AGENTDECK_API` or installer `--api` |
| Grimoire | Optional/not required | Optional; configured by the control plane |

Both paths preserve the agent CLI's own provider and model settings. Choose
the remote client when one board should manage agents on several machines;
choose local when the terminal workspace should stay on this computer.
