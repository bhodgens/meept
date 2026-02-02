"""Memory orchestrator -- routes storage and retrieval across subsystems.

The :class:`MemoryManager` acts as the single entry-point for the rest of
meept to interact with episodic, task, and personality memory.  It delegates
to the specialised backends while providing a unified search API.
"""

from __future__ import annotations

import logging
from pathlib import Path
from typing import Any

from meept.memory.episodic import EpisodicMemory
from meept.memory.personality import PersonalityModel
from meept.memory.task_memory import TaskMemory
from meept.models.config_schema import MemoryConfig
from meept.models.memory_types import MemoryResult, MemoryStats, MemoryType

logger = logging.getLogger(__name__)


class MemoryManager:
    """Unified facade over the episodic, task, and personality subsystems.

    Parameters
    ----------
    config:
        The ``[memory]`` section of the meept configuration.
    llm_client:
        Optional :class:`~meept.llm.client.LLMClient` used for smart
        retrieval and personality evolution.
    """

    def __init__(self, config: MemoryConfig, llm_client: Any = None) -> None:
        self._config = config
        self._llm_client = llm_client

        self._episodic: EpisodicMemory | None = None
        self._task: TaskMemory | None = None
        self._personality: PersonalityModel | None = None
        self._data_dir: Path | None = None
        self._initialized: bool = False

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def initialize(self, data_dir: Path) -> None:
        """Bootstrap every enabled memory subsystem.

        Parameters
        ----------
        data_dir:
            Root directory under which each subsystem creates its own
            subdirectory (e.g. ``data_dir/episodic/``, ``data_dir/task/``).
        """
        self._data_dir = data_dir
        data_dir.mkdir(parents=True, exist_ok=True)

        # -- Episodic --
        if self._config.episodic.enabled:
            self._episodic = EpisodicMemory()
            episodic_dir = data_dir / "episodic"
            await self._episodic.initialize(episodic_dir, llm_client=self._llm_client)
            logger.info("Episodic memory subsystem initialised.")
        else:
            logger.info("Episodic memory disabled by configuration.")

        # -- Task --
        if self._config.task.enabled:
            self._task = TaskMemory()
            task_dir = data_dir / "task"
            await self._task.initialize(task_dir, domains=self._config.task.domains)
            logger.info("Task memory subsystem initialised.")
        else:
            logger.info("Task memory disabled by configuration.")

        # -- Personality --
        if self._config.personality.enabled:
            self._personality = PersonalityModel(
                data_dir=data_dir, llm_client=self._llm_client
            )
            await self._personality.load()
            logger.info("Personality model loaded.")
        else:
            logger.info("Personality model disabled by configuration.")

        self._initialized = True
        logger.info("MemoryManager fully initialised (data_dir=%s).", data_dir)

    # ------------------------------------------------------------------
    # Store
    # ------------------------------------------------------------------

    async def store(
        self,
        content: str,
        memory_type: str = "episodic",
        metadata: dict | None = None,
    ) -> str:
        """Store content in the appropriate memory subsystem.

        Parameters
        ----------
        content:
            The text to store.
        memory_type:
            ``"episodic"`` or ``"task"``.  Determines which backend receives
            the data.
        metadata:
            Optional key/value pairs attached to the memory.  For task
            memories a ``"domain"`` key inside metadata is extracted and used
            as the domain parameter; likewise ``"category"`` is used for
            episodic memories.

        Returns
        -------
        str
            The unique id of the stored memory.

        Raises
        ------
        RuntimeError
            If the manager has not been initialised or the target subsystem
            is disabled.
        ValueError
            If *memory_type* is not ``"episodic"`` or ``"task"``.
        """
        self._ensure_initialized()
        meta = dict(metadata) if metadata else {}

        if memory_type == MemoryType.EPISODIC or memory_type == "episodic":
            if self._episodic is None:
                raise RuntimeError("Episodic memory is disabled.")
            category = meta.pop("category", "conversation")
            return await self._episodic.store(content, category=category, metadata=meta)

        if memory_type == MemoryType.TASK or memory_type == "task":
            if self._task is None:
                raise RuntimeError("Task memory is disabled.")
            domain = meta.pop("domain", "general")
            return await self._task.store(content, domain=domain, metadata=meta)

        raise ValueError(
            f"Unknown memory_type {memory_type!r}. Expected 'episodic' or 'task'."
        )

    # ------------------------------------------------------------------
    # Search
    # ------------------------------------------------------------------

    async def search(
        self,
        query: str,
        memory_type: str | None = None,
        limit: int = 10,
    ) -> list[MemoryResult]:
        """Search one or both memory subsystems.

        Parameters
        ----------
        query:
            Free-text search string.
        memory_type:
            ``"episodic"``, ``"task"``, or ``None`` to search both.
        limit:
            Maximum number of results per subsystem.

        Returns
        -------
        list[MemoryResult]
            Combined results sorted by relevance score (descending).
        """
        self._ensure_initialized()
        results: list[MemoryResult] = []

        search_episodic = memory_type in (None, "episodic", MemoryType.EPISODIC)
        search_task = memory_type in (None, "task", MemoryType.TASK)

        if search_episodic and self._episodic is not None:
            results.extend(await self._episodic.search(query, limit=limit))

        if search_task and self._task is not None:
            results.extend(await self._task.search(query, limit=limit))

        # Sort by relevance descending, then by created_at descending as
        # tie-breaker.
        results.sort(
            key=lambda r: (r.relevance_score, r.item.created_at),
            reverse=True,
        )
        return results[:limit]

    async def get_relevant_context(
        self,
        query: str,
        max_items: int = 20,
    ) -> list[MemoryResult]:
        """Smart retrieval combining episodic and task memory.

        When an LLM client is available the query is optionally expanded to
        improve recall.  Results from both subsystems are interleaved and
        ranked.

        Parameters
        ----------
        query:
            The user's query or topic of interest.
        max_items:
            Maximum total items to return.

        Returns
        -------
        list[MemoryResult]
            Merged, ranked list of memory results.
        """
        self._ensure_initialized()

        # If an LLM is available, attempt to expand the query for better
        # recall before falling back to the raw query.
        expanded_query = await self._maybe_expand_query(query)

        # Allocate budget across subsystems.
        episodic_limit = max_items // 2 or max_items
        task_limit = max_items - episodic_limit or max_items

        results: list[MemoryResult] = []

        if self._episodic is not None:
            ep_results = await self._episodic.search(expanded_query, limit=episodic_limit)
            results.extend(ep_results)

            # Also include very recent memories for conversational continuity
            # (but only those not already in the search results).
            recent = await self._episodic.get_recent(limit=5)
            seen_ids = {r.item.id for r in results}
            for r in recent:
                if r.item.id not in seen_ids:
                    results.append(r)
                    seen_ids.add(r.item.id)

        if self._task is not None:
            task_results = await self._task.search(expanded_query, limit=task_limit)
            seen_ids = {r.item.id for r in results}
            for r in task_results:
                if r.item.id not in seen_ids:
                    results.append(r)
                    seen_ids.add(r.item.id)

        # Final sort: relevance descending, recency as tie-breaker.
        results.sort(
            key=lambda r: (r.relevance_score, r.item.created_at),
            reverse=True,
        )
        return results[:max_items]

    # ------------------------------------------------------------------
    # Stats
    # ------------------------------------------------------------------

    async def get_stats(self) -> MemoryStats:
        """Aggregate statistics across all subsystems."""
        self._ensure_initialized()

        ep_count = 0
        task_count = 0
        oldest: list[Any] = []
        newest: list[Any] = []

        if self._episodic is not None:
            ep_count = await self._episodic.count()
            ts = await self._episodic.get_oldest_timestamp()
            if ts:
                oldest.append(ts)
            ts = await self._episodic.get_newest_timestamp()
            if ts:
                newest.append(ts)

        if self._task is not None:
            task_count = await self._task.count()
            ts = await self._task.get_oldest_timestamp()
            if ts:
                oldest.append(ts)
            ts = await self._task.get_newest_timestamp()
            if ts:
                newest.append(ts)

        return MemoryStats(
            total_count=ep_count + task_count,
            episodic_count=ep_count,
            task_count=task_count,
            oldest=min(oldest) if oldest else None,
            newest=max(newest) if newest else None,
        )

    # ------------------------------------------------------------------
    # Subsystem accessors
    # ------------------------------------------------------------------

    @property
    def episodic(self) -> EpisodicMemory | None:
        """Direct access to the episodic memory subsystem (or ``None``)."""
        return self._episodic

    @property
    def task(self) -> TaskMemory | None:
        """Direct access to the task memory subsystem (or ``None``)."""
        return self._task

    @property
    def personality(self) -> PersonalityModel | None:
        """Direct access to the personality model (or ``None``)."""
        return self._personality

    @property
    def config(self) -> MemoryConfig:
        """The memory configuration this manager was built with."""
        return self._config

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------

    async def close(self) -> None:
        """Gracefully shut down every subsystem."""
        if self._episodic is not None:
            await self._episodic.close()
            self._episodic = None

        if self._task is not None:
            await self._task.close()
            self._task = None

        if self._personality is not None:
            await self._personality.save()
            self._personality = None

        self._initialized = False
        logger.info("MemoryManager closed.")

    # ------------------------------------------------------------------
    # Context-manager support
    # ------------------------------------------------------------------

    async def __aenter__(self) -> MemoryManager:
        return self

    async def __aexit__(self, *exc: object) -> None:
        await self.close()

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _ensure_initialized(self) -> None:
        if not self._initialized:
            raise RuntimeError(
                "MemoryManager has not been initialised. "
                "Call ``await initialize(data_dir)`` first."
            )

    async def _maybe_expand_query(self, query: str) -> str:
        """Use the LLM to expand a search query for better recall.

        Returns the original query if no LLM is available or the expansion
        fails.
        """
        if self._llm_client is None:
            return query

        from meept.llm.models import ChatMessage, Role

        messages = [
            ChatMessage(
                role=Role.SYSTEM,
                content=(
                    "You are a search query expander.  Given a user query, "
                    "output a single expanded version that includes synonyms "
                    "and related terms to improve full-text search recall.  "
                    "Output ONLY the expanded query -- no explanation."
                ),
            ),
            ChatMessage(role=Role.USER, content=query),
        ]

        try:
            response = await self._llm_client.chat(
                messages, temperature=0.3, max_tokens=128
            )
            expanded = response.content
            if expanded and expanded.strip():
                logger.debug("Query expanded: %r -> %r", query, expanded.strip())
                return expanded.strip()
        except Exception:
            logger.debug("Query expansion failed; using original query.", exc_info=True)

        return query
