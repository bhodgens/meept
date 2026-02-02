"""Tests for the skill registry."""

from __future__ import annotations

import pytest

from meept.skills.models import SkillDefinition
from meept.skills.registry import SkillRegistry


def test_register_and_get() -> None:
    """Registering a skill should make it retrievable by name."""
    reg = SkillRegistry()
    skill = SkillDefinition(name="test", description="A test")
    reg.register(skill)

    assert reg.get("test") is skill
    assert "test" in reg
    assert len(reg) == 1


def test_get_unknown_returns_none() -> None:
    """Looking up an unknown skill should return None."""
    reg = SkillRegistry()
    assert reg.get("unknown") is None


def test_unregister() -> None:
    """Unregistering a skill should remove it."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="test", description="A"))
    reg.unregister("test")
    assert reg.get("test") is None
    assert len(reg) == 0


def test_unregister_unknown_raises() -> None:
    """Unregistering an unknown skill should raise KeyError."""
    reg = SkillRegistry()
    with pytest.raises(KeyError):
        reg.unregister("ghost")


def test_list_skills() -> None:
    """list_skills should return all registered definitions."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="a", description="A"))
    reg.register(SkillDefinition(name="b", description="B"))

    skills = reg.list_skills()
    assert len(skills) == 2


def test_names_sorted() -> None:
    """names should return sorted skill names."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="bravo", description="B"))
    reg.register(SkillDefinition(name="alpha", description="A"))

    assert reg.names == ["alpha", "bravo"]


def test_replace_existing() -> None:
    """Re-registering a skill should replace the old one."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="test", description="Old"))
    reg.register(SkillDefinition(name="test", description="New"))

    assert reg.get("test").description == "New"
    assert len(reg) == 1


def test_iter() -> None:
    """Iterating should yield all skill definitions."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="a", description="A"))
    reg.register(SkillDefinition(name="b", description="B"))

    names = {s.name for s in reg}
    assert names == {"a", "b"}


def test_repr() -> None:
    """repr should include skill names."""
    reg = SkillRegistry()
    reg.register(SkillDefinition(name="test", description="T"))
    assert "test" in repr(reg)
