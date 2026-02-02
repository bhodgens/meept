"""Tests for skills data models."""

from __future__ import annotations

from meept.skills.models import SkillDefinition
from meept.skills.parser import ParsedSkill, SkillMetadata


def test_skill_definition_defaults() -> None:
    """SkillDefinition should have sensible defaults."""
    skill = SkillDefinition(name="test", description="A test skill")
    assert skill.name == "test"
    assert skill.requires == []
    assert skill.allowed_tools == []
    assert skill.temperature is None
    assert skill.max_tokens is None
    assert skill.risk_level == "medium"
    assert skill.max_iterations == 10
    assert skill.source_path == ""


def test_skill_definition_custom_fields() -> None:
    """SkillDefinition should accept custom values."""
    skill = SkillDefinition(
        name="code_review",
        description="Reviews code",
        requires=["code", "reasoning"],
        system_prompt="You are a reviewer",
        instructions="Be thorough",
        allowed_tools=["file_read", "shell"],
        temperature=0.3,
        max_tokens=8192,
        risk_level="low",
        max_iterations=15,
    )
    assert skill.name == "code_review"
    assert skill.requires == ["code", "reasoning"]
    assert skill.allowed_tools == ["file_read", "shell"]
    assert skill.temperature == 0.3
    assert skill.max_tokens == 8192
    assert skill.max_iterations == 15


def test_from_parsed() -> None:
    """from_parsed should construct from a ParsedSkill."""
    parsed = ParsedSkill(
        metadata=SkillMetadata(
            name="parsed-skill",
            description="A parsed skill",
            requires=["code"],
            allowed_tools=["shell"],
            risk_level="high",
            max_iterations=20,
            temperature=0.5,
            max_tokens=4096,
        ),
        body="# Instructions\nDo the thing.",
    )

    skill = SkillDefinition.from_parsed(parsed)
    assert skill.name == "parsed-skill"
    assert skill.description == "A parsed skill"
    assert skill.requires == ["code"]
    assert skill.allowed_tools == ["shell"]
    assert skill.risk_level == "high"
    assert skill.max_iterations == 20
    assert skill.temperature == 0.5
    assert skill.max_tokens == 4096
    assert "Instructions" in skill.instructions
