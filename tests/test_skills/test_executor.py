"""Tests for the Orchestrator + WorkerFactory (replaces TaskExecutor tests).

These tests verify that the new Orchestrator and WorkerFactory provide
equivalent functionality to the old TaskExecutor.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.agent.orchestrator import Orchestrator, OrchestratorResult
from meept.agent.worker_factory import WorkerFactory
from meept.models.messages import BusMessage
from meept.models.tasks import TaskPlan, TaskStatus, TaskStep
from meept.scheduler.pipelines import PipelineExecutor
from meept.skills.models import SkillDefinition
from meept.skills.registry import SkillRegistry
from meept.tools.interface import Tool, ToolDefinition, ToolParameter, ToolRegistry


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


class MockBus:
    def __init__(self) -> None:
        self.messages: list[tuple[str, BusMessage]] = []

    async def publish(self, topic: str, msg: BusMessage) -> None:
        self.messages.append((topic, msg))


@dataclass
class MockLLMResponse:
    content: str
    tool_calls: list = field(default_factory=list)
    usage: Any = None
    model: str = "test"
    finish_reason: str = "stop"


class MockLLMClient:
    def __init__(self, response: str = "Done.") -> None:
        self._response = response

    async def chat(self, messages, tools=None, **kwargs):
        return MockLLMResponse(content=self._response)


class MockSecurity:
    async def check_permission(self, action: str, details: dict | None = None):
        return True, "Allowed"


class _DummyTool(Tool):
    def __init__(self, name: str) -> None:
        self._name = name

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name=self._name,
            description=f"Dummy: {self._name}",
            parameters=[ToolParameter(name="arg", type="string", description="Arg")],
        )

    async def execute(self, **kwargs) -> dict[str, Any]:
        return {"result": f"executed {self._name}"}


def _mock_llm_factory(model_name: str) -> MockLLMClient:
    return MockLLMClient(response=f"Processed by {model_name}")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_execute_with_skill() -> None:
    """Orchestrator should execute a skill step via WorkerFactory."""
    bus = MockBus()
    registry = ToolRegistry()
    registry.register(_DummyTool("file_read"))

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    executor = PipelineExecutor(bus)
    skill_reg = SkillRegistry()
    skill_reg.register(SkillDefinition(
        name="test_skill",
        description="A test skill",
        system_prompt="You are a test.",
    ))

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
        skill_registry=skill_reg,
    )

    step = TaskStep(description="Test message", skill_name="test_skill")
    result = await orch.execute_single(step)
    assert isinstance(result, str)
    assert len(result) > 0


@pytest.mark.asyncio
async def test_execute_with_skill_error_handling() -> None:
    """Error during skill execution should be handled gracefully."""

    class FailingFactory:
        def create(self, skill=None):
            return None

        def create_handler(self, skill=None, step_description=""):
            async def _handler(ctx):
                raise RuntimeError("LLM error")
            return _handler

    bus = MockBus()
    executor = PipelineExecutor(bus)

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=FailingFactory(),
        bus=bus,
    )

    step = TaskStep(description="Test", skill_name="fail_skill")
    result = await orch.execute([step])

    assert result.success is False
    assert result.step_results[step.id].success is False


@pytest.mark.asyncio
async def test_execute_plan() -> None:
    """Orchestrator should execute plan steps and return combined results."""
    bus = MockBus()
    registry = ToolRegistry()

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    skill_reg = SkillRegistry()
    skill_reg.register(SkillDefinition(name="skill_a", description="A"))

    executor = PipelineExecutor(bus)
    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
        skill_registry=skill_reg,
    )

    steps = [
        TaskStep(id="step_1", description="Do A", skill_name="skill_a"),
        TaskStep(id="step_2", description="Do B", skill_name="skill_a"),
    ]

    result = await orch.execute(steps)

    assert "step_1" in result.step_results
    assert "step_2" in result.step_results
    assert result.step_results["step_1"].success is True


@pytest.mark.asyncio
async def test_execute_plan_without_skills_uses_default() -> None:
    """Steps without skill_name should use the default handler."""
    bus = MockBus()
    registry = ToolRegistry()

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    executor = PipelineExecutor(bus)
    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    steps = [TaskStep(id="step_1", description="Simple task")]
    result = await orch.execute(steps)

    assert result.step_results["step_1"].success is True


@pytest.mark.asyncio
async def test_execute_plan_step_failure() -> None:
    """Failed steps should be marked FAILED."""

    class BrokenFactory:
        def create(self, skill=None):
            return None

        def create_handler(self, skill=None, step_description=""):
            async def _handler(ctx):
                raise RuntimeError("LLM error")
            return _handler

    bus = MockBus()
    executor = PipelineExecutor(bus)

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=BrokenFactory(),
        bus=bus,
    )

    step = TaskStep(id="step_1", description="Will fail", skill_name="broken")
    result = await orch.execute([step])

    assert step.status == TaskStatus.FAILED


@pytest.mark.asyncio
async def test_worker_factory_uses_system_prompt() -> None:
    """The WorkerFactory should set system_prompt_override from the skill."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    skill = SkillDefinition(
        name="prompted",
        description="Has prompt",
        system_prompt="Custom system prompt",
        instructions="Extra instructions",
        allowed_tools=["file_read"],
    )

    loop = factory.create(skill)
    assert loop._system_prompt_override is not None
    assert "Custom system prompt" in loop._system_prompt_override
    assert "Extra instructions" in loop._system_prompt_override
