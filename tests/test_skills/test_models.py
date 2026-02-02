"""Tests for skills data models."""

from __future__ import annotations

from meept.skills.models import SkillDefinition, TriageResult


def test_skill_definition_defaults() -> None:
    """SkillDefinition should have sensible defaults."""
    skill = SkillDefinition(name="test", description="A test skill")
    assert skill.name == "test"
    assert skill.model == "default"
    assert skill.allowed_tools == []
    assert skill.temperature is None
    assert skill.max_tokens is None
    assert skill.risk_level == "medium"
    assert skill.trigger_keywords == []
    assert skill.examples == []
    assert skill.max_iterations == 10


def test_skill_definition_custom_fields() -> None:
    """SkillDefinition should accept custom values."""
    skill = SkillDefinition(
        name="code_review",
        description="Reviews code",
        model="deepseek",
        system_prompt="You are a reviewer",
        instructions="Be thorough",
        allowed_tools=["file_read", "shell"],
        temperature=0.3,
        max_tokens=8192,
        risk_level="low",
        trigger_keywords=["review", "check"],
        examples=["Review my code"],
        max_iterations=15,
    )
    assert skill.name == "code_review"
    assert skill.model == "deepseek"
    assert skill.allowed_tools == ["file_read", "shell"]
    assert skill.temperature == 0.3
    assert skill.max_tokens == 8192
    assert skill.max_iterations == 15


def test_triage_result_defaults() -> None:
    """TriageResult should default to 'default' skill."""
    result = TriageResult()
    assert result.skill_name == "default"
    assert result.confidence == 0.0
    assert result.extracted_params == {}
    assert result.reasoning == ""
    assert result.fallback_to_default is False


def test_triage_result_custom() -> None:
    """TriageResult should accept custom values."""
    result = TriageResult(
        skill_name="code_review",
        confidence=0.95,
        extracted_params={"file": "src/main.py"},
        reasoning="User asked for code review",
    )
    assert result.skill_name == "code_review"
    assert result.confidence == 0.95
    assert result.extracted_params == {"file": "src/main.py"}
