"""Self-model / personality tracking for the meept bot.

Maintains a Markdown document (``personality.md``) that captures the bot's
evolving communication style, observed expertise areas, creator preferences,
and recurring themes.  When an LLM client is available the personality
description is updated automatically by asking the model to integrate new
interaction summaries.
"""

from __future__ import annotations

import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Default personality template
# ---------------------------------------------------------------------------

_DEFAULT_PERSONALITY = """\
# Meept Personality Profile

## Communication Style
- Clear, concise, and helpful
- Adapts formality to match the conversation partner
- Prefers actionable answers over vague suggestions

## Areas of Expertise Observed
- General-purpose assistance

## Creator Preferences
- No specific preferences recorded yet

## Recurring Themes
- None observed yet

## Interaction Notes
- Profile initialised; will evolve with further interactions.
"""

# ---------------------------------------------------------------------------
# LLM prompt for personality evolution
# ---------------------------------------------------------------------------

_UPDATE_SYSTEM_PROMPT = """\
You are a self-reflective assistant maintaining a personality profile document.
Given the current profile and a summary of recent interactions, produce an
UPDATED version of the profile in Markdown.

Rules:
1. Preserve the section headings exactly (## Communication Style, ## Areas of
   Expertise Observed, ## Creator Preferences, ## Recurring Themes,
   ## Interaction Notes).
2. Only add or refine bullet points -- never remove information unless it
   directly contradicts new evidence.
3. Keep bullet points concise (one line each).
4. The document should remain under 80 lines.
5. Output ONLY the updated Markdown -- no commentary or code fences.
"""


# ---------------------------------------------------------------------------
# Public class
# ---------------------------------------------------------------------------


class PersonalityModel:
    """Manages the bot's evolving self-model as a Markdown personality file.

    Parameters
    ----------
    data_dir:
        Directory where ``personality.md`` lives.
    llm_client:
        Optional :class:`~meept.llm.client.LLMClient` used to evolve the
        profile via summarisation.  Without it, the profile is static.
    """

    def __init__(self, data_dir: Path, llm_client: Any = None) -> None:
        self._data_dir = data_dir
        self._llm_client = llm_client
        self._file_path = data_dir / "personality.md"
        self._description: str = _DEFAULT_PERSONALITY
        self._interaction_count: int = 0
        self._last_updated: datetime | None = None

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    async def load(self) -> None:
        """Load the personality profile from disk, or create the default."""
        self._data_dir.mkdir(parents=True, exist_ok=True)

        if self._file_path.exists():
            self._description = self._file_path.read_text(encoding="utf-8")
            logger.info("Loaded personality profile from %s.", self._file_path)
        else:
            self._description = _DEFAULT_PERSONALITY
            await self.save()
            logger.info("Created default personality profile at %s.", self._file_path)

    # ------------------------------------------------------------------
    # Update
    # ------------------------------------------------------------------

    async def update(self, interaction_summary: str) -> None:
        """Integrate a new interaction summary into the personality profile.

        If an LLM client is available the model is asked to rewrite the
        profile to incorporate the new evidence.  Otherwise the summary is
        appended verbatim under **Interaction Notes**.

        Parameters
        ----------
        interaction_summary:
            A short textual summary of a recent interaction or set of
            interactions (e.g. ``"User asked about Python asyncio patterns;
            prefers code examples over prose."``).
        """
        self._interaction_count += 1
        now_iso = datetime.now(timezone.utc).isoformat(timespec="seconds")

        if self._llm_client is not None:
            updated = await self._update_via_llm(interaction_summary)
            if updated:
                self._description = updated
                self._last_updated = datetime.now(timezone.utc)
                await self.save()
                logger.info("Personality profile updated via LLM (interaction #%d).", self._interaction_count)
                return

        # Fallback: append the summary under Interaction Notes.
        note_line = f"- [{now_iso}] {interaction_summary}"
        self._description = _append_to_section(
            self._description, "## Interaction Notes", note_line
        )
        self._last_updated = datetime.now(timezone.utc)
        await self.save()
        logger.info(
            "Personality profile updated with manual note (interaction #%d).",
            self._interaction_count,
        )

    # ------------------------------------------------------------------
    # Accessors
    # ------------------------------------------------------------------

    def get_description(self) -> str:
        """Return the current personality description as Markdown."""
        return self._description

    @property
    def interaction_count(self) -> int:
        """Number of ``update()`` calls since last load."""
        return self._interaction_count

    @property
    def last_updated(self) -> datetime | None:
        """Timestamp of the most recent ``update()`` call."""
        return self._last_updated

    # ------------------------------------------------------------------
    # Persistence
    # ------------------------------------------------------------------

    async def save(self) -> None:
        """Persist the current personality profile to disk."""
        self._data_dir.mkdir(parents=True, exist_ok=True)
        self._file_path.write_text(self._description, encoding="utf-8")
        logger.debug("Saved personality profile to %s.", self._file_path)

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    async def _update_via_llm(self, interaction_summary: str) -> str | None:
        """Ask the LLM to produce an updated personality profile.

        Returns the new Markdown string on success, or ``None`` on failure.
        """
        # Import here to avoid circular dependency at module level.
        from meept.llm.models import ChatMessage, Role

        messages = [
            ChatMessage(role=Role.SYSTEM, content=_UPDATE_SYSTEM_PROMPT),
            ChatMessage(
                role=Role.USER,
                content=(
                    f"## Current Profile\n\n{self._description}\n\n"
                    f"---\n\n"
                    f"## Recent Interaction Summary\n\n{interaction_summary}\n\n"
                    f"---\n\n"
                    f"Please produce the updated personality profile."
                ),
            ),
        ]

        try:
            response = await self._llm_client.chat(messages, temperature=0.4, max_tokens=2048)
            content = response.content
            if content and content.strip():
                return content.strip()
        except Exception:
            logger.warning(
                "LLM-based personality update failed -- falling back to manual append.",
                exc_info=True,
            )
        return None


# ---------------------------------------------------------------------------
# Module-level helpers
# ---------------------------------------------------------------------------


def _append_to_section(document: str, heading: str, line: str) -> str:
    """Append *line* after the last bullet under *heading* in a Markdown doc.

    If the heading is not found, the line is appended at the end.
    """
    lines = document.split("\n")
    insert_index: int | None = None
    in_section = False

    for i, raw_line in enumerate(lines):
        stripped = raw_line.strip()
        if stripped == heading:
            in_section = True
            insert_index = i + 1
            continue
        if in_section:
            # Another heading means we left the target section.
            if stripped.startswith("## "):
                break
            # Track last non-empty line within the section.
            if stripped:
                insert_index = i + 1

    if insert_index is not None:
        lines.insert(insert_index, line)
    else:
        # Heading not found -- append at end.
        lines.append("")
        lines.append(heading)
        lines.append(line)

    return "\n".join(lines)
