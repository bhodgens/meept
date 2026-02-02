"""Task and job data models used by the planner and scheduler."""

from __future__ import annotations

import enum
import uuid
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import Any


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------


class TaskStatus(str, enum.Enum):
    """Lifecycle status of a task or step."""

    PENDING = "pending"
    IN_PROGRESS = "in_progress"
    COMPLETED = "completed"
    FAILED = "failed"
    CANCELLED = "cancelled"


# ---------------------------------------------------------------------------
# Dataclasses
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class TaskStep:
    """A single step within a task plan.

    Parameters
    ----------
    id:
        Unique identifier for this step.
    description:
        Human-readable description of what this step does.
    tool_hint:
        Optional name of the tool most likely needed for this step.
    depends_on:
        List of step ids that must complete before this step can start.
    status:
        Current lifecycle status.
    result:
        Output produced by executing this step, if any.
    """

    id: str = field(default_factory=lambda: uuid.uuid4().hex[:12])
    description: str = ""
    tool_hint: str | None = None
    skill_name: str | None = None
    depends_on: list[str] = field(default_factory=list)
    status: TaskStatus = TaskStatus.PENDING
    result: Any = None


@dataclass(slots=True)
class TaskPlan:
    """A plan consisting of ordered steps to accomplish a goal.

    Parameters
    ----------
    id:
        Unique identifier for this plan.
    description:
        High-level description of the goal this plan addresses.
    steps:
        Ordered list of steps to execute.
    created_at:
        UTC timestamp when the plan was created.
    status:
        Overall plan status.
    """

    id: str = field(default_factory=lambda: uuid.uuid4().hex[:16])
    description: str = ""
    steps: list[TaskStep] = field(default_factory=list)
    created_at: datetime = field(default_factory=lambda: datetime.now(UTC))
    status: TaskStatus = TaskStatus.PENDING


@dataclass(slots=True)
class JobDefinition:
    """A scheduled job definition for the scheduler.

    Parameters
    ----------
    id:
        Unique identifier for this job.
    name:
        Human-readable name.
    schedule:
        Cron expression string (e.g. ``"0 9 * * *"`` for 9am daily).
    action:
        The action to perform -- typically a tool name or built-in action.
    args:
        Arguments to pass to the action.
    enabled:
        Whether the job is currently active.
    """

    id: str = field(default_factory=lambda: uuid.uuid4().hex[:16])
    name: str = ""
    schedule: str = ""
    action: str = ""
    args: dict[str, Any] = field(default_factory=dict)
    enabled: bool = True
