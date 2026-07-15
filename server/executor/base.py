"""Executor protocol — everything above this layer is target-kind agnostic."""
from dataclasses import dataclass


@dataclass
class ExecResult:
    rc: int
    stdout: str
    stderr: str

    @property
    def ok(self) -> bool:
        return self.rc == 0


class ExecutorError(Exception):
    pass


class Executor:
    """One instance per target. Implementations: local, ssh, mock."""

    async def run(self, cmd: str, cwd: str = "", timeout: float = 120) -> ExecResult:
        raise NotImplementedError

    async def read_file(self, path: str, offset: int = 0) -> bytes:
        """Bytes from offset to EOF; b'' if missing/short. Never raises on absence."""
        raise NotImplementedError

    async def write_file(self, path: str, data: bytes) -> None:
        raise NotImplementedError

    async def check(self) -> dict:
        """Capability probe: versions + disk. Raises ExecutorError if unreachable."""
        info = {}
        for key, cmd in {
            "git": "git --version",
            "tmux": "tmux -V",
            "claude": "claude --version",
            "codex": "codex --version",
            "gemini": "gemini --version",
            "python3": "python3 --version",
            "disk_free": "df -h --output=avail / | tail -1",
        }.items():
            r = await self.run(cmd, timeout=20)
            info[key] = r.stdout.strip() if r.ok else None
        return info

    async def close(self) -> None:
        pass
