"""Task state machine — single source of truth for legal transitions."""

STATUSES = ("backlog", "queued", "running", "review", "done", "failed", "cancelled")

TRANSITIONS: dict[str, set[str]] = {
    "backlog": {"queued", "cancelled"},
    "queued": {"running", "cancelled", "failed", "backlog"},
    "running": {"review", "failed", "cancelled"},
    "review": {"done", "queued", "cancelled"},   # queued = follow-up attempt
    "failed": {"queued", "backlog", "cancelled"},
    "done": set(),
    "cancelled": {"backlog"},
}


class IllegalTransition(Exception):
    pass


def check(old: str, new: str) -> None:
    if new not in STATUSES:
        raise IllegalTransition(f"unknown status {new!r}")
    if new == old:
        return
    if new not in TRANSITIONS.get(old, set()):
        raise IllegalTransition(f"{old} → {new} not allowed")
