# Multi-repository workspace implementation plan

This is an internal staging design. Browser multi-repository selection is implemented; terminal creation selection is still pending. Session creation accepts
`worktree.extra_repositories` entries with `project_id` and optional `base`.
A primary project is required; all projects must use the same target, and a
working-directory override is rejected for grouped creation. The planner, target validation and grouped creation/removal
worker exist, with real Git tests for successful/partial creation, all-repository
cleanup preflight and supervisor death during checkout. This branch is not deployed.

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

## Grouped worker evidence and limits

The root records its full owned plan before the first child allocation, then
fsyncs progress after each child. Each child starts behind a pipe gate: its process
group is recorded before it may execute Git. An inherited operation lock and
process-group receipt keep cleanup from racing an orphaned checkout. Linux zombie
processes do not count as writers. A test kills the supervisor inside a waiting
checkout hook, verifies refusal, releases the hook, and recovers the allocation.
Hooks that intentionally daemonize outside their process group need further policy
before reusable setup hooks are exposed; this is not universal descendant tracking.

Removal checks active terminals at the shared root as well as child worktrees,
checks every repository before removing any, rechecks each at removal, and retains
branches. It leaves the owned root and durable receipt after removing children;
it never recursively deletes the root. Additional root files block cleanup. A
later repository failure preserves earlier worktrees and the failed tree's files.

Remaining checks before release include target-side group execution over SSH,
metadata replacement/corruption handling, concurrent launch/removal attempts,
partial removal and full API/native-continuation/browser/PTY integration. The
120-second caller deadline also needs a deliberate asynchronous lifecycle for
slow multi-repository setup; cancellation safety alone is not a usable progress UI.

## Session API integration

The manager resolves project selections before inserting a session, validates the
plan on its target, persists it, and then runs grouped creation. Failed creation
retains its session and child receipts; failed cleanup also saves partial progress.
The primary project remains the source of agent/environment configuration.
Additional repositories do not silently merge their launch settings.

`GET /api/term/session/{id}/changes?repository=N` selects a recorded repository by
zero-based index. Grouped responses include `repositories` and
`selected_repository`; an omitted selection defaults to the primary repository.
The server resolves paths from the saved allocation, never from a client path.
Browser diff review now offers a repository dropdown; terminal review cycles
repositories with Tab and labels the selected repository. Creation selectors
still need wiring before rollout.

A real API/Git/tmux lifecycle test creates both repositories, reviews a change in
the second without mixing the first, rejects out-of-range selections, protects an
active session, removes ended worktrees, rejects duplicate/mixed-target projects
without inserting sessions, and recovers a failed second checkout after its
untracked artifact is explicitly removed. Public child ownership tokens are
redacted. This is staging evidence, not deployed multi-repository functionality.


Repository review UI proof: `e2e/test_multi_workspace.py` launches a real grouped
session through the API, edits both worktrees and switches between their distinct
diffs in desktop1440/phone390 browsers and a real terminal PTY. Browser checks
also preserve the live terminal connection and reject horizontal overflow/JS
errors. Terminal unit coverage rejects a stale previous-repository response.
The first browser run exposed an unstable implicit accessible label containing
option text; the select now has an explicit Repository label. All three final
end-to-end cases pass. SWv47 is staged; deployed remainsv46.


Browser creation now offers a collapsed Additional repositories section under
worktree options. Only other registered projects on the primary target are
eligible; add/remove and per-repository base fields retain draft values, cap seven
extras, and clear incompatible selections when the primary project changes.
The primary project's base and shared branch controls remain unchanged.
`test_browser_creates_grouped_workspace` verifies390/1440 real creation with a
base tag, exclusion of another target, no horizontal overflow, and draft retention
after a503 followed by successful retry. Both cases pass. SWv48 is staged.
