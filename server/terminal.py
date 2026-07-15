"""One-click terminal attach: spawn a ttyd on the control plane that wraps
`tmux attach` (locally or over ssh) for a running attempt. Ports are ephemeral,
--once makes ttyd exit when the client disconnects.
"""
import asyncio
import shutil
import socket

PORT_LO, PORT_HI = 7710, 7730


class TerminalError(Exception):
    pass


class TerminalManager:
    def __init__(self) -> None:
        self._procs: dict[int, tuple[int, asyncio.subprocess.Process]] = {}

    def _reap(self) -> None:
        for aid, (_, proc) in list(self._procs.items()):
            if proc.returncode is not None:
                del self._procs[aid]

    def _free_port(self) -> int:
        used = {p for p, _ in self._procs.values()}
        for port in range(PORT_LO, PORT_HI + 1):
            if port in used:
                continue
            with socket.socket() as s:
                if s.connect_ex(("127.0.0.1", port)) != 0:
                    return port
        raise TerminalError("no free terminal ports")

    async def spawn(self, attempt: dict, target: dict) -> int:
        self._reap()
        if attempt["id"] in self._procs:
            return self._procs[attempt["id"]][0]
        if not shutil.which("ttyd"):
            raise TerminalError("ttyd is not installed on the control plane")
        sess = attempt["tmux_session"]
        if target["kind"] == "sandbox" and attempt.get("sandbox_vmid"):
            inner = ["sudo", "pct", "exec", str(attempt["sandbox_vmid"]), "--",
                     "tmux", "attach", "-t", sess]
        elif target["kind"] == "pct":
            inner = ["sudo", "pct", "exec", str(target["host"]), "--",
                     "tmux", "attach", "-t", sess]
        elif target["kind"] == "ssh":
            inner = ["ssh", "-tt", "-o", "StrictHostKeyChecking=accept-new"]
            if target["key_path"]:
                inner += ["-i", target["key_path"]]
            inner += [f"{target['user'] or 'root'}@{target['host']}",
                      "tmux", "attach", "-t", sess]
        else:
            inner = ["tmux", "attach", "-t", sess]
        port = self._free_port()
        proc = await asyncio.create_subprocess_exec(
            "ttyd", "-p", str(port), "-W", "--once", *inner,
            stdout=asyncio.subprocess.DEVNULL, stderr=asyncio.subprocess.DEVNULL)
        await asyncio.sleep(0.3)
        if proc.returncode is not None:
            raise TerminalError("ttyd exited immediately")
        self._procs[attempt["id"]] = (port, proc)
        return port

    async def shutdown(self) -> None:
        for _, proc in self._procs.values():
            if proc.returncode is None:
                proc.terminate()
        self._procs.clear()


terminals = TerminalManager()
