"""Tests for the ScheduleTool."""

from __future__ import annotations

from typing import Any

import pytest

from meept.tools.builtin.schedule_tool import ScheduleTool


# ---------------------------------------------------------------------------
# Mock objects
# ---------------------------------------------------------------------------


class MockScheduler:
    """Records add_agent_job calls."""

    def __init__(self) -> None:
        self.jobs: list[dict[str, Any]] = []

    def add_agent_job(self, **kwargs: Any) -> str:
        self.jobs.append(kwargs)
        return kwargs.get("job_id", "test-id")


class MockSecurity:
    """Security that allows or denies based on constructor flag."""

    def __init__(self, allow: bool = True) -> None:
        self._allow = allow

    async def check_permission(self, action: str, details: Any = None) -> tuple[bool, str]:
        if self._allow:
            return True, "Allowed"
        return False, "Not permitted"


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_definition() -> None:
    """The tool definition should have the correct name and parameters."""
    tool = ScheduleTool(scheduler=MockScheduler())
    defn = tool.definition()

    assert defn.name == "schedule_job"
    param_names = [p.name for p in defn.parameters]
    assert "task_description" in param_names
    assert "trigger_type" in param_names
    assert "trigger_args" in param_names
    assert "skill_hint" in param_names


@pytest.mark.asyncio
async def test_execute_schedules_job() -> None:
    """Executing the tool should call add_agent_job on the scheduler."""
    scheduler = MockScheduler()
    tool = ScheduleTool(scheduler=scheduler)

    result = await tool.execute(
        task_description="Check the weather",
        trigger_type="interval",
        trigger_args={"hours": 6},
    )

    assert result["success"] is True
    assert "job_id" in result
    assert len(scheduler.jobs) == 1
    assert scheduler.jobs[0]["task_description"] == "Check the weather"
    assert scheduler.jobs[0]["trigger"] == "interval"


@pytest.mark.asyncio
async def test_execute_with_skill_hint() -> None:
    """The skill_hint parameter should be forwarded to the scheduler."""
    scheduler = MockScheduler()
    tool = ScheduleTool(scheduler=scheduler)

    result = await tool.execute(
        task_description="Summarize news",
        trigger_type="cron",
        trigger_args={"hour": 9},
        skill_hint="news_summary",
    )

    assert result["success"] is True
    assert scheduler.jobs[0]["skill_hint"] == "news_summary"


@pytest.mark.asyncio
async def test_execute_missing_description() -> None:
    """Missing task_description should return an error."""
    tool = ScheduleTool(scheduler=MockScheduler())
    result = await tool.execute(trigger_type="date", trigger_args={})
    assert result["success"] is False
    assert "required" in result["error"]


@pytest.mark.asyncio
async def test_execute_invalid_trigger_type() -> None:
    """Invalid trigger_type should return an error."""
    tool = ScheduleTool(scheduler=MockScheduler())
    result = await tool.execute(
        task_description="Test",
        trigger_type="invalid",
        trigger_args={},
    )
    assert result["success"] is False
    assert "Invalid" in result["error"]


@pytest.mark.asyncio
async def test_execute_permission_denied() -> None:
    """When security denies, the tool should return permission error."""
    tool = ScheduleTool(
        scheduler=MockScheduler(),
        security=MockSecurity(allow=False),
    )

    result = await tool.execute(
        task_description="Test",
        trigger_type="date",
        trigger_args={},
    )

    assert result["success"] is False
    assert "Permission denied" in result["error"]


@pytest.mark.asyncio
async def test_execute_permission_allowed() -> None:
    """When security allows, the tool should proceed normally."""
    scheduler = MockScheduler()
    tool = ScheduleTool(
        scheduler=scheduler,
        security=MockSecurity(allow=True),
    )

    result = await tool.execute(
        task_description="Test",
        trigger_type="date",
        trigger_args={},
    )

    assert result["success"] is True
    assert len(scheduler.jobs) == 1


@pytest.mark.asyncio
async def test_execute_invalid_trigger_args() -> None:
    """Non-dict trigger_args should return an error."""
    tool = ScheduleTool(scheduler=MockScheduler())
    result = await tool.execute(
        task_description="Test",
        trigger_type="date",
        trigger_args="not a dict",
    )
    assert result["success"] is False
    assert "dict" in result["error"]
