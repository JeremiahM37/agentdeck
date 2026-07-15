# Quickstart

## 1. Run the control plane

```bash
python3 -m venv .venv && .venv/bin/pip install -r requirements.txt
.venv/bin/python -m server        # http://<host>:9110
```

Try it with fake agents first (no infrastructure needed):

```bash
AGENTDECK_MOCK=1 .venv/bin/python -m server
```

## 2. Register a target

A target is any machine that runs agents. It needs `git`, `tmux`, `python3`, and
a coding-agent CLI (`claude`), reachable one of these ways:

| kind | reaches | `host` field |
|------|---------|--------------|
| `local` | the control-plane host | — |
| `ssh` | anything with sshd (key auth) | hostname/IP |
| `pct` | a Proxmox LXC on this node (no SSH) | container vmid |
| `sandbox` | a **fresh ephemeral LXC per task** | template vmid |

```bash
curl -X POST http://localhost:9110/api/targets \
  -d '{"name":"box1","kind":"ssh","host":"10.0.0.5","user":"dev","key_path":"~/.ssh/id_ed25519"}'
curl -X POST http://localhost:9110/api/targets/1/check     # probe toolchain + auth
```

## 3. Register a project (a git repo on a target)

```bash
curl -X POST http://localhost:9110/api/projects -d '{
  "name":"myrepo","target_id":1,"repo_path":"/home/dev/myrepo",
  "verify_cmd":"pytest -q"}'          # auto-run after each attempt, badges the card
```

## 4. Dispatch — or just open the board

Open `http://<host>:9110` on your phone, type a task in the quick bar, hit enter.
Or via API:

```bash
curl -X POST http://localhost:9110/api/tasks -d '{
  "project_id":1,"title":"Add /health","prompt":"Add a health endpoint returning {ok:true}",
  "permission_mode":"acceptEdits"}' | jq .id
curl -X POST http://localhost:9110/api/tasks/1/dispatch -d '{}'
```

The agent works in an isolated git worktree; you watch the live timeline, review
the diff on your phone, and mark it done — or drag the card to the done column.

## Approvals from your phone

Use `"permission_mode":"default"` and the agent's risky tool calls block on a
push notification. Configure a sink in the Targets tab: web-push, a Discord
webhook, or ntfy (whose notifications carry ✅/⛔ buttons — decide from the
lock screen).

## Local / alternative models

Point a project's `env` at any Anthropic-compatible endpoint:

```json
{"env": {"ANTHROPIC_BASE_URL": "http://ollama:11434",
         "ANTHROPIC_AUTH_TOKEN": "ollama"}}
```

Then dispatch with `"model": "<any served model>"`. (Agentic quality tracks the
model — small local models may respond conversationally instead of editing.)

## MCP

`server/mcp_server.py` exposes the board over MCP so any MCP client can file and
steer tasks. Register with Claude Code:

```bash
claude mcp add agentdeck /path/.venv/bin/python /path/server/mcp_server.py
```

## Deploy for real

`deploy/` has a `Dockerfile`, `docker-compose.yml`, and a `systemd` unit. Put it
behind a reverse proxy with auth; set `AGENTDECK_BASE_URL` to an address your
phone can reach (approval callbacks and ntfy buttons use it).
