import pytest

from server import state


@pytest.mark.parametrize("old,new", [
    ("backlog", "queued"), ("queued", "running"), ("running", "review"),
    ("review", "done"), ("review", "queued"), ("running", "failed"),
    ("failed", "queued"), ("queued", "cancelled"), ("cancelled", "backlog"),
])
def test_legal(old, new):
    state.check(old, new)


@pytest.mark.parametrize("old,new", [
    ("backlog", "running"), ("backlog", "review"), ("done", "queued"),
    ("done", "backlog"), ("running", "done"), ("queued", "review"),
    ("review", "running"), ("cancelled", "running"),
])
def test_illegal(old, new):
    with pytest.raises(state.IllegalTransition):
        state.check(old, new)


def test_same_status_is_noop():
    state.check("running", "running")


def test_unknown_status_rejected():
    with pytest.raises(state.IllegalTransition):
        state.check("backlog", "warp")
