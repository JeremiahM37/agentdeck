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


def base_agent_env() -> dict:
    """Auth env injected into every agent launch. API key wins when configured."""
    if config.ANTHROPIC_API_KEY:
        return {"ANTHROPIC_API_KEY": config.ANTHROPIC_API_KEY}
    return {}


async def provision(ex: Executor, target: dict) -> None:
    """Make sure the target can authenticate for this dispatch. No-op with an API
    key (env injection covers it) or for local/mock (uses the control plane's own
    creds). Otherwise push the control plane's *current* OAuth credentials.
    Best-effort: a push failure is logged, not fatal — the agent may still have a
    working local copy, and the deep probe surfaces genuine auth failures.
    """
    if config.ANTHROPIC_API_KEY:
        return
    if target["kind"] in ("local", "mock"):
        return
    if not os.path.exists(CREDS_PATH):
        log.warning("no control-plane credentials at %s to provision", CREDS_PATH)
        return
    try:
        b64 = base64.b64encode(open(CREDS_PATH, "rb").read()).decode()
        # ~ expands to the target user's home across local/ssh/pct uniformly
        r = await ex.run(
            "mkdir -p ~/.claude && chmod 700 ~/.claude && "
            f"echo {b64} | base64 -d > ~/.claude/.credentials.json && "
            "chmod 600 ~/.claude/.credentials.json", timeout=60)
        if not r.ok:
            log.warning("credential provision to %s failed: %s",
                        target["name"], r.stderr.strip()[:200])
    except ExecutorError as e:
        log.warning("credential provision to %s errored: %s", target["name"], e)
