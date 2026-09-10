# Named launch profiles

A launch profile names reusable settings for future interactive sessions: an
agent, optional command and model overrides, and an environment object. It does
not partition the session database or change a running process. Profile paths
are interpreted on the session's selected target.

In the web UI, choose **Manage launch profiles** from New session or command
search. In the terminal dashboard press **P** (also in All actions). Create, edit
and delete profiles there, then choose one in New session. The selected profile
supplies the agent; an explicit model field overrides its default model. Failed
saves and launches retain the form draft.

The API supports:

- `GET /api/launch-profiles` to list definitions.
- `POST /api/launch-profiles` to create one.
- `PUT /api/launch-profiles/{id}` to replace its settings.
- `DELETE /api/launch-profiles/{id}` to remove the reusable definition.
- `POST /api/sessions` with `profile_id` to launch with that definition.

A definition contains `name`, `agent`, `command`, `model`, and `env_json` (a JSON
object encoded as a string, consistent with project environment settings).
Names are unique without regard to ASCII case. Deleted IDs are never reused,
so a stale selector cannot silently launch a replacement definition.

Environment precedence is agent settings, project settings, selected profile,
then any explicit internal launch overrides. A supplied model overrides the
profile model. Supplying a conflicting agent fails. Omitting a profile preserves
existing launch behavior.

The session captures its resolved launch settings and profile name. Later profile
edits or deletion affect new launches; native forks and resumes retain the
captured settings. Public session responses include only `launch_profile`, the
captured label, rather than profile environment values. The profile configuration
API itself returns its environment, just like the existing agent and project
configuration APIs, and sends `Cache-Control: no-store`.

Global conversation search includes configured Claude/Codex profile histories
before a first tracked session exists, and offers the named settings for forks.
It does not create placeholder tracking records.

Backend evidence includes database migration/reopen, preserved unrelated
settings, deleted-ID protection, profile/default precedence, explicit model and
agent boundaries, captured settings after edit/delete, private session output,
and real local history search from a profile with no tracked session. Real tmux browser tests at 390/1440 pixels and a real PTY test cover management,
selection, failed-save drafts, pending-save Escape handling, actual command/
model/environment execution and continuation after profile deletion. A nested
dialog Escape regression is covered by the browser flow. Full-suite verification
and deployment are still required.
