"""Agent authentication provisioning.

Two supported auth models, in precedence order:

1. **API key** (`AGENTDECK_ANTHROPIC_API_KEY`) — rotation-proof. Injected as
   `ANTHROPIC_API_KEY` into every agent launch; nothing is pushed to targets.

2. **OAuth / subscription** (the default) — the control plane holds
   `~/.claude/.credentials.json`, which its own Claude Code keeps refreshed. The
   subtle failure this module fixes: when the source refreshes, the OAuth
   *refresh token* rotates, so any copy previously pushed to a target becomes
   invalid → the target 401s ("Invalid authentication credentials"). The fix is
   to push the CURRENT credentials to the target at dispatch time, so an agent
   never runs on a stale, rotated-out copy. The target's own Claude Code then
   refreshes the short-lived access token from the (current) refresh token.
"""
import base64
import logging
import os

from . import config
from .executor.base import Executor, ExecutorError

log = logging.getLogger("agentdeck.credentials")

CREDS_PATH = os.environ.get("AGENTDECK_CREDS",
                            os.path.expanduser("~/.claude/.credentials.json"))
CODEX_CREDS_PATH = os.environ.get("AGENTDECK_CODEX_CREDS",
                                  os.path.expanduser("~/.codex/auth.json"))

# per-agent: where the control plane keeps auth, and where the target expects it
AGENT_CREDS = {
    "claude": (lambda: CREDS_PATH, "~/.claude", "~/.claude/.credentials.json"),
    "codex": (lambda: CODEX_CREDS_PATH, "~/.codex", "~/.codex/auth.json"),
}


def base_agent_env() -> dict:
    """Auth env injected into every agent launch. API key wins when configured."""
    if config.ANTHROPIC_API_KEY:
        return {"ANTHROPIC_API_KEY": config.ANTHROPIC_API_KEY}
    return {}


async def provision(ex: Executor, target: dict, agent: str = "claude") -> None:
    """Make sure the target can authenticate for this dispatch. No-op with an API
    key (env injection covers it, claude only) or for local/mock (uses the control
    plane's own creds). Otherwise push the control plane's *current* OAuth
    credentials for the agent being dispatched — codex tokens rotate exactly like
    claude's, so a remote codex run needs the same treatment.
    Best-effort: a push failure is logged, not fatal — the agent may still have a
    working local copy, and the deep probe surfaces genuine auth failures.
    """
    if agent == "claude" and config.ANTHROPIC_API_KEY:
        return
    if target["kind"] in ("local", "mock"):
        return
    if agent not in AGENT_CREDS:
        return                      # gemini: no known credential file to push
    path_fn, home_dir, dest = AGENT_CREDS[agent]
    path = path_fn()
    if not os.path.exists(path):
        log.warning("no control-plane %s credentials at %s to provision", agent, path)
        return
    try:
        b64 = base64.b64encode(open(path, "rb").read()).decode()
        # ~ expands to the target user's home across local/ssh/pct uniformly
        r = await ex.run(
            f"mkdir -p {home_dir} && chmod 700 {home_dir} && "
            f"echo {b64} | base64 -d > {dest} && chmod 600 {dest}", timeout=60)
        if not r.ok:
            log.warning("%s credential provision to %s failed: %s",
                        agent, target["name"], r.stderr.strip()[:200])
    except ExecutorError as e:
        log.warning("%s credential provision to %s errored: %s",
                    agent, target["name"], e)
