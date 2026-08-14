# Context parity

A dispatched agent is not the same as the agent in your terminal. It opens a
**fresh session** in a throwaway worktree, so it has no conversation history —
and on an `ssh`, `pct`, or `sandbox` target it also has none of the control
plane user's Claude config: no `CLAUDE.md`, no MCP servers, no memory.

Nothing announces that gap. The run just produces worse output.

These four settings close it. All are optional and default to off, so an
existing install behaves exactly as it did before.

| Setting | Where | Fixes |
|---|---|---|
| `context_paths` | target + project | agent doesn't know your conventions |
| `mcp` / `strict_mcp` | project | agent has fewer tools on remote targets |
| `permissions` | project | tools silently denied with no prompt |
| `memory_dir` | target | agent starts memory-blind every attempt |

## Staged context

Files listed in `context_paths` are read from the **control plane's** filesystem
at dispatch and copied into `<worktree>/.agentdeck/context/`, with a header
prepended to the prompt telling the agent to read them first. One place to
curate, identical result on every target kind. Globs are supported.

```bash
# every project on this target gets the house rules
curl -X PATCH .../api/targets/1 \
  -d '{"context_paths":["/home/you/CLAUDE.md","/home/you/docs/*.md"]}'

# plus something only this project needs
curl -X PATCH .../api/projects/3 \
  -d '{"context_paths":["/home/you/notes/schema.md"]}'
```

Target files are staged first, then the project's. Files are truncated at 256KB
and the bundle stops at 1MB — both cases are reported in the prompt and in
`.agentdeck/context/INDEX.md` rather than dropped silently. A path that doesn't
exist is reported too, so a typo shows up as a note instead of missing context.

On a `local` target you may not need this: Claude Code already discovers
`CLAUDE.md` by walking up from the worktree, which usually lands inside your home
directory. It is the remote targets that have nothing.

## MCP servers

The host user's MCP config doesn't exist on a remote target, so the same task
runs with strictly fewer tools there. Give a project its own:

```bash
curl -X PATCH .../api/projects/3 -d '{
  "mcp": {"myserver": {"command":"python3","args":["-m","myserver"]}},
  "strict_mcp": false}'
```

Written to `.agentdeck/mcp.json` and passed as `--mcp-config`. A bare mapping is
wrapped in `mcpServers` for you; a full `{"mcpServers": {...}}` document is
passed through. `strict_mcp` adds `--strict-mcp-config`, which ignores the host's
own MCP configuration entirely — use it when you want the tool surface to be
reproducible rather than dependent on whoever set up the box.

## Permissions

**This is the one that bites.** `claude -p` is headless: there is no prompt. A
tool the permission rules don't grant is simply denied, and the only trace is a
tool error the agent may or may not mention.

So `acceptEdits` — the default mode — lets an agent edit files but **not run
Bash**, unless you grant it:

```bash
curl -X PATCH .../api/projects/3 \
  -d '{"permissions":{"allow":["Bash(pytest*)","Bash(git status*)"],
                      "deny":["Bash(rm *)"]}}'
```

These are written to `.agentdeck/settings.json` for every mode. Accepted keys are
`allow`, `deny`, `ask`, `defaultMode`, `additionalDirectories`; anything else is
rejected with a 400 when you set it, rather than becoming a mystery denial later.

### Gated mode

`permission_mode: "default"` routes tool calls through the approval hook and onto
your phone. The matcher defaults to `*` — **every** tool. It used to be
`Bash|Write|Edit|MultiEdit|NotebookEdit`, which meant MCP tools, WebFetch, and
Task were never gated and therefore never *allowed* either. Narrow it per project
if you want fewer taps:

```bash
curl -X PATCH .../api/projects/3 -d '{"gate_matcher":"Bash|Write|Edit"}'
```

Anything the matcher excludes is denied, not allowed — narrow it deliberately.

## Shared memory (opt-in)

Claude Code keys its memory store by working directory, so a fresh worktree per
attempt starts memory-blind and everything an agent learns dies with the
worktree. Point a target's attempts at one shared store:

```bash
curl -X PATCH .../api/targets/1 \
  -d '{"memory_dir":"/home/you/.claude/projects/-home-you/memory"}'
```

At dispatch, agentdeck symlinks that attempt's session memory directory at your
store.

> **Caveat.** There is no CLI flag for this, so it works by mirroring Claude
> Code's internal `~/.claude/projects/<slugified-cwd>/memory` layout. That is not
> a public API and could change in a future release — which is why it is off
> unless you set it. A failed link logs a warning and never fails the run.
>
> If you'd rather not depend on internals, agentdeck's own project memory does a
> similar job through a supported path: agents call
> `python3 .agentdeck/adk.py add-note "..."` and the notes are prepended to every
> later prompt on that project.
