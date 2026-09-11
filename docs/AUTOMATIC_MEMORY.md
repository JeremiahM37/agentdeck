# Automatic project memory

## New projects get their own durable location

Creating, importing, or promoting a project allocates a random, persistent
`memory_topic` in AgentDeck's database. With automatic Grimoire memory enabled,
AgentDeck creates `memory/<memory_topic>.md` in the vault. Renaming the project
does not move its memory; similarly named projects cannot collide. The project
response exposes `memory_topic` and `memory_status` (`ready`, `unavailable`, or
`disabled`). A network failure does not discard the project or pretend that
setup succeeded: retry `POST /api/projects/{id}/memory` when Grimoire is back.
Retries never overwrite an existing note. Deleting a project retains its memory.

New projects read their own managed note by default. Explicit per-project paths
add other references alongside it. Existing projects keep their configured or
legacy paths; no bulk migration is performed. Manual/off policy performs no
provisioning or automatic reads. New launch/task prompts include a short memory
destination hint, and requested session handoffs write to that same topic with
topic-scoped reconciliation. Memory writes still require an explicit agent or
handoff action; ordinary prompts are not recorded automatically.

The default 2,400-byte retrieval ceiling does not include the short, once-per-
launch/task destination hint. Preview a project's brief to inspect context and
setup/lookup status before launching.

AgentDeck remains useful without Grimoire. When `AGENTDECK_GRIMOIRE_URL` is
configured, the optional provider can supply project knowledge automatically
without delegating that decision to the agent.

The default `AGENTDECK_GRIMOIRE_CONTEXT_MODE=project` narrows retrieval to
the assigned project's conventional paths: `memory/<slug>.md`,
`memory/<slug>/`, and `Agent Memory/project_<underscore_slug>.md`.
It does not search unrelated notes that merely mention the project.

Override mappings with `AGENTDECK_GRIMOIRE_CONTEXT_PROJECTS`, a JSON object
keyed by the exact registered project name:

```json
{
  "kestrel": {
    "mode": "scoped",
    "paths": ["memory/kestrel.md", "Projects/Kestrel/"],
    "max_bytes": 1800
  },
  "experiment": {"mode": "manual"}
}
```

Directories require a trailing `/`; other paths match exactly. A missing scoped
mapping never falls back to the whole vault. `all` permits the whole readable
corpus, while `manual` or `off` makes no automatic request. Per-project settings
override the global mode. Explicit MCP calls remain available separately.

## Delivery and cost

- Non-reviewer task dispatch uses the assigned project and actual task prompt.
- Interactive launches/resumes get a small project-start briefing even without
  `brief: true`. Existing repo-document and local-handoff briefings are separate.
- Messages sent through AgentDeck's send API request relevant project context;
  continuation/acknowledgement messages are skipped.
- Unassigned scratch sessions get no project memory.
- Each automatic response is bounded to 2,400 UTF-8 bytes by default (maximum
  8,000) and five items. These are byte bounds, not exact model token counts.
- Grimoire performs lexical retrieval without LLM or embedding calls. It returns
  current accepted facts plus note excerpts, excluding private/untrusted content.
  Scope filters never grant access beyond the caller's permissions.
- Delivered fingerprints are cached per interactive session for 30 minutes.
  Revised text has a new fingerprint. Failed sends are not marked delivered.
  Restarting AgentDeck resets this cache.
- Retrieval has a 1.5-second context deadline. Failure/older Grimoire versions
  add no automatic context; there is no unscoped legacy fallback.
- No forced reflection turn or automatic memory writes are introduced. Existing
  explicit handoff storage is unchanged.

Direct keyboard input in an attached terminal does not pass through AgentDeck's
send API, and AgentDeck cannot observe a host's context compaction. For per-prompt
coverage there, Grimoire ships a native Claude Code/Codex command hook. Install
it separately with the same explicit project scope; avoid overlapping delivery
paths if duplicate context is unwanted. AgentDeck does not silently modify host
hook configuration or send its Grimoire administrative credential to agents.

This integration is not a guarantee of semantic recall or agent compliance.
The conservative lexical pass can miss paraphrases. Explicit MCP lookup remains
appropriate when context is missing, stale, or insufficient. Operational facts
still require live verification.
