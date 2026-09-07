"""Concurrent test runs must own distinct servers and only stop their own process."""
import json
import urllib.request

from conftest import _start, _stop, _unused_port


def test_servers_are_isolated_and_teardown_preserves_peers():
    first_port, second_port = _unused_port(), _unused_port()
    while second_port == first_port:
        second_port = _unused_port()
    first = _start(first_port, {})
    second = _start(second_port, {})
    try:
        # Stopping this run's process must not search a port and kill a peer.
        # The legacy implementation did exactly that in teardown.
        _stop(first, second_port)
        assert first.poll() is not None
        assert second.poll() is None
        with urllib.request.urlopen(f"http://127.0.0.1:{second_port}/api/health", timeout=5) as response:
            assert json.load(response)["ok"]
    finally:
        if first.poll() is None:
            _stop(first, first_port)
        _stop(second, second_port)
