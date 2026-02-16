"""Daemon-side index of installed ClawSkills.

Scans ``~/.meept/clawskills/`` at daemon startup, parses each
``{slug}/SKILL.md``, and produces :class:`SkillDefinition` objects with
hardened defaults (``claw:`` namespace prefix, HIGH risk, capped iterations).
"""

from __future__ import annotations

import logging
from pathlib import Path

from meept.clawskills.models import OriginMetadata
from meept.skills.models import SkillDefinition
from meept.skills.parser import parse_skill_file

log = logging.getLogger(__name__)

_MAX_ITERATIONS_CAP = 10


class ClawSkillIndex:
    """Discovers and indexes installed clawskills for daemon use.

    Parameters
    ----------
    base_dir:
        Root directory of installed clawskills (e.g. ``~/.meept/clawskills``).
    """

    def __init__(self, base_dir: Path) -> None:
        self._base_dir = base_dir
        self._skills: dict[str, SkillDefinition] = {}
        self._origins: dict[str, OriginMetadata] = {}
        self._loaded = False

    def scan(self) -> None:
        """Scan the install directory and build the skill index."""
        self._skills.clear()
        self._origins.clear()

        if not self._base_dir.is_dir():
            log.debug("clawskill_index: directory does not exist: %s", self._base_dir)
            self._loaded = True
            return

        for child in sorted(self._base_dir.iterdir()):
            if not child.is_dir():
                continue
            # Skip hidden dirs like .lock.json parent (shouldn't exist but be safe).
            if child.name.startswith("."):
                continue

            skill_md = child / "SKILL.md"
            if not skill_md.is_file():
                log.debug("clawskill_index: no SKILL.md in %s", child)
                continue

            self._load_skill(child.name, skill_md)

        self._loaded = True
        log.info(
            "clawskill_index: indexed %d clawskill(s) from %s",
            len(self._skills),
            self._base_dir,
        )

    def _load_skill(self, slug: str, skill_md: Path) -> None:
        """Parse a single SKILL.md and register it with hardened defaults."""
        try:
            parsed = parse_skill_file(skill_md)
            if parsed is None:
                return

            skill = SkillDefinition.from_parsed(parsed)

            # Namespace prefix to prevent shadowing local skills.
            skill.name = f"claw:{skill.name}"

            # Enforce security defaults.
            skill.risk_level = "high"
            skill.max_iterations = min(skill.max_iterations, _MAX_ITERATIONS_CAP)

            self._skills[skill.name] = skill

            # Load provenance metadata if available.
            origin_path = skill_md.parent / ".origin.json"
            if origin_path.is_file():
                try:
                    self._origins[slug] = OriginMetadata.load(origin_path)
                except Exception:
                    log.warning(
                        "clawskill_index: corrupt .origin.json for %s", slug,
                        exc_info=True,
                    )

            log.debug("clawskill_index: loaded %s from %s", skill.name, skill_md)

        except Exception:
            log.warning(
                "clawskill_index: failed to load %s", skill_md, exc_info=True,
            )

    def ensure_loaded(self) -> None:
        """Scan if not already loaded."""
        if not self._loaded:
            self.scan()

    @property
    def skills(self) -> dict[str, SkillDefinition]:
        """The full skill index (name -> definition)."""
        self.ensure_loaded()
        return dict(self._skills)

    def get(self, name: str) -> SkillDefinition | None:
        """Look up a clawskill by name (e.g. ``claw:gifgrep``)."""
        self.ensure_loaded()
        return self._skills.get(name)

    def get_origin(self, slug: str) -> OriginMetadata | None:
        """Get provenance metadata for a clawskill by slug."""
        self.ensure_loaded()
        return self._origins.get(slug)

    def __len__(self) -> int:
        self.ensure_loaded()
        return len(self._skills)

    def __contains__(self, name: str) -> bool:
        self.ensure_loaded()
        return name in self._skills

    def __repr__(self) -> str:
        return f"<ClawSkillIndex count={len(self._skills)}>"
