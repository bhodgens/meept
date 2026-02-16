"""3-tier SKILL.md filesystem discovery.

Scans three directories in priority order for ``SKILL.md`` files:

1. ``.meept/skills/``           (project-local, highest priority)
2. ``~/.meept/skills/``         (user-global)
3. ``~/.config/meept/skills/``  (system-wide, lowest priority)

Higher-priority tiers shadow same-named skills in lower tiers.
"""

from __future__ import annotations

import logging
from pathlib import Path

from meept.skills.models import SkillDefinition
from meept.skills.parser import parse_skill_file

log = logging.getLogger(__name__)

# Default discovery paths (highest to lowest priority).
_DEFAULT_TIERS: list[Path] = [
    Path(".meept/skills"),
    Path("~/.meept/skills").expanduser(),
    Path("~/.config/meept/skills").expanduser(),
]


class SkillIndex:
    """Discovers and indexes SKILL.md files across the 3-tier hierarchy.

    Parameters
    ----------
    tiers:
        Ordered list of directories to scan (highest priority first).
        Defaults to the standard 3-tier paths.
    """

    def __init__(self, tiers: list[Path] | None = None) -> None:
        self._tiers = tiers if tiers is not None else list(_DEFAULT_TIERS)
        self._skills: dict[str, SkillDefinition] = {}
        self._loaded = False

    def scan(self) -> None:
        """Scan all tiers and build the skill index.

        Skills discovered in higher-priority tiers shadow those with the
        same name in lower-priority tiers.
        """
        self._skills.clear()

        # Scan in reverse order so higher-priority tiers overwrite lower ones.
        for tier in reversed(self._tiers):
            self._scan_tier(tier)

        self._loaded = True
        log.info("skill_index: discovered %d skill(s) across %d tier(s)",
                 len(self._skills), len(self._tiers))

    def _scan_tier(self, directory: Path) -> None:
        """Scan a single tier directory for SKILL.md files."""
        directory = directory.expanduser().resolve()
        if not directory.is_dir():
            log.debug("skill_index: tier directory does not exist: %s", directory)
            return

        # Each skill lives in a subdirectory: skills/<skill-name>/SKILL.md
        for child in sorted(directory.iterdir()):
            if not child.is_dir():
                # Also support flat SKILL.md files named <skill-name>.md
                if child.suffix == ".md" and child.stem != "README":
                    self._load_skill_file(child)
                continue

            skill_file = child / "SKILL.md"
            if skill_file.is_file():
                self._load_skill_file(skill_file)

    def _load_skill_file(self, path: Path) -> None:
        """Parse a single SKILL.md file and add it to the index."""
        try:
            parsed = parse_skill_file(path)
            if parsed is None:
                return

            skill = SkillDefinition.from_parsed(parsed)
            if skill.name:
                self._skills[skill.name] = skill
                log.debug("skill_index: loaded skill %r from %s", skill.name, path)
        except Exception:
            log.warning("skill_index: failed to load %s", path, exc_info=True)

    def ensure_loaded(self) -> None:
        """Scan if not already loaded."""
        if not self._loaded:
            self.scan()

    def get(self, name: str) -> SkillDefinition | None:
        """Look up a skill by name."""
        self.ensure_loaded()
        return self._skills.get(name)

    def find(self, query: str = "") -> list[SkillDefinition]:
        """Return skills matching a query string (searches name + description).

        If *query* is empty, returns all skills.
        """
        self.ensure_loaded()
        if not query:
            return list(self._skills.values())

        query_lower = query.lower()
        results = []
        for skill in self._skills.values():
            if (query_lower in skill.name.lower()
                    or query_lower in skill.description.lower()):
                results.append(skill)
        return results

    def list_names(self) -> list[str]:
        """Return sorted list of all discovered skill names."""
        self.ensure_loaded()
        return sorted(self._skills.keys())

    @property
    def skills(self) -> dict[str, SkillDefinition]:
        """The full skill index (name -> definition)."""
        self.ensure_loaded()
        return dict(self._skills)

    def __len__(self) -> int:
        self.ensure_loaded()
        return len(self._skills)

    def __contains__(self, name: str) -> bool:
        self.ensure_loaded()
        return name in self._skills

    def __repr__(self) -> str:
        return f"<SkillIndex skills={self.list_names()!r}>"
