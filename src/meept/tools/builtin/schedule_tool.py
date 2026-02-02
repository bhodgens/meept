"""Agent-callable tool for scheduling future work via the scheduler."""

from __future__ import annotations

import logging
from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolParameter

log = logging.getLogger(__name__)


class ScheduleTool(Tool):
    """Tool allowing any AgentLoop to schedule tasks for future execution.

    Parameters
    ----------
    scheduler:
        The :class:`~meept.scheduler.scheduler.MeeptScheduler` instance.
    security:
        Security engine for permission gating (optional).
    """

    def __init__(self, scheduler: Any, security: Any | None = None) -> None:
        self._scheduler = scheduler
        self._security = security

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="schedule_job",
            description=(
                "Schedule a task for future execution. Creates a scheduled job "
                "that will run the specified task at the given time or interval."
            ),
            parameters=[
                ToolParameter(
                    name="task_description",
                    type="string",
                    description="Description of the task to execute when the job fires.",
                ),
                ToolParameter(
                    name="trigger_type",
                    type="string",
                    description="Type of trigger: 'cron', 'interval', or 'date'.",
                    enum=["cron", "interval", "date"],
                ),
                ToolParameter(
                    name="trigger_args",
                    type="object",
                    description=(
                        "Arguments for the trigger. For 'interval': {hours: 6}, "
                        "for 'date': {run_date: '2025-01-01T09:00:00'}, "
                        "for 'cron': {hour: 9, minute: 0}."
                    ),
                ),
                ToolParameter(
                    name="skill_hint",
                    type="string",
                    description="Optional skill name to use when executing the task.",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        task_description = kwargs.get("task_description", "")
        trigger_type = kwargs.get("trigger_type", "date")
        trigger_args = kwargs.get("trigger_args", {})
        skill_hint = kwargs.get("skill_hint")

        if not task_description:
            return {"success": False, "error": "task_description is required"}

        if trigger_type not in ("cron", "interval", "date"):
            return {"success": False, "error": f"Invalid trigger_type: {trigger_type}"}

        if not isinstance(trigger_args, dict):
            return {"success": False, "error": "trigger_args must be a dict"}

        # Permission check if security is available.
        if self._security is not None:
            check = getattr(self._security, "check_permission", None)
            if check is not None:
                import inspect

                if inspect.iscoroutinefunction(check):
                    allowed, reason = await check("schedule", {"task": task_description})
                else:
                    allowed, reason = check("schedule", {"task": task_description})
                if not allowed:
                    return {"success": False, "error": f"Permission denied: {reason}"}

        try:
            import uuid

            job_id = f"agent-{uuid.uuid4().hex[:8]}"
            self._scheduler.add_agent_job(
                job_id=job_id,
                task_description=task_description,
                trigger=trigger_type,
                skill_hint=skill_hint,
                **trigger_args,
            )

            return {
                "success": True,
                "job_id": job_id,
                "message": f"Scheduled job '{job_id}' with trigger '{trigger_type}'",
            }

        except Exception as exc:
            log.error("schedule_job failed: %s", exc, exc_info=True)
            return {"success": False, "error": str(exc)}
