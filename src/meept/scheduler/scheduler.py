"""APScheduler wrapper with async-first interface and bus integration."""

from __future__ import annotations

import asyncio
import json
import logging
import uuid
from datetime import datetime, timezone
from pathlib import Path
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
        if trigger == "cron":
            raise ValueError(
                "Cron triggers are not supported without apscheduler. "
                "Install apscheduler or use an interval trigger instead."
            )
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
        raise ValueError(
            f"Cron trigger for job {job.id!r} is not supported without apscheduler. "
            f"Install apscheduler or use an interval trigger instead."
        )

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

    def __init__(self, config: Any, bus: Any, data_dir: Path | None = None) -> None:
        self._config = config
        self._bus = bus
        self._scheduler: AsyncIOScheduler | _FallbackScheduler | None = None
        self._started = False
        self._data_dir = data_dir
        self._persisted_jobs: dict[str, dict[str, Any]] = {}

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

        # Restore persisted jobs.
        self._load_persisted_jobs()

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
    # Bus subscribers
    # ------------------------------------------------------------------

    async def subscribe_to_bus(self) -> None:
        """Subscribe to scheduler RPC topics on the bus.

        Handles ``scheduler.list_jobs`` and ``scheduler.add_job`` messages
        and publishes responses to ``scheduler.result``.
        """
        self._bus.subscribe("scheduler.list_jobs", self._handle_bus_list_jobs)
        self._bus.subscribe("scheduler.add_job", self._handle_bus_add_job)
        log.info("scheduler: subscribed to bus topics")

    async def _handle_bus_list_jobs(self, _topic: str, msg: BusMessage) -> None:
        """Handle a list-jobs request from the bus."""
        jobs = self.list_jobs()
        await self._bus.publish(
            "scheduler.result",
            BusMessage(
                type=MessageType.SCHEDULE_RESULT,
                payload={"jobs": jobs},
                source="scheduler",
                reply_to=msg.id,
            ),
        )

    async def _handle_bus_add_job(self, _topic: str, msg: BusMessage) -> None:
        """Handle an add-job request from the bus."""
        payload = msg.payload
        name = payload.get("name", "")
        trigger = payload.get("trigger", "interval")
        trigger_args = payload.get("trigger_args", {})
        task_description = payload.get("task_description")
        max_retries = payload.get("max_retries", 0)
        retry_delay = payload.get("retry_delay", 1.0)

        job_id = payload.get("job_id") or f"bus-{uuid.uuid4().hex[:8]}"

        try:
            if task_description:
                self.add_agent_job(
                    job_id=job_id,
                    task_description=task_description,
                    trigger=trigger,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                    **trigger_args,
                )
            else:
                # No handler provided via bus -- create a no-op placeholder.
                self.add_job(
                    job_id=job_id,
                    func=lambda: None,
                    trigger=trigger,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                    **trigger_args,
                )

            await self._bus.publish(
                "scheduler.result",
                BusMessage(
                    type=MessageType.SCHEDULE_RESULT,
                    payload={"success": True, "job_id": job_id, "name": name},
                    source="scheduler",
                    reply_to=msg.id,
                ),
            )
        except Exception as exc:
            await self._bus.publish(
                "scheduler.result",
                BusMessage(
                    type=MessageType.SCHEDULE_RESULT,
                    payload={"success": False, "error": str(exc)},
                    source="scheduler",
                    reply_to=msg.id,
                ),
            )

    # ------------------------------------------------------------------
    # Job management
    # ------------------------------------------------------------------

    def add_job(
        self,
        job_id: str,
        func: Callable[..., Any],
        trigger: str,
        max_retries: int = 0,
        retry_delay: float = 1.0,
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
        max_retries:
            Number of times to retry on failure (0 = no retries).
        retry_delay:
            Base delay in seconds between retries (exponential backoff).
        **trigger_args:
            Keyword arguments forwarded to the trigger constructor (e.g.
            ``hours=6`` for an interval trigger).
        """
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")

        wrapped = self._wrap_handler(
            job_id, func,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )

        if _HAS_APSCHEDULER:
            trg = _make_trigger(trigger, **trigger_args)
            self._scheduler.add_job(wrapped, trigger=trg, id=job_id, replace_existing=True)
        else:
            self._scheduler.add_job(wrapped, trigger=trigger, id=job_id, **trigger_args)

        log.info("scheduler: added job %r (trigger=%s)", job_id, trigger)
        return job_id

    def add_agent_job(
        self,
        job_id: str,
        task_description: str,
        trigger: str,
        skill_hint: str | None = None,
        max_retries: int = 0,
        retry_delay: float = 1.0,
        **trigger_args: Any,
    ) -> str:
        """Add a job that publishes a CHAT_REQUEST when triggered.

        Parameters
        ----------
        job_id:
            Unique identifier for the job.
        task_description:
            Text to send as the chat request when the job fires.
        trigger:
            Trigger type (``"cron"``, ``"interval"``, or ``"date"``).
        skill_hint:
            Optional skill name hint passed in the payload.
        max_retries:
            Number of retries on failure.
        retry_delay:
            Base retry delay in seconds.
        **trigger_args:
            Forwarded to the trigger constructor.
        """
        bus = self._bus

        async def _agent_handler() -> None:
            await bus.publish(
                "chat.request",
                BusMessage(
                    type=MessageType.CHAT_REQUEST,
                    payload={
                        "text": task_description,
                        "conversation_id": f"scheduled-{job_id}",
                        "scheduled_job_id": job_id,
                        "skill_hint": skill_hint,
                    },
                    source="scheduler",
                ),
            )

        result = self.add_job(
            job_id=job_id,
            func=_agent_handler,
            trigger=trigger,
            max_retries=max_retries,
            retry_delay=retry_delay,
            **trigger_args,
        )

        # Persist so this job survives restarts.
        self._persist_job(job_id, {
            "type": "agent",
            "task_description": task_description,
            "trigger": trigger,
            "trigger_args": trigger_args,
            "skill_hint": skill_hint,
            "max_retries": max_retries,
            "retry_delay": retry_delay,
        })

        return result

    def remove_job(self, job_id: str) -> None:
        """Remove a scheduled job by id."""
        if self._scheduler is None:
            raise RuntimeError("Scheduler has not been started")
        self._scheduler.remove_job(job_id)
        self._unpersist_job(job_id)
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

    # ------------------------------------------------------------------
    # Job persistence
    # ------------------------------------------------------------------

    @property
    def _jobs_file(self) -> Path | None:
        if self._data_dir is None:
            return None
        return self._data_dir / "scheduled_jobs.json"

    def _persist_job(self, job_id: str, definition: dict[str, Any]) -> None:
        """Save a job definition so it can be restored after restart."""
        self._persisted_jobs[job_id] = definition
        self._write_jobs_file()

    def _unpersist_job(self, job_id: str) -> None:
        """Remove a persisted job definition."""
        self._persisted_jobs.pop(job_id, None)
        self._write_jobs_file()

    def _write_jobs_file(self) -> None:
        path = self._jobs_file
        if path is None:
            return
        try:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(self._persisted_jobs, indent=2), encoding="utf-8")
        except Exception:
            log.warning("scheduler: failed to write jobs file %s", path, exc_info=True)

    def _load_persisted_jobs(self) -> None:
        """Restore previously persisted jobs."""
        path = self._jobs_file
        if path is None or not path.exists():
            return
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            if not isinstance(data, dict):
                return
            for job_id, defn in data.items():
                if not isinstance(defn, dict):
                    continue
                job_type = defn.get("type", "")
                if job_type == "agent":
                    try:
                        self.add_agent_job(
                            job_id=job_id,
                            task_description=defn["task_description"],
                            trigger=defn.get("trigger", "interval"),
                            skill_hint=defn.get("skill_hint"),
                            max_retries=defn.get("max_retries", 0),
                            retry_delay=defn.get("retry_delay", 1.0),
                            **defn.get("trigger_args", {}),
                        )
                        log.info("scheduler: restored persisted job %r", job_id)
                    except Exception:
                        log.warning("scheduler: failed to restore job %r", job_id, exc_info=True)
            self._persisted_jobs = data
        except Exception:
            log.warning("scheduler: failed to load jobs file %s", path, exc_info=True)

    def _wrap_handler(
        self,
        job_id: str,
        func: Callable[..., Any],
        max_retries: int = 0,
        retry_delay: float = 1.0,
    ) -> Callable[..., Any]:
        """Wrap *func* so that its result is published to the bus.

        When *max_retries* > 0, failed executions are retried with
        exponential backoff before the failure is published.
        """
        bus = self._bus

        async def _wrapped() -> None:
            error: str | None = None
            result: Any = None
            max_attempts = 1 + max_retries

            for attempt in range(max_attempts):
                if attempt > 0:
                    delay = retry_delay * (2 ** (attempt - 1))
                    log.info(
                        "scheduler: retrying job %r (attempt %d/%d, delay=%.1fs)",
                        job_id, attempt + 1, max_attempts, delay,
                    )
                    await asyncio.sleep(delay)

                try:
                    res = func()
                    if asyncio.iscoroutine(res):
                        res = await res
                    result = res
                    error = None
                    break  # Success.
                except Exception as exc:
                    error = f"{type(exc).__name__}: {exc}"
                    log.exception("scheduler: job %r failed (attempt %d)", job_id, attempt + 1)

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
                        type=MessageType.SCHEDULE_RESULT,
                        payload=payload,
                        source="scheduler",
                    ),
                )
            except Exception:
                log.exception("scheduler: failed to publish result for job %r", job_id)

        return _wrapped
