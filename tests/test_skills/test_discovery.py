"""Tests for 3-tier skill discovery."""

from __future__ import annotations

from pathlib import Path

from meept.skills.discovery import SkillIndex


def _write_skill(directory: Path, name: str, description: str = "") -> None:
    """Helper to create a skill directory with SKILL.md."""
    skill_dir = directory / name
    skill_dir.mkdir(parents=True, exist_ok=True)
    (skill_dir / "SKILL.md").write_text(
        f"---\nname: {name}\ndescription: {description or name}\n---\n# {name}\n",
        encoding="utf-8",
    )


def test_scan_empty_tiers(tmp_path: Path) -> None:
    """Scanning nonexistent directories should produce an empty index."""
    index = SkillIndex(tiers=[tmp_path / "none1", tmp_path / "none2"])
    index.scan()
    assert len(index) == 0


def test_scan_single_tier(tmp_path: Path) -> None:
    """Skills in a single tier should be discovered."""
    tier = tmp_path / "skills"
    _write_skill(tier, "alpha")
    _write_skill(tier, "beta")

    index = SkillIndex(tiers=[tier])
    index.scan()

    assert len(index) == 2
    assert "alpha" in index
    assert "beta" in index


def test_higher_tier_shadows(tmp_path: Path) -> None:
    """A skill in a higher-priority tier should shadow the same name in a lower tier."""
    low = tmp_path / "low"
    high = tmp_path / "high"

    _write_skill(low, "shared", description="from low")
    _write_skill(high, "shared", description="from high")

    index = SkillIndex(tiers=[high, low])
    index.scan()

    assert len(index) == 1
    skill = index.get("shared")
    assert skill is not None
    assert skill.description == "from high"


def test_find_all(tmp_path: Path) -> None:
    """find() with empty query should return all skills."""
    tier = tmp_path / "skills"
    _write_skill(tier, "code-review")
    _write_skill(tier, "web-search")

    index = SkillIndex(tiers=[tier])
    index.scan()

    results = index.find("")
    assert len(results) == 2


def test_find_by_query(tmp_path: Path) -> None:
    """find() should filter by name/description."""
    tier = tmp_path / "skills"
    _write_skill(tier, "code-review", description="Reviews code for bugs")
    _write_skill(tier, "web-search", description="Searches the web")

    index = SkillIndex(tiers=[tier])
    index.scan()

    results = index.find("code")
    assert len(results) == 1
    assert results[0].name == "code-review"


def test_list_names(tmp_path: Path) -> None:
    """list_names should return sorted names."""
    tier = tmp_path / "skills"
    _write_skill(tier, "bravo")
    _write_skill(tier, "alpha")

    index = SkillIndex(tiers=[tier])
    index.scan()

    assert index.list_names() == ["alpha", "bravo"]


def test_lazy_loading(tmp_path: Path) -> None:
    """Accessing skills should trigger lazy scan."""
    tier = tmp_path / "skills"
    _write_skill(tier, "lazy")

    index = SkillIndex(tiers=[tier])
    # Don't call scan() -- should auto-scan on access.
    assert "lazy" in index
    assert len(index) == 1


def test_flat_md_files(tmp_path: Path) -> None:
    """Flat .md files (not in subdirectories) should also be discovered."""
    tier = tmp_path / "skills"
    tier.mkdir(parents=True)
    (tier / "flat-skill.md").write_text(
        "---\nname: flat-skill\ndescription: A flat skill\n---\nBody.\n",
        encoding="utf-8",
    )

    index = SkillIndex(tiers=[tier])
    index.scan()

    assert "flat-skill" in index


def test_multi_tier_merge(tmp_path: Path) -> None:
    """Skills from multiple tiers should be merged."""
    project = tmp_path / "project"
    user = tmp_path / "user"

    _write_skill(project, "proj-only")
    _write_skill(user, "user-only")

    index = SkillIndex(tiers=[project, user])
    index.scan()

    assert len(index) == 2
    assert "proj-only" in index
    assert "user-only" in index
