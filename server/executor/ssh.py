"""SSH target — key auth only, one reused connection with keepalive."""
import asyncio

import asyncssh

from .base import ExecResult, Executor, ExecutorError


class SSHExecutor(Executor):
    def __init__(self, host: str, user: str = "root", port: int = 22, key_path: str = ""):
        self.host, self.user, self.port = host, user, port
        self.key_path = key_path
        self._conn: asyncssh.SSHClientConnection | None = None
        self._lock = asyncio.Lock()

    async def _connect(self) -> asyncssh.SSHClientConnection:
        async with self._lock:
            if self._conn is not None:
                try:
                    if not self._conn.is_closed():
                        return self._conn
                except Exception:
                    pass
            opts: dict = dict(
                host=self.host, port=self.port, username=self.user,
                known_hosts=None, keepalive_interval=15, connect_timeout=10,
            )
            if self.key_path:
                opts["client_keys"] = [self.key_path]
            try:
                self._conn = await asyncssh.connect(**opts)
            except (OSError, asyncssh.Error) as e:
                raise ExecutorError(f"ssh connect {self.user}@{self.host}:{self.port}: {e}") from e
            return self._conn

    async def run(self, cmd: str, cwd: str = "", timeout: float = 120) -> ExecResult:
        conn = await self._connect()
        full = f"cd {_q(cwd)} && {cmd}" if cwd else cmd
        try:
            r = await asyncio.wait_for(conn.run(full, check=False), timeout=timeout)
        except asyncio.TimeoutError:
            return ExecResult(124, "", f"timeout after {timeout}s: {cmd}")
        except (OSError, asyncssh.Error) as e:
            self._conn = None
            raise ExecutorError(f"ssh run failed: {e}") from e
        return ExecResult(r.exit_status or 0, r.stdout or "", r.stderr or "")

    async def read_file(self, path: str, offset: int = 0) -> bytes:
        # dd keeps this dependency-free on the target (no sftp subsystem assumptions)
        r = await self.run(f"dd if={_q(path)} bs=1 skip={offset} 2>/dev/null || true", timeout=60)
        return r.stdout.encode(errors="replace") if isinstance(r.stdout, str) else r.stdout

    async def write_file(self, path: str, data: bytes) -> None:
        conn = await self._connect()
        parent = path.rsplit("/", 1)[0]
        await conn.run(f"mkdir -p {_q(parent)}", check=False)
        async with conn.start_sftp_client() as sftp:
            async with sftp.open(path, "wb") as f:
                await f.write(data)

    async def close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None


def _q(s: str) -> str:
    return "'" + s.replace("'", "'\\''") + "'"
