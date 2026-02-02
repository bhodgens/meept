"""Tests for the ClawSkillIndex (daemon-side)."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from meept.clawskills.index import ClawSkillIndex
from meept.clawskills.models import OriginMetadata
from tests.test_clawskills.conftest import SAMPLE_SKILL_MD, SAMPLE_SKILL_MD_MINIMAL


def _write_skill(base: Path, slug: str, skill_md: str = SAMPLE_SKILL_MD) -> Path:
    """Write a SKILL.md into base/{slug}/SKILL.md."""
    skill_dir = base / slug
    skill_dir.mkdir(parents=True, exist_ok=True)
    (skill_dir / "SKILL.md").write_text(skill_md, encoding="utf-8")
    return skill_dir


class TestClawSkillIndex:
    def test_scan_empty_dir(self, install_dir: Path) -> None:
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        assert len(index) == 0

    def test_scan_nonexistent_dir(self, tmp_path: Path) -> None:
        index = ClawSkillIndex(base_dir=tmp_path / "missing")
        index.scan()
        assert len(index) == 0

    def test_scan_discovers_skill(self, install_dir: Path) -> None:
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        assert len(index) == 1
        assert "claw:test-skill" in index

    def test_claw_prefix(self, install_dir: Path) -> None:
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        skill = index.get("claw:test-skill")
        assert skill is not None
        assert skill.name.startswith("claw:")

    def test_risk_level_always_high(self, install_dir: Path) -> None:
        # SAMPLE_SKILL_MD sets risk-level: low.
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        skill = index.get("claw:test-skill")
        assert skill is not None
        assert skill.risk_level == "high"

    def test_max_iterations_capped(self, install_dir: Path) -> None:
        # SAMPLE_SKILL_MD sets max-iterations: 20.
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        skill = index.get("claw:test-skill")
        assert skill is not None
        assert skill.max_iterations <= 10

    def test_multiple_skills(self, install_dir: Path) -> None:
        _write_skill(install_dir, "skill-a")
        _write_skill(install_dir, "skill-b", SAMPLE_SKILL_MD_MINIMAL)
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        # Both parse to the same "name" from frontmatter, so last wins.
        # But the minimal one has name="minimal".
        assert len(index) == 2
        assert "claw:test-skill" in index
        assert "claw:minimal" in index

    def test_origin_metadata_loaded(self, install_dir: Path) -> None:
        skill_dir = _write_skill(install_dir, "gifgrep")
        origin = OriginMetadata(
            slug="gifgrep", version="1.0.0", sha256="abc123",
        )
        origin.save(skill_dir / ".origin.json")

        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        loaded_origin = index.get_origin("gifgrep")
        assert loaded_origin is not None
        assert loaded_origin.version == "1.0.0"

    def test_corrupt_origin_handled(self, install_dir: Path) -> None:
        skill_dir = _write_skill(install_dir, "gifgrep")
        (skill_dir / ".origin.json").write_text("not json", encoding="utf-8")

        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        # Skill should still be loaded, just no origin.
        assert "claw:test-skill" in index
        assert index.get_origin("gifgrep") is None

    def test_ensure_loaded_lazy(self, install_dir: Path) -> None:
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        assert not index._loaded
        # Accessing skills triggers lazy scan.
        skills = index.skills
        assert index._loaded
        assert len(skills) == 1

    def test_skills_returns_copy(self, install_dir: Path) -> None:
        _write_skill(install_dir, "gifgrep")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        s1 = index.skills
        s2 = index.skills
        assert s1 is not s2

    def test_hidden_dirs_skipped(self, install_dir: Path) -> None:
        hidden = install_dir / ".hidden"
        hidden.mkdir()
        (hidden / "SKILL.md").write_text(SAMPLE_SKILL_MD, encoding="utf-8")
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        assert len(index) == 0

    def test_repr(self, install_dir: Path) -> None:
        index = ClawSkillIndex(base_dir=install_dir)
        index.scan()
        assert "ClawSkillIndex" in repr(index)
