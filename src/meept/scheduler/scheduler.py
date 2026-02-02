"""APScheduler wrapper with async-first interface and bus integration."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
from typing import Any, Callable

from meept.models.messages import BusMessage, MessageType

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional APScheduler import -- fall back to a minimal asyncio scheduler
# if the library is not installed.
# ---------------------------------------------------------------------------

try:
    from apscheduler.schedulers.asyncio import AsyncIOScheduler
    from apscheduler.triggers.cron import CronTrigger
    from apscheduler.triggers.date import DateTrigger
    from apscheduler.triggers.interval import IntervalTrigger

    _HAS_APSCHEDULER = True
except ImportError:
    _HAS_APSCHEDULER = False
    log.warning(
        "apscheduler is not installed -- using a minimal asyncio fallback scheduler. "
        "Install it with: pip install 'apscheduler>=3.11'"
    )


# ---------------------------------------------------------------------------
# Minimal fallback scheduler (asyncio only, no cron support)
# ---------------------------------------------------------------------------


class _FallbackJob:
    """Thin stand-in for an APScheduler job."""

    __slots__ = ("id", "name", "func", "trigger", "trigger_args", "next_run_time", "paused", "_task")

    def __init__(
        self,
        job_id: str,
        func: Callable[..., Any],
        trigger: str,
        **trigger_args: Any,
    ) -> None:
        self.id = job_id
        self.name = job_id
        self.func = func
        self.trigger = trigger
        self.trigger_args = trigger_args
        self.next_run_time: datetime | None = datetime.now(timezone.utc)
        self.paused = False
        self._task: asyncio.Task[None] | None = None


class _FallbackScheduler:
    """Minimal interval/date scheduler built on plain :mod:`asyncio`.

    Supports ``"interval"`` and ``"date"`` triggers.  ``"cron"`` triggers are
    silently degraded to ``"interval"`` with ``seconds=3600`` (1 hour) and a
    warning is logged.
    """

    def __init__(self) -> None:
        self._jobs: dict[str, _FallbackJob] = {}
        self._running = False

    def start(self) -> None:
        self._running = True
        for job in self._jobs.values():
            self._launch(job)

    def shutdown(self, wait: bool = True) -> None:  # noqa: ARG002
        self._running = False
        for job in self._jobs.values():
            if job._task is not None:
                job._task.cancel()
                job._task = None

    # -- job management -----------------------------------------------------

    def add_job(
        self,
        func: Callable[..., Any],
        trigger: str,
        id: str | None = None,  # noqa: A002
        **trigger_args: Any,
    ) -> _FallbackJob:
        job_id = id or f"fallback-{len(self._jobs)}"
        job = _FallbackJob(job_id, func, trigger, **trigger_args)
        self._jobs[job_id] = job
        if self._running:
            self._launch(job)
        return job

    def remove_job(self, job_id: str) -> None:
        job = self._jobs.pop(job_id, None)
        if job is not None and job._task is not None:
            job._task.cancel()
            job._task = None

    def get_job(self, job_id: str) -> _FallbackJob | None:
        return self._jobs.get(job_id)

    def get_jobs(self) -> list[_FallbackJob]:
        return list(self._jobs.values())

    def pause_job(self, job_id: str) -> None:
        job = self._jobs.get(job_id)
        if job is not None:
            job.paused = True
            job.next_run_time = None

    def resume_job(self, job_id: str) -> None:
        job = self._jobs.get(job_id)
        if job is not None:
            job.paused = False
            job.next_run_time = datetime.now(timezone.utc)

    # -- internal -----------------------------------------------------------

    def _launch(self, job: _FallbackJob) -> None:
        if job._task is not None:
            return
        job._task = asyncio.ensure_future(self._run_loop(job))

    async def _run_loop(self, job: _FallbackJob) -> None:
        """Execute the job on the requested schedule."""
        try:
            if job.trigger == "date":
                # One-shot execution.
                await self._invoke(job)
                return

            interval = self._resolve_interval(job)
            while self._running:
                if not job.paused:
                    await self._invoke(job)
                    job.next_run_time = datetime.now(timezone.utc)
                await asyncio.sleep(interval)
        except asyncio.CancelledError:
            pass

    def _resolve_interval(self, job: _FallbackJob) -> float:
        """Return the sleep interval in seconds for *job*."""
        ta = job.trigger_args
        if job.trigger == "interval":
            return (
                ta.get("seconds", 0)
                + ta.get("minutes", 0) * 60
                + ta.get("hours", 0) * 3600
            ) or 60.0
        # cron -> degrade to 1-hour interval
        log.warning(
            "fallback scheduler: cron trigger for job %r degraded to 1-hour interval",
            job.id,
        )
        return 3600.0

    @staticmethod
    async def _invoke(job: _FallbackJob) -> None:
        try:
            result = job.func()
            if asyncio.iscoroutine(result):
                await result
        except Exception:
            log.exception("fallback scheduler: error executing job %r", job.id)


# ---------------------------------------------------------------------------
# Trigger helper
# ---------------------------------------------------------------------------

_TRIGGER_MAP: dict[str, type] = {}
if _HAS_APSCHEDULER:
    _TRIGGER_MAP = {
        "cron": CronTrigger,
        "interval": IntervalTrigger,
        "date": DateTrigger,
    }


def _make_trigger(trigger: str, **kwargs: Any) -> Any:
    """Build an APScheduler trigger instance (only when APScheduler available)."""
    cls = _TRIGGER_MAP.get(trigger)
    if cls is None:
        raise ValueError(
            f"Unknown trigger type {trigger!r}. Valid types: {list(_TRIGGER_MAP)}"
        )
    return cls(**kwargs)


# ---------------------------------------------------------------------------
# Main scheduler class
# ---------------------------------------------------------------------------


class MeeptScheduler:
    """High-level scheduler that delegates to APScheduler or a fallback.

    Parameters
    ----------
    config:
        A :class:`~meept.models.config_schema.SchedulerConfig` (or compatible
        object with ``enabled`` and ``timezone`` attributes).
    bus:
        The application :class:`~meept.core.bus.MessageBus`.
    """

    def __init__(self, config: Any, bus: Any) -> None:
        self._config = config
        self._bus = bus
        self._scheduler: AsyncIOScheduler | _FallbackScheduler | None = None
        self._started = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def start(self) -> None:
        """Initialise and start the underlying scheduler."""
        if self._started:
            return

        if _HAS_APSCHEDULER:
            tz = getattr(self._config, "timezone", "UTC") or "UTC"
            self._scheduler = AsyncIOScheduler(timezone=tz)
        else:
            self._scheduler = _FallbackScheduler()

        self._scheduler.start()
        self._started = True
        log.info(
            "scheduler: started (%s)",
            "apscheduler" if _HAS_APSCHEDULER else "fallback",
        )

    async def stop(self) -> None:
        """Gracefully shut down the scheduler."""
        if not self._started or self._scheduler is None:
            return
        self._scheduler.shutdown(wait=True)
        self._started = False
        log.info("scheduler: stopped")

    # ------------------------------------------------------------------
    # Job management
    # ------------------------------------------------------------------

    def add_job(
        self,
        job_id: str,
        func: Callable[..., Any],
        trigger: str,
        **trigger_args: Any,
    ) -> str:
        """Add a scheduled job and return its *job_id*.

        Parameters
        ----------
        job_id:
            Unique identifier for the job.
        func:
            The callable to execute.  May be a coroutine function.
        trigger:
            One of ``"cron"``, ``"interval"``, or ``"date"``.
        **trigger_args:
            Keyword arguments forwarded to the trigger constructor (e.g.
            ``hours=6`` for an interval trigger).
        """
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")

        wrapped = self._wrap_handler(job_id, func)

        if _HAS_APSCHEDULER:
            trg = _make_trigger(trigger, **trigger_args)
            self._scheduler.add_job(wrapped, trigger=trg, id=job_id, replace_existing=True)
        else:
            self._scheduler.add_job(wrapped, trigger=trigger, id=job_id, **trigger_args)

        log.info("scheduler: added job %r (trigger=%s)", job_id, trigger)
        return job_id

    def remove_job(self, job_id: str) -> None:
        """Remove a scheduled job by id."""
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")
        self._scheduler.remove_job(job_id)
        log.info("scheduler: removed job %r", job_id)

    def list_jobs(self) -> list[dict[str, Any]]:
        """Return a list of dicts describing every scheduled job."""
        if self._scheduler is None:
            return []
        result: list[dict[str, Any]] = []
        for job in self._scheduler.get_jobs():
            nrt = getattr(job, "next_run_time", None)
            result.append(
                {
                    "id": job.id,
                    "name": getattr(job, "name", job.id),
                    "next_run_time": nrt.isoformat() if nrt else None,
                    "paused": nrt is None,
                }
            )
        return result

    def pause_job(self, job_id: str) -> None:
        """Pause a job (it will not fire until resumed)."""
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")
        self._scheduler.pause_job(job_id)
        log.info("scheduler: paused job %r", job_id)

    def resume_job(self, job_id: str) -> None:
        """Resume a previously paused job."""
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")
        self._scheduler.resume_job(job_id)
        log.info("scheduler: resumed job %r", job_id)

    def get_job_status(self, job_id: str) -> dict[str, Any] | None:
        """Return status information for a single job, or ``None``."""
        if self._scheduler is None:
            return None
        job = self._scheduler.get_job(job_id)
        if job is None:
            return None
        nrt = getattr(job, "next_run_time", None)
        return {
            "id": job.id,
            "name": getattr(job, "name", job.id),
            "next_run_time": nrt.isoformat() if nrt else None,
            "paused": nrt is None,
        }

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _wrap_handler(self, job_id: str, func: Callable[..., Any]) -> Callable[..., Any]:
        """Wrap *func* so that its result is published to the bus."""
        bus = self._bus

        async def _wrapped() -> None:
            error: str | None = None
            result: Any = None
            try:
                res = func()
                if asyncio.iscoroutine(res):
                    res = await res
                result = res
            except Exception as exc:
                error = f"{type(exc).__name__}: {exc}"
                log.exception("scheduler: job %r failed", job_id)

            payload: dict[str, Any] = {
                "job_id": job_id,
                "success": error is None,
                "result": result,
                "error": error,
                "timestamp": datetime.now(timezone.utc).isoformat(),
            }
            try:
                await bus.publish(
                    f"scheduler.job.{job_id}",
                    BusMessage(
                        type=MessageType.STATUS_UPDATE,
                        payload=payload,
                        source="scheduler",
                    ),
                )
            except Exception:
                log.exception("scheduler: failed to publish result for job %r", job_id)

        return _wrapped
