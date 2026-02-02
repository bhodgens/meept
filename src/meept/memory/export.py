"""Human-reviewable export of the memory store.

Produces either structured Markdown or JSON files containing every memory
entry with full metadata so a human operator can audit, edit, or archive
the bot's knowledge base.
"""

from __future__ import annotations

import json
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from meept.models.memory_types import MemoryResult, MemoryType

logger = logging.getLogger(__name__)


class MemoryExporter:
    """Export memories to Markdown or JSON for human review.

    Parameters
    ----------
    memory_manager:
        A fully-initialised :class:`~meept.memory.manager.MemoryManager`.
    """

    def __init__(self, memory_manager: Any) -> None:
        self._manager = memory_manager

    # ------------------------------------------------------------------
    # Markdown export
    # ------------------------------------------------------------------

    async def export_markdown(
        self,
        output_path: Path,
        memory_type: str | None = None,
    ) -> Path:
        """Export memories as a structured Markdown document.

        Parameters
        ----------
        output_path:
            Destination file path.
        memory_type:
            ``"episodic"``, ``"task"``, or ``None`` for all.

        Returns
        -------
        Path
            The path that was written to.
        """
        sections: list[str] = []
        now_iso = datetime.now(timezone.utc).isoformat(timespec="seconds")

        sections.append(f"# Meept Memory Export\n\nGenerated: {now_iso}\n")

        # -- Episodic --
        if memory_type in (None, "episodic", MemoryType.EPISODIC):
            episodic = self._manager.episodic
            if episodic is not None:
                results = await episodic.get_recent(limit=10_000)
                sections.append(self._render_section_md("Episodic Memories", results))

        # -- Task --
        if memory_type in (None, "task", MemoryType.TASK):
            task = self._manager.task
            if task is not None:
                results = await self._get_all_task_memories()
                sections.append(self._render_section_md("Task Memories", results))

        # -- Personality --
        if memory_type in (None, "personality", MemoryType.PERSONALITY):
            personality = self._manager.personality
            if personality is not None:
                sections.append("## Personality Profile\n")
                sections.append(personality.get_description())
                sections.append("")

        # -- Stats --
        stats = await self._manager.get_stats()
        sections.append("## Statistics\n")
        sections.append(f"- Total memories: {stats.total_count}")
        sections.append(f"- Episodic: {stats.episodic_count}")
        sections.append(f"- Task: {stats.task_count}")
        if stats.oldest:
            sections.append(f"- Oldest: {stats.oldest.isoformat(timespec='seconds')}")
        if stats.newest:
            sections.append(f"- Newest: {stats.newest.isoformat(timespec='seconds')}")
        sections.append("")

        content = "\n".join(sections)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(content, encoding="utf-8")
        logger.info("Exported memories to Markdown: %s", output_path)
        return output_path

    # ------------------------------------------------------------------
    # JSON export
    # ------------------------------------------------------------------

    async def export_json(
        self,
        output_path: Path,
        memory_type: str | None = None,
    ) -> Path:
        """Export memories as a JSON file.

        Parameters
        ----------
        output_path:
            Destination file path.
        memory_type:
            ``"episodic"``, ``"task"``, or ``None`` for all.

        Returns
        -------
        Path
            The path that was written to.
        """
        export_data: dict[str, Any] = {
            "exported_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
            "episodic": [],
            "task": [],
            "personality": None,
            "stats": {},
        }

        # -- Episodic --
        if memory_type in (None, "episodic", MemoryType.EPISODIC):
            episodic = self._manager.episodic
            if episodic is not None:
                results = await episodic.get_recent(limit=10_000)
                export_data["episodic"] = [
                    self._result_to_dict(r) for r in results
                ]

        # -- Task --
        if memory_type in (None, "task", MemoryType.TASK):
            task = self._manager.task
            if task is not None:
                results = await self._get_all_task_memories()
                export_data["task"] = [self._result_to_dict(r) for r in results]

        # -- Personality --
        if memory_type in (None, "personality", MemoryType.PERSONALITY):
            personality = self._manager.personality
            if personality is not None:
                export_data["personality"] = {
                    "description": personality.get_description(),
                    "interaction_count": personality.interaction_count,
                    "last_updated": (
                        personality.last_updated.isoformat(timespec="seconds")
                        if personality.last_updated
                        else None
                    ),
                }

        # -- Stats --
        stats = await self._manager.get_stats()
        export_data["stats"] = {
            "total_count": stats.total_count,
            "episodic_count": stats.episodic_count,
            "task_count": stats.task_count,
            "oldest": (
                stats.oldest.isoformat(timespec="seconds") if stats.oldest else None
            ),
            "newest": (
                stats.newest.isoformat(timespec="seconds") if stats.newest else None
            ),
        }

        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(
            json.dumps(export_data, indent=2, ensure_ascii=False),
            encoding="utf-8",
        )
        logger.info("Exported memories to JSON: %s", output_path)
        return output_path

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _get_all_task_memories(self) -> list[MemoryResult]:
        """Retrieve all task memories across domains via search fallback.

        Since ``TaskMemory`` does not expose a ``get_recent`` spanning all
        domains, we search with an empty-ish query that the FTS fallback
        will treat as "return most recent".
        """
        task = self._manager.task
        if task is None:
            return []
        return await task.search(query="*", limit=10_000)

    @staticmethod
    def _render_section_md(
        title: str, results: list[MemoryResult]
    ) -> str:
        """Render a list of memory results as a Markdown section."""
        lines: list[str] = [f"## {title}\n"]

        if not results:
            lines.append("_No memories in this category._\n")
            return "\n".join(lines)

        for r in results:
            item = r.item
            ts = item.created_at.isoformat(timespec="seconds")
            lines.append(f"### {item.id[:12]}... ({ts})")
            lines.append("")
            lines.append(f"- **Type:** {item.memory_type.value}")
            lines.append(f"- **Category:** {item.category}")
            if item.metadata:
                meta_str = json.dumps(item.metadata, ensure_ascii=False)
                lines.append(f"- **Metadata:** `{meta_str}`")
            if r.relevance_score > 0:
                lines.append(f"- **Relevance:** {r.relevance_score:.4f}")
            lines.append(f"- **Source:** {r.source}")
            lines.append("")
            # Content block -- indent as blockquote for readability.
            for content_line in item.content.split("\n"):
                lines.append(f"> {content_line}")
            lines.append("")

        return "\n".join(lines)

    @staticmethod
    def _result_to_dict(r: MemoryResult) -> dict[str, Any]:
        """Serialise a ``MemoryResult`` to a plain dict for JSON export."""
        item = r.item
        return {
            "id": item.id,
            "content": item.content,
            "memory_type": item.memory_type.value,
            "category": item.category,
            "metadata": item.metadata,
            "created_at": item.created_at.isoformat(timespec="seconds"),
            "updated_at": (
                item.updated_at.isoformat(timespec="seconds")
                if item.updated_at
                else None
            ),
            "relevance_score": r.relevance_score,
            "source": r.source,
        }
