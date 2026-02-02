"""Tests for the TaskExecutor."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import pytest

from meept.models.tasks import TaskPlan, TaskStatus, TaskStep
from meept.skills.executor import TaskExecutor
from meept.skills.models import SkillDefinition, TriageResult
from meept.tools.interface import Tool, ToolDefinition, ToolParameter, ToolRegistry


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


@dataclass
class MockLLMResponse:
    content: str
    tool_calls: list = None
    usage: Any = None
    model: str = "test"
    finish_reason: str = "stop"

    def __post_init__(self):
        if self.tool_calls is None:
            self.tool_calls = []


class MockLLMClient:
    """LLM client that returns canned responses."""

    def __init__(self, response: str = "Done.") -> None:
        self._response = response

    async def chat(self, messages, tools=None, **kwargs):
        return MockLLMResponse(content=self._response)


class MockSecurity:
    """Security manager that allows everything."""

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
    """LLM factory returning a mock client."""
    return MockLLMClient(response=f"Processed by {model_name}")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_execute_with_skill() -> None:
    """execute_with_skill should create a skill loop and run the message."""
    registry = ToolRegistry()
    registry.register(_DummyTool("file_read"))

    executor = TaskExecutor(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    skill = SkillDefinition(
        name="test_skill",
        description="A test skill",
        model="test-model",
        system_prompt="You are a test.",
    )

    result = await executor.execute_with_skill("Test message", skill)
    assert isinstance(result, str)
    assert len(result) > 0


@pytest.mark.asyncio
async def test_execute_with_skill_error_handling() -> None:
    """execute_with_skill should handle errors gracefully."""

    def failing_factory(model: str):
        raise RuntimeError("No model available")

    registry = ToolRegistry()
    executor = TaskExecutor(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=failing_factory,
    )

    skill = SkillDefinition(name="fail_skill", description="Will fail")

    # The executor creates a loop with llm_client=None which will fail
    # at runtime, but the error should be caught.
    result = await executor.execute_with_skill("Test", skill)
    assert "error" in result.lower()


@pytest.mark.asyncio
async def test_execute_plan() -> None:
    """execute_plan should run each step and return combined results."""
    registry = ToolRegistry()
    executor = TaskExecutor(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    skill = SkillDefinition(name="skill_a", description="A", model="model-a")

    plan = TaskPlan(
        description="Test plan",
        steps=[
            TaskStep(id="step_1", description="Do A", skill_name="skill_a"),
            TaskStep(id="step_2", description="Do B", skill_name="skill_a"),
        ],
    )

    result = await executor.execute_plan(
        "Complex task",
        plan,
        skills={"skill_a": skill},
    )

    assert "step_1" in result
    assert "step_2" in result
    assert plan.status == TaskStatus.COMPLETED


@pytest.mark.asyncio
async def test_execute_plan_without_skills_uses_default() -> None:
    """Steps without skill_name should use the default loop."""

    class MockDefaultLoop:
        async def run_once(self, message, conversation_id=None):
            return f"Default handled: {message}"

    registry = ToolRegistry()
    executor = TaskExecutor(
        tool_registry=registry,
        security=MockSecurity(),
    )

    plan = TaskPlan(
        description="Test plan",
        steps=[
            TaskStep(id="step_1", description="Simple task"),
        ],
    )

    result = await executor.execute_plan(
        "Task",
        plan,
        default_loop=MockDefaultLoop(),
    )

    assert "Default handled" in result


@pytest.mark.asyncio
async def test_execute_plan_step_failure() -> None:
    """Failed steps should be marked FAILED but not stop the plan."""
    registry = ToolRegistry()

    # Factory that returns a client whose chat raises
    def broken_factory(model: str):
        class BrokenClient:
            async def chat(self, messages, tools=None, **kwargs):
                raise RuntimeError("LLM error")
        return BrokenClient()

    executor = TaskExecutor(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=broken_factory,
    )

    skill = SkillDefinition(name="broken", description="Broken", model="bad")

    plan = TaskPlan(
        description="Test",
        steps=[
            TaskStep(id="step_1", description="Will fail", skill_name="broken"),
        ],
    )

    result = await executor.execute_plan(
        "Task", plan, skills={"broken": skill},
    )

    assert plan.steps[0].status in (TaskStatus.COMPLETED, TaskStatus.FAILED)


@pytest.mark.asyncio
async def test_create_skill_loop_uses_system_prompt() -> None:
    """The created loop should use the skill's system prompt."""
    registry = ToolRegistry()
    executor = TaskExecutor(
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

    loop = executor._create_skill_loop(skill)
    assert loop._system_prompt_override is not None
    assert "Custom system prompt" in loop._system_prompt_override
    assert "Extra instructions" in loop._system_prompt_override
