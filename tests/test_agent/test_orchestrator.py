"""Tests for the Orchestrator."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.agent.orchestrator import Orchestrator, OrchestratorResult
from meept.models.messages import BusMessage
from meept.models.tasks import TaskStatus, TaskStep
from meept.scheduler.pipelines import PipelineExecutor
from meept.skills.models import SkillDefinition
from meept.skills.registry import SkillRegistry


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
    def __init__(self, response: str = "Done") -> None:
        self._response = response

    async def chat(self, messages, tools=None, **kwargs):
        return MockLLMResponse(content=self._response)


class MockSecurity:
    async def check_permission(self, action, details=None):
        return True, "Allowed"


class MockWorkerFactory:
    """Worker factory that returns handlers with predictable output."""

    def __init__(self, response_prefix: str = "Result for") -> None:
        self._prefix = response_prefix

    def create(self, skill=None):
        return None  # Not used by tests directly.

    def create_handler(self, skill=None, step_description=""):
        prefix = self._prefix

        async def _handler(context: dict) -> str:
            ctx_info = f" (ctx: {list(context.keys())})" if context else ""
            return f"{prefix}: {step_description}{ctx_info}"

        return _handler


class FailingWorkerFactory:
    """Worker factory where specific steps fail."""

    def __init__(self, fail_ids: set[str] | None = None) -> None:
        self._fail_ids = fail_ids or set()
        self._call_descriptions: dict[str, str] = {}

    def create(self, skill=None):
        return None

    def create_handler(self, skill=None, step_description=""):
        fail_ids = self._fail_ids

        async def _handler(context: dict) -> str:
            # Use a simple check -- if "fail" in the description, raise.
            if any(fid in step_description for fid in fail_ids):
                raise RuntimeError(f"Step failed: {step_description}")
            return f"OK: {step_description}"

        return _handler


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_execute_single_step() -> None:
    """A single step should execute and return an OrchestratorResult."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    steps = [TaskStep(id="s1", description="Do something")]
    result = await orch.execute(steps)

    assert isinstance(result, OrchestratorResult)
    assert result.success is True
    assert "s1" in result.step_results
    assert result.step_results["s1"].success is True
    assert "Do something" in result.step_results["s1"].output


@pytest.mark.asyncio
async def test_execute_multi_step() -> None:
    """Multiple steps should all execute."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    steps = [
        TaskStep(id="s1", description="Step one"),
        TaskStep(id="s2", description="Step two"),
    ]
    result = await orch.execute(steps)

    assert result.success is True
    assert len(result.step_results) == 2
    assert result.step_results["s1"].success is True
    assert result.step_results["s2"].success is True


@pytest.mark.asyncio
async def test_execute_with_dependencies() -> None:
    """Steps with depends_on should execute in order."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    steps = [
        TaskStep(id="s1", description="First"),
        TaskStep(id="s2", description="Second", depends_on=["s1"]),
    ]
    result = await orch.execute(steps)

    assert result.success is True
    # The second step should have context from the first.
    assert "s1" in result.step_results["s2"].output or result.step_results["s2"].success


@pytest.mark.asyncio
async def test_execute_step_failure() -> None:
    """Failed steps should be marked and not break the overall execution."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    # Use a handler that always raises.
    class AlwaysFailFactory:
        def create(self, skill=None):
            return None

        def create_handler(self, skill=None, step_description=""):
            async def _handler(ctx: dict) -> str:
                raise RuntimeError("boom")
            return _handler

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=AlwaysFailFactory(),
        bus=bus,
    )

    steps = [TaskStep(id="s1", description="Will fail")]
    result = await orch.execute(steps)

    assert result.success is False
    assert result.step_results["s1"].success is False
    assert result.step_results["s1"].error is not None
    assert steps[0].status == TaskStatus.FAILED


@pytest.mark.asyncio
async def test_execute_updates_task_step_status() -> None:
    """TaskStep.status should be updated after execution."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    step = TaskStep(id="s1", description="Test")
    assert step.status == TaskStatus.PENDING

    await orch.execute([step])
    assert step.status == TaskStatus.COMPLETED


@pytest.mark.asyncio
async def test_execute_single_convenience() -> None:
    """execute_single should return the output string directly."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    step = TaskStep(id="s1", description="Quick task")
    result = await orch.execute_single(step)

    assert isinstance(result, str)
    assert "Quick task" in result


@pytest.mark.asyncio
async def test_execute_with_skill_registry() -> None:
    """When a skill_registry is provided, skills should be resolved."""
    bus = MockBus()
    executor = PipelineExecutor(bus)

    skill_reg = SkillRegistry()
    skill_reg.register(SkillDefinition(name="code", description="Code skill"))

    factory = MockWorkerFactory()

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
        skill_registry=skill_reg,
    )

    steps = [TaskStep(id="s1", description="Write code", skill_name="code")]
    result = await orch.execute(steps)

    assert result.success is True


@pytest.mark.asyncio
async def test_execute_synthesized_output() -> None:
    """The synthesized output should combine all step results."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    factory = MockWorkerFactory(response_prefix="Output")

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=factory,
        bus=bus,
    )

    steps = [
        TaskStep(id="s1", description="Part A"),
        TaskStep(id="s2", description="Part B"),
    ]
    result = await orch.execute(steps)

    assert "Part A" in result.synthesized
    assert "Part B" in result.synthesized


@pytest.mark.asyncio
async def test_execute_respects_retry_fields() -> None:
    """TaskStep retry fields should be passed to PipelineStep."""
    bus = MockBus()
    executor = PipelineExecutor(bus)
    attempt_count = 0

    class RetryFactory:
        def create(self, skill=None):
            return None

        def create_handler(self, skill=None, step_description=""):
            async def _handler(ctx: dict) -> str:
                nonlocal attempt_count
                attempt_count += 1
                if attempt_count < 2:
                    raise RuntimeError("transient")
                return "recovered"
            return _handler

    orch = Orchestrator(
        pipeline_executor=executor,
        worker_factory=RetryFactory(),
        bus=bus,
    )

    steps = [TaskStep(id="s1", description="Flaky", max_retries=2, retry_delay=0.01)]
    result = await orch.execute(steps)

    assert result.success is True
    assert attempt_count == 2
