"""Skill discovery and invocation tools.

Provides three tools that allow the LLM to discover and use skills
dynamically via tool calls, replacing the old TriageAgent:

- ``skill_find``: Search for available skills by query.
- ``skill_use``: Activate a skill for the current task.
- ``skill_resource``: Read the full instructions for a skill.
"""

from __future__ import annotations

import logging
from typing import Any

from meept.tools.interface import Tool, ToolDefinition, ToolParameter

log = logging.getLogger(__name__)


class SkillFindTool(Tool):
    """Search for available skills by name or description."""

    def __init__(self, skill_index: Any) -> None:
        self._index = skill_index

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="skill_find",
            description=(
                "Search for available skills. Returns a list of skills matching "
                "the query. Use with no query to list all available skills."
            ),
            parameters=[
                ToolParameter(
                    name="query",
                    type="string",
                    description="Search query to filter skills by name or description. "
                                "Leave empty to list all skills.",
                    required=False,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        query = kwargs.get("query", "")
        skills = self._index.find(query)
        results = [
            {
                "name": s.name,
                "description": s.description,
                "requires": s.requires,
                "risk_level": s.risk_level,
            }
            for s in skills
        ]
        return {"result": results, "count": len(results)}


class SkillUseTool(Tool):
    """Activate a skill for the current task."""

    def __init__(self, skill_index: Any, model_resolver: Any = None) -> None:
        self._index = skill_index
        self._resolver = model_resolver

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="skill_use",
            description=(
                "Activate a named skill. Returns the skill's instructions and "
                "configuration. If the skill requires capabilities not available "
                "on the current model, suggests an appropriate model."
            ),
            parameters=[
                ToolParameter(
                    name="name",
                    type="string",
                    description="The exact name of the skill to activate.",
                    required=True,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        name = kwargs.get("name", "")
        if not name:
            return {"error": "Skill name is required."}

        skill = self._index.get(name)
        if skill is None:
            available = self._index.list_names()
            return {
                "error": f"Skill {name!r} not found.",
                "available_skills": available,
            }

        result: dict[str, Any] = {
            "name": skill.name,
            "description": skill.description,
            "instructions": skill.instructions,
            "allowed_tools": skill.allowed_tools,
            "risk_level": skill.risk_level,
            "max_iterations": skill.max_iterations,
            "requires": skill.requires,
        }

        # Resolve model if resolver is available.
        if self._resolver is not None and skill.requires:
            try:
                resolved = self._resolver.resolve_for_skill(skill)
                result["resolved_model"] = {
                    "model_id": resolved.model_id,
                    "provider": resolved.provider_id,
                    "capabilities": sorted(resolved.capabilities),
                }
            except Exception as exc:
                result["model_warning"] = str(exc)

        return {"result": result}


class SkillResourceTool(Tool):
    """Read the full content of a skill's SKILL.md file."""

    def __init__(self, skill_index: Any) -> None:
        self._index = skill_index

    def definition(self) -> ToolDefinition:
        return ToolDefinition(
            name="skill_resource",
            description=(
                "Read the full instructions and documentation for a skill. "
                "Returns the complete Markdown body from the skill's SKILL.md file."
            ),
            parameters=[
                ToolParameter(
                    name="name",
                    type="string",
                    description="The exact name of the skill.",
                    required=True,
                ),
            ],
        )

    async def execute(self, **kwargs: Any) -> dict[str, Any]:
        name = kwargs.get("name", "")
        if not name:
            return {"error": "Skill name is required."}

        skill = self._index.get(name)
        if skill is None:
            return {"error": f"Skill {name!r} not found."}

        return {
            "result": {
                "name": skill.name,
                "body": skill.instructions,
                "system_prompt": skill.system_prompt,
                "source_path": skill.source_path,
            }
        }
