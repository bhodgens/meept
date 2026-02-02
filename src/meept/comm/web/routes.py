"""API route definitions for the meept web interface.

Provides :func:`create_router` which returns a configured
:class:`~fastapi.APIRouter` with all meept REST endpoints.
"""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
from typing import TYPE_CHECKING, Annotated, Any

from pydantic import BaseModel, Field

from meept.models.messages import BusMessage, MessageType

from .auth import require_auth

if TYPE_CHECKING:
    from meept.core.bus import MessageBus
    from meept.core.config import MeeptConfig

# ---------------------------------------------------------------------------
# Conditional import of FastAPI
# ---------------------------------------------------------------------------

try:
    from fastapi import APIRouter, Depends, HTTPException, Query, status
except ImportError as _exc:
    raise ImportError(
        "The 'fastapi' package is required for the web interface. "
        "Install it with:  pip install fastapi"
    ) from _exc

log = logging.getLogger(__name__)

# Timeout (seconds) for agent chat responses.
_CHAT_TIMEOUT: float = 120.0

# ---------------------------------------------------------------------------
# Request / Response models
# ---------------------------------------------------------------------------


class ChatRequest(BaseModel):
    """Body for ``POST /api/chat``."""

    message: str = Field(..., min_length=1, max_length=32_000)


class ChatResponse(BaseModel):
    """Response from ``POST /api/chat``."""

    reply: str
    message_id: str


class StatusResponse(BaseModel):
    """Response from ``GET /api/status``."""

    status: str
    uptime_seconds: float | None = None
    bus_subscribers: int = 0


class MemorySearchResult(BaseModel):
    """A single memory item in search results."""

    id: str
    content: str
    memory_type: str
    category: str = ""
    relevance_score: float = 0.0


class MemorySearchResponse(BaseModel):
    """Response from ``GET /api/memory/search``."""

    results: list[MemorySearchResult]
    total: int


class MemoryExportRequest(BaseModel):
    """Body for ``POST /api/memory/export``."""

    format: str = Field(
        default="markdown",
        pattern="^(markdown|json)$",
        description="Export format: 'markdown' or 'json'",
    )


class MemoryExportResponse(BaseModel):
    """Response from ``POST /api/memory/export``."""

    status: str
    message: str


class JobItem(BaseModel):
    """Representation of a scheduled job."""

    id: str
    name: str
    schedule: str
    action: str
    args: dict[str, Any] = Field(default_factory=dict)
    enabled: bool = True


class JobListResponse(BaseModel):
    """Response from ``GET /api/jobs``."""

    jobs: list[JobItem]


class JobCreateRequest(BaseModel):
    """Body for ``POST /api/jobs``."""

    name: str = Field(..., min_length=1, max_length=256)
    schedule: str = Field(..., min_length=1, description="Cron expression")
    action: str = Field(..., min_length=1)
    args: dict[str, Any] = Field(default_factory=dict)
    enabled: bool = True


class JobCreateResponse(BaseModel):
    """Response from ``POST /api/jobs``."""

    id: str
    message: str


class JobDeleteResponse(BaseModel):
    """Response from ``DELETE /api/jobs/{job_id}``."""

    message: str


class ConfigReloadResponse(BaseModel):
    """Response from ``POST /api/config/reload``."""

    status: str
    message: str


class HealthResponse(BaseModel):
    """Response from ``GET /api/health``."""

    status: str
    timestamp: str


# ---------------------------------------------------------------------------
# Router factory
# ---------------------------------------------------------------------------


