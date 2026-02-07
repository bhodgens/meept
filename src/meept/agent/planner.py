"""Task decomposition and planning.

The :class:`Planner` uses an LLM call to break complex, multi-step user
requests into an ordered sequence of :class:`TaskStep` objects.  The
agent loop can then execute each step individually, tracking progress
and handling failures per-step rather than treating the whole request
as a monolith.
"""

from __future__ import annotations

import asyncio
import json
import logging
import re
import uuid
from typing import Any

from meept.models.tasks import TaskStep, TaskStatus

log = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Heuristic complexity indicators
# ---------------------------------------------------------------------------

_COMPLEXITY_KEYWORDS: list[str] = [
    "and then",
    "after that",
    "first",
    "second",
    "third",
    "finally",
    "next",
    "step 1",
    "step 2",
    "1.",
    "2.",
    "3.",
    "also",
    "additionally",
    "meanwhile",
    "in addition",
    "once you",
    "once that",
    "before you",
    "make sure to",
]

_MULTI_QUESTION_RE = re.compile(r"\?\s*\S", re.MULTILINE)

# Minimum message length before we even consider planning.
_MIN_LENGTH_FOR_PLAN = 80


# ---------------------------------------------------------------------------
# Planning prompt
# ---------------------------------------------------------------------------

_PLAN_SYSTEM_PROMPT = """\
You are a task planner for an autonomous assistant. Given a user request,
break it down into discrete, sequential steps. Each step should be a
single action that can be performed by one tool call.

Return your plan as a JSON array of objects with these fields:
- "id": a short unique identifier (e.g. "step_1", "step_2")
- "description": what this step accomplishes
- "tool_hint": the tool most likely needed (or null if unsure). Available
  tools include: shell, file_read, file_write, file_delete, list_directory,
  web_search, web_fetch
- "depends_on": array of step ids that must complete first (empty array if none)
- "skill_hint": (optional) name of a skill best suited for this step, or null

Return ONLY the JSON array, no explanation or markdown fences.
"""


# ---------------------------------------------------------------------------
# Planner class
# ---------------------------------------------------------------------------


class Planner:
    """Decomposes complex user requests into ordered task steps.

    Parameters
    ----------
    llm_client:
        An object with an async ``chat(messages, **kwargs)`` method that
        returns an :class:`~meept.llm.models.LLMResponse`.
    skill_names:
        Optional list of available skill names for skill-aware planning.
    """

    def __init__(self, llm_client: Any, skill_names: list[str] | None = None) -> None:
        self._llm = llm_client
        self._skill_names = skill_names or []

    async def decompose(self, task_description: str) -> list[TaskStep]:
        """Break *task_description* into an ordered list of steps.

        Uses an LLM call to reason about task decomposition. Falls back
        to a single-step plan if the LLM response cannot be parsed.

        Parameters
        ----------
        task_description:
            The full user message / task to decompose.

        Returns
        -------
        list[TaskStep]
            Ordered steps, each with an id, description, and optional
            tool hint and dependency list.
        """
        from meept.llm.models import ChatMessage, Role

        system_prompt = _PLAN_SYSTEM_PROMPT
        if self._skill_names:
            system_prompt += (
                "\nAvailable skills: " + ", ".join(self._skill_names) + "\n"
                "Use the \"skill_hint\" field to assign steps to the most appropriate skill.\n"
            )

        messages = [
            ChatMessage(role=Role.SYSTEM, content=system_prompt),
            ChatMessage(role=Role.USER, content=task_description),
        ]

        try:
            response = await self._llm.chat(messages)
        except (asyncio.CancelledError, KeyboardInterrupt):
            raise
        except Exception:
            log.warning("LLM call failed during planning; falling back to single step", exc_info=True)
            return self._single_step(task_description)

        content = response.content or ""
        return self._parse_plan(content, task_description)

    async def should_plan(self, message: str) -> bool:
        """Heuristic check whether *message* is complex enough to warrant planning.

        This does **not** call the LLM -- it uses simple text analysis.

        Parameters
        ----------
        message:
            The user's message.

        Returns
        -------
        bool
            ``True`` if the message appears to describe a multi-step task.
        """
        if len(message) < _MIN_LENGTH_FOR_PLAN:
            return False

        message_lower = message.lower()

        # Check for complexity keywords.
        keyword_hits = sum(
            1 for kw in _COMPLEXITY_KEYWORDS if kw in message_lower
        )
        if keyword_hits >= 2:
            return True

        # Check for multiple questions.
        question_marks = message.count("?")
        if question_marks >= 2:
            return True

        # Check for numbered/bulleted lists.
        list_pattern = re.compile(r"(?:^|\n)\s*(?:\d+[.):]|\-|\*)\s+", re.MULTILINE)
        list_items = list_pattern.findall(message)
        if len(list_items) >= 2:
            return True

        return False

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    def _parse_plan(self, content: str, fallback_desc: str) -> list[TaskStep]:
        """Parse the LLM's JSON response into TaskStep objects."""
        # Try to extract JSON array from the response.
        content = content.strip()

        # Strip markdown fences if present.
        if content.startswith("```"):
            lines = content.split("\n")
            # Remove first and last fence lines.
            lines = [
                line for line in lines
                if not line.strip().startswith("```")
            ]
            content = "\n".join(lines).strip()

        try:
            raw_steps: list[dict[str, Any]] = json.loads(content)
        except json.JSONDecodeError:
            # Try to find a JSON array somewhere in the response.
            match = re.search(r"\[.*\]", content, re.DOTALL)
            if match:
                try:
                    raw_steps = json.loads(match.group())
                except json.JSONDecodeError:
                    log.warning("Could not parse plan JSON; falling back to single step")
                    return self._single_step(fallback_desc)
            else:
                log.warning("No JSON array found in plan response; falling back")
                return self._single_step(fallback_desc)

        if not isinstance(raw_steps, list) or not raw_steps:
            return self._single_step(fallback_desc)

        steps: list[TaskStep] = []
        for raw in raw_steps:
            if not isinstance(raw, dict):
                continue
            step = TaskStep(
                id=str(raw.get("id", uuid.uuid4().hex[:12])),
                description=str(raw.get("description", "")),
                tool_hint=raw.get("tool_hint"),
                skill_name=raw.get("skill_hint"),
                depends_on=raw.get("depends_on", []),
                status=TaskStatus.PENDING,
            )
            steps.append(step)

        return steps if steps else self._single_step(fallback_desc)

    @staticmethod
    def _single_step(description: str) -> list[TaskStep]:
        """Create a trivial single-step plan."""
        return [
            TaskStep(
                id="step_1",
                description=description,
                tool_hint=None,
                depends_on=[],
                status=TaskStatus.PENDING,
            )
        ]
