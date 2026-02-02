"""Orchestrator -- bridges task plans to the PipelineExecutor.

Converts :class:`TaskStep` lists into :class:`Pipeline` objects, uses
:class:`WorkerFactory` to create step handlers, and manages shared context
and worker lifecycle.
"""

from __future__ import annotations

import logging
import uuid
from dataclasses import dataclass, field
from typing import Any

from meept.models.tasks import TaskStatus, TaskStep
from meept.scheduler.pipelines import Pipeline, PipelineExecutor, PipelineStep
from meept.skills.models import SkillDefinition

log = logging.getLogger(__name__)


@dataclass
class StepResult:
    """Result of a single orchestrated step."""

    success: bool
    output: str
    error: str | None = None
    duration: float = 0.0


@dataclass
class OrchestratorResult:
    """Aggregated result of an orchestrated pipeline execution."""

    success: bool
    step_results: dict[str, StepResult] = field(default_factory=dict)
    synthesized: str = ""


class Orchestrator:
    """Bridges task plans to the :class:`PipelineExecutor`.

    Converts :class:`TaskStep` lists into a :class:`Pipeline`, attaches
    worker handlers via :class:`WorkerFactory`, and executes the DAG.

    Parameters
    ----------
    pipeline_executor:
        The DAG pipeline executor.
    worker_factory:
        Factory for creating ephemeral AgentLoop workers.
    bus:
        Internal message bus.
    skill_registry:
        Optional skill registry for resolving step skill names.
    """

    def __init__(
        self,
        pipeline_executor: PipelineExecutor,
        worker_factory: Any,
        bus: Any | None = None,
        skill_registry: Any | None = None,
    ) -> None:
        self._executor = pipeline_executor
        self._worker_factory = worker_factory
        self._bus = bus
        self._skill_registry = skill_registry

    async def execute(
        self,
        steps: list[TaskStep],
        context: dict[str, Any] | None = None,
    ) -> OrchestratorResult:
        """Execute a list of task steps as a pipeline.

        Parameters
        ----------
        steps:
            Task steps to execute (may have dependencies).
        context:
            Optional initial context dict seeded into the pipeline.

        Returns
        -------
        OrchestratorResult
            Aggregated results with per-step outcomes and a synthesized
            combined output.
        """
        pipeline_id = uuid.uuid4().hex[:12]
        pipeline_steps: list[PipelineStep] = []

        for step in steps:
            skill = self._resolve_skill(step.skill_name)
            handler = self._worker_factory.create_handler(
                skill=skill,
                step_description=step.description,
            )

            pipeline_steps.append(PipelineStep(
                id=step.id,
                name=step.description[:60],
                handler=handler,
                depends_on=list(step.depends_on),
                max_retries=step.max_retries,
                retry_delay=step.retry_delay,
                context_key=step.id,
            ))

        pipeline = Pipeline(
            id=pipeline_id,
            name=f"orchestrated-{pipeline_id}",
            steps=pipeline_steps,
        )

        raw_results = await self._executor.execute(pipeline)

        # Map raw results to StepResult objects and update TaskStep statuses.
        step_results: dict[str, StepResult] = {}
        output_parts: list[str] = []

        for step in steps:
            raw = raw_results.get(step.id, {})
            success = raw.get("success", False)
            result_val = raw.get("result", "")
            error = raw.get("error")
            duration = raw.get("duration", 0.0)

            step.status = TaskStatus.COMPLETED if success else TaskStatus.FAILED
            step.result = result_val

            output_str = str(result_val) if result_val else ""
            step_results[step.id] = StepResult(
                success=success,
                output=output_str,
                error=error,
                duration=duration,
            )

            if success and output_str:
                output_parts.append(output_str)
            elif not success and error:
                output_parts.append(f"[{step.id} failed: {error}]")

        all_ok = all(sr.success for sr in step_results.values())
        synthesized = "\n\n".join(output_parts) if output_parts else "No results produced."

        return OrchestratorResult(
            success=all_ok,
            step_results=step_results,
            synthesized=synthesized,
        )

    async def execute_single(
        self,
        step: TaskStep,
        context: dict[str, Any] | None = None,
    ) -> str:
        """Execute a single task step and return the result text.

        Convenience method for simple 1-step pipelines.

        Parameters
        ----------
        step:
            The task step to execute.
        context:
            Optional context dict.

        Returns
        -------
        str
            The step's output text, or an error message.
        """
        result = await self.execute([step], context=context)
        sr = result.step_results.get(step.id)
        if sr is not None and sr.success:
            return sr.output
        return result.synthesized

    def _resolve_skill(self, skill_name: str | None) -> SkillDefinition | None:
        """Look up a skill by name from the registry."""
        if skill_name is None or self._skill_registry is None:
            return None
        return self._skill_registry.get(skill_name)
