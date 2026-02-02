"""Tests for the FrontAgent dispatch (replaces old SkillDispatcher tests).

These tests verify that the FrontAgent correctly routes messages through
the default loop, planner, and orchestrator pipeline (without triage).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.agent.front import FrontAgent
from meept.models.messages import BusMessage, MessageType
from meept.models.tasks import TaskStep
from meept.skills.models import SkillDefinition
from meept.skills.registry import SkillRegistry


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


@dataclass
class MockLLMResponse:
    content: str
    tool_calls: list = field(default_factory=list)
    usage: Any = None
    model: str = "test"
    finish_reason: str = "stop"


class MockDefaultLoop:
    """Mock agent loop that returns a canned response."""

    def __init__(self, response: str = "Default loop response") -> None:
        self._response = response
        self.call_count = 0

    async def run_once(self, message: str, conversation_id: str | None = None) -> str:
        self.call_count += 1
        return self._response


class MockOrchestrator:
    """Orchestrator that records calls and returns canned results."""

    def __init__(self, response: str = "Orchestrated response") -> None:
        self._response = response
        self.calls: list[dict] = []

    async def execute_single(self, step: TaskStep, context: dict | None = None) -> str:
        self.calls.append({"step": step, "context": context})
        return self._response

    async def execute(self, steps, context=None):
        from meept.agent.orchestrator import OrchestratorResult
        self.calls.append({"steps": steps, "context": context})
        return OrchestratorResult(success=True, synthesized=self._response)


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_dispatch_to_default() -> None:
    """Without planner, messages should go to the default loop."""
    default_loop = MockDefaultLoop(response="Hello from default")
    orchestrator = MockOrchestrator()

    agent = FrontAgent(
        orchestrator=orchestrator,
        default_loop=default_loop,
    )

    result = await agent.dispatch("Hello")
    assert result == "Hello from default"
    assert default_loop.call_count == 1


@pytest.mark.asyncio
async def test_handle_chat_request() -> None:
    """handle_chat_request should dispatch and publish response."""
    default_loop = MockDefaultLoop(response="Bus response")
    orchestrator = MockOrchestrator()

    published: list[tuple[str, BusMessage]] = []

    class MockBus:
        async def publish(self, topic: str, msg: BusMessage):
            published.append((topic, msg))

    agent = FrontAgent(
        orchestrator=orchestrator,
        default_loop=default_loop,
        bus=MockBus(),
    )

    request = BusMessage(
        type=MessageType.CHAT_REQUEST,
        payload={"text": "Hello", "conversation_id": "conv1"},
        source="test",
    )

    await agent.handle_chat_request(request)

    assert default_loop.call_count == 1
    response_msgs = [
        (t, m) for t, m in published
        if m.type == MessageType.CHAT_RESPONSE
    ]
    assert len(response_msgs) == 1
    assert response_msgs[0][1].payload["text"] == "Bus response"


@pytest.mark.asyncio
async def test_dispatch_with_conversation_id() -> None:
    """Conversation ID should be passed through to the default loop."""
    default_loop = MockDefaultLoop(response="With conv ID")
    orchestrator = MockOrchestrator()

    agent = FrontAgent(
        orchestrator=orchestrator,
        default_loop=default_loop,
    )

    result = await agent.dispatch("Test", conversation_id="conv123")
    assert result == "With conv ID"
    assert default_loop.call_count == 1


@pytest.mark.asyncio
async def test_dispatch_falls_to_orchestrator() -> None:
    """Without a default loop, should fall back to orchestrator."""
    orchestrator = MockOrchestrator(response="Orch response")

    agent = FrontAgent(orchestrator=orchestrator)

    result = await agent.dispatch("Do something")
    assert result == "Orch response"
    assert len(orchestrator.calls) == 1


@pytest.mark.asyncio
async def test_dispatch_empty_message_via_bus() -> None:
    """Empty chat requests should be rejected."""
    default_loop = MockDefaultLoop()
    orchestrator = MockOrchestrator()

    published: list[tuple[str, BusMessage]] = []

    class MockBus:
        async def publish(self, topic: str, msg: BusMessage):
            published.append((topic, msg))

    agent = FrontAgent(
        orchestrator=orchestrator,
        default_loop=default_loop,
        bus=MockBus(),
    )

    request = BusMessage(
        type=MessageType.CHAT_REQUEST,
        payload={"text": "", "conversation_id": "conv1"},
        source="test",
    )

    await agent.handle_chat_request(request)

    assert default_loop.call_count == 0
    assert len(published) == 0
