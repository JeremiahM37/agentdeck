# Native conversation search: implementation in progress

The target-local index is implemented and tested. It is not yet connected to an
HTTP endpoint, the command palette, or the terminal dashboard. The deployed app
still uses its existing workspace history picker.

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
the module currently has no production caller.

The JSON worker is executable as `python3 native_search.py AGENT -- QUERY`.
It honors CODEX_HOME or CLAUDE_CONFIG_DIR and writes its private cache under
AGENTDECK_NATIVE_SEARCH_CACHE, otherwise XDG_CACHE_HOME/agentdeck or
~/.cache/agentdeck. Each invocation advances a bounded indexing pass and returns
progress plus current matches. Repeat while progress is incomplete; pass --reset
only on the first invocation of an explicit rebuild. The Go service still needs
to embed and invoke this worker through its target executor.

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

## Remaining integration

- Run the index through the target executor using the session's captured agent
  configuration; deduplicate target/profile scopes across tracked sessions.
- Expose progress, partial-source issues and cancellation without blocking the
  ordinary session/action search. Bound concurrency and handle unreachable targets.
- Add result browsing to web and terminal; show target, workspace and agent, and
  open the exact matching message with surrounding context.
- Support native histories outside an existing session's recorded workspace
  through validated provider metadata, rather than accepting arbitrary file paths.
- Expose the full-rebuild/cache-reset operation for unusual rewrites.
- Test actual local/SSH flows, large histories, failures and desktop/mobile UX;
  run final-head verification before deployment.
