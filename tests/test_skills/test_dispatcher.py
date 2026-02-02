"""Tests for the SkillDispatcher."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.models.messages import BusMessage, MessageType
from meept.skills.dispatcher import SkillDispatcher
from meept.skills.models import SkillDefinition, TriageResult
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


class MockLLMClient:
    def __init__(self, response: str = "Default response") -> None:
        self._response = response

    async def chat(self, messages, tools=None, **kwargs):
        return MockLLMResponse(content=self._response)


class MockDefaultLoop:
    """Mock agent loop that returns a canned response."""

    def __init__(self, response: str = "Default loop response") -> None:
        self._response = response
        self.call_count = 0

    async def run_once(self, message: str, conversation_id: str | None = None) -> str:
        self.call_count += 1
        return self._response


class MockTriageAgent:
    """Triage agent that returns a pre-configured result."""

    def __init__(self, result: TriageResult) -> None:
        self._result = result
        self.call_count = 0

    async def classify(self, message: str) -> TriageResult:
        self.call_count += 1
        return self._result


class MockTaskExecutor:
    """Task executor that records calls."""

    def __init__(self, response: str = "Skill response") -> None:
        self._response = response
        self.calls: list[dict] = []

    async def execute_with_skill(self, message, skill, triage_result=None, conversation_id=None):
        self.calls.append({
            "message": message,
            "skill": skill.name,
            "conversation_id": conversation_id,
        })
        return self._response


class MockSecurity:
    async def check_permission(self, action, details=None):
        return True, "Allowed"


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_dispatch_to_default_no_triage() -> None:
    """Without triage, messages should go to the default loop."""
    registry = SkillRegistry()
    default_loop = MockDefaultLoop(response="Hello from default")
    executor = MockTaskExecutor()

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=None,
        task_executor=executor,
        default_loop=default_loop,
    )

    result = await dispatcher.dispatch("Hello")
    assert result == "Hello from default"
    assert default_loop.call_count == 1


@pytest.mark.asyncio
async def test_dispatch_to_skill_via_triage() -> None:
    """High-confidence triage should route to the matched skill."""
    registry = SkillRegistry()
    skill = SkillDefinition(name="code_review", description="Reviews code")
    registry.register(skill)

    triage = MockTriageAgent(
        TriageResult(skill_name="code_review", confidence=0.9)
    )
    executor = MockTaskExecutor(response="Review complete")
    default_loop = MockDefaultLoop()

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=triage,
        task_executor=executor,
        default_loop=default_loop,
    )

    result = await dispatcher.dispatch("Review my code")
    assert result == "Review complete"
    assert triage.call_count == 1
    assert len(executor.calls) == 1
    assert executor.calls[0]["skill"] == "code_review"
    assert default_loop.call_count == 0


@pytest.mark.asyncio
async def test_dispatch_fallback_on_low_confidence() -> None:
    """Low confidence triage should fall back to default."""
    registry = SkillRegistry()
    registry.register(SkillDefinition(name="test", description="Test"))

    triage = MockTriageAgent(
        TriageResult(skill_name="test", confidence=0.3, fallback_to_default=True)
    )
    executor = MockTaskExecutor()
    default_loop = MockDefaultLoop(response="Default handled it")

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=triage,
        task_executor=executor,
        default_loop=default_loop,
    )

    result = await dispatcher.dispatch("Something vague")
    assert result == "Default handled it"
    assert default_loop.call_count == 1


@pytest.mark.asyncio
async def test_dispatch_fallback_on_unknown_skill() -> None:
    """Triage returning an unregistered skill should fall back to default."""
    registry = SkillRegistry()

    triage = MockTriageAgent(
        TriageResult(skill_name="nonexistent", confidence=0.95)
    )
    executor = MockTaskExecutor()
    default_loop = MockDefaultLoop(response="Fallback")

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=triage,
        task_executor=executor,
        default_loop=default_loop,
    )

    result = await dispatcher.dispatch("Do something")
    assert result == "Fallback"
    assert default_loop.call_count == 1


@pytest.mark.asyncio
async def test_handle_chat_request() -> None:
    """handle_chat_request should dispatch and publish response."""
    registry = SkillRegistry()
    default_loop = MockDefaultLoop(response="Bus response")
    executor = MockTaskExecutor()

    published: list[tuple[str, BusMessage]] = []

    class MockBus:
        async def publish(self, topic: str, msg: BusMessage):
            published.append((topic, msg))

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=None,
        task_executor=executor,
        default_loop=default_loop,
        bus=MockBus(),
    )

    request = BusMessage(
        type=MessageType.CHAT_REQUEST,
        payload={"text": "Hello", "conversation_id": "conv1"},
        source="test",
    )

    await dispatcher.handle_chat_request(request)

    assert default_loop.call_count == 1
    # Should have published a CHAT_RESPONSE.
    response_msgs = [
        (t, m) for t, m in published
        if m.type == MessageType.CHAT_RESPONSE
    ]
    assert len(response_msgs) == 1
    assert response_msgs[0][1].payload["text"] == "Bus response"


@pytest.mark.asyncio
async def test_dispatch_with_conversation_id() -> None:
    """Conversation ID should be passed through to the skill executor."""
    registry = SkillRegistry()
    skill = SkillDefinition(name="test_skill", description="Test")
    registry.register(skill)

    triage = MockTriageAgent(
        TriageResult(skill_name="test_skill", confidence=0.9)
    )
    executor = MockTaskExecutor()
    default_loop = MockDefaultLoop()

    dispatcher = SkillDispatcher(
        skill_registry=registry,
        triage_agent=triage,
        task_executor=executor,
        default_loop=default_loop,
    )

    await dispatcher.dispatch("Test", conversation_id="conv123")

    assert len(executor.calls) == 1
    assert executor.calls[0]["conversation_id"] == "conv123"