def create_router(bus: MessageBus, config: MeeptConfig) -> APIRouter:
    """Build and return the API router with all meept endpoints.

    Parameters
    ----------
    bus:
        The shared :class:`MessageBus` for publishing commands and queries.
    config:
        The loaded :class:`MeeptConfig` instance.
    """
    router = APIRouter()

    # In-flight chat requests: message_id -> Future[str]
    pending: dict[str, asyncio.Future[str]] = {}

    # Track daemon start time for uptime reporting.
    _start_time = datetime.now(timezone.utc)

    # ------------------------------------------------------------------
    # Bus callback for chat responses
    # ------------------------------------------------------------------

    async def _on_chat_response(_topic: str, msg: BusMessage) -> None:
        """Resolve the matching pending future when a response arrives."""
        reply_to = msg.reply_to
        if reply_to is None:
            return
        future = pending.get(reply_to)
        if future is not None and not future.done():
            future.set_result(msg.payload.get("text", ""))

    bus.subscribe("chat.response", _on_chat_response)

    # ------------------------------------------------------------------
    # Health (no auth)
    # ------------------------------------------------------------------

    @router.get(
        "/api/health",
        response_model=HealthResponse,
        tags=["health"],
    )
    async def health() -> HealthResponse:
        """Simple health-check endpoint -- no authentication required."""
        return HealthResponse(
            status="ok",
            timestamp=datetime.now(timezone.utc).isoformat(),
        )

    # ------------------------------------------------------------------
    # Chat
    # ------------------------------------------------------------------

    @router.post(
        "/api/chat",
        response_model=ChatResponse,
        tags=["chat"],
    )
    async def chat(
        body: ChatRequest,
        user_id: str = Depends(require_auth),
    ) -> ChatResponse:
        """Send a message and receive the agent's response."""
        request_msg = BusMessage(
            type=MessageType.CHAT_REQUEST,
            payload={
                "text": body.message,
                "source_channel": "web",
                "user_id": user_id,
            },
            source="web",
        )

        loop = asyncio.get_running_loop()
        future: asyncio.Future[str] = loop.create_future()
        pending[request_msg.id] = future

        await bus.publish("chat.request", request_msg)

        try:
            reply_text = await asyncio.wait_for(future, timeout=_CHAT_TIMEOUT)
        except asyncio.TimeoutError:
            raise HTTPException(
                status_code=status.HTTP_504_GATEWAY_TIMEOUT,
                detail="Agent did not respond within the time limit",
            )
        finally:
            pending.pop(request_msg.id, None)

        return ChatResponse(reply=reply_text, message_id=request_msg.id)

    # ------------------------------------------------------------------
    # Status
    # ------------------------------------------------------------------

    @router.get(
        "/api/status",
        response_model=StatusResponse,
        tags=["status"],
    )
    async def get_status(
        user_id: str = Depends(require_auth),
    ) -> StatusResponse:
        """Return current daemon status information."""
        uptime = (datetime.now(timezone.utc) - _start_time).total_seconds()
        return StatusResponse(
            status="running",
            uptime_seconds=round(uptime, 2),
            bus_subscribers=bus.subscriber_count,
        )

    # ------------------------------------------------------------------
    # Memory
    # ------------------------------------------------------------------

    @router.get(
        "/api/memory/search",
        response_model=MemorySearchResponse,
        tags=["memory"],
    )
    async def search_memory(
        user_id: str = Depends(require_auth),
        q: str = Query(..., min_length=1, description="Search query"),
        type: str | None = Query(
            default=None, description="Memory type filter (episodic, task, personality)"
        ),
        limit: int = Query(default=10, ge=1, le=100, description="Max results"),
    ) -> MemorySearchResponse:
        """Search stored memories by text query."""
        request_msg = BusMessage(
            type=MessageType.MEMORY_QUERY,
            payload={
                "query": q,
                "memory_type": type,
                "limit": limit,
            },
            source="web",
        )

        loop = asyncio.get_running_loop()
        future: asyncio.Future[str] = loop.create_future()
        pending[request_msg.id] = future

        await bus.publish("memory.query", request_msg)

        try:
            raw_response = await asyncio.wait_for(future, timeout=15.0)
        except asyncio.TimeoutError:
            # Return empty results rather than failing when memory system
            # is not available.
            return MemorySearchResponse(results=[], total=0)
        finally:
            pending.pop(request_msg.id, None)

        # Parse the raw response into structured results.
        results: list[MemorySearchResult] = []
        try:
            import json

            parsed = json.loads(raw_response) if isinstance(raw_response, str) else raw_response
            if isinstance(parsed, dict):
                items = parsed.get("results", parsed.get("items", []))
            elif isinstance(parsed, list):
                items = parsed
            else:
                items = []

            for item in items:
                if isinstance(item, dict):
                    results.append(
                        MemorySearchResult(
                            id=str(item.get("id", "")),
                            content=str(item.get("content", "")),
                            memory_type=str(item.get("memory_type", "unknown")),
                            category=str(item.get("category", "")),
                            relevance_score=float(item.get("relevance_score", 0.0)),
                        )
                    )
        except (json.JSONDecodeError, TypeError, ValueError):
            log.debug("Could not parse memory search response: %s", raw_response)

        return MemorySearchResponse(results=results, total=len(results))

    @router.post(
        "/api/memory/export",
        response_model=MemoryExportResponse,
        tags=["memory"],
    )
    async def export_memory(
        body: MemoryExportRequest,
        user_id: str = Depends(require_auth),
    ) -> MemoryExportResponse:
        """Trigger an export of all memories in the requested format."""
        request_msg = BusMessage(
            type=MessageType.AGENT_ACTION,
            payload={
                "action": "memory_export",
                "format": body.format,
            },
            source="web",
        )
        await bus.publish("agent.action", request_msg)

        return MemoryExportResponse(
            status="accepted",
            message=f"Memory export in '{body.format}' format has been queued.",
        )

    # ------------------------------------------------------------------
    # Jobs
    # ------------------------------------------------------------------

    @router.get(
        "/api/jobs",
        response_model=JobListResponse,
        tags=["jobs"],
    )
    async def list_jobs(
        user_id: str = Depends(require_auth),
    ) -> JobListResponse:
        """List all scheduled jobs."""
        request_msg = BusMessage(
            type=MessageType.STATUS_UPDATE,
            payload={"request": "job_list"},
            source="web",
        )

        loop = asyncio.get_running_loop()
        future: asyncio.Future[str] = loop.create_future()
        pending[request_msg.id] = future

        await bus.publish("scheduler.list", request_msg)

        try:
            raw = await asyncio.wait_for(future, timeout=10.0)
        except asyncio.TimeoutError:
            # Scheduler may not be running; return empty list.
            return JobListResponse(jobs=[])
        finally:
            pending.pop(request_msg.id, None)

        # Parse the raw response into structured job items.
        jobs: list[JobItem] = []
        try:
            import json

            parsed = json.loads(raw) if isinstance(raw, str) else raw
            if isinstance(parsed, dict):
                items = parsed.get("jobs", [])
            elif isinstance(parsed, list):
                items = parsed
            else:
                items = []

            for item in items:
                if isinstance(item, dict):
                    jobs.append(
                        JobItem(
                            id=str(item.get("id", "")),
                            name=str(item.get("name", "")),
                            schedule=str(item.get("schedule", item.get("trigger", ""))),
                            action=str(item.get("action", "")),
                            args=item.get("args", {}),
                            enabled=not item.get("paused", False),
                        )
                    )
        except (json.JSONDecodeError, TypeError, ValueError):
            log.debug("Could not parse job list response: %s", raw)

        return JobListResponse(jobs=jobs)

    @router.post(
        "/api/jobs",
        response_model=JobCreateResponse,
        status_code=status.HTTP_201_CREATED,
        tags=["jobs"],
    )
    async def create_job(
        body: JobCreateRequest,
        user_id: str = Depends(require_auth),
    ) -> JobCreateResponse:
        """Add a new scheduled job."""
        import uuid

        job_id = uuid.uuid4().hex[:16]

        request_msg = BusMessage(
            type=MessageType.AGENT_ACTION,
            payload={
                "action": "job_create",
                "job_id": job_id,
                "name": body.name,
                "schedule": body.schedule,
                "job_action": body.action,
                "args": body.args,
                "enabled": body.enabled,
            },
            source="web",
        )
        await bus.publish("scheduler.add", request_msg)

        return JobCreateResponse(
            id=job_id,
            message=f"Job '{body.name}' created with id {job_id}.",
        )

    @router.delete(
        "/api/jobs/{job_id}",
        response_model=JobDeleteResponse,
        tags=["jobs"],
    )
    async def delete_job(
        job_id: str,
        user_id: str = Depends(require_auth),
    ) -> JobDeleteResponse:
        """Remove a scheduled job by id."""
        request_msg = BusMessage(
            type=MessageType.AGENT_ACTION,
            payload={
                "action": "job_delete",
                "job_id": job_id,
            },
            source="web",
        )
        await bus.publish("scheduler.remove", request_msg)

        return JobDeleteResponse(
            message=f"Job {job_id} removal requested.",
        )

    # ------------------------------------------------------------------
    # Config
    # ------------------------------------------------------------------

    @router.post(
        "/api/config/reload",
        response_model=ConfigReloadResponse,
        tags=["config"],
    )
    async def reload_config(
        user_id: str = Depends(require_auth),
    ) -> ConfigReloadResponse:
        """Trigger a configuration reload."""
        request_msg = BusMessage(
            type=MessageType.CONFIG_RELOAD,
            payload={},
            source="web",
        )
        await bus.publish("control.config_reload", request_msg)

        return ConfigReloadResponse(
            status="ok",
            message="Configuration reload triggered.",
        )

    return router
