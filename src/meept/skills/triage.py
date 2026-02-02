"""Triage agent -- classifies user intent via a fast/cheap LLM call.

The :class:`TriageAgent` builds a dynamic system prompt from loaded skill
definitions and asks the LLM to return a structured JSON classification.
"""

from __future__ import annotations

import json
import logging
from typing import Any

from meept.skills.models import SkillDefinition, TriageResult

log = logging.getLogger(__name__)

_TRIAGE_SYSTEM_PROMPT_TEMPLATE = """\
You are a message classifier. Given a user message, determine which skill \
should handle it. Respond with ONLY a JSON object (no markdown fences):

{{
  "skill_name": "<skill name or 'default'>",
  "confidence": <0.0 to 1.0>,
  "extracted_params": {{}},
  "reasoning": "<brief explanation>"
}}

Available skills:
{skills_block}

If no skill is a good match, use skill_name "default" with low confidence.
"""


class TriageAgent:
    """Classifies user messages to determine which skill should handle them.

    Parameters
    ----------
    llm_client:
        An object with ``async chat(messages, **kw) -> LLMResponse``.
        Should be backed by a fast/cheap model.
    skills:
        List of available skill definitions used to build the classification
        prompt.
    confidence_threshold:
        Minimum confidence to accept a skill match.  Below this value the
        result is marked as ``fallback_to_default=True``.
    """

    def __init__(
        self,
        llm_client: Any,
        skills: list[SkillDefinition],
        confidence_threshold: float = 0.5,
    ) -> None:
        self._llm = llm_client
        self._skills = skills
        self._threshold = confidence_threshold
        self._system_prompt = self._build_system_prompt()

    def _build_system_prompt(self) -> str:
        """Build the triage system prompt from skill definitions."""
        lines: list[str] = []
        for skill in self._skills:
            parts = [f'- **{skill.name}**: {skill.description}']
            if skill.trigger_keywords:
                parts.append(f'  Keywords: {", ".join(skill.trigger_keywords)}')
            if skill.examples:
                parts.append(f'  Examples: {"; ".join(skill.examples[:3])}')
            lines.append("\n".join(parts))

        skills_block = "\n".join(lines) if lines else "No skills loaded."
        return _TRIAGE_SYSTEM_PROMPT_TEMPLATE.format(skills_block=skills_block)

    async def classify(self, message: str) -> TriageResult:
        """Classify a user message and return a :class:`TriageResult`.

        Falls back to ``"default"`` on any LLM or parsing error.
        """
        from meept.llm.models import ChatMessage, Role

        messages = [
            ChatMessage(role=Role.SYSTEM, content=self._system_prompt),
            ChatMessage(role=Role.USER, content=message),
        ]

        try:
            response = await self._llm.chat(messages)
        except Exception:
            log.warning("Triage LLM call failed; falling back to default", exc_info=True)
            return TriageResult(fallback_to_default=True)

        content = (response.content or "").strip()
        return self._parse_result(content)

    def _parse_result(self, content: str) -> TriageResult:
        """Parse the LLM JSON response into a TriageResult."""
        # Strip markdown fences if present.
        if content.startswith("```"):
            lines = content.split("\n")
            lines = [l for l in lines if not l.strip().startswith("```")]
            content = "\n".join(lines).strip()

        try:
            data: dict[str, Any] = json.loads(content)
        except json.JSONDecodeError:
            log.warning("Triage response not valid JSON: %s", content[:200])
            return TriageResult(fallback_to_default=True)

        skill_name = str(data.get("skill_name", "default"))
        confidence = float(data.get("confidence", 0.0))
        extracted_params = data.get("extracted_params", {})
        reasoning = str(data.get("reasoning", ""))

        # Validate skill name exists.
        known_names = {s.name for s in self._skills}
        if skill_name != "default" and skill_name not in known_names:
            log.warning("Triage returned unknown skill %r; falling back", skill_name)
            return TriageResult(
                skill_name="default",
                confidence=confidence,
                reasoning=reasoning,
                fallback_to_default=True,
            )

        fallback = confidence < self._threshold or skill_name == "default"

        return TriageResult(
            skill_name=skill_name,
            confidence=confidence,
            extracted_params=extracted_params if isinstance(extracted_params, dict) else {},
            reasoning=reasoning,
            fallback_to_default=fallback,
        )
