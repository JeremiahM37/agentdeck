"""Executor registry — one cached executor per target id."""
from .. import config
from .base import ExecResult, Executor, ExecutorError
from .local import LocalExecutor
from .mock import MockExecutor
from .pct import PctExecutor
from .ssh import SSHExecutor

_cache: dict[int, Executor] = {}
# tests set this to inject an ASGI-bound httpx client factory into MockExecutor
mock_http_client_factory = None


def get_executor(target: dict) -> Executor:
    tid = target["id"]
    if tid in _cache:
        return _cache[tid]
    kind = "mock" if config.MOCK else target["kind"]
    if kind == "mock":
        ex: Executor = MockExecutor(http_client_factory=mock_http_client_factory)
    elif kind in ("local", "sandbox"):   # sandbox host-side ops (pct clone/destroy) run locally
        ex = LocalExecutor()
    elif kind == "ssh":
        ex = SSHExecutor(host=target["host"], user=target["user"] or "root",
                         port=target["port"] or 22, key_path=target["key_path"] or "")
    elif kind == "pct":
        ex = PctExecutor(vmid=target["host"])
    else:
        raise ExecutorError(f"unknown target kind {kind!r}")
    _cache[tid] = ex
    return ex


def reset_cache() -> None:
    _cache.clear()


__all__ = ["Executor", "ExecResult", "ExecutorError", "LocalExecutor",
           "SSHExecutor", "MockExecutor", "PctExecutor", "get_executor",
           "reset_cache"]
