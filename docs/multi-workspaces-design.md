# Multi-repository workspace implementation plan

This is an internal staging design. Multi-repository execution and UI controls
are not available yet. The planner and cleanup preflight are implemented and
unit/integration tested. `check-create` resolves and validates the grouped plan
on its target without changing Git or creating directories. `RunInteractive`
still rejects grouped creation/removal until their worker exists, rather than
accidentally creating only the primary repository.

A workspace owns one root directory containing separately owned repository
worktrees. `Interactive.Repositories` records each display name, optional project
ID, and child allocation. Each child has its own source repository, base commit,
path, token and state. One branch name spans the repositories; bases may differ.
The planner produces bounded, distinct child directory names and never mutates
Git. Ownership is recursively redacted from public session responses.

The remaining implementation must provide:

1. Resolve selected registered projects on the same target. The target-side
   preflight now canonicalizes Git common directories (including symlink and
   linked-worktree aliases), rejects duplicates, resolves each base commit and
   checks branches and allocation boundaries without mutations. Creation must
   repeat these checks under its operation lock: preflight is not a reservation
   of paths or refs. The API project selection/resolution is still outstanding.
2. Persist the entire plan before mutation. Create an exclusively owned root,
   then allocate children while retaining progress and partial failures. Protect
   against cancellation leaving a Git or hook process writing into an allocation
   that cleanup considers idle. An operation lock or equivalent durable process
   evidence must survive the supervising command's failure.
3. Preflight removal across every repository using `check-remove` before removing
   any of them. Refuse unexpected root files and changed/untracked/ignored files,
   active terminals, ownership mismatches or ongoing operations. Record partial
   removal errors and retain all branches. No forced cleanup.
4. Start the agent in the shared root; keep native fork/resume directory behavior
   and captured launch profiles intact. Keep single-repository compatibility.
5. Expose repository choices and per-repository bases in web and TUI creation,
   plus repository selection in diff review. File browsing must reach the shared
   workspace without reaching neighboring allocations.
6. Support adding a repository to an existing workspace with the same durable
   allocation and failure handling. Define the transition for older sessions
   whose working directory is a single worktree rather than a workspace root.
7. Verify real local/SSH Git, partial failure, cancellation, dirty cleanup,
   preserved source/index/branches, native continuation, desktop/phone and PTY
   workflows before rollout.

The existing `check-remove` action validates ownership, branch, active terminals
and dirty files without removing the worktree. Git status runs with optional
locks disabled so preflight does not refresh the index; the test compares index
bytes and confirms registration and allocation state remain unchanged.
