"""Tests for the WorkerFactory."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import pytest

from meept.agent.worker_factory import WorkerFactory
from meept.llm.models import ModelConfig
from meept.llm.providers import ModelsConfig, ModelDefinition, ProviderConfig, ProviderOptions
from meept.llm.resolver import ModelResolver
from meept.skills.models import SkillDefinition
from meept.tools.interface import Tool, ToolDefinition, ToolParameter, ToolRegistry


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
    def __init__(self, response: str = "Worker response") -> None:
        self._response = response

    async def chat(self, messages, tools=None, **kwargs):
        return MockLLMResponse(content=self._response)


class MockSecurity:
    async def check_permission(self, action, details=None):
        return True, "Allowed"


class MockScheduler:
    def __init__(self) -> None:
        self.jobs: list[dict] = []

    def add_agent_job(self, **kwargs):
        self.jobs.append(kwargs)
        return kwargs.get("job_id", "test")


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


def _make_resolver() -> ModelResolver:
    """Create a test ModelResolver."""
    config = ModelsConfig(
        model="test/default",
        providers={
            "test": ProviderConfig(
                options=ProviderOptions(baseURL="http://test:11434/v1"),
                models={
                    "default": ModelDefinition(
                        name="default",
                        capabilities=["code", "tool_use", "reasoning"],
                    ),
                },
            ),
        },
    )
    return ModelResolver(config)


def _mock_llm_factory(model_name: str) -> MockLLMClient:
    return MockLLMClient(response=f"Response from {model_name}")


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_create_default_worker_with_resolver() -> None:
    """Creating a worker without a skill using model_resolver should work."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
    )

    loop = factory.create(skill=None)
    assert loop is not None
    assert loop._system_prompt_override is None


def test_create_default_worker_with_legacy_factory() -> None:
    """Creating a worker with legacy llm_factory should still work."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=_mock_llm_factory,
    )

    loop = factory.create(skill=None)
    assert loop is not None
    assert loop._system_prompt_override is None


def test_create_skill_worker() -> None:
    """Creating a worker with a skill should set the system prompt."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
    )

    skill = SkillDefinition(
        name="code_review",
        description="Reviews code",
        system_prompt="You are a code reviewer.",
        instructions="Be thorough.",
        requires=["code"],
        max_iterations=5,
    )

    loop = factory.create(skill)
    assert loop._system_prompt_override is not None
    assert "code reviewer" in loop._system_prompt_override
    assert "Be thorough" in loop._system_prompt_override
    assert loop._max_iterations == 5


def test_create_worker_with_filtered_tools() -> None:
    """Workers should get filtered tool registries based on skill config."""
    registry = ToolRegistry()
    registry.register(_DummyTool("file_read"))
    registry.register(_DummyTool("shell"))
    registry.register(_DummyTool("web_fetch"))

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
    )

    skill = SkillDefinition(
        name="limited",
        description="Limited tools",
        allowed_tools=["file_read"],
    )

    loop = factory.create(skill)
    # The filtered registry should only expose file_read.
    assert "file_read" in loop._registry
    assert "shell" not in loop._registry


def test_create_worker_injects_schedule_tool() -> None:
    """When a scheduler is provided, schedule_job should be in the registry."""
    registry = ToolRegistry()
    scheduler = MockScheduler()

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
        scheduler=scheduler,
    )

    factory.create(skill=None)
    assert "schedule_job" in registry


def test_create_handler_returns_callable() -> None:
    """create_handler should return a callable."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
    )

    handler = factory.create_handler(skill=None, step_description="Do something")
    assert callable(handler)


@pytest.mark.asyncio
async def test_handler_calls_run_once() -> None:
    """The handler from create_handler should invoke the loop."""
    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=_make_resolver(),
    )

    handler = factory.create_handler(skill=None, step_description="Test task")
    result = await handler({})
    assert isinstance(result, str)
    assert len(result) > 0


@pytest.mark.asyncio
async def test_handler_passes_context() -> None:
    """The handler should include context from previous steps in the prompt."""
    registry = ToolRegistry()

    # Use a custom LLM that echoes back the message it receives.
    class EchoLLM:
        async def chat(self, messages, tools=None, **kwargs):
            last_user_msg = [m for m in messages if m.role.value == "user"][-1]
            return MockLLMResponse(content=last_user_msg.content)

    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        llm_factory=lambda model: EchoLLM(),
    )

    handler = factory.create_handler(skill=None, step_description="Process data")
    result = await handler({"step_a": "data from A"})
    assert "step_a" in result
    assert "data from A" in result


def test_create_worker_handles_resolver_failure() -> None:
    """If model_resolver raises, the worker should still be created."""
    class FailingResolver:
        def resolve_for_skill(self, skill, current_model=None):
            raise RuntimeError("No model")

    registry = ToolRegistry()
    factory = WorkerFactory(
        tool_registry=registry,
        security=MockSecurity(),
        model_resolver=FailingResolver(),
    )

    loop = factory.create(skill=None)
    assert loop is not None
    assert loop._llm is None
