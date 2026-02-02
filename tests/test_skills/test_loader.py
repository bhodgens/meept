"""Tests for skill TOML file loading."""

from __future__ import annotations

from pathlib import Path

from meept.skills.loader import SkillLoader


def test_load_all_from_empty_dir(tmp_path: Path) -> None:
    """Loading from an empty directory should return an empty list."""
    loader = SkillLoader(tmp_path)
    skills = loader.load_all()
    assert skills == []


def test_load_all_from_nonexistent_dir(tmp_path: Path) -> None:
    """Loading from a nonexistent directory should return an empty list."""
    loader = SkillLoader(tmp_path / "nonexistent")
    skills = loader.load_all()
    assert skills == []


def test_load_single_skill(tmp_path: Path) -> None:
    """A valid skill TOML should be parsed into a SkillDefinition."""
    skill_file = tmp_path / "review.toml"
    skill_file.write_text(
        'name = "code_review"\n'
        'description = "Reviews code"\n'
        'model = "deepseek"\n'
        'temperature = 0.3\n'
        'max_tokens = 8192\n'
        'risk_level = "low"\n'
        'max_iterations = 15\n'
        'allowed_tools = ["file_read", "shell"]\n'
        'trigger_keywords = ["review", "check"]\n'
        'examples = ["Review my code"]\n'
        'system_prompt = "You are a code reviewer."\n'
        'instructions = "Focus on bugs."\n',
        encoding="utf-8",
    )

    loader = SkillLoader(tmp_path)
    skills = loader.load_all()

    assert len(skills) == 1
    s = skills[0]
    assert s.name == "code_review"
    assert s.description == "Reviews code"
    assert s.model == "deepseek"
    assert s.temperature == 0.3
    assert s.max_tokens == 8192
    assert s.risk_level == "low"
    assert s.max_iterations == 15
    assert s.allowed_tools == ["file_read", "shell"]
    assert s.trigger_keywords == ["review", "check"]
    assert s.examples == ["Review my code"]
    assert s.system_prompt == "You are a code reviewer."
    assert s.instructions == "Focus on bugs."


def test_load_multiple_skills(tmp_path: Path) -> None:
    """Multiple TOML files should all be loaded."""
    (tmp_path / "a.toml").write_text('name = "alpha"\ndescription = "A"\n', encoding="utf-8")
    (tmp_path / "b.toml").write_text('name = "beta"\ndescription = "B"\n', encoding="utf-8")

    loader = SkillLoader(tmp_path)
    skills = loader.load_all()

    assert len(skills) == 2
    names = {s.name for s in skills}
    assert names == {"alpha", "beta"}


def test_load_skips_nameless(tmp_path: Path) -> None:
    """TOML files without a 'name' field should be skipped."""
    (tmp_path / "bad.toml").write_text('description = "No name"\n', encoding="utf-8")

    loader = SkillLoader(tmp_path)
    skills = loader.load_all()
    assert skills == []


def test_load_skips_invalid_toml(tmp_path: Path) -> None:
    """Invalid TOML files should be skipped without raising."""
    (tmp_path / "bad.toml").write_text("this is not valid toml {{{{", encoding="utf-8")
    (tmp_path / "good.toml").write_text('name = "good"\ndescription = "OK"\n', encoding="utf-8")

    loader = SkillLoader(tmp_path)
    skills = loader.load_all()

    assert len(skills) == 1
    assert skills[0].name == "good"


def test_load_file_returns_none_for_nameless(tmp_path: Path) -> None:
    """load_file should return None when the file has no name."""
    path = tmp_path / "no_name.toml"
    path.write_text('description = "No name"\n', encoding="utf-8")

    loader = SkillLoader(tmp_path)
    result = loader.load_file(path)
    assert result is None


def test_directory_property(tmp_path: Path) -> None:
    """The directory property should return the resolved path."""
    loader = SkillLoader(tmp_path)
    assert loader.directory == tmp_path.resolve()
