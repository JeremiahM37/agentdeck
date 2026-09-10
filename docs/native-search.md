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

Indexing runs in bounded increments, resumes from complete JSONL boundaries,
handles an incomplete trailing write, and rotates progress between conversations.
Warm unchanged files are not reparsed. Source deletion, replacement, truncation,
and observed rewrites invalidate cached matches. Native histories are normally
append-only; checkpoint hashes validate the existing prefix and offset boundary.
An in-place edit to an old middle segment combined with append and unchanged
checkpoints is not comprehensively detected; a full-rebuild operation is still
needed before presenting the cache as a complete product workflow.

Records up to 10 MiB are decoded, including captions alongside large image data.
Larger individual entries are counted and skipped without hiding later messages.
Malformed or unreadable headers are reported as issues. Results currently provide
one matching message per conversation, with a snippet, role and byte boundaries.
Whitespace-separated query terms must occur in the same message. FTS operators
are quoted as literal terms. A result cap tells callers when to narrow the query.

## Evidence

Fourteen index tests cover old text beyond the reader's recent window, long text,
large image captions, oversized records, progressive indexing, append/partial
writes, rewrites/deletion, concurrent writers, profile/path boundaries, private
channels, malformed content, file permissions and future-cache preservation.
Existing native-history API tests and eleven browser/terminal cases passed after
the parser extraction.

A synthetic local sample of 50 conversations / 5,000 messages (10,312,600 source
bytes) indexed in 40 bounded passes in 0.157 seconds; 100 warm queries averaged
2.316 ms, and an unchanged sync reparsed zero bytes. These are fixture measurements,
not an end-to-end or upstream comparison. SSH transport, discovery time and UI
latency still need measurement when the index is wired into the application.

## Remaining integration

- Run the index through the target executor using the session's captured agent
  configuration; deduplicate target/profile scopes across tracked sessions.
- Expose progress, partial-source issues and cancellation without blocking the
  ordinary session/action search. Bound concurrency and handle unreachable targets.
- Add result browsing to web and terminal; show target, workspace and agent, and
  open the exact matching message with surrounding context.
- Support native histories outside an existing session's recorded workspace
  through validated provider metadata, rather than accepting arbitrary file paths.
- Add full rebuild/cache reset, and define invalidation for unusual rewrites.
- Test actual local/SSH flows, large histories, failures and desktop/mobile UX;
  run final-head verification before deployment.
