"""Message types used on the internal message bus."""

from __future__ import annotations

import enum
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any


class MessageType(enum.Enum):
    """Enumeration of every message kind that may traverse the bus."""

    CHAT_REQUEST = "chat_request"
    CHAT_RESPONSE = "chat_response"
    AGENT_ACTION = "agent_action"
    AGENT_RESULT = "agent_result"
    MEMORY_QUERY = "memory_query"
    MEMORY_RESULT = "memory_result"
    STATUS_UPDATE = "status_update"
    CONFIG_RELOAD = "config_reload"
    SHUTDOWN = "shutdown"
    TRIAGE_RESULT = "triage_result"
    SKILL_TASK_START = "skill_task_start"
    SKILL_TASK_COMPLETE = "skill_task_complete"
    PIPELINE_PROGRESS = "pipeline_progress"
    PIPELINE_COMPLETE = "pipeline_complete"
    CHAT_PROGRESS = "chat_progress"
    SCHEDULE_REQUEST = "schedule_request"
    SCHEDULE_RESULT = "schedule_result"


@dataclass(slots=True)
class BusMessage:
    """A single message that flows through the :class:`MessageBus`.

    Parameters
    ----------
    type:
        Semantic kind of this message.
    payload:
        Arbitrary data dictionary carried by the message.
    source:
        Identifier of the component that created the message (e.g.
        ``"agent"``, ``"telegram"``, ``"scheduler"``).
    id:
        Globally-unique message identifier.  Auto-generated when omitted.
    timestamp:
        UTC creation time.  Auto-set to *now* when omitted.
    reply_to:
        Optional id of a prior message this one responds to.
    """

    type: MessageType
    payload: dict[str, Any]
    source: str
    id: str = field(default_factory=lambda: uuid.uuid4().hex)
    timestamp: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    reply_to: str | None = None
