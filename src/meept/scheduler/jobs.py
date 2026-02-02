"""Built-in job definitions and handler factory for the meept scheduler."""

from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Callable

log = logging.getLogger(__name__)


@dataclass(slots=True)
class JobDefinition:
    """Declarative description of a scheduled job.

    Parameters
    ----------
    id:
        Unique identifier used as the APScheduler job id.
    name:
        Human-readable display name.
    description:
        Short description of what the job does.
    schedule_type:
        Trigger kind -- ``"cron"``, ``"interval"``, or ``"date"``.
    schedule_args:
        Keyword arguments forwarded to the trigger constructor.
    handler:
        Dotted name of the registry component that implements the actual
        work.  The component must expose an async ``__call__`` or a method
        whose name matches *handler* (resolved at runtime).
    handler_args:
        Extra keyword arguments passed to the handler when invoked.
    enabled:
        Whether the job should be registered on startup.
    """

    id: str
    name: str
    description: str
    schedule_type: str
    schedule_args: dict[str, Any]
    handler: str
    handler_args: dict[str, Any] = field(default_factory=dict)
    enabled: bool = True


# ---------------------------------------------------------------------------
# Built-in jobs
# ---------------------------------------------------------------------------


def get_builtin_jobs() -> list[JobDefinition]:
    """Return the default set of meept scheduled jobs.

    These cover routine maintenance tasks that keep the autonomous bot
    healthy between conversations.
    """
    return [
        JobDefinition(
            id="memory_consolidation",
            name="Memory Consolidation",
            description=(
                "Consolidate recent episodic memories into long-term storage, "
                "prune stale entries, and update relevance scores."
            ),
            schedule_type="interval",
            schedule_args={"hours": 6},
            handler="memory_manager",
            handler_args={"operation": "consolidate"},
        ),
        JobDefinition(
            id="personality_update",
            name="Personality Model Update",
            description=(
                "Re-evaluate the personality model based on recent conversations "
                "and feedback signals."
            ),
            schedule_type="interval",
            schedule_args={"hours": 24},
            handler="personality_manager",
            handler_args={"operation": "update"},
        ),
        JobDefinition(
            id="health_check",
            name="Health Check",
            description=(
                "Run a lightweight self-diagnostic and publish a status message "
                "to the bus so that dashboards and alerting can react."
            ),
            schedule_type="interval",
            schedule_args={"minutes": 5},
            handler="health_check",
            handler_args={},
        ),
        JobDefinition(
            id="budget_reset",
            name="Daily Token Budget Reset",
            description=(
                "Reset the daily LLM token budget counters at midnight UTC."
            ),
            schedule_type="cron",
            schedule_args={"hour": 0, "minute": 0, "timezone": "UTC"},
            handler="budget_manager",
            handler_args={"operation": "reset_daily"},
        ),
    ]


# ---------------------------------------------------------------------------
# Handler factory
# ---------------------------------------------------------------------------


def create_job_handler(
    job_def: JobDefinition,
    registry: Any,
) -> Callable[..., Any]:
    """Create an async handler that resolves its dependency from *registry*.

    The returned coroutine function:

    1. Looks up the component named ``job_def.handler`` in *registry*.
    2. If the component is callable, invokes it with ``**job_def.handler_args``.
    3. If the component exposes a method matching ``job_def.handler_args["operation"]``
       (when present), calls that method instead.
    4. Returns the result (which the scheduler wrapper will publish on the bus).

    If the component is not yet registered or the lookup fails, the handler
    logs a warning and returns gracefully rather than crashing the scheduler.
    """

    async def _handler() -> dict[str, Any]:
        component_name = job_def.handler
        try:
            component = await registry.get_or_create(component_name)
        except KeyError:
            log.warning(
                "jobs: component %r not registered -- skipping job %r",
                component_name,
                job_def.id,
            )
            return {
                "job_id": job_def.id,
                "status": "skipped",
                "reason": f"component {component_name!r} not registered",
            }

        # Determine callable target.
        operation = job_def.handler_args.get("operation")
        kwargs = {k: v for k, v in job_def.handler_args.items() if k != "operation"}

        target: Callable[..., Any]
        if operation and hasattr(component, operation):
            target = getattr(component, operation)
        elif callable(component):
            target = component
            kwargs = dict(job_def.handler_args)  # pass everything including operation
        else:
            log.error(
                "jobs: component %r (type %s) is not callable and has no method %r",
                component_name,
                type(component).__name__,
                operation,
            )
            return {
                "job_id": job_def.id,
                "status": "error",
                "reason": f"component {component_name!r} has no callable interface",
            }

        log.debug("jobs: executing %s.%s(%s)", component_name, operation or "__call__", kwargs)

        result = target(**kwargs)
        if asyncio.iscoroutine(result):
            result = await result

        return {
            "job_id": job_def.id,
            "status": "ok",
            "result": result,
            "executed_at": datetime.now(timezone.utc).isoformat(),
        }

    # Attach metadata so the scheduler/logging can identify the handler.
    _handler.__qualname__ = f"job_handler[{job_def.id}]"
    _handler.__name__ = f"job_handler_{job_def.id}"

    return _handler
