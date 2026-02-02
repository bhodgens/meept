"""Memory consolidation -- compacts and summarises old memories.

The :class:`MemoryConsolidator` periodically processes the memory store:

1. Fetches old episodic memories (older than a configurable threshold).
2. Groups them by date and topic using the LLM.
3. Creates summary memories and archives the originals.
4. Identifies and removes duplicate task memories.
"""

from __future__ import annotations

import json
import logging
from datetime import datetime, timedelta, timezone
from typing import Any

from meept.models.memory_types import MemoryResult, MemoryStats

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# LLM prompts
# ---------------------------------------------------------------------------

_SUMMARISE_SYSTEM_PROMPT = """\
You are a memory consolidation assistant.  Given a list of conversation
memories (each with a timestamp and content), produce a concise JSON array
of summary objects.

Each summary object has:
  - "topic": a short label (2-5 words)
  - "summary": a 1-3 sentence summary of the group
  - "ids": list of memory ids that were merged into this summary

Group by topic similarity first, then by date proximity.  A single group
should contain no more than ~20 source memories.

Output ONLY valid JSON -- no commentary, no code fences.
"""


# ---------------------------------------------------------------------------
# Public class
# ---------------------------------------------------------------------------


class MemoryConsolidator:
    """Compacts episodic and task memory stores.

    Parameters
    ----------
    memory_manager:
        A fully-initialised :class:`~meept.memory.manager.MemoryManager`.
    llm_client:
        An :class:`~meept.llm.client.LLMClient` used for summarisation.
        When ``None`` a simpler date-based consolidation is performed that
        does not require an LLM.
    """

    def __init__(self, memory_manager: Any, llm_client: Any = None) -> None:
        # Import type only for isinstance check at call sites, not at module
        # level, so that this module can be imported independently.
        self._manager = memory_manager
        self._llm_client = llm_client

    # ------------------------------------------------------------------
    # Main entry point
    # ------------------------------------------------------------------

    async def consolidate(self, older_than_hours: int = 24) -> dict[str, Any]:
        """Run the full consolidation pipeline.

        Parameters
        ----------
        older_than_hours:
            Only episodic memories older than this many hours will be
            considered for summarisation.

        Returns
        -------
        dict
            Summary report with keys ``"episodic_archived"``,
            ``"summaries_created"``, ``"duplicates_removed"``.
        """
        report: dict[str, Any] = {
            "episodic_archived": 0,
            "summaries_created": 0,
            "duplicates_removed": 0,
        }

        # -- Episodic consolidation --
        episodic = self._manager.episodic
        if episodic is not None:
            cutoff = datetime.now(timezone.utc) - timedelta(hours=older_than_hours)
            old_memories = await episodic.get_old_memories(cutoff, limit=500)

            if old_memories:
                summaries = await self._summarise_memories(old_memories)
                archived_ids: list[str] = []

                for summary in summaries:
                    topic = summary.get("topic", "misc")
                    text = summary.get("summary", "")
                    ids = summary.get("ids", [])

                    if text:
                        await episodic.store(
                            content=text,
                            category=f"summary:{topic}",
                            metadata={"consolidated_from": ids, "type": "summary"},
                        )
                        report["summaries_created"] += 1

                    archived_ids.extend(ids)

                if archived_ids:
                    deleted = await episodic.delete_by_ids(archived_ids)
                    report["episodic_archived"] = deleted

                logger.info(
                    "Episodic consolidation: archived %d memories, created %d summaries.",
                    report["episodic_archived"],
                    report["summaries_created"],
                )

        # -- Task deduplication --
        task = self._manager.task
        if task is not None:
            dup_groups = await task.find_duplicates()
            ids_to_remove: list[str] = []
            for group in dup_groups:
                # Keep the first (oldest), remove the rest.
                ids_to_remove.extend(group[1:])

            if ids_to_remove:
                removed = await task.delete_by_ids(ids_to_remove)
                report["duplicates_removed"] = removed
                logger.info("Task deduplication: removed %d duplicates.", removed)

        logger.info("Consolidation complete: %s", report)
        return report

    # ------------------------------------------------------------------
    # Stats
    # ------------------------------------------------------------------

    async def get_stats(self) -> MemoryStats:
        """Return aggregate memory statistics.

        Delegates to :meth:`MemoryManager.get_stats`.
        """
        return await self._manager.get_stats()

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _summarise_memories(
        self, memories: list[MemoryResult]
    ) -> list[dict[str, Any]]:
        """Group and summarise a batch of memories.

        When an LLM is available, it is asked to cluster and summarise.
        Otherwise a simple date-based grouping is used.
        """
        if self._llm_client is not None:
            result = await self._summarise_via_llm(memories)
            if result is not None:
                return result

        # Fallback: group by date (one summary per day).
        return self._summarise_by_date(memories)

    async def _summarise_via_llm(
        self, memories: list[MemoryResult]
    ) -> list[dict[str, Any]] | None:
        """Ask the LLM to cluster and summarise."""
        from meept.llm.models import ChatMessage, Role

        # Build a text block of memories for the LLM.
        lines: list[str] = []
        for mem in memories:
            ts = mem.item.created_at.isoformat(timespec="seconds")
            lines.append(f"[{mem.item.id}] ({ts}) {mem.item.content}")
        memories_text = "\n".join(lines)

        messages = [
            ChatMessage(role=Role.SYSTEM, content=_SUMMARISE_SYSTEM_PROMPT),
            ChatMessage(
                role=Role.USER,
                content=f"Memories to consolidate:\n\n{memories_text}",
            ),
        ]

        try:
            response = await self._llm_client.chat(
                messages, temperature=0.3, max_tokens=2048
            )
            content = response.content
            if content:
                # Strip potential markdown fences.
                content = content.strip()
                if content.startswith("```"):
                    content = content.split("\n", 1)[-1]
                if content.endswith("```"):
                    content = content.rsplit("```", 1)[0]
                summaries = json.loads(content.strip())
                if isinstance(summaries, list):
                    return summaries
        except (json.JSONDecodeError, Exception):
            logger.warning(
                "LLM summarisation failed -- falling back to date grouping.",
                exc_info=True,
            )
        return None

    @staticmethod
    def _summarise_by_date(
        memories: list[MemoryResult],
    ) -> list[dict[str, Any]]:
        """Simple date-based grouping when no LLM is available.

        Groups memories by calendar date and produces a brief concatenation
        of their content as the "summary".
        """
        groups: dict[str, list[MemoryResult]] = {}
        for mem in memories:
            day_key = mem.item.created_at.strftime("%Y-%m-%d")
            groups.setdefault(day_key, []).append(mem)

        summaries: list[dict[str, Any]] = []
        for day_key, mems in sorted(groups.items()):
            ids = [m.item.id for m in mems]
            # Build a compact summary from the first ~500 chars of each.
            snippets: list[str] = []
            total_chars = 0
            for m in mems:
                snippet = m.item.content[:200].replace("\n", " ").strip()
                if snippet:
                    snippets.append(snippet)
                    total_chars += len(snippet)
                if total_chars > 2000:
                    snippets.append(f"... and {len(mems) - len(snippets)} more")
                    break

            summary_text = (
                f"Consolidated memories from {day_key} "
                f"({len(mems)} items): " + "; ".join(snippets)
            )
            summaries.append(
                {"topic": day_key, "summary": summary_text, "ids": ids}
            )
        return summaries
