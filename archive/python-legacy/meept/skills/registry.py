"""Skill registry -- holds loaded skills with lookup by name and capabilities."""

from __future__ import annotations

import logging
from typing import Any, Iterator

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

    # ------------------------------------------------------------------
    # Bus integration
    # ------------------------------------------------------------------

    async def subscribe_to_bus(self, bus: Any) -> None:
        """Subscribe to skills-related bus topics."""
        self._bus = bus
        bus.subscribe("skills.list", self._handle_bus_list)
        bus.subscribe("skills.triage", self._handle_bus_triage)

    async def _handle_bus_list(self, topic: str, msg: Any) -> None:
        """Handle a skills.list bus message."""
        from meept.models.messages import BusMessage, MessageType

        skills = self.list_skills()
        serialized = [
            {
                "name": s.name,
                "description": s.description,
                "requires": s.requires,
                "risk_level": s.risk_level,
            }
            for s in skills
        ]
        await self._bus.publish(
            "skills.result",
            BusMessage(
                type=MessageType.STATUS_UPDATE,
                payload={"skills": serialized},
                source="skills",
                reply_to=msg.id,
            ),
        )

    async def _handle_bus_triage(self, topic: str, msg: Any) -> None:
        """Handle a skills.triage bus message with keyword matching."""
        from meept.models.messages import BusMessage, MessageType

        message = msg.payload.get("message", "").lower()
        scored: list[tuple[SkillDefinition, int]] = []
        for skill in self.list_skills():
            score = 0
            words = message.split()
            for word in words:
                if word in skill.name.lower():
                    score += 3
                if word in skill.description.lower():
                    score += 1
            if score > 0:
                scored.append((skill, score))
        scored.sort(key=lambda x: x[1], reverse=True)
        best = [
            {"name": s.name, "description": s.description, "score": sc}
            for s, sc in scored[:5]
        ]
        await self._bus.publish(
            "skills.result",
            BusMessage(
                type=MessageType.STATUS_UPDATE,
                payload={"matches": best, "message": message},
                source="skills",
                reply_to=msg.id,
            ),
        )

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
