"""Tests for the async publish/subscribe message bus."""

from __future__ import annotations

import asyncio

import pytest

from meept.core.bus import MessageBus
from meept.models.messages import BusMessage, MessageType


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_msg(msg_type: MessageType = MessageType.STATUS_UPDATE, **payload) -> BusMessage:
    """Create a BusMessage with sensible defaults for testing."""
    return BusMessage(type=msg_type, payload=payload, source="test")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


async def test_publish_subscribe(mock_bus: MessageBus) -> None:
    """Publishing a message should deliver it to a matching subscriber."""
    received: list[tuple[str, BusMessage]] = []

    async def handler(topic: str, message: BusMessage) -> None:
        received.append((topic, message))

    mock_bus.subscribe("test.topic", handler)
    await mock_bus.start()

    msg = _make_msg(payload_key="value")
    await mock_bus.publish("test.topic", msg)

    # Give the dispatcher a moment to deliver.
    await asyncio.sleep(0.1)
    await mock_bus.stop()

    assert len(received) == 1
    assert received[0][0] == "test.topic"
    assert received[0][1] is msg


async def test_multiple_subscribers(mock_bus: MessageBus) -> None:
    """Multiple subscribers on the same topic all receive the message."""
    calls_a: list[BusMessage] = []
    calls_b: list[BusMessage] = []

    async def handler_a(topic: str, message: BusMessage) -> None:
        calls_a.append(message)

    async def handler_b(topic: str, message: BusMessage) -> None:
        calls_b.append(message)

    mock_bus.subscribe("events", handler_a)
    mock_bus.subscribe("events", handler_b)
    await mock_bus.start()

    msg = _make_msg()
    await mock_bus.publish("events", msg)

    await asyncio.sleep(0.1)
    await mock_bus.stop()

    assert len(calls_a) == 1
    assert len(calls_b) == 1
    assert calls_a[0] is msg
    assert calls_b[0] is msg


async def test_wildcard_subscribe(mock_bus: MessageBus) -> None:
    """A subscriber using 'memory.*' should receive 'memory.store' and 'memory.query'."""
    received: list[str] = []

    async def handler(topic: str, message: BusMessage) -> None:
        received.append(topic)

    mock_bus.subscribe("memory.*", handler)
    await mock_bus.start()

    await mock_bus.publish("memory.store", _make_msg())
    await mock_bus.publish("memory.query", _make_msg())
    await mock_bus.publish("scheduler.tick", _make_msg())  # Should NOT match.

    await asyncio.sleep(0.2)
    await mock_bus.stop()

    assert sorted(received) == ["memory.query", "memory.store"]


async def test_unsubscribe(mock_bus: MessageBus) -> None:
    """After unsubscribing, a callback should no longer receive messages."""
    received: list[BusMessage] = []

    async def handler(topic: str, message: BusMessage) -> None:
        received.append(message)

    mock_bus.subscribe("events", handler)
    await mock_bus.start()

    await mock_bus.publish("events", _make_msg())
    await asyncio.sleep(0.1)

    mock_bus.unsubscribe("events", handler)

    await mock_bus.publish("events", _make_msg())
    await asyncio.sleep(0.1)
    await mock_bus.stop()

    assert len(received) == 1


async def test_no_subscribers(mock_bus: MessageBus) -> None:
    """Publishing when there are no subscribers should not raise."""
    await mock_bus.start()
    await mock_bus.publish("nobody.listening", _make_msg())
    await asyncio.sleep(0.1)
    await mock_bus.stop()
    # No assertion -- we just verify no exception was raised.
