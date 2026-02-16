"""Data models for the memory subsystem."""

from __future__ import annotations

import enum
from dataclasses import dataclass, field
from datetime import datetime, timezone


# ---------------------------------------------------------------------------
# Enums
# ---------------------------------------------------------------------------


class MemoryType(str, enum.Enum):
    """Classification of memory storage subsystems."""

    EPISODIC = "episodic"
    TASK = "task"
    PERSONALITY = "personality"


# ---------------------------------------------------------------------------
# Core data structures
# ---------------------------------------------------------------------------


@dataclass(slots=True)
class MemoryItem:
    """A single stored memory entry.

    Parameters
    ----------
    id:
        Unique identifier for this memory (typically a UUID hex string).
    content:
        The textual content of the memory.
    memory_type:
        Which subsystem owns this memory.
    category:
        A finer-grained label within the memory type (e.g. ``"conversation"``,
        ``"code"``, ``"commands"``).
    metadata:
        Arbitrary key/value data attached to the memory.
    created_at:
        When the memory was first stored.
    updated_at:
        When the memory was last modified (``None`` if never updated).
    """

    id: str
    content: str
    memory_type: MemoryType
    category: str = ""
    metadata: dict = field(default_factory=dict)
    created_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    updated_at: datetime | None = None


@dataclass(slots=True)
class MemoryResult:
    """A memory item returned from a search, annotated with relevance info.

    Parameters
    ----------
    item:
        The underlying memory entry.
    relevance_score:
        A ``[0.0, 1.0]`` score indicating how well this memory matches the
        query.  ``1.0`` means a perfect match; ``0.0`` means no relevance
        signal (e.g. chronological retrieval).
    source:
        Human-readable label for the subsystem that produced this result
        (e.g. ``"episodic"``, ``"task:code"``).
    """

    item: MemoryItem
    relevance_score: float = 0.0
    source: str = ""


@dataclass(slots=True)
class MemoryQuery:
    """Describes a search request against the memory system.

    Parameters
    ----------
    query:
        Free-text search string.
    memory_type:
        Restrict results to a single subsystem, or ``None`` for all.
    category:
        Restrict results to a single category, or ``None`` for all.
    limit:
        Maximum number of results to return.
    min_relevance:
        Discard results with a relevance score below this threshold.
    """

    query: str
    memory_type: MemoryType | None = None
    category: str | None = None
    limit: int = 10
    min_relevance: float = 0.0


@dataclass(slots=True)
class MemoryStats:
    """Aggregate statistics about stored memories.

    Parameters
    ----------
    total_count:
        Total number of memory items across all subsystems.
    episodic_count:
        Number of episodic (conversation) memories.
    task_count:
        Number of task (technical/domain) memories.
    oldest:
        Timestamp of the oldest memory, or ``None`` if the store is empty.
    newest:
        Timestamp of the newest memory, or ``None`` if the store is empty.
    """

    total_count: int = 0
    episodic_count: int = 0
    task_count: int = 0
    oldest: datetime | None = None
    newest: datetime | None = None
