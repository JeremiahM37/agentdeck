"""In-process SSE pub/sub. Channels: 'board' (global) and 'task:{id}'."""
import asyncio
import itertools
import json


class Bus:
    def __init__(self) -> None:
        self._subs: dict[str, set[asyncio.Queue]] = {}
        self._seq = itertools.count(1)

    def subscribe(self, channel: str) -> asyncio.Queue:
        q: asyncio.Queue = asyncio.Queue(maxsize=500)
        self._subs.setdefault(channel, set()).add(q)
        return q

    def unsubscribe(self, channel: str, q: asyncio.Queue) -> None:
        self._subs.get(channel, set()).discard(q)

    def publish(self, channel: str, event_type: str, data: dict) -> None:
        payload = {"id": next(self._seq), "event": event_type, "data": data}
        for q in list(self._subs.get(channel, ())):
            try:
                q.put_nowait(payload)
            except asyncio.QueueFull:
                pass  # slow client: drop; UI refetches on reconnect

    @staticmethod
    def sse_format(payload: dict) -> str:
        return (
            f"id: {payload['id']}\n"
            f"event: {payload['event']}\n"
            f"data: {json.dumps(payload['data'], ensure_ascii=False)}\n\n"
        )


bus = Bus()
