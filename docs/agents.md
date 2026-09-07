# Agents

AgentDeck drives more than one coding CLI. Everything CLI-specific lives in
`internal/agents/` (`launch.go` and `parse.go`, plus `claude.go` for Claude
Code's settings and permission rules), so the run
protocol — worktree, tmux, events file, exit code — is identical whichever agent
you pick.

| Agent | Status | Gated approvals | Session resume | Credentials pushed to remote targets |
|---|---|---|---|---|
| `claude` | first-class | ✅ | ✅ | `~/.claude/.credentials.json` |
| `codex` | first-class | ❌ | ❌ | `~/.codex/auth.json` |
| `gemini` | experimental | ❌ | ❌ | — |

## Choosing one

Set a project's default and every task inherits it:

```bash
curl -X PATCH .../api/projects/3 -d '{"default_agent":"codex"}'
```

In the PWA, the **Agent** toggle in the new-task sheet picks per task, starting
from the project default. A task's explicit `agent` always wins.

The toggle reshapes the form, because the options are not interchangeable:
gated approvals and the Claude model aliases (`fable`/`opus`/`sonnet`/`haiku`)
are Claude-only, so picking Codex disables gated mode and hides the A/B row
rather than letting you build a dispatch that fails later. If the target's last
probe didn't find the binary, the toggle says so instead of failing at dispatch.

## Permission modes

AgentDeck's modes map onto each CLI's own sandboxing:

| Mode | claude | codex |
|---|---|---|
| `plan` | `--permission-mode plan` | `--sandbox read-only` |
| `acceptEdits` | `--permission-mode acceptEdits` | `--sandbox workspace-write` |
| `bypassPermissions` | `--permission-mode bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` |
| `default` (gated) | PreToolUse hook → approval on your phone | **rejected** — codex has no hook |

Codex requires **≥ 0.140**: the older `--full-auto` flag was removed upstream,
and `item.started` events (which make the timeline live rather than
after-the-fact) only appear in newer builds.

## Context and tools

The staged context bundle reaches every agent — it is prepended to the prompt, so
it needs no CLI support. Per-project MCP servers and permission rules are written
for Claude only (`--mcp-config`, `--settings`); codex reads its own
`~/.codex/config.toml`. See [context-parity.md](context-parity.md).

## Binary not found

Agents installed under `~/.local/bin` are invisible to a systemd unit, whose
`PATH` doesn't include it — the probe then reports the agent as missing. Point
AgentDeck at the real path:

```ini
# /etc/systemd/system/agentdeck.service.d/override.conf
[Service]
Environment=AGENTDECK_CODEX_BIN=/home/you/.local/bin/codex
```

`AGENTDECK_CLAUDE_BIN` and `AGENTDECK_GEMINI_BIN` work the same way.

## Any CLI, for sessions

Dispatched tasks use the three built-in adapters, because a task needs its output
parsed into a timeline. An interactive **session** does not — a human is reading
the terminal — so any CLI can drive one. Define it once:

```bash
curl -X PUT .../api/agents -d '[{
  "name": "aider",
  "command": "aider",
  "args": ["--no-auto-commits"],
  "model_flag": "--model",
  "prompt_arg": true,
  "env": {"OPENAI_API_BASE": "http://ollama-host:11434/v1"}
}]'
```

`prompt_arg` says the CLI accepts an opening message as a positional argument.
When it does, the project briefing rides on the command line and there is no
timing to lose; when it does not, agentdeck waits for the pane to settle at a
prompt and types it in. A definition sharing a built-in's name overrides it,
which is how a CLI whose flags have drifted gets fixed without a release.

A project's `env` is layered over the agent's, so pointing one project at a local
model does not require redefining the agent.

## Adding an agent (as a first-class task adapter)

Add a case to two functions in `internal/agents/`: build the inner shell command
in `Launcher.Command` (launch.go), and map the CLI's output to AgentDeck's event
types (`init` / `text` / `tool_use` / `tool_result` / `result`) in
`ParseStreamLines` (parse.go).

One trap worth inheriting: both codex and gemini read stdin even when the prompt
is passed as an argument, and a tmux pane's stdin never reaches EOF — so the
launcher redirects `< /dev/null`. Without it the agent waits forever and the
attempt looks alive but never moves.
