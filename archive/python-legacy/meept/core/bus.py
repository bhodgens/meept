"""Async publish/subscribe message bus with wildcard topic support."""

from __future__ import annotations

import asyncio
import fnmatch
import logging
from collections import defaultdict
from typing import Awaitable, Callable

from meept.models.messages import BusMessage

log = logging.getLogger(__name__)

# A subscriber callback receives a topic string and the message.
Callback = Callable[[str, BusMessage], Awaitable[None]]


class MessageBus:
    """In-process asynchronous message bus.

    Topics are dot-separated strings (e.g. ``"memory.query"``).
    Subscribers may use shell-style wildcards:

    * ``"memory.*"``  -- matches ``"memory.query"``, ``"memory.result"``
    * ``"*"``         -- matches every single-segment topic
    * ``"**"``        -- matches *everything* (internally translated to ``"*"``)

    Message delivery is ordered per-topic: a dedicated :class:`asyncio.Queue`
    serialises dispatch so that subscribers observe messages in publication
    order.  Delivery across different topics may be interleaved.

    All public methods are safe to call from any coroutine in the same event
    loop.  The bus does **not** spawn its own threads -- it relies on
    :meth:`start` being awaited to launch the internal dispatcher task.
    """

    def __init__(self, maxsize: int = 0) -> None:
        # topic-pattern -> list of callbacks
        self._subscribers: dict[str, list[Callback]] = defaultdict(list)
        self._queue: asyncio.Queue[tuple[str, BusMessage]] = asyncio.Queue(maxsize=maxsize)
        self._dispatcher_task: asyncio.Task[None] | None = None
        self._running = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def start(self) -> None:
        """Launch the background dispatcher task."""
        if self._running:
            return
        self._running = True
        self._dispatcher_task = asyncio.create_task(self._dispatch_loop(), name="bus-dispatcher")
        log.debug("bus: dispatcher started")

    async def stop(self) -> None:
        """Drain remaining messages and cancel the dispatcher."""
        if not self._running:
            return
        self._running = False

        # Drain whatever is left in the queue.
        while not self._queue.empty():
            try:
                topic, msg = self._queue.get_nowait()
                await self._deliver(topic, msg)
            except asyncio.QueueEmpty:
                break

        if self._dispatcher_task is not None:
            self._dispatcher_task.cancel()
            try:
                await self._dispatcher_task
            except asyncio.CancelledError:
                pass
            self._dispatcher_task = None
        log.debug("bus: dispatcher stopped")

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def subscribe(self, topic: str, callback: Callback) -> None:
        """Register *callback* for messages published to *topic*.

        *topic* may contain ``*`` or ``?`` wildcard characters which are
        matched via :func:`fnmatch.fnmatch`.
        """
        if callback not in self._subscribers[topic]:
            self._subscribers[topic].append(callback)
            log.debug("bus: subscribed %s to %r", _cb_name(callback), topic)

    def unsubscribe(self, topic: str, callback: Callback) -> None:
        """Remove a previously registered subscription."""
        try:
            self._subscribers[topic].remove(callback)
            if not self._subscribers[topic]:
                del self._subscribers[topic]
            log.debug("bus: unsubscribed %s from %r", _cb_name(callback), topic)
        except (ValueError, KeyError):
            pass

    async def publish(self, topic: str, message: BusMessage) -> None:
        """Enqueue *message* for delivery to all matching subscribers.

        If the bus has not been started yet the message is still enqueued and
        will be delivered once :meth:`start` is called.
        """
        await self._queue.put((topic, message))
        log.debug("bus: published %s on %r (queue=%d)", message.type.value, topic, self._queue.qsize())

    # ------------------------------------------------------------------
    # Internals
    # ------------------------------------------------------------------

    async def _dispatch_loop(self) -> None:
        """Continuously pull from the queue and fan-out to subscribers."""
        try:
            while self._running:
                try:
                    topic, msg = await asyncio.wait_for(self._queue.get(), timeout=0.5)
                except asyncio.TimeoutError:
                    continue
                await self._deliver(topic, msg)
        except asyncio.CancelledError:
            pass

    async def _deliver(self, topic: str, message: BusMessage) -> None:
        """Fan-out *message* to every callback whose pattern matches *topic*."""
        for pattern, callbacks in list(self._subscribers.items()):
            if pattern == topic or fnmatch.fnmatch(topic, pattern):
                for cb in list(callbacks):
                    try:
                        await cb(topic, message)
                    except Exception:
                        log.exception(
                            "bus: error in subscriber %s for topic %r",
                            _cb_name(cb),
                            topic,
                        )

    @property
    def subscriber_count(self) -> int:
        """Total number of active subscriptions (across all topics)."""
        return sum(len(cbs) for cbs in self._subscribers.values())


def _cb_name(cb: Callback) -> str:
    """Best-effort human-readable name for a callback."""
    return getattr(cb, "__qualname__", None) or getattr(cb, "__name__", repr(cb))
