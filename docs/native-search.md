# Native conversation search: implementation in progress

The target-local index, exact-match reader and background HTTP API are implemented
on the development branch. Web search is available from Sessions, the command
palette and attached-terminal tools. The terminal dashboard also supports search through the same API. The deployed app still uses its existing workspace history picker.

`internal/api/scripts/native_records.py` supplies the shared visible-message
parser used by the history reader and search index. Search includes user,
assistant and tool text. It excludes system/developer roles, Claude thinking and
sidechain records, and non-public Codex channels. Image data is not indexed;
readable captions remain searchable. The first Codex native header owns the
conversation identity even when a fork copies its parent's header afterward.

`NativeSearchIndex` stores a private SQLite FTS5 cache on the target. Its caller
supplies the cache root and resolved native configuration directory. Separate
agent/configuration directories get separate hashed cache files. The directory
is mode 0700 and database mode 0600. No full transcript needs to be copied to the
AgentDeck server. The caller must choose an appropriate target cache location;
the development API supplies the target executor and captured environment.

The JSON worker is executable as `python3 native_search.py AGENT -- QUERY`.
It honors CODEX_HOME or CLAUDE_CONFIG_DIR and writes its private cache under
AGENTDECK_NATIVE_SEARCH_CACHE, otherwise XDG_CACHE_HOME/agentdeck or
~/.cache/agentdeck. Each invocation advances a bounded indexing pass and returns
progress plus current matches. Repeat while progress is incomplete; pass --reset
only on the first invocation of an explicit rebuild. The Go service embeds and invokes this worker through its target executor.

Indexing runs in bounded increments, resumes from complete JSONL boundaries,
handles an incomplete trailing write, and rotates progress between conversations.
Warm unchanged files are not reparsed. Source deletion, replacement, truncation,
and observed rewrites invalidate cached matches. Native histories are normally
append-only; checkpoint hashes validate the existing prefix and offset boundary.
An in-place edit to an old middle segment combined with append and unchanged
checkpoints is not comprehensively detected; the index provides a reset operation for a full rebuild. The integration must
expose it without changing source histories.

Records up to 10 MiB are decoded, including captions alongside large image data.
Larger individual entries are counted and skipped without hiding later messages.
Malformed or unreadable headers are reported as issues. Failure checkpoints
prevent one slow bad file from starving other conversations; changed files retry
immediately, and transient failures are eligible for retry after 30 seconds. Results currently provide
one matching message per conversation, with a bounded snippet, role, byte boundaries and a visible-message fingerprint.
Whitespace-separated query terms must occur in the same message. FTS operators
are quoted as literal terms. A result cap tells callers when to narrow the query.

The exact-match reader revalidates the native profile, conversation identity,
file identity, JSONL boundary and visible-message fingerprint before returning
source text. It reads up to five visible messages on either side of the match;
changed neighbors are omitted and counted, and a changed selected message is
rejected. Large selected messages are clipped around the matching text. Returned
context counts describe the included messages, not pagination availability.
The reader uses cache schema v2, leaving prior development caches untouched.
It closes its read transaction on success and failure so subsequent indexing can
proceed. Context pagination and source context beyond the indexed portion remain
integration work.

## Background API (development branch)

- `POST /api/conversation-search`: `{query, target_id?, agent?, reset?}` starts
  an explicit search. Agent is Claude or Codex; omitted filters cover all known
  native profiles. The response includes a job ID, progress and ready results.
- `GET /api/conversation-search/{id}`: poll progress and results. `done` means
  every worker finished; `complete` additionally requires every scope to finish
  indexing successfully. Per-scope issues and oversized-entry counts still apply.
- `DELETE /api/conversation-search/{id}`: cancel outstanding indexing. Ready
  results remain readable; starting another search resumes persisted checkpoints.
- `GET /api/conversation-search/{id}/results/{result}`: revalidate and read the
  exact matching message with context. No client-supplied file path is accepted.

Scopes include captured settings from ended/archived sessions, project overrides,
and current agent defaults on targets with no tracked sessions. Identical declared
settings are deduplicated before execution; canonical native profile identity
also deduplicates results from path aliases. Unsupported/unreachable targets have
individual errors, so available results remain usable. Configuration/environment
values and cache fingerprints stay private. Reads keep the search's captured
profile settings; editing a target connection requires a fresh search.

