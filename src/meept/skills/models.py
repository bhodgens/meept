"""Data models for the skills subsystem.

Defines :class:`SkillDefinition` (loaded from TOML files) and
:class:`TriageResult` (returned by the triage agent).
"""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(slots=True)
class SkillDefinition:
    """A single skill loaded from a ``.toml`` file.

    Parameters
    ----------
    name:
        Unique identifier (e.g. ``"code_review"``).
    description:
        Human-readable description of what the skill does.
    model:
        Key from ``[llm.models]`` in meept.toml.  ``"default"`` uses the
        default model.
    system_prompt:
        Skill-specific system prompt injected into the agent loop.
    instructions:
        Additional instructions appended after the system prompt.
    allowed_tools:
        Subset of tool names this skill may use.  Empty list means *all*.
    temperature:
        LLM temperature override for this skill.
    max_tokens:
        LLM max_tokens override for this skill.
    risk_level:
        Risk classification: ``"low"``, ``"medium"``, ``"high"``.
    trigger_keywords:
        Keywords used by the triage agent for intent matching.
    examples:
        Few-shot example messages for triage classification.
    max_iterations:
        Maximum agent-loop iterations for this skill.
    """

    name: str = ""
    description: str = ""
    model: str = "default"
    system_prompt: str = ""
    instructions: str = ""
    allowed_tools: list[str] = field(default_factory=list)
    temperature: float | None = None
    max_tokens: int | None = None
    risk_level: str = "medium"
    trigger_keywords: list[str] = field(default_factory=list)
    examples: list[str] = field(default_factory=list)
    max_iterations: int = 10


@dataclass(slots=True)
class TriageResult:
    """Result of the triage agent's intent classification.

    Parameters
    ----------
    skill_name:
        Name of the matched skill, or ``"default"`` for the standard agent.
    confidence:
        Classification confidence between 0.0 and 1.0.
    extracted_params:
        Parameters extracted from the user message (e.g. file paths).
    reasoning:
        Brief explanation of why the skill was chosen.
    fallback_to_default:
        When ``True``, the dispatcher should use the default agent loop.
    """

    skill_name: str = "default"
    confidence: float = 0.0
    extracted_params: dict = field(default_factory=dict)
    reasoning: str = ""
    fallback_to_default: bool = False
