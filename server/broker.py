"""Approval broker — hook calls block here until a human (or expiry) decides."""
import asyncio

from . import db, sinks
from .bus import bus

_waiters: dict[int, asyncio.Event] = {}


def reset() -> None:
    """Drop loop-bound waiter events (app restart / tests)."""
    _waiters.clear()


def create(attempt_id: int, tool_name: str, tool_input: dict, quiet: bool = False) -> int:
    aid = db.insert("approvals", {
        "attempt_id": attempt_id, "tool_name": tool_name,
        "input_json": db.j(tool_input), "status": "pending", "created_at": db.now()})
    _waiters[aid] = asyncio.Event()
    if quiet:   # policy auto-approval pending — audit row only, don't page the human
        return aid
    row = db.one("SELECT a.*, at.task_id FROM approvals a "
                 "JOIN attempts at ON at.id=a.attempt_id WHERE a.id=?", (aid,))
    bus.publish("board", "approval", row)
    bus.publish(f"task:{row['task_id']}", "approval", row)
    summary = tool_input.get("command") or tool_input.get("file_path") or ""
    sinks.notify("Approval needed",
                 f"{tool_name}: {str(summary)[:120]}", url="/#approvals",
                 extra={"kind": "approval", "approval_id": aid})
    return aid


def decide(approval_id: int, decision: str, note: str = "", decided_by: str = "user") -> dict | None:
    row = db.one("SELECT * FROM approvals WHERE id=?", (approval_id,))
    if not row or row["status"] != "pending":
        return None
    db.update("approvals", approval_id, {
        "status": decision, "note": note, "decided_by": decided_by,
        "decided_at": db.now()})
    ev = _waiters.get(approval_id)
    if ev:
        ev.set()
    row = db.one("SELECT a.*, at.task_id FROM approvals a "
                 "JOIN attempts at ON at.id=a.attempt_id WHERE a.id=?", (approval_id,))
    bus.publish("board", "approval", row)
    bus.publish(f"task:{row['task_id']}", "approval", row)
    return row


def expire_for_attempt(attempt_id: int) -> int:
    """Resolve any still-pending approvals for an attempt that has ended
    (cancelled/finalized). Without this they hang 'pending' forever — the board
    badge sticks and the blocked hook never gets a decision."""
    n = 0
    for row in db.query("SELECT id FROM approvals WHERE attempt_id=? AND "
                        "status='pending'", (attempt_id,)):
        if decide(row["id"], "expired", note="attempt ended before decision",
                  decided_by="system"):
            n += 1
    return n


async def wait(approval_id: int, timeout: float) -> dict:
    """Long-poll helper: returns the row after decision, expiry, or poll timeout."""
    row = db.one("SELECT * FROM approvals WHERE id=?", (approval_id,))
    if not row:
        return {"status": "unknown"}
    if row["status"] == "pending":
        from . import config
        if db.now() - row["created_at"] > config.APPROVAL_EXPIRE_SECONDS:
            decide(approval_id, "expired", decided_by="system")
        else:
            ev = _waiters.setdefault(approval_id, asyncio.Event())
            try:
                await asyncio.wait_for(ev.wait(), timeout=timeout)
            except asyncio.TimeoutError:
                pass
        row = db.one("SELECT * FROM approvals WHERE id=?", (approval_id,))
    return row
