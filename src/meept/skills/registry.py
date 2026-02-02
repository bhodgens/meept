"""Skill registry -- holds loaded skills with lookup by name and capabilities."""

from __future__ import annotations

import logging
from typing import Iterator

from meept.skills.models import SkillDefinition

log = logging.getLogger(__name__)


class SkillRegistry:
    """In-memory registry of loaded skill definitions.

    Skills are indexed by name for O(1) lookup.
    """

    def __init__(self) -> None:
        self._skills: dict[str, SkillDefinition] = {}

    def register(self, skill: SkillDefinition) -> None:
        """Add or replace a skill definition."""
        if skill.name in self._skills:
            log.warning("Replacing existing skill registration: %s", skill.name)
        self._skills[skill.name] = skill
        log.info("Registered skill: %s", skill.name)

    def unregister(self, name: str) -> None:
        """Remove a skill by name.

        Raises :class:`KeyError` if the name is unknown.
        """
        if name not in self._skills:
            raise KeyError(f"No skill registered with name {name!r}")
        del self._skills[name]
        log.info("Unregistered skill: %s", name)

    def get(self, name: str) -> SkillDefinition | None:
        """Look up a skill by name."""
        return self._skills.get(name)

    def list_skills(self) -> list[SkillDefinition]:
        """Return all registered skill definitions."""
        return list(self._skills.values())

    def find_by_capabilities(self, capabilities: set[str]) -> list[SkillDefinition]:
        """Return skills whose ``requires`` are satisfied by *capabilities*.

        Parameters
        ----------
        capabilities:
            The set of capability tags provided by a model.

        Returns
        -------
        list[SkillDefinition]
            Skills where every entry in ``requires`` is present in *capabilities*.
            Skills with empty ``requires`` are always included.
        """
        results: list[SkillDefinition] = []
        for skill in self._skills.values():
            if not skill.requires or set(skill.requires) <= capabilities:
                results.append(skill)
        return results

    def get_requirements(self, name: str) -> set[str]:
        """Return the capability requirements for a named skill.

        Returns an empty set if the skill is not found or has no requirements.
        """
        skill = self._skills.get(name)
        if skill is None:
            return set()
        return set(skill.requires)

    @property
    def names(self) -> list[str]:
        """Sorted list of registered skill names."""
        return sorted(self._skills.keys())

    def __len__(self) -> int:
        return len(self._skills)

    def __contains__(self, name: str) -> bool:
        return name in self._skills

    def __iter__(self) -> Iterator[SkillDefinition]:
        return iter(self._skills.values())

    def __repr__(self) -> str:
        return f"<SkillRegistry skills={self.names!r}>"
