"""Multi-step DAG pipeline executor with bus progress reporting."""

from __future__ import annotations

import asyncio
import logging
import time
from dataclasses import dataclass, field
from typing import Any, Callable

from meept.models.messages import BusMessage, MessageType

log = logging.getLogger(__name__)


@dataclass(slots=True)
class PipelineStep:
    """A single step inside a :class:`Pipeline`.

    Parameters
    ----------
    id:
        Unique identifier within the pipeline.
    name:
        Human-readable display name.
    handler:
        Async (or sync) callable that performs the work.  It receives a
        single ``context`` dict and should return an arbitrary result value.
    depends_on:
        List of step ids that must complete successfully before this step
        may run.
    timeout:
        Maximum wall-clock seconds the step is allowed to run before being
        cancelled.
    """

    id: str
    name: str
    handler: Callable[..., Any]
    depends_on: list[str] = field(default_factory=list)
    timeout: float = 300.0


@dataclass(slots=True)
class Pipeline:
    """An ordered collection of :class:`PipelineStep` objects forming a DAG.

    Parameters
    ----------
    id:
        Unique pipeline identifier.
    name:
        Human-readable name.
    steps:
        The steps that make up the pipeline.  Dependency ordering is
        derived from :attr:`PipelineStep.depends_on` -- the list order
        itself is not significant.
    """

    id: str
    name: str
    steps: list[PipelineStep]


# ---------------------------------------------------------------------------
# Step result type
# ---------------------------------------------------------------------------

_RESULT_KEYS = ("success", "result", "error", "duration", "status")


def _make_result(
    *,
    success: bool,
    result: Any = None,
    error: str | None = None,
    duration: float = 0.0,
    status: str = "completed",
) -> dict[str, Any]:
    return {
        "success": success,
        "result": result,
        "error": error,
        "duration": round(duration, 4),
        "status": status,
    }


# ---------------------------------------------------------------------------
# Executor
# ---------------------------------------------------------------------------


class PipelineExecutor:
    """Execute a :class:`Pipeline` respecting DAG dependency ordering.

    Steps whose dependencies have all completed successfully are launched in
    parallel.  If a step fails, every transitive dependent is marked as
    ``"skipped"``.  Progress events are published to the message bus.

    Parameters
    ----------
    bus:
        The application :class:`~meept.core.bus.MessageBus`.
    """

    def __init__(self, bus: Any) -> None:
        self._bus = bus

    async def execute(self, pipeline: Pipeline) -> dict[str, dict[str, Any]]:
        """Run all steps in *pipeline* and return a results dict.

        Returns
        -------
        dict[str, dict]
            Mapping of ``step_id`` to a result dict with keys ``success``,
            ``result``, ``error``, ``duration``, and ``status``.
        """
        results: dict[str, dict[str, Any]] = {}
        step_map: dict[str, PipelineStep] = {s.id: s for s in pipeline.steps}
        # Track completion events so dependents can await them.
        done_events: dict[str, asyncio.Event] = {s.id: asyncio.Event() for s in pipeline.steps}

        # Build reverse dependency graph for skip propagation.
        dependents: dict[str, list[str]] = {s.id: [] for s in pipeline.steps}
        for step in pipeline.steps:
            for dep_id in step.depends_on:
                if dep_id in dependents:
                    dependents[dep_id].append(step.id)

        await self._publish_progress(pipeline, "started", {})

        async def _run_step(step: PipelineStep) -> None:
            # Wait for all dependencies.
            for dep_id in step.depends_on:
                if dep_id in done_events:
                    await done_events[dep_id].wait()

                # Check whether the dependency succeeded.
                dep_result = results.get(dep_id)
                if dep_result is None or not dep_result["success"]:
                    # Dependency failed or was skipped -- skip this step.
                    results[step.id] = _make_result(
                        success=False,
                        error=f"dependency {dep_id!r} did not complete successfully",
                        status="skipped",
                    )
                    done_events[step.id].set()
                    await self._publish_progress(
                        pipeline,
                        "step_skipped",
                        {"step_id": step.id, "reason": results[step.id]["error"]},
                    )
                    return

            # All dependencies satisfied -- execute.
            await self._publish_progress(
                pipeline, "step_started", {"step_id": step.id, "step_name": step.name}
            )

            t0 = time.monotonic()
            try:
                res = step.handler()
                if asyncio.iscoroutine(res):
                    res = await asyncio.wait_for(res, timeout=step.timeout)
                duration = time.monotonic() - t0

                results[step.id] = _make_result(
                    success=True,
                    result=res,
                    duration=duration,
                )
                await self._publish_progress(
                    pipeline,
                    "step_completed",
                    {"step_id": step.id, "duration": round(duration, 4)},
                )

            except asyncio.TimeoutError:
                duration = time.monotonic() - t0
                error_msg = f"step {step.id!r} timed out after {step.timeout}s"
                log.error("pipeline %r: %s", pipeline.id, error_msg)
                results[step.id] = _make_result(
                    success=False,
                    error=error_msg,
                    duration=duration,
                    status="timeout",
                )
                await self._publish_progress(
                    pipeline, "step_failed", {"step_id": step.id, "error": error_msg}
                )

            except Exception as exc:
                duration = time.monotonic() - t0
                error_msg = f"{type(exc).__name__}: {exc}"
                log.exception("pipeline %r: step %r failed", pipeline.id, step.id)
                results[step.id] = _make_result(
                    success=False,
                    error=error_msg,
                    duration=duration,
                    status="failed",
                )
                await self._publish_progress(
                    pipeline, "step_failed", {"step_id": step.id, "error": error_msg}
                )

            finally:
                done_events[step.id].set()

        # Validate DAG: check for missing dependency references.
        all_ids = set(step_map)
        for step in pipeline.steps:
            missing = set(step.depends_on) - all_ids
            if missing:
                raise ValueError(
                    f"Step {step.id!r} depends on unknown step(s): {missing}"
                )

        # Launch all steps concurrently -- each one internally awaits its deps.
        tasks = [asyncio.create_task(_run_step(s), name=f"pipeline-{pipeline.id}-{s.id}")
                 for s in pipeline.steps]

        # Wait for every step to finish.
        await asyncio.gather(*tasks, return_exceptions=True)

        # Determine overall status.
        all_ok = all(r.get("success", False) for r in results.values())
        await self._publish_progress(
            pipeline,
            "completed" if all_ok else "completed_with_errors",
            {"results_summary": {sid: r["status"] for sid, r in results.items()}},
        )

        return results

    # ------------------------------------------------------------------
    # Bus helpers
    # ------------------------------------------------------------------

    async def _publish_progress(
        self,
        pipeline: Pipeline,
        event: str,
        data: dict[str, Any],
    ) -> None:
        """Publish a pipeline progress event to the bus."""
        try:
            await self._bus.publish(
                f"pipeline.{pipeline.id}.{event}",
                BusMessage(
                    type=MessageType.STATUS_UPDATE,
                    payload={
                        "pipeline_id": pipeline.id,
                        "pipeline_name": pipeline.name,
                        "event": event,
                        **data,
                    },
                    source="pipeline",
                ),
            )
        except Exception:
            log.exception(
                "pipeline %r: failed to publish progress event %r",
                pipeline.id,
                event,
            )
