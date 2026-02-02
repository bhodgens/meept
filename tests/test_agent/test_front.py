"""Tests for the FrontAgent."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.agent.front import FrontAgent
from meept.agent.orchestrator import OrchestratorResult
from meept.models.messages import BusMessage, MessageType
from meept.models.tasks import TaskPlan, TaskStep
from meept.skills.models import SkillDefinition, TriageResult
from meept.skills.registry import SkillRegistry


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


class MockBus:
    def __init__(self) -> None:
        self.messages: list[tuple[str, BusMessage]] = []

    async def publish(self, topic: str, msg: BusMessage) -> None:
        self.messages.append((topic, msg))


class MockDefaultLoop:
    def __init__(self, response: str = "Default response") -> None:
        self._response = response
        self.call_count = 0
        self.last_message: str | None = None
        self.last_conv_id: str | None = None

    async def run_once(self, message: str, conversation_id: str | None = None) -> str:
        self.call_count += 1
        self.last_message = message
        self.last_conv_id = conversation_id
        return self._response


class MockTriageAgent:
    def __init__(self, result: TriageResult) -> None:
        self._result = result
        self.call_count = 0

    async def classify(self, message: str) -> TriageResult:
        self.call_count += 1
        return self._result


class MockOrchestrator:
    def __init__(self, response: str = "Orchestrated") -> None:
        self._response = response
        self.single_calls: list[TaskStep] = []
        self.multi_calls: list[list[TaskStep]] = []

    async def execute_single(self, step: TaskStep, context: dict | None = None) -> str:
        self.single_calls.append(step)
        return self._response

    async def execute(self, steps: list[TaskStep], context: dict | None = None) -> OrchestratorResult:
        self.multi_calls.append(steps)
        return OrchestratorResult(success=True, synthesized=self._response)


class MockPlanner:
    def __init__(self, should: bool = True) -> None:
        self._should = should
        self._plan = TaskPlan(
            description="Test plan",
            steps=[
                TaskStep(id="p1", description="Plan step 1"),
                TaskStep(id="p2", description="Plan step 2"),
            ],
        )

    def should_plan(self, message: str) -> bool:
        return self._should

    async def decompose(self, task: str) -> TaskPlan:
        return self._plan


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_dispatch_simple_no_triage() -> None:
    """Without triage, should fall back to default loop."""
    default = MockDefaultLoop(response="Hello")
    orch = MockOrchestrator()

    agent = FrontAgent(
        orchestrator=orch,
        default_loop=default,
    )

    result = await agent.dispatch("Hi")
    assert result == "Hello"
    assert default.call_count == 1


@pytest.mark.asyncio
async def test_dispatch_skill_via_triage() -> None:
    """Confident triage should route to orchestrator with skill step."""
    registry = SkillRegistry()
    registry.register(SkillDefinition(name="weather", description="Weather"))

    triage = MockTriageAgent(TriageResult(skill_name="weather", confidence=0.9))
    orch = MockOrchestrator(response="Sunny")
    default = MockDefaultLoop()

    agent = FrontAgent(
        orchestrator=orch,
        triage_agent=triage,
        default_loop=default,
        skill_registry=registry,
    )

    result = await agent.dispatch("What's the weather?")
    assert result == "Sunny"
    assert len(orch.single_calls) == 1
    assert orch.single_calls[0].skill_name == "weather"
    assert default.call_count == 0


@pytest.mark.asyncio
async def test_dispatch_low_confidence_to_default() -> None:
    """Low-confidence triage should fall through to default."""
    registry = SkillRegistry()
    registry.register(SkillDefinition(name="x", description="X"))

    triage = MockTriageAgent(
        TriageResult(skill_name="x", confidence=0.2, fallback_to_default=True)
    )
    orch = MockOrchestrator()
    default = MockDefaultLoop(response="Default handled")

    agent = FrontAgent(
        orchestrator=orch,
        triage_agent=triage,
        default_loop=default,
        skill_registry=registry,
    )

    result = await agent.dispatch("Maybe?")
    assert result == "Default handled"
    assert default.call_count == 1


@pytest.mark.asyncio
async def test_dispatch_planner_route() -> None:
    """When planner says yes, should decompose and run multi-step pipeline."""
    orch = MockOrchestrator(response="Plan result")
    planner = MockPlanner(should=True)
    default = MockDefaultLoop()

    agent = FrontAgent(
        orchestrator=orch,
        planner=planner,
        default_loop=default,
    )

    result = await agent.dispatch("Do a complex thing with many steps and then combine them")
    assert result == "Plan result"
    assert len(orch.multi_calls) == 1
    assert default.call_count == 0


@pytest.mark.asyncio
async def test_dispatch_planner_says_no() -> None:
    """When planner says no, should fall through to default."""
    orch = MockOrchestrator()
    planner = MockPlanner(should=False)
    default = MockDefaultLoop(response="Simple answer")

    agent = FrontAgent(
        orchestrator=orch,
        planner=planner,
        default_loop=default,
    )

    result = await agent.dispatch("Simple question")
    assert result == "Simple answer"
    assert default.call_count == 1


@pytest.mark.asyncio
async def test_handle_chat_request_publishes_response() -> None:
    """Bus handler should publish CHAT_RESPONSE."""
    bus = MockBus()
    default = MockDefaultLoop(response="Reply")
    orch = MockOrchestrator()

    agent = FrontAgent(
        orchestrator=orch,
        default_loop=default,
        bus=bus,
    )

    request = BusMessage(
        type=MessageType.CHAT_REQUEST,
        payload={"text": "Hello", "conversation_id": "c1"},
        source="test",
    )
    await agent.handle_chat_request(request)

    responses = [(t, m) for t, m in bus.messages if m.type == MessageType.CHAT_RESPONSE]
    assert len(responses) == 1
    assert responses[0][1].payload["text"] == "Reply"
    assert responses[0][1].reply_to == request.id


@pytest.mark.asyncio
async def test_handle_chat_request_rejects_empty() -> None:
    """Empty text should not dispatch."""
    bus = MockBus()
    default = MockDefaultLoop()
    orch = MockOrchestrator()

    agent = FrontAgent(orchestrator=orch, default_loop=default, bus=bus)

    for text in ["", "   ", None]:
        request = BusMessage(
            type=MessageType.CHAT_REQUEST,
            payload={"text": text or "", "conversation_id": "c1"},
            source="test",
        )
        await agent.handle_chat_request(request)

    assert default.call_count == 0
    assert len(bus.messages) == 0


@pytest.mark.asyncio
async def test_triage_publishes_events() -> None:
    """Triage and skill events should be published to the bus."""
    bus = MockBus()
    registry = SkillRegistry()
    registry.register(SkillDefinition(name="code", description="Code"))

    triage = MockTriageAgent(TriageResult(skill_name="code", confidence=0.95))
    orch = MockOrchestrator(response="Done")

    agent = FrontAgent(
        orchestrator=orch,
        triage_agent=triage,
        skill_registry=registry,
        bus=bus,
    )

    await agent.dispatch("Write code")

    msg_types = [m.type for _, m in bus.messages]
    assert MessageType.TRIAGE_RESULT in msg_types
    assert MessageType.SKILL_TASK_START in msg_types
    assert MessageType.SKILL_TASK_COMPLETE in msg_types


@pytest.mark.asyncio
async def test_planner_publishes_progress() -> None:
    """Multi-step planning should publish CHAT_PROGRESS."""
    bus = MockBus()
    orch = MockOrchestrator(response="Done")
    planner = MockPlanner(should=True)

    agent = FrontAgent(
        orchestrator=orch,
        planner=planner,
        bus=bus,
    )

    await agent.dispatch("Do a complex thing with many steps and then combine the results")

    msg_types = [m.type for _, m in bus.messages]
    assert MessageType.CHAT_PROGRESS in msg_types


@pytest.mark.asyncio
async def test_no_default_loop_falls_to_orchestrator() -> None:
    """Without a default loop, should fall back to orchestrator single step."""
    orch = MockOrchestrator(response="Orch fallback")

    agent = FrontAgent(orchestrator=orch)

    result = await agent.dispatch("Do something")
    assert result == "Orch fallback"
    assert len(orch.single_calls) == 1
