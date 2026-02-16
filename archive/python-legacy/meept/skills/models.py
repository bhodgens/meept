"""Data models for the skills subsystem.

Defines :class:`SkillDefinition` loaded from ``SKILL.md`` files.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from meept.skills.parser import ParsedSkill


@dataclass(slots=True)
class SkillDefinition:
    """A single skill definition.

    Parameters
    ----------
    name:
        Unique identifier (e.g. ``"code-review"``).
    description:
        Human-readable description of what the skill does.
    requires:
        Capability tags that a model must satisfy to run this skill.
    system_prompt:
        Skill-specific system prompt injected into the agent loop.
    instructions:
        Additional instructions (Markdown body from SKILL.md).
    allowed_tools:
        Subset of tool names this skill may use.  Empty list means *all*.
    temperature:
        LLM temperature override for this skill.
    max_tokens:
        LLM max_tokens override for this skill.
    risk_level:
        Risk classification: ``"low"``, ``"medium"``, ``"high"``.
    max_iterations:
        Maximum agent-loop iterations for this skill.
    source_path:
        Filesystem path the skill was loaded from (if any).
    """

    name: str = ""
    description: str = ""
    requires: list[str] = field(default_factory=list)
    system_prompt: str = ""
    instructions: str = ""
    allowed_tools: list[str] = field(default_factory=list)
    temperature: float | None = None
    max_tokens: int | None = None
    risk_level: str = "medium"
    max_iterations: int = 10
    source_path: str = ""

    @classmethod
    def from_parsed(cls, parsed: ParsedSkill) -> SkillDefinition:
        """Construct from a :class:`ParsedSkill` produced by the parser."""
        meta = parsed.metadata
        return cls(
            name=meta.name,
            description=meta.description,
            requires=list(meta.requires),
            instructions=parsed.body,
            allowed_tools=list(meta.allowed_tools),
            temperature=meta.temperature,
            max_tokens=meta.max_tokens,
            risk_level=meta.risk_level,
            max_iterations=meta.max_iterations,
            source_path=str(parsed.source_path) if parsed.source_path else "",
        )
