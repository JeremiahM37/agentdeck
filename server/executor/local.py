"""Run on the control-plane host itself."""
import asyncio
from pathlib import Path

from .base import ExecResult, Executor


class LocalExecutor(Executor):
    async def run(self, cmd: str, cwd: str = "", timeout: float = 120) -> ExecResult:
        proc = await asyncio.create_subprocess_shell(
            cmd,
            cwd=cwd or None,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        try:
            out, err = await asyncio.wait_for(proc.communicate(), timeout=timeout)
        except TimeoutError:
            proc.kill()
            return ExecResult(124, "", f"timeout after {timeout}s: {cmd}")
        return ExecResult(proc.returncode, out.decode(errors="replace"), err.decode(errors="replace"))

    async def read_file(self, path: str, offset: int = 0) -> bytes:
        p = Path(path)
        if not p.exists():
            return b""
        with p.open("rb") as f:
            f.seek(offset)
            return f.read()

    async def write_file(self, path: str, data: bytes) -> None:
        p = Path(path)
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(data)
