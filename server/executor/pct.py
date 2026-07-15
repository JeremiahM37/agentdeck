"""Proxmox-native executor: `pct exec` into an LXC on this node — no SSH, no
per-container keys. Control plane user needs passwordless sudo for pct.
Target: kind='pct', host=<vmid>.
"""
import base64
import shlex

from .base import ExecResult, Executor
from .local import LocalExecutor


def wrap(vmid: str, cmd: str, cwd: str = "") -> str:
    inner = f"cd {shlex.quote(cwd)} && {cmd}" if cwd else cmd
    return f"sudo pct exec {shlex.quote(str(vmid))} -- bash -c {shlex.quote(inner)}"


class PctExecutor(Executor):
    def __init__(self, vmid: str):
        self.vmid = str(vmid)
        self._local = LocalExecutor()

    async def run(self, cmd: str, cwd: str = "", timeout: float = 120) -> ExecResult:
        return await self._local.run(wrap(self.vmid, cmd, cwd), timeout=timeout)

    async def read_file(self, path: str, offset: int = 0) -> bytes:
        r = await self.run(f"dd if={shlex.quote(path)} bs=1 skip={offset} "
                           f"2>/dev/null || true", timeout=60)
        return r.stdout.encode(errors="replace") if isinstance(r.stdout, str) else r.stdout

    async def write_file(self, path: str, data: bytes) -> None:
        b64 = base64.b64encode(data).decode()
        parent = path.rsplit("/", 1)[0]
        await self.run(f"mkdir -p {shlex.quote(parent)} && "
                       f"echo {b64} | base64 -d > {shlex.quote(path)}", timeout=60)