There are at most four active jobs and four simultaneous target commands across
searching and reading. Jobs have a two-minute deadline; each indexing command has
a fifteen-second timeout and advances the worker's bounded pass. Up to sixteen
jobs are retained for fifteen minutes, with old completed jobs evicted when full.
Ready result IDs remain stable across progress polls. Each scope's scanned-byte
count describes its latest indexing pass, not the entire history size.

## Web search (development branch)

Search saved conversations opens a separate dialog without taking ownership of an
attached terminal. Choose target/agent filters and submit the query explicitly;
ordinary session/action ranking does not start remote indexing. The dialog shows
ready matches during indexing, expandable per-profile progress/issues, stop and
connection-retry controls, plus an explicit rebuild under Search options.
Keyboard arrows move from the query through results; Enter opens the exact match.
Back returns to the selected result. Match context is plain text, with tool
messages collapsible and the selected message marked. Results survive a progress
fetch failure. Closing cancels indexing, including a search whose start response
arrives after the dialog closes. Late poll/reader responses cannot reopen it.
Desktop/mobile dimensions follow the visual viewport when the keyboard opens.

## Terminal dashboard search (development branch)

Press `F` to search saved conversation text, using target/agent choices in the
form. Submit with Ctrl-S. Arrow keys select matches, Enter opens matching context,
`p` shows per-profile progress and issues, `s` stops indexing, `r` retries, and
`R` explicitly rebuilds the index. `n` opens a new query. Esc returns from the
reader/progress view to results, then back to the unchanged dashboard selection.
Mouse and page scrolling stay inside search and progress updates preserve the
reading position. Native text is stripped of terminal control sequences before
rendering. Late replies cannot replace a newer query or reopen a closed view.
Retrying an expired job works in both interfaces; cancellation uses a bounded
request before quitting the dashboard.

## Evidence

Twenty-four index and reader tests cover old text beyond the reader's recent window, long text,
large image captions, oversized records, progressive indexing, append/partial
writes, rewrites/deletion, concurrent writers, profile/path boundaries, private
channels, malformed content, file permissions, explicit rebuilds, future-cache preservation,
exact old matches, stale/replaced files, profile changes and large-message excerpts.
Existing native-history API tests and eleven browser/terminal cases passed after
the parser extraction.

A synthetic local sample of 50 conversations / 5,000 messages (10,312,600 source
bytes) indexed in 40 bounded passes in 0.157 seconds; 100 warm queries averaged
2.316 ms, and an unchanged sync reparsed zero bytes. These are fixture measurements,
not an end-to-end or upstream comparison. SSH transport, discovery time and UI
latency still need measurement when the index is wired into the application.
The embedded search and read workers also passed on the actual SSH target with
a private synthetic profile: Unicode matching, warm-cache reuse, rebuild, exact
source reading and stale-match rejection. The fixture cleaned its own files; no
user history was indexed or edited.
The background API has four integration tests covering archived profiles, project
aliases, configuration changes, untracked workspaces, partial failures, cancellation
and the active-job limit; these passed with the Go race detector. A temporary Go
server also passed an actual API-to-SSH search/read round trip on main-pc with
Unicode text, zero-byte warm indexing and stale-source rejection.
Four browser cases cover actual saved histories outside tracked workspaces,
desktop/phone keyboard navigation and reading, retained terminal identity,
source-change rejection/rebuild, network retry/stop, direct entry points and
closing while the start request is pending. Desktop and phone reader screenshots
were inspected. The first full web run reached100% but exceeded its600-second suite allowance;
verify reported FAIL5/6. The overall allowance is now780seconds, retaining
individual test deadlines, and a full rerun is required.
Five terminal unit tests passed with the race detector, including stale replies,
selection stability, control-sequence removal, resizing, expired-job retry and
mouse/progress scroll preservation. A real PTY test passed through query/filter
entry, old-message reading, rebuild after source change, dashboard return and
terminal mode restoration.

## Remaining integration

- Add broader context pagination/latest and validated native fork actions to
  the global result reader in both interfaces.
- Support native histories outside an existing session's recorded workspace
  through validated provider metadata, rather than accepting arbitrary file paths.
- Test actual local/SSH flows, large histories, failures and desktop/mobile UX;
  run final-head verification before deployment.
