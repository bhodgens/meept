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
    max_retries:
        Maximum number of retry attempts on failure (0 means no retries).
    retry_delay:
        Base delay in seconds between retries (exponential backoff applied).
    context_key:
        Key under which this step's result is stored in the shared context
        dict.  Defaults to the step id if not set.
    """

    id: str
    name: str
    handler: Callable[..., Any]
    depends_on: list[str] = field(default_factory=list)
    timeout: float = 300.0
    max_retries: int = 0
    retry_delay: float = 1.0
    context_key: str | None = None


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
        self._cancelled = False
        self._running_tasks: list[asyncio.Task[None]] = []

    async def execute(self, pipeline: Pipeline) -> dict[str, dict[str, Any]]:
        """Run all steps in *pipeline* and return a results dict.

        Returns
        -------
        dict[str, dict]
            Mapping of ``step_id`` to a result dict with keys ``success``,
            ``result``, ``error``, ``duration``, and ``status``.
        """
        self._cancelled = False
        self._running_tasks = []

        results: dict[str, dict[str, Any]] = {}
        context: dict[str, Any] = {}
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
            if self._cancelled:
                results[step.id] = _make_result(
                    success=False, error="pipeline cancelled", status="cancelled",
                )
                done_events[step.id].set()
                return

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

            # All dependencies satisfied -- execute with retry support.
            await self._publish_progress(
                pipeline, "step_started", {"step_id": step.id, "step_name": step.name}
            )

            max_attempts = 1 + step.max_retries
            last_error: str | None = None
            total_duration = 0.0

            for attempt in range(max_attempts):
                if self._cancelled:
                    results[step.id] = _make_result(
                        success=False, error="pipeline cancelled", status="cancelled",
                    )
                    done_events[step.id].set()
                    return

                if attempt > 0:
                    delay = step.retry_delay * (2 ** (attempt - 1))
                    log.info(
                        "pipeline %r: retrying step %r (attempt %d/%d, delay=%.1fs)",
                        pipeline.id, step.id, attempt + 1, max_attempts, delay,
                    )
                    await self._publish_progress(
                        pipeline, "step_retrying",
                        {"step_id": step.id, "attempt": attempt + 1, "delay": delay},
                    )
                    await asyncio.sleep(delay)

                t0 = time.monotonic()
                try:
                    res = step.handler(context)
                    if asyncio.iscoroutine(res):
                        res = await asyncio.wait_for(res, timeout=step.timeout)
                    duration = time.monotonic() - t0
                    total_duration += duration

                    # Store result in context under the step's context_key.
                    ctx_key = step.context_key or step.id
                    context[ctx_key] = res

                    results[step.id] = _make_result(
                        success=True,
                        result=res,
                        duration=total_duration,
                    )
                    await self._publish_progress(
                        pipeline,
                        "step_completed",
                        {"step_id": step.id, "duration": round(total_duration, 4)},
                    )
                    done_events[step.id].set()
                    return  # Success -- exit retry loop.

                except asyncio.TimeoutError:
                    duration = time.monotonic() - t0
                    total_duration += duration
                    last_error = f"step {step.id!r} timed out after {step.timeout}s"
                    log.error("pipeline %r: %s", pipeline.id, last_error)

                except asyncio.CancelledError:
                    results[step.id] = _make_result(
                        success=False, error="pipeline cancelled", status="cancelled",
                    )
                    done_events[step.id].set()
                    return

                except Exception as exc:
                    duration = time.monotonic() - t0
                    total_duration += duration
                    last_error = f"{type(exc).__name__}: {exc}"
                    log.exception("pipeline %r: step %r failed", pipeline.id, step.id)

            # All attempts exhausted.
            status = "timeout" if last_error and "timed out" in last_error else "failed"
            results[step.id] = _make_result(
                success=False,
                error=last_error,
                duration=total_duration,
                status=status,
            )
            await self._publish_progress(
                pipeline, "step_failed", {"step_id": step.id, "error": last_error}
            )
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
        self._running_tasks = tasks

        # Wait for every step to finish.
        await asyncio.gather(*tasks, return_exceptions=True)
        self._running_tasks = []

        # Determine overall status.
        all_ok = all(r.get("success", False) for r in results.values())
        await self._publish_progress(
            pipeline,
            "completed" if all_ok else "completed_with_errors",
            {"results_summary": {sid: r["status"] for sid, r in results.items()}},
        )

        return results

    def cancel(self) -> None:
        """Cancel a running pipeline.

        Sets the cancelled flag so new steps will not start, and cancels
        any currently running asyncio tasks.
        """
        self._cancelled = True
        for task in self._running_tasks:
            if not task.done():
                task.cancel()

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
                    type=MessageType.PIPELINE_PROGRESS,
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
