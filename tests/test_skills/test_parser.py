"""Tests for SKILL.md parser."""

from __future__ import annotations

from pathlib import Path

from meept.skills.parser import ParsedSkill, SkillMetadata, parse_skill_file, parse_skill_text


def test_parse_basic_skill() -> None:
    """A well-formed SKILL.md should parse correctly."""
    text = """\
---
name: code-review
description: Reviews code for bugs and style
requires: [code, reasoning]
allowed-tools: [shell, file_read]
risk-level: low
max-iterations: 15
---

# Code Review

You are an expert code reviewer.
"""
    result = parse_skill_text(text)
    assert result is not None
    assert result.metadata.name == "code-review"
    assert result.metadata.description == "Reviews code for bugs and style"
    assert result.metadata.requires == ["code", "reasoning"]
    assert result.metadata.allowed_tools == ["shell", "file_read"]
    assert result.metadata.risk_level == "low"
    assert result.metadata.max_iterations == 15
    assert "expert code reviewer" in result.body


def test_parse_minimal_skill() -> None:
    """A SKILL.md with only name should parse."""
    text = """\
---
name: simple
---
Body text.
"""
    result = parse_skill_text(text)
    assert result is not None
    assert result.metadata.name == "simple"
    assert result.metadata.requires == []
    assert result.metadata.risk_level == "medium"
    assert result.body == "Body text."


def test_parse_no_frontmatter() -> None:
    """Text without frontmatter should return None."""
    result = parse_skill_text("Just some markdown\n# Heading\n")
    assert result is None


def test_parse_no_name() -> None:
    """Frontmatter without 'name' should return None."""
    text = """\
---
description: No name here
---
Body.
"""
    result = parse_skill_text(text)
    assert result is None


def test_parse_invalid_yaml() -> None:
    """Invalid YAML frontmatter should return None."""
    text = """\
---
name: [unclosed
---
Body.
"""
    result = parse_skill_text(text)
    assert result is None


def test_parse_underscore_keys() -> None:
    """Both hyphenated and underscored keys should work."""
    text = """\
---
name: test
allowed_tools: [shell]
risk_level: high
max_iterations: 5
---
Body.
"""
    result = parse_skill_text(text)
    assert result is not None
    assert result.metadata.allowed_tools == ["shell"]
    assert result.metadata.risk_level == "high"
    assert result.metadata.max_iterations == 5


def test_parse_requires_as_string() -> None:
    """A comma-separated string for requires should be split into a list."""
    text = """\
---
name: test
requires: "code, reasoning"
---
Body.
"""
    result = parse_skill_text(text)
    assert result is not None
    assert result.metadata.requires == ["code", "reasoning"]


def test_parse_skill_file(tmp_path: Path) -> None:
    """parse_skill_file should read from disk."""
    skill_file = tmp_path / "SKILL.md"
    skill_file.write_text("""\
---
name: disk-skill
description: Loaded from disk
---
Instructions here.
""", encoding="utf-8")

    result = parse_skill_file(skill_file)
    assert result is not None
    assert result.metadata.name == "disk-skill"
    assert result.source_path == skill_file


def test_parse_skill_file_nonexistent(tmp_path: Path) -> None:
    """parse_skill_file should return None for missing files."""
    result = parse_skill_file(tmp_path / "missing.md")
    assert result is None


def test_parse_temperature_and_max_tokens() -> None:
    """Temperature and max-tokens should be parsed."""
    text = """\
---
name: test
temperature: 0.3
max-tokens: 8192
---
Body.
"""
    result = parse_skill_text(text)
    assert result is not None
    assert result.metadata.temperature == 0.3
    assert result.metadata.max_tokens == 8192
