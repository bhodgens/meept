"""Tests for skill_find, skill_use, and skill_resource tools."""

from __future__ import annotations

from pathlib import Path

import pytest

from meept.skills.discovery import SkillIndex
from meept.skills.models import SkillDefinition
from meept.tools.builtin.skill_tools import SkillFindTool, SkillResourceTool, SkillUseTool


def _write_skill(directory: Path, name: str, **extra) -> None:
    """Helper to create a skill directory with SKILL.md."""
    skill_dir = directory / name
    skill_dir.mkdir(parents=True, exist_ok=True)

    desc = extra.get("description", name)
    requires = extra.get("requires", [])
    requires_str = "[" + ", ".join(requires) + "]" if requires else "[]"

    (skill_dir / "SKILL.md").write_text(
        f"---\nname: {name}\ndescription: {desc}\nrequires: {requires_str}\n---\n# {name}\nInstructions for {name}.\n",
        encoding="utf-8",
    )


@pytest.fixture()
def skill_index(tmp_path: Path) -> SkillIndex:
    tier = tmp_path / "skills"
    _write_skill(tier, "code-review", description="Reviews code", requires=["code", "reasoning"])
    _write_skill(tier, "web-search", description="Searches the web")
    index = SkillIndex(tiers=[tier])
    index.scan()
    return index


@pytest.mark.asyncio
async def test_skill_find_all(skill_index: SkillIndex) -> None:
    """skill_find with no query should return all skills."""
    tool = SkillFindTool(skill_index)
    result = await tool.execute()
    assert result["count"] == 2


@pytest.mark.asyncio
async def test_skill_find_query(skill_index: SkillIndex) -> None:
    """skill_find with a query should filter results."""
    tool = SkillFindTool(skill_index)
    result = await tool.execute(query="code")
    assert result["count"] == 1
    assert result["result"][0]["name"] == "code-review"


@pytest.mark.asyncio
async def test_skill_use_found(skill_index: SkillIndex) -> None:
    """skill_use should return skill details."""
    tool = SkillUseTool(skill_index)
    result = await tool.execute(name="code-review")
    assert "result" in result
    assert result["result"]["name"] == "code-review"
    assert "Instructions for code-review" in result["result"]["instructions"]
    assert result["result"]["requires"] == ["code", "reasoning"]


@pytest.mark.asyncio
async def test_skill_use_not_found(skill_index: SkillIndex) -> None:
    """skill_use with unknown name should return error."""
    tool = SkillUseTool(skill_index)
    result = await tool.execute(name="nonexistent")
    assert "error" in result
    assert "available_skills" in result


@pytest.mark.asyncio
async def test_skill_use_empty_name(skill_index: SkillIndex) -> None:
    """skill_use with empty name should return error."""
    tool = SkillUseTool(skill_index)
    result = await tool.execute(name="")
    assert "error" in result


@pytest.mark.asyncio
async def test_skill_resource_found(skill_index: SkillIndex) -> None:
    """skill_resource should return full skill body."""
    tool = SkillResourceTool(skill_index)
    result = await tool.execute(name="code-review")
    assert "result" in result
    assert "Instructions for code-review" in result["result"]["body"]


@pytest.mark.asyncio
async def test_skill_resource_not_found(skill_index: SkillIndex) -> None:
    """skill_resource with unknown name should return error."""
    tool = SkillResourceTool(skill_index)
    result = await tool.execute(name="nonexistent")
    assert "error" in result


@pytest.mark.asyncio
async def test_skill_find_definition(skill_index: SkillIndex) -> None:
    """Tool definitions should be well-formed."""
    for ToolClass in [SkillFindTool, SkillUseTool, SkillResourceTool]:
        tool = ToolClass(skill_index)
        defn = tool.definition()
        assert defn.name
        assert defn.description
        schema = defn.to_openai_schema()
        assert schema["type"] == "function"
