"""Google Calendar API wrapper with async interface."""

from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass
from datetime import datetime, timezone
from functools import partial
from typing import Any

from meept.calendar.auth import CalendarAuth

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Conditional import for the Google API client.
# ---------------------------------------------------------------------------

_GOOGLE_INSTALL_HINT = (
    "Google Calendar integration requires the optional 'calendar' extras. "
    "Install them with:  pip install 'meept[calendar]'  "
    "(needs google-api-python-client and google-auth-oauthlib)"
)

try:
    from googleapiclient.discovery import build as _build_service

    _HAS_GOOGLE = True
except ImportError:
    _HAS_GOOGLE = False
    _build_service = None  # type: ignore[assignment]


# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class CalendarEvent:
    """Lightweight representation of a Google Calendar event.

    Attributes
    ----------
    id:
        Unique event identifier from the Google Calendar API.
    summary:
        Event title.
    start:
        Start time as a timezone-aware :class:`datetime`.
    end:
        End time as a timezone-aware :class:`datetime`.
    description:
        Free-text description / notes.
    location:
        Human-readable location string.
    status:
        Event status (``"confirmed"``, ``"tentative"``, or ``"cancelled"``).
    """

    id: str
    summary: str
    start: datetime
    end: datetime
    description: str = ""
    location: str = ""
    status: str = "confirmed"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _parse_event_datetime(raw: dict[str, Any]) -> datetime:
    """Parse the ``start`` or ``end`` dict from the Calendar API.

    The API returns either ``{"dateTime": "..."}`` for timed events or
    ``{"date": "YYYY-MM-DD"}`` for all-day events.
    """
    if "dateTime" in raw:
        return datetime.fromisoformat(raw["dateTime"])
    if "date" in raw:
        # All-day events have no time component -- normalise to midnight UTC.
        return datetime.fromisoformat(raw["date"]).replace(tzinfo=timezone.utc)
    raise ValueError(f"Cannot parse event datetime from {raw!r}")


def _event_from_resource(resource: dict[str, Any]) -> CalendarEvent:
    """Convert a raw Google Calendar API resource dict into a :class:`CalendarEvent`."""
    return CalendarEvent(
        id=resource["id"],
        summary=resource.get("summary", "(no title)"),
        start=_parse_event_datetime(resource.get("start", {})),
        end=_parse_event_datetime(resource.get("end", {})),
        description=resource.get("description", ""),
        location=resource.get("location", ""),
        status=resource.get("status", "confirmed"),
    )


def _datetime_to_rfc3339(dt: datetime) -> str:
    """Format a :class:`datetime` for the Google Calendar API.

    If the datetime is naive it is assumed to be UTC.
    """
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.isoformat()


# ---------------------------------------------------------------------------
# Main class
# ---------------------------------------------------------------------------


