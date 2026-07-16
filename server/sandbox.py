"""Ephemeral LXC sandbox lifecycle: clone template → start → run → destroy.

Target kind 'sandbox': `host` is the Proxmox TEMPLATE vmid. Every attempt gets
its own linked clone; the container IS the isolation, so no git worktree is
needed and `bypassPermissions` is safe. Host-side pct commands run through the
target's (local) executor so mock mode can script the whole lifecycle.
"""
import logging
import os

from .executor.base import Executor, ExecutorError

log = logging.getLogger("agentdeck.sandbox")

CREDS_PATH = os.environ.get("AGENTDECK_CREDS",
                            os.path.expanduser("~/.claude/.credentials.json"))


async def provision(host: Executor, template_vmid: str, attempt_id: int) -> str:
    """Clone + start + wait ready + push fresh claude creds. Returns new vmid."""
    r = await host.run("sudo pvesh get /cluster/nextid", timeout=30)
    if not r.ok or not r.stdout.strip():
        raise ExecutorError(f"could not allocate vmid: {r.stderr.strip()}")
    vmid = r.stdout.strip()
    r = await host.run(f"sudo pct clone {template_vmid} {vmid} "
                       f"--hostname adk-sb-{attempt_id}", timeout=300)
    if not r.ok:
        raise ExecutorError(f"pct clone failed: {r.stderr.strip()}")
    r = await host.run(f"sudo pct start {vmid}", timeout=120)
    if not r.ok:
        await destroy(host, vmid)
        raise ExecutorError(f"pct start failed: {r.stderr.strip()}")
    import asyncio
    for _ in range(30):
        if (await host.run(f"sudo pct exec {vmid} -- true", timeout=20)).ok:
            break
        await asyncio.sleep(2)
    else:
        await destroy(host, vmid)
        raise ExecutorError(f"sandbox {vmid} never became ready")
    # NB: auth is provisioned by the scheduler right before launch (via the
    # attempt's own executor), so it shares the one credentials code path and
    # works under the mock executor in tests.
    log.info("sandbox %s provisioned from template %s", vmid, template_vmid)
    return vmid


async def destroy(host: Executor, vmid: str) -> None:
    await host.run(f"sudo pct stop {vmid} 2>/dev/null || true", timeout=120)
    r = await host.run(f"sudo pct destroy {vmid}", timeout=120)
    if r.ok:
        log.info("sandbox %s destroyed", vmid)
    else:
        log.warning("sandbox %s destroy failed: %s", vmid, r.stderr.strip())


def is_repo_url(repo_path: str) -> bool:
    return repo_path.startswith(("http://", "https://", "git@", "ssh://"))