class GoogleCalendar:
    """Async wrapper around the Google Calendar v3 REST API.

    All Google API calls are synchronous under the hood (the official client
    library does not support ``asyncio``).  This class transparently
    off-loads every call to :func:`asyncio.loop.run_in_executor` so that
    the event loop is never blocked.

    Parameters
    ----------
    auth:
        A :class:`CalendarAuth` instance that provides valid credentials.
    calendar_id:
        The calendar to operate on.  Defaults to the user's primary
        calendar.
    """

    def __init__(self, auth: CalendarAuth, calendar_id: str = "primary") -> None:
        if not _HAS_GOOGLE:
            raise ImportError(_GOOGLE_INSTALL_HINT)

        self._auth = auth
        self._calendar_id = calendar_id
        self._service: Any | None = None

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _get_service(self) -> Any:
        """Lazily build (and cache) the Google Calendar service object."""
        if self._service is not None:
            return self._service

        creds = await self._auth.get_credentials()
        loop = asyncio.get_running_loop()
        self._service = await loop.run_in_executor(
            None,
            partial(_build_service, "calendar", "v3", credentials=creds),
        )
        return self._service

    async def _run_sync(self, func: Any, *args: Any, **kwargs: Any) -> Any:
        """Run a synchronous callable in the default executor."""
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(None, partial(func, *args, **kwargs))

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    async def list_events(
        self,
        start: datetime,
        end: datetime,
        max_results: int = 50,
    ) -> list[CalendarEvent]:
        """Return events between *start* and *end* (inclusive).

        Parameters
        ----------
        start / end:
            Time window boundaries.  Naive datetimes are treated as UTC.
        max_results:
            Maximum number of events to return.
        """
        service = await self._get_service()

        def _fetch() -> list[dict[str, Any]]:
            result = (
                service.events()
                .list(
                    calendarId=self._calendar_id,
                    timeMin=_datetime_to_rfc3339(start),
                    timeMax=_datetime_to_rfc3339(end),
                    maxResults=max_results,
                    singleEvents=True,
                    orderBy="startTime",
                )
                .execute()
            )
            return result.get("items", [])

        items = await self._run_sync(_fetch)
        events = [_event_from_resource(item) for item in items]
        log.debug("gcal: listed %d events between %s and %s", len(events), start, end)
        return events

    async def create_event(
        self,
        summary: str,
        start: datetime,
        end: datetime,
        description: str = "",
        location: str = "",
    ) -> CalendarEvent:
        """Create a new calendar event and return its representation.

        Parameters
        ----------
        summary:
            Event title.
        start / end:
            Event time boundaries.  Naive datetimes are treated as UTC.
        description:
            Optional long-form description.
        location:
            Optional human-readable location string.
        """
        service = await self._get_service()

        body: dict[str, Any] = {
            "summary": summary,
            "start": {"dateTime": _datetime_to_rfc3339(start)},
            "end": {"dateTime": _datetime_to_rfc3339(end)},
        }
        if description:
            body["description"] = description
        if location:
            body["location"] = location

        def _insert() -> dict[str, Any]:
            return (
                service.events()
                .insert(calendarId=self._calendar_id, body=body)
                .execute()
            )

        resource = await self._run_sync(_insert)
        event = _event_from_resource(resource)
        log.info("gcal: created event %r (%s)", event.summary, event.id)
        return event

    async def update_event(self, event_id: str, **kwargs: Any) -> CalendarEvent:
        """Update fields of an existing event and return the new state.

        Supported keyword arguments: ``summary``, ``start``, ``end``,
        ``description``, ``location``, ``status``.  Datetime values should
        be :class:`datetime` objects.
        """
        service = await self._get_service()

        # Build the patch body from caller kwargs.
        body: dict[str, Any] = {}
        for key in ("summary", "description", "location", "status"):
            if key in kwargs:
                body[key] = kwargs[key]
        if "start" in kwargs:
            body["start"] = {"dateTime": _datetime_to_rfc3339(kwargs["start"])}
        if "end" in kwargs:
            body["end"] = {"dateTime": _datetime_to_rfc3339(kwargs["end"])}

        if not body:
            raise ValueError("update_event requires at least one field to update")

        def _patch() -> dict[str, Any]:
            return (
                service.events()
                .patch(calendarId=self._calendar_id, eventId=event_id, body=body)
                .execute()
            )

        resource = await self._run_sync(_patch)
        event = _event_from_resource(resource)
        log.info("gcal: updated event %s", event_id)
        return event

    async def delete_event(self, event_id: str) -> None:
        """Delete a calendar event by its id."""
        service = await self._get_service()

        def _delete() -> None:
            service.events().delete(
                calendarId=self._calendar_id, eventId=event_id
            ).execute()

        await self._run_sync(_delete)
        log.info("gcal: deleted event %s", event_id)

    async def get_upcoming(self, hours: int = 24) -> list[CalendarEvent]:
        """Convenience method: return events in the next *hours* hours.

        Parameters
        ----------
        hours:
            Look-ahead window from *now*.  Defaults to 24.
        """
        from datetime import timedelta

        now = datetime.now(timezone.utc)
        end = now + timedelta(hours=hours)
        return await self.list_events(start=now, end=end)
